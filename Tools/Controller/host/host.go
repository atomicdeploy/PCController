// Package host provides the supported in-process PCController lifecycle.
//
// An embedded Host owns the same controller Client and JSON-RPC dispatcher as
// the standalone executable. In-process calls, protected native-local IPC, and
// the optional HTTP/WebSocket endpoint may run concurrently without creating a
// second serial owner or a second method implementation.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostbridge"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/webui"
	"pccontroller.local/controller/rpc"
)

var (
	ErrAlreadyStarted = errors.New("PCController host is already started")
	ErrAlreadyRunning = errors.New("PCController host identity is already owned in this process")
	ErrNotStarted     = errors.New("PCController host is not started")
)

// Logger receives lifecycle diagnostics. A nil Logger keeps the library quiet.
type Logger interface {
	Printf(format string, values ...any)
}

// Branding supplies process-lifetime presentation and endpoint identity without
// patching PCController source or persisting the values into host configuration.
type Branding struct {
	AppID   string
	AppName string
	Tagline string
}

// BuildInfo is diagnostic only. It never selects a parser or behavior.
type BuildInfo struct {
	Version    string
	SourceHash string
	BuildTime  string
}

// NativeOptions configures protected same-machine IPC. The default endpoint is
// derived from Branding.AppID and the current OS user.
type NativeOptions struct {
	Endpoint *rpc.Endpoint
}

// HTTPOptions configures the optional network control plane. Address defaults
// to 127.0.0.1:8787. Non-loopback binding requires AllowRemote.
type HTTPOptions struct {
	Address       string
	AllowRemote   bool
	WebSocketPath string
}

// Options configures an embedded Host. In-process RPC is always available.
// Protected native-local IPC is enabled unless DisableNative is true. HTTP is
// opt-in by supplying HTTP.
type Options struct {
	ConfigPath string
	DataRoot   string
	Branding   Branding
	Build      BuildInfo
	Logger     Logger

	// ControllerOptions bypasses file-to-client mapping when the embedding
	// application already owns a complete typed controller configuration. That
	// typed value remains caller-owned for this Host lifetime and is not
	// overwritten by file reloads. The file-backed store still supplies live RPC
	// configuration through the service callbacks.
	ControllerOptions *controller.Options

	DisableAutoConnect bool
	DisableNative      bool
	Native             NativeOptions
	HTTP               *HTTPOptions
	EnableIntegrations bool
}

type lifecycleState uint8

const (
	stateNew lifecycleState = iota
	stateStarting
	stateRunning
	stateStopping
	stateStopped
)

// Host owns one PCController client, dispatcher, and endpoint set. A Host is a
// single-use lifecycle object; construct a new Host after Stop.
type Host struct {
	options Options

	mu           sync.RWMutex
	state        lifecycleState
	startTime    time.Time
	store        *appconfig.Store
	client       *controller.Client
	service      *ipcjson.Service
	inProcess    *rpc.Client
	endpoints    []rpc.Endpoint
	listeners    []net.Listener
	integrations *hostbridge.Manager
	actions      *hostui.ActionBroker
	instances    *hostui.InstanceRegistry

	ctx       context.Context
	cancel    context.CancelFunc
	serveWait sync.WaitGroup
	stopOnce  sync.Once
	done      chan struct{}
	errors    chan error
	stopErr   error
	ownerKey  string
}

var processOwners = struct {
	sync.Mutex
	values map[string]*Host
}{values: make(map[string]*Host)}

// New creates an idle, single-use Host.
func New(options Options) (*Host, error) {
	if err := validateOptions(&options); err != nil {
		return nil, err
	}
	return &Host{
		options: options,
		state:   stateNew,
		done:    make(chan struct{}),
		errors:  make(chan error, 16),
	}, nil
}

func validateOptions(options *Options) error {
	if options == nil {
		return errors.New("host options are required")
	}
	if options.HTTP != nil {
		options.HTTP.Address = strings.TrimSpace(options.HTTP.Address)
		if options.HTTP.Address == "" {
			options.HTTP.Address = ipcjson.DefaultListen
		}
		options.HTTP.WebSocketPath = normalizeWebSocketPath(options.HTTP.WebSocketPath)
	}
	options.Branding.AppID = strings.TrimSpace(options.Branding.AppID)
	if options.Branding.AppID == "" {
		options.Branding.AppID = productidentity.StableAppID
	}
	if strings.TrimSpace(options.DataRoot) != "" {
		absolute, err := filepath.Abs(options.DataRoot)
		if err != nil {
			return fmt.Errorf("resolve PCController data root: %w", err)
		}
		options.DataRoot = absolute
	}
	if strings.TrimSpace(options.ConfigPath) != "" {
		absolute, err := filepath.Abs(options.ConfigPath)
		if err != nil {
			return fmt.Errorf("resolve PCController configuration path: %w", err)
		}
		options.ConfigPath = absolute
	}
	if options.ConfigPath != "" && options.DataRoot != "" {
		expected := filepath.Join(options.DataRoot, "config.json")
		if filepath.Clean(options.ConfigPath) != expected {
			return errors.New("ConfigPath must equal DataRoot/config.json when both are set")
		}
	}
	return nil
}

func normalizeWebSocketPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/ipc"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

// Start claims the host identity, constructs the canonical controller and RPC
// service, and starts configured endpoints. It returns after listeners are
// ready; board discovery proceeds in the background.
func (host *Host) Start(parent context.Context) error {
	if host == nil {
		return errors.New("PCController host is nil")
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return err
	}
	host.mu.Lock()
	if host.state != stateNew {
		host.mu.Unlock()
		return ErrAlreadyStarted
	}
	host.state = stateStarting
	host.mu.Unlock()

	if err := host.start(parent); err != nil {
		host.releaseStartFailure(err)
		return err
	}
	host.mu.Lock()
	host.state = stateRunning
	host.startTime = time.Now().UTC()
	host.mu.Unlock()
	if host.options.ControllerOptions == nil {
		host.serveWait.Add(1)
		go func() {
			defer host.serveWait.Done()
			host.watchConfiguration(host.ctx)
		}()
	}
	go func() {
		select {
		case <-parent.Done():
		case <-host.ctx.Done():
		case <-host.done:
			return
		}
		stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = host.Stop(stopContext)
	}()
	if !host.options.DisableAutoConnect {
		host.serveWait.Add(1)
		go func() {
			defer host.serveWait.Done()
			host.connect(host.ctx)
		}()
	}
	host.logf("PCController embedded host started with %d external endpoint(s)", len(host.Endpoints()))
	return nil
}

func (host *Host) start(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	configPath := host.options.ConfigPath
	if host.options.DataRoot != "" {
		if err := os.MkdirAll(host.options.DataRoot, 0o700); err != nil {
			return fmt.Errorf("create PCController data root: %w", err)
		}
		configPath = filepath.Join(host.options.DataRoot, "config.json")
	}
	store, err := appconfig.Open(configPath)
	if err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	if err := store.SetPresentationOverrides(
		host.options.Branding.AppName,
		host.options.Branding.Tagline,
	); err != nil {
		return err
	}
	runtimeConfig, err := store.Runtime()
	if err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ownerKey := host.options.Branding.AppID + "\x00" + strings.ToLower(store.Path())
	if err := host.claimProcessOwner(ownerKey); err != nil {
		return err
	}

	controllerOptions := host.options.ControllerOptions
	if controllerOptions == nil {
		value, mapErr := controllerOptionsFromConfig(runtimeConfig, store.Path())
		if mapErr != nil {
			host.releaseProcessOwner()
			return mapErr
		}
		controllerOptions = &value
	}
	client := controller.New(*controllerOptions)
	if err := configureHistory(client, runtimeConfig, store.Path()); err != nil {
		_ = client.Shutdown()
		host.releaseProcessOwner()
		return err
	}
	if err := parent.Err(); err != nil {
		_ = client.Shutdown()
		host.releaseProcessOwner()
		return err
	}

	ctx, cancel := context.WithCancel(parent)
	actions := hostui.NewActionBroker()
	instances := hostui.NewInstanceRegistry()
	instanceID := fmt.Sprintf("embedded:%d:%s", os.Getpid(), host.options.Branding.AppID)
	webSocketPath := "/ipc"
	if host.options.HTTP != nil {
		webSocketPath = host.options.HTTP.WebSocketPath
	}
	service := &ipcjson.Service{
		Client:                client,
		WebSocketPath:         webSocketPath,
		SocketIOPath:          runtimeConfig.IPC.SocketIOPath,
		WebUI:                 webui.Handler(webSocketPath),
		AuthToken:             runtimeConfig.IPC.AuthToken,
		AuthorizationDisabled: true,
		AllowedOrigins:        append([]string(nil), runtimeConfig.IPC.AllowedOrigins...),
		InboundWebhooks:       runtimeConfig.Integrations.InboundWebhooksEnabled,
		HostVersion:           host.options.Build.Version,
		HostSourceHash:        host.options.Build.SourceHash,
		HostBuildTime:         host.options.Build.BuildTime,
		HostInstanceID:        instanceID,
		HostProcessID:         os.Getpid(),
		HostSurface:           "embedded",
		CoordinatorInstanceID: instanceID,
		AppAction:             actions.Publish,
		AppInstances:          instances,
		Shutdown:              cancel,
		HostConfig:            store.CurrentRuntime,
		PersistentHostConfig:  store.Persistent,
		SubscribeHostConfig:   store.SubscribeRuntime,
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			_, updateErr := store.Update(change)
			return updateErr
		},
	}
	service.BridgeList = func() any {
		host.mu.RLock()
		manager := host.integrations
		host.mu.RUnlock()
		if manager == nil {
			return []hostbridge.PeerInfo{}
		}
		return manager.BridgePeers()
	}
	service.BridgeCall = func(callContext context.Context, peer string, request ipcjson.Request) (ipcjson.Response, error) {
		host.mu.RLock()
		manager := host.integrations
		host.mu.RUnlock()
		if manager == nil {
			return ipcjson.Response{}, errors.New("host bridge integration is disabled")
		}
		return manager.CallBridge(callContext, peer, request)
	}
	service.WebhookAdmin = func() ipcjson.WebhookAdminService {
		host.mu.RLock()
		manager := host.integrations
		host.mu.RUnlock()
		if manager == nil {
			return nil
		}
		return manager
	}
	service.HotkeyStatus = func() any {
		host.mu.RLock()
		manager := host.integrations
		host.mu.RUnlock()
		if manager == nil {
			return hostui.HotkeyStatus{}
		}
		return manager.HotkeyStatus()
	}

	inProcess, err := rpc.NewInProcess(service.Dispatch)
	if err != nil {
		cancel()
		_ = client.Shutdown()
		host.releaseProcessOwner()
		return err
	}

	host.mu.Lock()
	host.ctx = ctx
	host.cancel = cancel
	host.store = store
	host.client = client
	host.service = service
	host.inProcess = inProcess
	host.actions = actions
	host.instances = instances
	host.mu.Unlock()

	if host.options.EnableIntegrations {
		// Manager.Close needs a live context to release held keys and prepare
		// device integrations safely. Its own Close controls cancellation, so do
		// not let parent/Host cancellation preempt that shutdown sequence.
		manager, integrationErr := hostbridge.Start(context.WithoutCancel(ctx), client, store, actions, hostbridge.DiscoveryHostIdentity{
			InstanceID: instanceID,
			Version:    host.options.Build.Version,
			SourceHash: host.options.Build.SourceHash,
			BuildTime:  host.options.Build.BuildTime,
		})
		if integrationErr != nil {
			return fmt.Errorf("start PCController host integrations: %w", integrationErr)
		}
		host.mu.Lock()
		host.integrations = manager
		host.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if !host.options.DisableNative {
		endpoint, endpointErr := host.nativeEndpoint()
		if endpointErr != nil {
			return endpointErr
		}
		listener, listenErr := rpc.Listen(endpoint, rpc.ListenOptions{
			RecoverStaleNative: true,
		})
		if listenErr != nil {
			return fmt.Errorf("listen on native PCController endpoint: %w", listenErr)
		}
		host.addEndpoint(endpoint, listener, true)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if host.options.HTTP != nil {
		endpoint := rpc.Endpoint{
			Transport: rpc.TransportTCP,
			Address:   host.options.HTTP.Address,
			Scope:     httpScope(host.options.HTTP.AllowRemote),
			Priority:  20,
		}
		listener, listenErr := rpc.Listen(endpoint, rpc.ListenOptions{
			AllowRemote: host.options.HTTP.AllowRemote,
		})
		if listenErr != nil {
			return fmt.Errorf("listen on PCController HTTP endpoint: %w", listenErr)
		}
		endpoint.Address = listener.Addr().String()
		host.addEndpoint(endpoint, listener, false)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (host *Host) nativeEndpoint() (rpc.Endpoint, error) {
	if host.options.Native.Endpoint != nil {
		endpoint := *host.options.Native.Endpoint
		if err := endpoint.Validate(); err != nil {
			return rpc.Endpoint{}, err
		}
		if endpoint.Transport != rpc.TransportNamedPipe && endpoint.Transport != rpc.TransportUnix {
			return rpc.Endpoint{}, errors.New("native endpoint must use a named pipe or Unix-domain socket")
		}
		return endpoint, nil
	}
	return rpc.DefaultNativeEndpoint(host.options.Branding.AppID)
}

func httpScope(allowRemote bool) string {
	if allowRemote {
		return "network"
	}
	return "local"
}

func (host *Host) addEndpoint(endpoint rpc.Endpoint, listener net.Listener, raw bool) {
	host.mu.Lock()
	host.endpoints = append(host.endpoints, endpoint)
	host.listeners = append(host.listeners, listener)
	host.serveWait.Add(1)
	host.mu.Unlock()
	go func() {
		defer host.serveWait.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				host.endpointFailed(fmt.Errorf("PCController endpoint panic: %v\n%s", recovered, debug.Stack()))
			}
		}()
		var err error
		if raw {
			err = ipcjson.ServeRaw(host.ctx, listener, host.service, func(connection net.Conn) ipcjson.Access {
				return ipcjson.Access{
					Remote:         false,
					Transport:      endpoint.Transport,
					Principal:      "local-user",
					Authentication: "native-local",
				}
			})
		} else {
			err = ipcjson.Serve(host.ctx, listener, host.service)
		}
		if err != nil && host.ctx.Err() == nil {
			host.endpointFailed(fmt.Errorf("PCController %s endpoint stopped: %w", endpoint.Transport, err))
		}
	}()
}

func (host *Host) endpointFailed(err error) {
	host.report(err)
	host.mu.RLock()
	cancel := host.cancel
	host.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (host *Host) connect(ctx context.Context) {
	connectContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := host.client.Connect(connectContext); err != nil && ctx.Err() == nil {
		host.report(fmt.Errorf("initial PCController auto-connect: %w", err))
	}
}

func (host *Host) watchConfiguration(ctx context.Context) {
	host.store.Watch(
		ctx,
		appconfig.DefaultWatchInterval,
		func(value appconfig.Config) {
			options, err := controllerOptionsFromConfig(value, host.store.Path())
			if err != nil {
				host.report(fmt.Errorf("reload PCController configuration: %w", err))
				return
			}
			host.client.ApplyHostOptions(options)
			if err := configureHistory(host.client, value, host.store.Path()); err != nil {
				host.report(fmt.Errorf("reload PCController history configuration: %w", err))
			}
			host.logf("PCController configuration reloaded")
		},
		func(err error) {
			host.report(fmt.Errorf("reload PCController configuration: %w", err))
		},
	)
}

// RPC returns the in-process client that uses the exact dispatcher and envelope
// shared by native-local and HTTP/WebSocket clients.
func (host *Host) RPC() (*rpc.Client, error) {
	if host == nil {
		return nil, ErrNotStarted
	}
	host.mu.RLock()
	defer host.mu.RUnlock()
	if host.state != stateRunning || host.inProcess == nil {
		return nil, ErrNotStarted
	}
	return host.inProcess, nil
}

// Call invokes one canonical RPC method in process and decodes its result.
func (host *Host) Call(ctx context.Context, method string, params, result any) error {
	client, err := host.RPC()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	response, err := client.Call(ctx, rpc.Request{Method: method, Params: encoded})
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	encodedResult, err := json.Marshal(response.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encodedResult, result)
}

// Controller returns the canonical typed controller client owned by this Host.
// Callers must not shut it down directly; use Host.Stop.
func (host *Host) Controller() (*controller.Client, error) {
	if host == nil {
		return nil, ErrNotStarted
	}
	host.mu.RLock()
	defer host.mu.RUnlock()
	if host.state != stateRunning || host.client == nil {
		return nil, ErrNotStarted
	}
	return host.client, nil
}

// Events returns the ordered event stream from the canonical controller. A nil
// channel is returned before Start or after Stop.
func (host *Host) Events() <-chan controller.Event {
	client, err := host.Controller()
	if err != nil {
		return nil
	}
	return client.Events()
}

// Errors reports non-fatal background failures. The channel closes after Stop.
func (host *Host) Errors() <-chan error {
	if host == nil {
		return nil
	}
	return host.errors
}

// Endpoints returns a defensive copy of live external endpoints. In-process RPC
// is obtained from RPC and is intentionally not represented as a dialable URL.
func (host *Host) Endpoints() []rpc.Endpoint {
	if host == nil {
		return nil
	}
	host.mu.RLock()
	defer host.mu.RUnlock()
	return append([]rpc.Endpoint(nil), host.endpoints...)
}

// Done closes after complete shutdown.
func (host *Host) Done() <-chan struct{} {
	if host == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return host.done
}

// Stop cancels endpoints and integrations, releases serial ownership, and waits
// for background serving goroutines. Repeated calls return the first result.
func (host *Host) Stop(ctx context.Context) error {
	if host == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	host.mu.RLock()
	state := host.state
	stopErr := host.stopErr
	host.mu.RUnlock()
	if state == stateNew || state == stateStarting {
		return ErrNotStarted
	}
	if state == stateStopped {
		return stopErr
	}
	host.stopOnce.Do(func() {
		host.mu.Lock()
		host.state = stateStopping
		cancel := host.cancel
		listeners := append([]net.Listener(nil), host.listeners...)
		manager := host.integrations
		client := host.client
		host.mu.Unlock()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		if manager != nil {
			manager.Close()
		}
		if cancel != nil {
			cancel()
		}
		var shutdownErr error
		if client != nil {
			shutdownErr = client.Shutdown()
		}
		host.mu.Lock()
		host.stopErr = errors.Join(host.stopErr, shutdownErr)
		host.mu.Unlock()
		go func() {
			host.serveWait.Wait()
			host.releaseProcessOwner()
			host.mu.Lock()
			host.state = stateStopped
			host.mu.Unlock()
			close(host.errors)
			close(host.done)
		}()
	})
	select {
	case <-host.done:
		host.logf("PCController embedded host stopped")
		host.mu.RLock()
		defer host.mu.RUnlock()
		return host.stopErr
	case <-ctx.Done():
		host.mu.RLock()
		defer host.mu.RUnlock()
		return errors.Join(host.stopErr, ctx.Err())
	}
}

func (host *Host) claimProcessOwner(key string) error {
	processOwners.Lock()
	defer processOwners.Unlock()
	if owner := processOwners.values[key]; owner != nil && owner != host {
		return ErrAlreadyRunning
	}
	processOwners.values[key] = host
	host.ownerKey = key
	return nil
}

func (host *Host) releaseProcessOwner() {
	processOwners.Lock()
	if processOwners.values[host.ownerKey] == host {
		delete(processOwners.values, host.ownerKey)
	}
	processOwners.Unlock()
	host.ownerKey = ""
}

func (host *Host) releaseStartFailure(err error) {
	host.cleanupStartedResources()
	host.mu.Lock()
	host.state = stateStopped
	host.stopErr = err
	host.mu.Unlock()
	close(host.errors)
	close(host.done)
}

func (host *Host) cleanupStartedResources() {
	host.mu.RLock()
	cancel := host.cancel
	listeners := append([]net.Listener(nil), host.listeners...)
	manager := host.integrations
	client := host.client
	host.mu.RUnlock()
	for _, listener := range listeners {
		_ = listener.Close()
	}
	if manager != nil {
		manager.Close()
	}
	if cancel != nil {
		cancel()
	}
	if client != nil {
		_ = client.Shutdown()
	}
	host.serveWait.Wait()
	host.releaseProcessOwner()
}

func (host *Host) report(err error) {
	if err == nil {
		return
	}
	host.logf("PCController background failure reported; read Host.Errors for details")
	select {
	case host.errors <- err:
	default:
	}
}

func (host *Host) logf(format string, values ...any) {
	if host != nil && host.options.Logger != nil {
		host.options.Logger.Printf(format, values...)
	}
}
