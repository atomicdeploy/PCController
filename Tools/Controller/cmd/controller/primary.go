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
	cancel       context.CancelFunc
	listener     net.Listener
	done         chan error
	quit         chan struct{}
	quitOnce     sync.Once
	client       *controllerapi.Client
	integrations atomic.Pointer[hostbridge.Manager]
	localDevice  *localDeviceHost
	actions      *hostui.ActionBroker
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
	server, err := startPrimaryIPCAt(
		parent,
		currentPrimaryEndpoint().Listen,
		runtime,
		engine,
		store,
	)
	if err != nil {
		return nil, err
	}
	manager, err := hostbridge.Start(parent, server.client, store, server.actions)
	if err != nil {
		_ = server.Close()
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
		return nil, fmt.Errorf("register global-hotkey status command: %w", err)
	}
	if err := engine.Register(shell.Command{
		Name: "keyboard", Usage: "keyboard status|list|enable|disable|stop",
		Summary: "inspect or control the primary low-level PC keyboard hook",
		Run:     manager.KeyboardCommand,
	}); err != nil {
		manager.Close()
		_ = server.Close()
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
		return nil, fmt.Errorf("register bridge command: %w", err)
	}
	return server, nil
}

func startPrimaryIPCAt(
	parent context.Context,
	address string,
	runtime *control.Runtime,
	engine *shell.Engine,
	stores ...*appconfig.Store,
) (*primaryIPC, error) {
	listener, err := ipcjson.ListenWithRemote(
		address,
		currentPrimaryEndpoint().AllowRemote,
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
		quit: make(chan struct{}),
	}
	server.actions = hostui.NewActionBroker()
	server.actions.SetObserver(func(action hostui.AppAction) {
		if event, ok := browserAppActionEvent(action); ok {
			runtime.PublishStructuredEvent(event)
		}
	})
	sharedClient := controllerapi.AttachSharedRuntime(runtime, engine)
	server.client = sharedClient
	service := &ipcjson.Service{
		Client:          sharedClient,
		WebSocketPath:   currentPrimaryEndpoint().WebSocketPath,
		SocketIOPath:    currentPrimaryEndpoint().SocketIOPath,
		WebUI:           webui.Handler(currentPrimaryEndpoint().WebSocketPath),
		AuthToken:       currentPrimaryEndpoint().AuthToken,
		AllowedOrigins:  append([]string(nil), currentPrimaryEndpoint().AllowedOrigins...),
		InboundWebhooks: currentPrimaryEndpoint().InboundWebhooks,
		AppAction:       server.actions.Publish,
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
	if len(stores) > 0 && stores[0] != nil {
		store := stores[0]
		proxy, proxyErr := newIntegrationProxy(store)
		if proxyErr != nil {
			_ = listener.Close()
			cancel()
			return nil, fmt.Errorf("configure local integration proxy: %w", proxyErr)
		}
		service.IntegrationProxy = proxy
		server.localDevice = startLocalDeviceHost(ctx, sharedClient, store)
		service.LocalDevice = server.localDevice
		service.HostConfig = store.Current
		service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
			_, err := store.Update(change)
			return err
		}
	}
	go func() {
		server.done <- ipcjson.Serve(ctx, listener, service)
		close(server.done)
	}()
	return server, nil
}

func browserAppActionEvent(action hostui.AppAction) (control.Event, bool) {
	page := strings.TrimSpace(action.Value)
	if action.Kind != "app.page" || page == "" {
		return control.Event{}, false
	}
	return control.Event{
		Kind:   "app.page",
		Text:   "Open page " + page,
		Source: action.Source,
		Target: "app.clients",
		Action: "navigate",
		Metadata: map[string]string{
			"page":  page,
			"value": page,
		},
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

func (server *primaryIPC) Close() error {
	if server == nil {
		return nil
	}
	if integrations := server.integrations.Swap(nil); integrations != nil {
		integrations.Close()
	}
	if server.localDevice != nil {
		server.localDevice.Close()
	}
	server.cancel()
	_ = server.listener.Close()
	select {
	case err := <-server.done:
		return err
	case <-time.After(time.Second):
		return errors.New("primary IPC server did not stop within one second")
	}
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
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	auth := ""
	configured := currentPrimaryEndpoint()
	if strings.EqualFold(strings.TrimSpace(address), strings.TrimSpace(configured.Listen)) {
		auth = configured.AuthToken
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
	var result struct {
		Output string `json:"output"`
	}
	err := callPrimaryAt(
		ctx,
		address,
		"controller.execute",
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
	go streamPrimaryEvents(ctx, stdout, &outputMu)

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for {
		outputMu.Lock()
		fmt.Fprint(stdout, "pc[ipc]> ")
		outputMu.Unlock()
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
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
		output, err := executeThroughPrimary(requestContext, line)
		requestCancel()
		if output != "" {
			writeLine(stdout, output)
		}
		if err != nil {
			writeLine(stderr, "error:", err)
		}
	}
	return scanner.Err()
}

func streamPrimaryEvents(
	ctx context.Context,
	output io.Writer,
	outputMu *sync.Mutex,
) {
	var latest struct {
		ID uint64 `json:"id"`
	}
	probeContext, cancel := context.WithTimeout(ctx, time.Second)
	err := callPrimary(
		probeContext,
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
			if ctx.Err() != nil {
				return
			}
			continue
		}
		cursor = event.ID
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
