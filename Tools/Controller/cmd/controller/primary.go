package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostbridge"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/webui"
)

var errPrimaryAlreadyRunning = errors.New(
	"another host controller process already owns the board",
)

var primaryEndpoint atomic.Value

type primaryEndpointConfig struct {
	Listen          string
	WebSocketPath   string
	SocketIOPath    string
	AuthToken       string
	AllowedOrigins  []string
	InboundWebhooks bool
	AllowRemote     bool
}

func init() {
	primaryEndpoint.Store(primaryEndpointConfig{
		Listen: ipcjson.DefaultListen, WebSocketPath: "/ipc",
		SocketIOPath: "/socket.io/",
	})
}

func configurePrimaryIPC(hostConfig appconfig.Config) {
	config := hostConfig.IPC
	listen := strings.TrimSpace(config.Listen)
	if listen == "" {
		listen = ipcjson.DefaultListen
	}
	path := strings.TrimSpace(config.WebSocketPath)
	if path == "" {
		path = "/ipc"
	}
	primaryEndpoint.Store(primaryEndpointConfig{
		Listen: listen, WebSocketPath: path,
		SocketIOPath:    config.SocketIOPath,
		AuthToken:       config.AuthToken,
		AllowedOrigins:  append([]string(nil), config.AllowedOrigins...),
		InboundWebhooks: hostConfig.Integrations.InboundWebhooksEnabled,
		AllowRemote:     config.AllowRemote,
	})
}

func currentPrimaryEndpoint() primaryEndpointConfig {
	return primaryEndpoint.Load().(primaryEndpointConfig)
}

type primaryIPC struct {
	cancel                context.CancelFunc
	listener              net.Listener
	done                  chan error
	quit                  chan struct{}
	quitOnce              sync.Once
	closeOnce             sync.Once
	closeErr              error
	client                *controllerapi.Client
	runtime               *control.Runtime
	sessionSnapshot       *hostSessionRecorder
	integrations          atomic.Pointer[hostbridge.Manager]
	localDevice           *localDeviceHost
	actions               *hostui.ActionBroker
	instances             *hostui.InstanceRegistry
	artifacts             *artifacts.Service
	ipc                   *ipcjson.Service
	releaseDiscovery      io.Closer
	instanceClaim         *hostInstanceClaim
	hostInstanceID        string
	coordinatorInstanceID string
}

type primaryExecutor struct{}

func (primaryExecutor) Execute(
	ctx context.Context,
	command string,
) (string, error) {
	return executeThroughPrimary(ctx, command)
}

func startPrimaryIPC(
	parent context.Context,
	runtime *control.Runtime,
	engine *shell.Engine,
	store *appconfig.Store,
) (*primaryIPC, error) {
	claimContext, claimCancel := context.WithTimeout(parent, 3*time.Second)
	defer claimCancel()
	claim, existing, err := claimOrResolveHostInstance(claimContext, "host")
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errPrimaryAlreadyRunning
	}
	return startPrimaryIPCClaimed(parent, runtime, engine, store, claim)
}

func startPrimaryIPCClaimed(
	parent context.Context,
	runtime *control.Runtime,
	engine *shell.Engine,
	store *appconfig.Store,
	claim *hostInstanceClaim,
) (*primaryIPC, error) {
	if claim == nil {
		return nil, errors.New("primary controller host requires a per-user ownership claim")
	}
	server, err := startPrimaryIPCAtWithIdentity(
		parent,
		currentPrimaryEndpoint().Listen,
		runtime,
		engine,
		claim.identity,
		store,
	)
	if err != nil {
		_ = claim.Close()
		return nil, err
	}
	manager, err := hostbridge.Start(parent, server.client, store, server.actions, hostbridge.DiscoveryHostIdentity{
		InstanceID: claim.identity.ID, Version: version, SourceHash: sourceHash, BuildTime: buildTime,
	})
	if err != nil {
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("start host integrations: %w", err)
	}
	server.integrations.Store(manager)
	if err := engine.Register(shell.Command{
		Name:    "hotkeys",
		Usage:   "hotkeys status",
		Summary: "inspect the active system-wide controller hotkey bindings",
		Run: func(_ context.Context, args []string) (string, error) {
			if len(args) != 1 || !strings.EqualFold(args[0], "status") {
				return "", errors.New("usage: hotkeys status")
			}
			encoded, encodeErr := json.MarshalIndent(manager.HotkeyStatus(), "", "  ")
			return string(encoded), encodeErr
		},
	}); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("register global-hotkey status command: %w", err)
	}
	if err := engine.Register(shell.Command{
		Name: "keyboard", Usage: "keyboard status|list|enable|disable|stop",
		Summary: "inspect or control the primary low-level PC keyboard hook",
		Run:     manager.KeyboardCommand,
	}); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("register keyboard-control command: %w", err)
	}
	if err := engine.Register(shell.Command{
		Name:    "bridge",
		Usage:   "bridge list | bridge call PEER METHOD [PARAMS_JSON]",
		Summary: "inspect or call authenticated remote controller hosts",
		Run:     manager.BridgeCommand,
	}); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("register bridge command: %w", err)
	}
	if err := engine.Register(shell.Command{
		Name: "peer-update", Usage: "peer-update host PEER ARTIFACT_SHA256",
		Summary: "transfer a verified host artifact and ask the peer coordinator to upgrade",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) != 3 || !strings.EqualFold(args[0], "host") {
				return "", errors.New("usage: peer-update host PEER ARTIFACT_SHA256")
			}
			params, _ := json.Marshal(map[string]any{
				"peer": args[1], "artifact_sha256": args[2], "authorized": true,
			})
			response := server.ipc.Dispatch(ctx, ipcjson.Request{
				JSONRPC: ipcjson.Version, Method: "controller.peer.update.host", Params: params,
			})
			if response.Error != nil {
				return "", response.Error
			}
			encoded, err := json.MarshalIndent(response.Result, "", "  ")
			return string(encoded), err
		},
	}); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("register peer update command: %w", err)
	}
	if err := engine.Register(shell.Command{
		Name: "webhook",
		Usage: "webhook status | pending [LIMIT] | dead [LIMIT] | " +
			"replay DELIVERY_ID|all CONFIRM | clear dead DELIVERY_ID|all CONFIRM",
		Summary: "inspect and operate the durable outbound-webhook delivery queue",
		Run:     manager.WebhookCommand,
	}); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("register outbound-webhook command: %w", err)
	}
	activeEndpoint := currentPrimaryEndpoint()
	activeEndpoint.Listen, err = localHostDialAddress(server.listener.Addr().String())
	if err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("resolve primary controller endpoint: %w", err)
	}
	if err := claim.publish(server.listener, activeEndpoint); err != nil {
		manager.Close()
		_ = server.Close()
		_ = claim.Close()
		return nil, fmt.Errorf("publish primary controller host: %w", err)
	}
	primaryEndpoint.Store(activeEndpoint)
	go func() {
		for config := range store.SubscribeRuntime(parent) {
			endpoint := currentPrimaryEndpoint()
			// The listener and protocol paths are fixed for this process, but
			// loopback delegates must immediately follow a rotated vault token.
			endpoint.AuthToken = config.IPC.AuthToken
			primaryEndpoint.Store(endpoint)
		}
	}()
	server.instanceClaim = claim
	return server, nil
}

func startPrimaryIPCAt(
	parent context.Context,
	address string,
	runtime *control.Runtime,
	engine *shell.Engine,
	stores ...*appconfig.Store,
) (*primaryIPC, error) {
	return startPrimaryIPCAtWithIdentity(
		parent, address, runtime, engine, hostInstanceIdentity{}, stores...,
	)
}

func startPrimaryIPCAtWithIdentity(
	parent context.Context,
	address string,
	runtime *control.Runtime,
	engine *shell.Engine,
	identity hostInstanceIdentity,
	stores ...*appconfig.Store,
) (*primaryIPC, error) {
	endpoint := currentPrimaryEndpoint()
	// Explicit in-process servers without a configuration store are test/tool
	// fixtures, not the configured primary endpoint. Keep them loopback and
	// unauthenticated so prior run() calls cannot leak machine configuration
	// through the package-global endpoint into an unrelated fixture.
	if len(stores) == 0 {
		endpoint = primaryEndpointConfig{
			Listen: address, WebSocketPath: "/ipc", SocketIOPath: "/socket.io/",
		}
	}
	listener, err := ipcjson.ListenWithRemote(
		address,
		endpoint.AllowRemote,
	)
	if err != nil {
		probeContext, cancel := context.WithTimeout(parent, 400*time.Millisecond)
		defer cancel()
		if primaryAvailableAt(probeContext, address) {
			return nil, errPrimaryAlreadyRunning
		}
		return nil, fmt.Errorf("claim primary controller IPC %s: %w", address, err)
	}
	ctx, cancel := context.WithCancel(parent)
	server := &primaryIPC{
		cancel: cancel, listener: listener, done: make(chan error, 1),
		quit: make(chan struct{}), runtime: runtime, hostInstanceID: identity.ID,
	}
	server.actions = hostui.NewActionBroker()
	server.instances = hostui.NewInstanceRegistry()
	server.instances.SetObserver(func(change hostui.InstanceChange) {
		runtime.PublishStructuredEvent(control.Event{
			Kind: "app.instance.changed", Text: change.Kind + " app instance " + change.Instance.ID,
			Source: "host", Target: "app.clients", MessageType: "event",
			Metadata: map[string]string{
				"change": change.Kind, "id": change.Instance.ID,
				"surface": change.Instance.Surface, "page": change.Instance.Page,
				"state": change.Instance.State,
			},
		})
	})
	server.actions.SetObserver(func(action hostui.AppAction) {
		if event, ok := browserAppActionEvent(action); ok {
			runtime.PublishStructuredEvent(event)
		}
	})
	if strings.TrimSpace(identity.ID) != "" {
		server.coordinatorInstanceID = identity.ID + ":bridge"
		process := hostui.CurrentProcessSelf(identity.StartedAt)
		if identity.PID > 0 {
			process.ProcessID = identity.PID
		}
		if _, registerErr := server.instances.Upsert(hostui.AppInstance{
			ID: server.coordinatorInstanceID, Surface: "bridge", State: "background",
			Values: map[string]string{
				"role": "coordinator", "version": version,
				"source_hash":    sourceHash,
				"listen":         listener.Addr().String(),
				"websocket_path": endpoint.WebSocketPath,
				"socketio_path":  endpoint.SocketIOPath,
			},
			Self: &process,
		}); registerErr != nil {
			_ = listener.Close()
			cancel()
			return nil, fmt.Errorf("register coordinator app instance: %w", registerErr)
		}
	}
	sharedClient := controllerapi.AttachSharedRuntime(runtime, engine)
	server.client = sharedClient
	service := &ipcjson.Service{
		Client:                sharedClient,
		WebSocketPath:         endpoint.WebSocketPath,
		SocketIOPath:          endpoint.SocketIOPath,
		WebUI:                 webui.Handler(endpoint.WebSocketPath),
		AuthToken:             endpoint.AuthToken,
		AllowedOrigins:        append([]string(nil), endpoint.AllowedOrigins...),
		InboundWebhooks:       endpoint.InboundWebhooks,
		HostVersion:           version,
		HostSourceHash:        sourceHash,
		HostBuildTime:         buildTime,
		HostInstanceID:        identity.ID,
		HostInstanceToken:     identity.Token,
		HostProcessID:         identity.PID,
		HostSurface:           identity.Surface,
		CoordinatorInstanceID: server.coordinatorInstanceID,
		AppAction:             server.actions.Publish,
		AppInstances:          server.instances,
		Shutdown: func() {
			server.quitOnce.Do(func() { close(server.quit) })
			if integrations := server.integrations.Load(); integrations != nil {
				_ = integrations.ReleaseKeyboard("ipc-shutdown")
			}
			_ = runtime.Close()
			cancel()
		},
		BridgeList: func() any {
			if integrations := server.integrations.Load(); integrations != nil {
				return integrations.BridgePeers()
			}
			return []hostbridge.PeerInfo{}
		},
		WebhookAdmin: func() ipcjson.WebhookAdminService {
			if integrations := server.integrations.Load(); integrations != nil {
				return integrations
			}
			return nil
		},
		HotkeyStatus: func() any {
			if integrations := server.integrations.Load(); integrations != nil {
				return integrations.HotkeyStatus()
			}
			return hostui.HotkeyStatus{}
		},
		BridgeCall: func(
			ctx context.Context,
			peer string,
			request ipcjson.Request,
		) (ipcjson.Response, error) {
			if integrations := server.integrations.Load(); integrations != nil {
				return integrations.CallBridge(ctx, peer, request)
			}
			return ipcjson.Response{}, errors.New("host bridge manager is unavailable")
		},
	}
	server.ipc = service
	if len(stores) > 0 && stores[0] != nil {
		store := stores[0]
		server.sessionSnapshot = newHostSessionRecorder(sharedClient, store)
		service.LastSessionSnapshot = server.sessionSnapshot.read
		proxy, proxyErr := newIntegrationProxy(store)
		if proxyErr != nil {
			_ = listener.Close()
			cancel()
			return nil, fmt.Errorf("configure local integration proxy: %w", proxyErr)
		}
		service.IntegrationProxy = proxy
		server.localDevice = startLocalDeviceHost(ctx, sharedClient, store)
		service.LocalDevice = server.localDevice
		service.HostConfig = store.CurrentRuntime
		service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
			_, err := store.Update(change)
			return err
		}
		artifactService, artifactErr := newArtifactHostService(sharedClient, store, service.Shutdown)
		if artifactErr != nil {
			server.localDevice.Close()
			_ = listener.Close()
			cancel()
			return nil, fmt.Errorf("configure artifact/update service: %w", artifactErr)
		}
		server.artifacts = artifactService
		service.Artifacts = artifactService
		server.sessionSnapshot.attachArtifactContext(artifactService)
		releaseDiscovery, discoveryErr := newReleaseHostService(sharedClient, artifactService)
		if discoveryErr != nil {
			artifactService.Close()
			server.localDevice.Close()
			_ = listener.Close()
			cancel()
			return nil, fmt.Errorf("configure release discovery: %w", discoveryErr)
		}
		server.releaseDiscovery = releaseDiscovery
		service.ReleaseDiscovery = releaseDiscovery
	}
	go func() {
		server.done <- ipcjson.Serve(ctx, listener, service)
		close(server.done)
	}()
	return server, nil
}

func browserAppActionEvent(action hostui.AppAction) (control.Event, bool) {
	kind := strings.ToLower(strings.TrimSpace(action.Kind))
	value := strings.TrimSpace(action.Value)
	if !strings.HasPrefix(kind, "app.") {
		return control.Event{}, false
	}
	target := strings.TrimSpace(action.Target)
	if target == "" {
		target = "*"
	}
	verb := strings.TrimPrefix(kind, "app.")
	text := verb
	if value != "" {
		text += " " + value
	}
	metadata := map[string]string{
		"value": value, "target_instance": target,
	}
	actionName := verb
	if kind == "app.page" {
		metadata["page"] = value
		actionName = "navigate"
		text = "Open page " + value
	}
	return control.Event{
		Kind:     kind,
		Text:     text,
		Source:   action.Source,
		Target:   "app.clients",
		Action:   actionName,
		Metadata: metadata,
	}, true
}

func (server *primaryIPC) QuitRequested() <-chan struct{} {
	if server == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return server.quit
}

func (server *primaryIPC) AppActions() <-chan hostui.AppAction {
	if server == nil || server.actions == nil {
		return nil
	}
	return server.actions.Events()
}

func (server *primaryIPC) IntegrationStatus() hostbridge.Status {
	if server == nil {
		return hostbridge.Status{}
	}
	integrations := server.integrations.Load()
	if integrations == nil {
		return hostbridge.Status{}
	}
	return integrations.Status()
}

func (server *primaryIPC) Notifier() hostui.Notifier {
	if server == nil {
		return nil
	}
	integrations := server.integrations.Load()
	if integrations == nil {
		return nil
	}
	return integrations.Notifier()
}

func (server *primaryIPC) HotkeyStatus() hostui.HotkeyStatus {
	if server == nil {
		return hostui.HotkeyStatus{}
	}
	integrations := server.integrations.Load()
	if integrations == nil {
		return hostui.HotkeyStatus{}
	}
	return integrations.HotkeyStatus()
}

func (server *primaryIPC) KeyboardStatus() hostui.KeyboardStatus {
	if server == nil {
		return hostui.KeyboardStatus{}
	}
	integrations := server.integrations.Load()
	if integrations == nil {
		return hostui.KeyboardStatus{}
	}
	return integrations.KeyboardStatus()
}

func (server *primaryIPC) HandleHostLifecycle(
	ctx context.Context,
	kind string,
) error {
	if server == nil {
		return errors.New("primary controller service is unavailable")
	}
	integrations := server.integrations.Load()
	if integrations == nil {
		return errors.New("host integrations are unavailable")
	}
	return integrations.HandleLifecycle(ctx, kind)
}

func (server *primaryIPC) Close() error {
	if server == nil {
		return nil
	}
	server.closeOnce.Do(func() {
		server.closeErr = server.close()
	})
	return server.closeErr
}

func (server *primaryIPC) close() error {
	var snapshotErr error
	if server.sessionSnapshot != nil {
		snapshotErr = server.sessionSnapshot.persistAndPublish(server.runtime)
	}
	if integrations := server.integrations.Swap(nil); integrations != nil {
		integrations.Close()
	}
	if server.localDevice != nil {
		server.localDevice.Close()
	}
	if server.releaseDiscovery != nil {
		_ = server.releaseDiscovery.Close()
	}
	if server.artifacts != nil {
		server.artifacts.Close()
	}
	server.cancel()
	_ = server.listener.Close()
	select {
	case err := <-server.done:
		return errors.Join(snapshotErr, err, server.closeInstanceClaim())
	case <-time.After(time.Second):
		return errors.Join(
			snapshotErr,
			server.closeInstanceClaim(),
			errors.New("primary IPC server did not stop within one second"),
		)
	}
}

func (server *primaryIPC) closeInstanceClaim() error {
	if server == nil || server.instanceClaim == nil {
		return nil
	}
	err := server.instanceClaim.Close()
	server.instanceClaim = nil
	return err
}

func primaryAvailable(ctx context.Context) bool {
	return primaryAvailableAt(ctx, currentPrimaryEndpoint().Listen)
}

func primaryAvailableAt(ctx context.Context, address string) bool {
	var result struct {
		OK bool `json:"ok"`
	}
	return callPrimaryAt(
		ctx,
		address,
		"controller.ping",
		map[string]any{},
		&result,
	) == nil && result.OK
}

func callPrimary(
	ctx context.Context,
	method string,
	params any,
	target any,
) error {
	return callPrimaryAt(
		ctx,
		currentPrimaryEndpoint().Listen,
		method,
		params,
		target,
	)
}

func callPrimaryAt(
	ctx context.Context,
	address, method string,
	params any,
	target any,
) error {
	auth := ""
	configured := currentPrimaryEndpoint()
	if strings.EqualFold(strings.TrimSpace(address), strings.TrimSpace(configured.Listen)) {
		auth = configured.AuthToken
	}
	return callPrimaryAtAuthenticated(ctx, address, auth, method, params, target)
}

func callPrimaryAtAuthenticated(
	ctx context.Context,
	address, auth, method string,
	params any,
	target any,
) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	response, err := ipcjson.Call(ctx, address, ipcjson.Request{
		Method: method,
		Params: encoded,
		Auth:   auth,
	})
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	encodedResult, err := json.Marshal(response.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encodedResult, target)
}

func executeThroughPrimary(
	ctx context.Context,
	command string,
) (string, error) {
	return executeThroughPrimaryAt(
		ctx,
		currentPrimaryEndpoint().Listen,
		command,
	)
}

func executeThroughPrimaryAt(
	ctx context.Context,
	address, command string,
) (string, error) {
	auth := ""
	configured := currentPrimaryEndpoint()
	if strings.EqualFold(strings.TrimSpace(address), strings.TrimSpace(configured.Listen)) {
		auth = configured.AuthToken
	}
	return executeThroughPrimaryAtAuthenticated(ctx, address, auth, command)
}

func executeThroughPrimaryAtAuthenticated(
	ctx context.Context,
	address, auth, command string,
) (string, error) {
	var result struct {
		Output string `json:"output"`
	}
	err := callPrimaryAtAuthenticated(
		ctx,
		address,
		auth,
		"controller.command.execute",
		map[string]string{"command": command},
		&result,
	)
	return result.Output, err
}

func joinControllerCommand(words []string) string {
	return shell.Join(words)
}

func runSecondaryConsole(
	input io.Reader,
	stdout, stderr io.Writer,
	configuredTitle string,
) error {
	configured := currentPrimaryEndpoint()
	return runSecondaryConsoleAt(
		input, stdout, stderr, configuredTitle,
		configured.Listen, configured.AuthToken,
	)
}

func runSecondaryConsoleAt(
	input io.Reader,
	stdout, stderr io.Writer,
	configuredTitle, address, auth string,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var outputMu sync.Mutex
	writeLine := func(output io.Writer, values ...any) {
		outputMu.Lock()
		defer outputMu.Unlock()
		fmt.Fprintln(output, values...)
	}
	writeLine(
		stdout,
		productidentity.ServiceName(configuredTitle, "secondary console (IPC).")+
			" The primary process retains exclusive serial ownership.",
	)
	hostRestart := make(chan struct{}, 1)
	go streamPrimaryEventsAt(ctx, stdout, &outputMu, hostRestart, address, auth)

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	lines := make(chan string)
	scanDone := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		scanDone <- scanner.Err()
	}()
	for {
		outputMu.Lock()
		fmt.Fprint(stdout, "pc[ipc]> ")
		outputMu.Unlock()
		var scanned string
		select {
		case <-hostRestart:
			writeLine(stdout, "Host update is ready; closing this secondary console so the verified coordinator replacement can proceed.")
			return nil
		case err := <-scanDone:
			return err
		case scanned = <-lines:
		}
		line := strings.TrimSpace(scanned)
		if strings.EqualFold(line, "exit") ||
			strings.EqualFold(line, "quit") {
			break
		}
		if line == "" {
			continue
		}
		requestContext, requestCancel := context.WithTimeout(
			ctx,
			10*time.Minute,
		)
		output, err := executeThroughPrimaryAtAuthenticated(requestContext, address, auth, line)
		requestCancel()
		if output != "" {
			writeLine(stdout, output)
		}
		if err != nil {
			writeLine(stderr, "error:", err)
		}
	}
	return nil
}

func streamPrimaryEvents(
	ctx context.Context,
	output io.Writer,
	outputMu *sync.Mutex,
	hostRestart chan<- struct{},
) {
	configured := currentPrimaryEndpoint()
	streamPrimaryEventsAt(ctx, output, outputMu, hostRestart, configured.Listen, configured.AuthToken)
}

func streamPrimaryEventsAt(
	ctx context.Context,
	output io.Writer,
	outputMu *sync.Mutex,
	hostRestart chan<- struct{},
	address, auth string,
) {
	var latest struct {
		ID uint64 `json:"id"`
	}
	probeContext, cancel := context.WithTimeout(ctx, time.Second)
	err := callPrimaryAtAuthenticated(
		probeContext,
		address,
		auth,
		"controller.event.latest",
		map[string]any{},
		&latest,
	)
	cancel()
	if err != nil {
		return
	}
	cursor := latest.ID
	for ctx.Err() == nil {
		requestContext, requestCancel := context.WithTimeout(
			ctx,
			3*time.Second,
		)
		var event controllerapi.Event
		err := callPrimaryAtAuthenticated(
			requestContext,
			address,
			auth,
			"controller.event.next",
			map[string]any{
				"after_id":   cursor,
				"stream":     "activity",
				"timeout_ms": 2000,
			},
			&event,
		)
		requestCancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		cursor = event.ID
		if eventRequestsSecondaryExit(event) {
			select {
			case hostRestart <- struct{}{}:
			default:
			}
			return
		}
		outputMu.Lock()
		fmt.Fprintf(
			output,
			"\n%s EVENT %-12s %s\n",
			event.Time.Format("15:04:05.000"),
			event.Kind,
			event.Text,
		)
		outputMu.Unlock()
	}
}

func eventRequestsSecondaryExit(event controllerapi.Event) bool {
	return strings.EqualFold(strings.TrimSpace(event.Kind), "update.staged") &&
		strings.EqualFold(strings.TrimSpace(event.Metadata["kind"]), "host")
}

func runRemoteMonitor(
	ctx context.Context,
	period time.Duration,
	count int,
	jsonOutput bool,
	stdout, stderr io.Writer,
) error {
	events := make(chan controllerapi.Event, 32)
	go pollPrimaryEvents(ctx, events)
	samples := 0
	refresh := func() {
		requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var status controllerapi.Status
		err := callPrimary(
			requestContext,
			"controller.status",
			map[string]any{},
			&status,
		)
		if err != nil {
			fmt.Fprintln(stderr, "monitor:", err)
			return
		}
		samples++
		if jsonOutput {
			_ = json.NewEncoder(stdout).Encode(struct {
				Type   string               `json:"type"`
				Time   time.Time            `json:"time"`
				Status controllerapi.Status `json:"status"`
				Via    string               `json:"via"`
			}{
				Type: "status", Time: time.Now(), Status: status, Via: "ipc",
			})
			return
		}
		fmt.Fprintf(
			stdout,
			"%s supply=%7.3fV bus=%7.3fV current=%6dmA power=%6dmW "+
				"tLED=%6.2fC tBT=%6.2fC keys=%X relays=%02X door=%t BT=%d "+
				"PWM=%d:%d reset=%d/0x%02X [IPC]\n",
			time.Now().Format("15:04:05.000"),
			float64(status.SupplyMV)/1000,
			float64(status.BusMV)/1000,
			status.CurrentMA,
			status.PowerMW,
			float64(status.TLEDCenti)/100,
			float64(status.TBTCenti)/100,
			status.ActiveKeys,
			status.ActiveRelays,
			status.DoorOpen,
			status.BluetoothState,
			status.PWMChannel,
			status.PWMValue,
			status.ResetCount,
			status.ResetCause,
		)
	}
	refresh()
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		if count != 0 && samples >= count {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			if jsonOutput {
				_ = json.NewEncoder(stdout).Encode(struct {
					Type  string              `json:"type"`
					Event controllerapi.Event `json:"event"`
					Via   string              `json:"via"`
				}{
					Type: "event", Event: event, Via: "ipc",
				})
			} else {
				fmt.Fprintf(
					stdout,
					"%s EVENT %-12s %s [IPC]\n",
					event.Time.Format("15:04:05.000"),
					event.Kind,
					event.Text,
				)
			}
		case <-ticker.C:
			refresh()
		}
	}
}

func pollPrimaryEvents(
	ctx context.Context,
	output chan<- controllerapi.Event,
) {
	var latest struct {
		ID uint64 `json:"id"`
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Second)
	err := callPrimary(
		requestContext,
		"controller.event.latest",
		map[string]any{},
		&latest,
	)
	cancel()
	if err != nil {
		return
	}
	cursor := latest.ID
	for ctx.Err() == nil {
		requestContext, requestCancel := context.WithTimeout(
			ctx,
			3*time.Second,
		)
		var event controllerapi.Event
		err := callPrimary(
			requestContext,
			"controller.event.next",
			map[string]any{
				"after_id":   cursor,
				"timeout_ms": 2000,
			},
			&event,
		)
		requestCancel()
		if err != nil {
			continue
		}
		cursor = event.ID
		select {
		case output <- event:
		case <-ctx.Done():
			return
		}
	}
}
