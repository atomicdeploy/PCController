//go:build windows

// Package headless composes the RayanLamp core, command dispatcher, and
// per-user IPC ownership gate without constructing the Windows GUI shell.
//
// New claims primary-instance ownership and persists the normalized
// configuration, but deliberately does not start lamp network activity.
// Callers can therefore Subscribe before Start and cannot miss an initial
// WebSocket or HTTP state event.
package headless

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/appcore"
	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/model"
)

var (
	// ErrPipeBusy means another process already owns the current user's
	// RayanLamp named pipe. Auto-mode callers should attach to that process
	// instead of starting another lamp controller.
	ErrPipeBusy = errors.New("RayanLamp primary IPC pipe is already owned")

	// ErrNotStarted is returned when a direct Call is made before Start.
	ErrNotStarted = errors.New("headless RayanLamp runtime is not started")

	// ErrClosed is returned when an operation requires a live runtime after
	// Close has begun.
	ErrClosed = errors.New("headless RayanLamp runtime is closed")
)

const defaultOwnershipProbeTimeout = 750 * time.Millisecond

// PipeBusyError retains the failed listener error while making ownership loss
// recognizable with both errors.As and errors.Is(err, ErrPipeBusy).
type PipeBusyError struct {
	Cause error
}

func (e *PipeBusyError) Error() string {
	if e == nil || e.Cause == nil {
		return ErrPipeBusy.Error()
	}
	return fmt.Sprintf("%s: %v", ErrPipeBusy, e.Cause)
}

func (e *PipeBusyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *PipeBusyError) Is(target error) bool {
	return target == ErrPipeBusy
}

// Options configures a headless primary runtime.
//
// ConfigPath must already be resolved to an absolute path. When Config is nil,
// New loads that path using the normal strict/default-overlay configuration
// loader. Supplying Config lets an auto-mode factory reuse an already-loaded
// value without regenerating a first-run API token.
type Options struct {
	ConfigPath string
	Config     *config.Config
	Hooks      commands.Hooks
}

type primaryIPCServer interface {
	Close() error
}

type dependencies struct {
	loadConfig    func(string) (config.Config, error)
	saveConfig    func(string, config.Config) error
	newCore       func(config.Config) (*appcore.Core, error)
	newDispatcher func(*appcore.Core, string) (*commands.Dispatcher, error)
	listenIPC     func(ipc.Handler) (primaryIPCServer, error)
	isIPCRunning  func(context.Context) bool
	startCore     func(*appcore.Core, context.Context) error
	stopCore      func(*appcore.Core)
	probeTimeout  time.Duration
}

func productionDependencies() dependencies {
	return dependencies{
		loadConfig: config.Load,
		saveConfig: config.Save,
		newCore:    appcore.New,
		newDispatcher: func(
			core *appcore.Core,
			configPath string,
		) (*commands.Dispatcher, error) {
			return commands.New(core, configPath)
		},
		listenIPC: func(handler ipc.Handler) (primaryIPCServer, error) {
			return ipc.Listen(handler)
		},
		isIPCRunning: ipc.IsRunning,
		startCore: func(core *appcore.Core, parent context.Context) error {
			return core.Start(parent)
		},
		stopCore: func(core *appcore.Core) {
			core.Stop()
		},
		probeTimeout: defaultOwnershipProbeTimeout,
	}
}

// Runtime owns the headless primary's pipe and application core.
type Runtime struct {
	configPath string
	core       *appcore.Core
	dispatcher *commands.Dispatcher
	ipcServer  primaryIPCServer
	ready      *readyIPCHandler
	deps       dependencies

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	directCalls sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// New validates or loads configuration, constructs the core and dispatcher,
// claims the per-user named pipe, then persists the normalized configuration.
// It never starts the lamp transport; call Subscribe followed by Start.
func New(options Options) (*Runtime, error) {
	return newWithDependencies(options, productionDependencies())
}

func newWithDependencies(options Options, deps dependencies) (*Runtime, error) {
	configPath, err := validateResolvedConfigPath(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}

	var cfg config.Config
	if options.Config == nil {
		cfg, err = deps.loadConfig(configPath)
		if err != nil {
			return nil, err
		}
	} else {
		cfg = *options.Config
	}

	core, err := deps.newCore(cfg)
	if err != nil {
		return nil, err
	}
	dispatcher, err := deps.newDispatcher(core, configPath)
	if err != nil {
		return nil, err
	}
	dispatcher.SetHooks(options.Hooks)

	ready := newReadyIPCHandler(dispatcher.Handler)
	server, err := deps.listenIPC(ready.Handle)
	if err != nil {
		if ownershipCollision(err) || probeExistingPrimary(deps) {
			return nil, &PipeBusyError{Cause: err}
		}
		return nil, fmt.Errorf("claim RayanLamp primary IPC pipe: %w", err)
	}

	// Saving only after Listen succeeds is the ownership boundary. A losing
	// auto-mode process may hold a freshly generated token and must never
	// replace the primary process's configuration with it.
	if err := deps.saveConfig(configPath, core.Config()); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("persist headless RayanLamp configuration: %w", err)
	}

	runtimeContext, cancel := context.WithCancel(context.Background())
	return &Runtime{
		configPath: configPath,
		core:       core,
		dispatcher: dispatcher,
		ipcServer:  server,
		ready:      ready,
		deps:       deps,
		ctx:        runtimeContext,
		cancel:     cancel,
	}, nil
}

func (d dependencies) validate() error {
	switch {
	case d.loadConfig == nil:
		return errors.New("headless: nil configuration loader")
	case d.saveConfig == nil:
		return errors.New("headless: nil configuration saver")
	case d.newCore == nil:
		return errors.New("headless: nil core factory")
	case d.newDispatcher == nil:
		return errors.New("headless: nil dispatcher factory")
	case d.listenIPC == nil:
		return errors.New("headless: nil IPC listener")
	case d.isIPCRunning == nil:
		return errors.New("headless: nil IPC ownership probe")
	case d.startCore == nil:
		return errors.New("headless: nil core starter")
	case d.stopCore == nil:
		return errors.New("headless: nil core stopper")
	default:
		return nil
	}
}

func validateResolvedConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("headless: configuration path is required")
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("headless: configuration path must already be absolute")
	}
	return path, nil
}

func ownershipCollision(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_EXISTS) ||
		errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
		errors.Is(err, windows.ERROR_PIPE_BUSY)
}

func probeExistingPrimary(deps dependencies) bool {
	timeout := deps.probeTimeout
	if timeout <= 0 {
		timeout = defaultOwnershipProbeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return deps.isIPCRunning(ctx)
}

// Start begins the core network lifecycle and then admits IPC requests that
// may have connected while the caller was installing subscriptions or hooks.
// Start is idempotent.
func (r *Runtime) Start(parent context.Context) error {
	if r == nil {
		return ErrClosed
	}
	if parent == nil {
		parent = context.Background()
	}

	r.lifecycleMu.Lock()
	if r.closed {
		r.lifecycleMu.Unlock()
		return ErrClosed
	}
	if r.started {
		r.lifecycleMu.Unlock()
		return nil
	}
	if err := r.deps.startCore(r.core, parent); err != nil {
		r.lifecycleMu.Unlock()
		_ = r.Close()
		return fmt.Errorf("start headless RayanLamp core: %w", err)
	}
	r.started = true
	r.ready.MarkReady()
	r.lifecycleMu.Unlock()
	return nil
}

// SetHooks replaces shell-facing command hooks. It is safe before or after
// Start and is useful when a Bubble Tea Program is created after New claims
// ownership.
func (r *Runtime) SetHooks(hooks commands.Hooks) error {
	if r == nil {
		return ErrClosed
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.closed {
		return ErrClosed
	}
	r.dispatcher.SetHooks(hooks)
	return nil
}

// Call invokes one request through the same dispatcher served over IPC.
func (r *Runtime) Call(ctx context.Context, request ipc.Request) (any, error) {
	if r == nil {
		return nil, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.lifecycleMu.Lock()
	switch {
	case r.closed:
		r.lifecycleMu.Unlock()
		return nil, ErrClosed
	case !r.started:
		r.lifecycleMu.Unlock()
		return nil, ErrNotStarted
	default:
		r.directCalls.Add(1)
	}
	r.lifecycleMu.Unlock()
	defer r.directCalls.Done()

	callContext, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	return r.dispatcher.Handler(callContext, request)
}

// Subscribe registers for immutable core events. It may and should be called
// before Start when the caller needs the initial state transition.
func (r *Runtime) Subscribe(buffer int) (<-chan model.Event, func()) {
	if r == nil {
		return closedEventChannel()
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.closed {
		return closedEventChannel()
	}
	return r.core.Subscribe(buffer)
}

func closedEventChannel() (<-chan model.Event, func()) {
	events := make(chan model.Event)
	close(events)
	return events, func() {}
}

// Snapshot returns the most recently merged lamp state.
func (r *Runtime) Snapshot() model.Snapshot {
	if r == nil || r.core == nil {
		return model.Snapshot{}
	}
	return r.core.Snapshot()
}

// Config returns a defensive copy of the active normalized configuration.
func (r *Runtime) Config() config.Config {
	if r == nil || r.core == nil {
		return config.Config{}
	}
	return r.core.Config()
}

// DiagnosticsSnapshot returns the current parsed diagnostics projection. The
// forwarding methods let a headless Runtime satisfy localapi.DiagnosticsSource
// without exposing its concrete application core to a web host.
func (r *Runtime) DiagnosticsSnapshot() diagnostics.Snapshot {
	if r == nil || r.core == nil {
		return diagnostics.Snapshot{}
	}
	return r.core.DiagnosticsSnapshot()
}

// RefreshDiagnostics refreshes the selected allow-listed diagnostic endpoints.
func (r *Runtime) RefreshDiagnostics(
	ctx context.Context,
	endpoints ...string,
) (diagnostics.Snapshot, error) {
	if r == nil || r.core == nil {
		return diagnostics.Snapshot{}, ErrClosed
	}
	return r.core.RefreshDiagnostics(ctx, endpoints...)
}

// SubscribeDiagnostics registers for immutable parsed diagnostics updates.
func (r *Runtime) SubscribeDiagnostics(
	buffer int,
) (<-chan diagnostics.Snapshot, func()) {
	if r == nil || r.core == nil {
		updates := make(chan diagnostics.Snapshot)
		close(updates)
		return updates, func() {}
	}
	return r.core.SubscribeDiagnostics(buffer)
}

// ConfigPath returns the absolute persistence path owned by this runtime.
func (r *Runtime) ConfigPath() string {
	if r == nil {
		return ""
	}
	return r.configPath
}

// Core exposes the shared application core for integrations that need
// operations not represented by the command dispatcher.
func (r *Runtime) Core() *appcore.Core {
	if r == nil {
		return nil
	}
	return r.core
}

// Dispatcher exposes the shared canonical command dispatcher.
func (r *Runtime) Dispatcher() *commands.Dispatcher {
	if r == nil {
		return nil
	}
	return r.dispatcher
}

// Close stops accepting IPC requests, cancels and joins direct calls, then
// stops and joins every core network worker. It is concurrent and idempotent.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.lifecycleMu.Lock()
		r.closed = true
		r.cancel()
		server := r.ipcServer
		r.ipcServer = nil
		r.lifecycleMu.Unlock()

		if server != nil {
			r.closeErr = server.Close()
		}
		r.directCalls.Wait()
		r.deps.stopCore(r.core)
	})
	return r.closeErr
}

// Started reports whether Start completed successfully.
func (r *Runtime) Started() bool {
	if r == nil {
		return false
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.started
}

// Closed reports whether Close has begun.
func (r *Runtime) Closed() bool {
	if r == nil {
		return true
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.closed
}

// readyIPCHandler claims the pipe before the headless controller starts while
// ensuring connected callers never observe a half-initialized dispatcher.
type readyIPCHandler struct {
	delegate ipc.Handler
	ready    chan struct{}
	once     sync.Once
}

func newReadyIPCHandler(delegate ipc.Handler) *readyIPCHandler {
	return &readyIPCHandler{
		delegate: delegate,
		ready:    make(chan struct{}),
	}
}

func (h *readyIPCHandler) Handle(ctx context.Context, request ipc.Request) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.ready:
		return h.delegate(ctx, request)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *readyIPCHandler) MarkReady() {
	h.once.Do(func() {
		close(h.ready)
	})
}
