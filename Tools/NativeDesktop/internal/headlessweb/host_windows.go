//go:build windows

// Package headlessweb hosts the embedded dashboard and lamp coordinator
// without constructing the Walk shell, a tray icon, or any native window.
package headlessweb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/eventeffects"
	"rayanlamp.local/app/internal/headless"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/localapi"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/sfx"
	"rayanlamp.local/app/internal/webui"
	"rayanlamp.local/app/internal/winfx"
)

var (
	// ErrClosed means the host is already shutting down.
	ErrClosed = errors.New("headless web dashboard is closed")
	// ErrDashboardUnavailable means the configured listener is disabled or
	// could not be started, so there is no URL to open.
	ErrDashboardUnavailable = errors.New("headless web dashboard is unavailable")
)

// Logger receives non-fatal lifecycle and event-effects messages.
type Logger func(format string, args ...any)

// Options configures a new GUI-free dashboard primary.
type Options struct {
	ConfigPath string
	// Listen is an optional loopback HOST:PORT override. An explicit serve
	// launch enables and persists the local API, including this override.
	Listen string
	Logger Logger
}

type controller interface {
	localapi.EventSource
	Start(context.Context) error
	Close() error
	SetHooks(commands.Hooks) error
	Call(context.Context, ipc.Request) (any, error)
}

var (
	_ controller                 = (*headless.Runtime)(nil)
	_ localapi.DiagnosticsSource = (*headless.Runtime)(nil)
)

type apiServer interface {
	BrowserURL() string
	BroadcastConfig(config.Config)
	Close(context.Context) error
}

type effectsService interface {
	PrepareConfig(config.Config) (config.Config, error)
	ApplyConfig(config.Config)
	PlayCount(eventeffects.Cue, int) error
	ResolveAudio(string) (sfx.Asset, error)
	ImportAudio(string, []byte) (sfx.ImportedAsset, error)
	ImportAudioFile(string) (sfx.ImportedAsset, error)
	Close()
}

type startWithWindowsPersistence interface {
	ApplySaved(bool) error
}

type dependencies struct {
	resolveConfig                  func(string) (string, error)
	loadConfig                     func(string) (config.Config, error)
	executable                     func() (string, error)
	reconcileStartWithWindows      func(string, bool) (bool, error)
	newStartWithWindowsPersistence func(
		string,
		bool,
	) (startWithWindowsPersistence, error)
	newController func(headless.Options) (controller, error)
	newAPI        func(localapi.Options) (apiServer, error)
	newEffects    func(
		string,
		config.Config,
		<-chan model.Event,
		eventeffects.LogFunc,
	) (effectsService, error)
	openURL func(string) error
}

func productionDependencies() dependencies {
	return dependencies{
		resolveConfig:             config.ResolvePath,
		loadConfig:                config.Load,
		executable:                os.Executable,
		reconcileStartWithWindows: winfx.ReconcileStartWithWindowsSetting,
		newStartWithWindowsPersistence: func(
			executable string,
			initial bool,
		) (startWithWindowsPersistence, error) {
			return winfx.NewStartWithWindowsPersistence(executable, initial)
		},
		newController: func(options headless.Options) (controller, error) {
			return headless.New(options)
		},
		newAPI: func(options localapi.Options) (apiServer, error) {
			return localapi.NewWithOptions(options)
		},
		newEffects: func(
			configPath string,
			cfg config.Config,
			events <-chan model.Event,
			logger eventeffects.LogFunc,
		) (effectsService, error) {
			return eventeffects.NewWindowsAt(configPath, cfg, events, logger)
		},
		openURL: winfx.OpenURL,
	}
}

type apiSettings struct {
	enabled bool
	listen  string
	token   string
}

// Host owns one headless primary, its loopback dashboard, and event effects.
type Host struct {
	configPath       string
	logger           Logger
	deps             dependencies
	controller       controller
	effects          effectsService
	unsubscribe      func()
	startWithWindows startWithWindowsPersistence

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	closed      bool
	workers     sync.WaitGroup
	closeOnce   sync.Once
	closeErr    error

	apiMu       sync.Mutex
	apiOpMu     sync.Mutex
	api         apiServer
	apiSettings apiSettings
	apiRevision atomic.Uint64

	quit     chan struct{}
	quitOnce sync.Once
}

// New claims primary-instance ownership, starts the embedded loopback server,
// then starts the lamp core. No native shell objects are constructed.
func New(options Options) (*Host, error) {
	return newWithDependencies(options, productionDependencies())
}

func newWithDependencies(options Options, deps dependencies) (*Host, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	configPath, err := deps.resolveConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	cfg, err := deps.loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	effectiveNotifications, soundWarning := sfx.PreflightAt(
		configPath,
		cfg.Notifications,
	)
	if soundWarning != nil &&
		!errors.Is(soundWarning, sfx.ErrCustomAsset) &&
		!errors.Is(soundWarning, sfx.ErrInvalidSelection) {
		return nil, fmt.Errorf(
			"preflight shared sound effects: %w",
			soundWarning,
		)
	}
	cfg.Notifications = effectiveNotifications
	if soundWarning != nil && options.Logger != nil {
		options.Logger(
			"selected custom sound is unavailable; repaired to bundled sounds: %v",
			soundWarning,
		)
	}
	executable, err := deps.executable()
	if err != nil {
		return nil, fmt.Errorf(
			"initialize start-with-Windows persistence: locate executable: %w",
			err,
		)
	}
	var reconciliationErr error
	cfg.Integration.StartWithWindows, reconciliationErr =
		deps.reconcileStartWithWindows(
			executable,
			cfg.Integration.StartWithWindows,
		)
	if reconciliationErr != nil && options.Logger != nil {
		options.Logger(
			"inspect start-with-Windows registration: %v",
			reconciliationErr,
		)
	}
	startWithWindows, err := deps.newStartWithWindowsPersistence(
		executable,
		cfg.Integration.StartWithWindows,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"initialize start-with-Windows persistence: %w",
			err,
		)
	}
	// `serve` is an explicit request to host the dashboard. Persisting this
	// setting keeps configuration, bootstrap state, and actual listener state
	// coherent instead of running a hidden one-shot override.
	cfg.Integration.LocalAPIEnabled = true
	if listen := strings.TrimSpace(options.Listen); listen != "" {
		cfg.Integration.LocalAPIListen = listen
	}
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}

	controller, err := deps.newController(headless.Options{
		ConfigPath: configPath,
		Config:     &cfg,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	host := &Host{
		configPath:       configPath,
		logger:           options.Logger,
		deps:             deps,
		controller:       controller,
		startWithWindows: startWithWindows,
		ctx:              ctx,
		cancel:           cancel,
		apiSettings:      settingsFromConfig(cfg),
		quit:             make(chan struct{}),
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = host.Close()
		}
	}()

	events, unsubscribe := controller.Subscribe(32)
	host.unsubscribe = unsubscribe
	effects, err := deps.newEffects(
		configPath,
		cfg,
		events,
		func(message string) { host.logf("%s", message) },
	)
	if err != nil {
		return nil, fmt.Errorf("initialize headless event effects: %w", err)
	}
	host.effects = effects

	if err := controller.SetHooks(host.hooks()); err != nil {
		return nil, fmt.Errorf("install headless dashboard hooks: %w", err)
	}
	server, err := host.startAPI(cfg)
	if err != nil {
		return nil, fmt.Errorf("start headless web dashboard: %w", err)
	}
	host.apiMu.Lock()
	host.api = server
	host.apiMu.Unlock()
	if err := controller.Start(ctx); err != nil {
		return nil, fmt.Errorf("start headless lamp coordinator: %w", err)
	}

	cleanup = false
	return host, nil
}

func (deps dependencies) validate() error {
	switch {
	case deps.resolveConfig == nil:
		return errors.New("headlessweb: nil configuration resolver")
	case deps.loadConfig == nil:
		return errors.New("headlessweb: nil configuration loader")
	case deps.executable == nil:
		return errors.New("headlessweb: nil executable resolver")
	case deps.reconcileStartWithWindows == nil:
		return errors.New("headlessweb: nil start-with-Windows reconciler")
	case deps.newStartWithWindowsPersistence == nil:
		return errors.New("headlessweb: nil start-with-Windows persistence factory")
	case deps.newController == nil:
		return errors.New("headlessweb: nil controller factory")
	case deps.newAPI == nil:
		return errors.New("headlessweb: nil API factory")
	case deps.newEffects == nil:
		return errors.New("headlessweb: nil event-effects factory")
	case deps.openURL == nil:
		return errors.New("headlessweb: nil browser launcher")
	default:
		return nil
	}
}

func (h *Host) hooks() commands.Hooks {
	return commands.Hooks{
		// Direct handoffs intended for a GUI remain useful: the dashboard is
		// the presentation surface of this primary.
		ShowWindow:      func() { h.openRoute("") },
		OpenSettings:    func() { h.openRoute("settings") },
		OpenDiagnostics: func() { h.openRoute("diagnostics") },
		OpenWebUI:       func() { h.openRoute("") },
		Quit:            h.requestQuit,
		PlayAudio: func(cue string, count int) error {
			return h.effects.PlayCount(eventeffects.Cue(cue), count)
		},
		ImportAudioFile: func(path string) (commands.AudioImportResult, error) {
			imported, err := h.effects.ImportAudioFile(path)
			return commands.AudioImportResult{
				ID:   imported.ID,
				Name: imported.Name,
			}, err
		},
		PrepareConfig: h.effects.PrepareConfig,
		ConfigApplied: h.configApplied,
	}
}

func headlessCapabilities() map[string]bool {
	return map[string]bool{
		"native_shell":   false,
		"window_control": false,
		"notifications":  false,
		"global_hotkeys": false,
		"hotkey_capture": false,
		"hotkey_probe":   false,
		"audio_sfx":      true,
	}
}

func (h *Host) startAPI(cfg config.Config) (apiServer, error) {
	if !cfg.Integration.LocalAPIEnabled {
		return nil, nil
	}
	server, err := h.deps.newAPI(localapi.Options{
		Listen:       cfg.Integration.LocalAPIListen,
		BearerToken:  cfg.Integration.LocalAPIToken,
		Handler:      h.controller.Call,
		EventSource:  h.controller,
		AppHandler:   webui.Handler(),
		Capabilities: headlessCapabilities(),
		AudioAsset: func(cue string) (localapi.AudioAsset, error) {
			asset, resolveErr := h.effects.ResolveAudio(cue)
			if resolveErr != nil {
				return localapi.AudioAsset{}, resolveErr
			}
			source := config.SoundSourceBundled
			if asset.Custom {
				source = config.SoundSourceCustom
			}
			return localapi.AudioAsset{
				Data:   asset.Data,
				Name:   asset.Name,
				SHA256: asset.ID,
				Source: source,
			}, nil
		},
		AudioImport: func(name string, data []byte) (localapi.ImportedAudio, error) {
			imported, importErr := h.effects.ImportAudio(name, data)
			return localapi.ImportedAudio{
				ID:   imported.ID,
				Name: imported.Name,
			}, importErr
		},
		HotkeyCapture: nil,
		HotkeyProbe:   nil,
	})
	if err != nil {
		return nil, err
	}
	h.logf("headless web dashboard listening at %s", server.BrowserURL())
	return server, nil
}

func settingsFromConfig(cfg config.Config) apiSettings {
	return apiSettings{
		enabled: cfg.Integration.LocalAPIEnabled,
		listen:  cfg.Integration.LocalAPIListen,
		token:   cfg.Integration.LocalAPIToken,
	}
}

func (h *Host) configApplied(cfg config.Config) {
	if h == nil {
		return
	}
	if h.effects != nil {
		h.effects.ApplyConfig(cfg)
	}
	if h.startWithWindows != nil {
		if err := h.startWithWindows.ApplySaved(
			cfg.Integration.StartWithWindows,
		); err != nil {
			h.logf("start with Windows: %v", err)
		}
	}

	next := settingsFromConfig(cfg)
	h.apiMu.Lock()
	restart := h.apiSettings != next
	h.apiSettings = next
	current := h.api
	if current != nil {
		// Existing tabs receive the authoritative change before a listener
		// migration disconnects them.
		current.BroadcastConfig(cfg)
	}
	h.apiMu.Unlock()
	if restart {
		h.scheduleAPIReconfigure(cfg)
	}
}

func (h *Host) scheduleAPIReconfigure(cfg config.Config) {
	h.lifecycleMu.Lock()
	if h.closed {
		h.lifecycleMu.Unlock()
		return
	}
	revision := h.apiRevision.Add(1)
	h.workers.Add(1)
	h.lifecycleMu.Unlock()
	go func() {
		defer h.workers.Done()
		h.reconfigureAPI(cfg, revision)
	}()
}

func (h *Host) reconfigureAPI(cfg config.Config, revision uint64) {
	h.apiOpMu.Lock()
	defer h.apiOpMu.Unlock()

	h.apiMu.Lock()
	if revision != h.apiRevision.Load() || h.isClosed() {
		h.apiMu.Unlock()
		return
	}
	existing := h.api
	h.api = nil
	h.apiMu.Unlock()

	// Never hold apiMu while graceful shutdown waits for active handlers. A
	// config handler invokes ConfigApplied before returning and must remain
	// able to observe/update host state during this wait.
	if existing != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := existing.Close(ctx); err != nil {
			h.logf("close headless web dashboard listener: %v", err)
		}
		cancel()
	}
	if revision != h.apiRevision.Load() || h.isClosed() ||
		!cfg.Integration.LocalAPIEnabled {
		return
	}
	replacement, err := h.startAPI(cfg)
	if err != nil {
		h.logf("restart headless web dashboard listener: %v", err)
		return
	}

	h.apiMu.Lock()
	if revision == h.apiRevision.Load() && !h.isClosed() {
		h.api = replacement
		replacement = nil
	}
	h.apiMu.Unlock()
	if replacement != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = replacement.Close(ctx)
		cancel()
	}
}

func (h *Host) isClosed() bool {
	if h == nil {
		return true
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	return h.closed
}

// BrowserURL returns the currently active loopback dashboard URL.
func (h *Host) BrowserURL() (string, error) {
	if h == nil || h.isClosed() {
		return "", ErrClosed
	}
	h.apiMu.Lock()
	defer h.apiMu.Unlock()
	if h.api == nil || h.api.BrowserURL() == "" {
		return "", ErrDashboardUnavailable
	}
	return h.api.BrowserURL(), nil
}

// OpenDashboard opens the dashboard root in the user's default browser.
func (h *Host) OpenDashboard() error {
	return h.openRouteWithError("")
}

func (h *Host) openRoute(route string) {
	if err := h.openRouteWithError(route); err != nil {
		h.logf("open headless dashboard route %q: %v", route, err)
	}
}

func (h *Host) openRouteWithError(route string) error {
	url, err := h.BrowserURL()
	if err != nil {
		return err
	}
	route = strings.Trim(strings.TrimSpace(route), "/#")
	if route != "" {
		url += "#/" + route
	}
	return h.deps.openURL(url)
}

func (h *Host) requestQuit() {
	if h == nil {
		return
	}
	h.quitOnce.Do(func() {
		close(h.quit)
	})
}

// Run blocks until IPC quit or parent cancellation, then closes the host.
func (h *Host) Run(parent context.Context) error {
	if h == nil {
		return ErrClosed
	}
	if parent == nil {
		parent = context.Background()
	}
	select {
	case <-parent.Done():
	case <-h.quit:
	case <-h.ctx.Done():
	}
	return h.Close()
}

// Close gracefully stops listener migrations, HTTP, effects, IPC, and the
// lamp transport. It is concurrent and idempotent.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		h.lifecycleMu.Lock()
		h.closed = true
		h.apiRevision.Add(1)
		h.cancel()
		h.lifecycleMu.Unlock()

		// Detach and close the active dashboard before waiting for listener
		// migration workers. This gives every browser an immediate WebSocket
		// GoingAway signal as soon as shutdown begins.
		h.apiMu.Lock()
		server := h.api
		h.api = nil
		h.apiMu.Unlock()
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			h.closeErr = server.Close(ctx)
			cancel()
		}
		h.workers.Wait()
		if h.unsubscribe != nil {
			h.unsubscribe()
		}
		if h.effects != nil {
			h.effects.Close()
		}
		if h.controller != nil {
			if err := h.controller.Close(); h.closeErr == nil {
				h.closeErr = err
			}
		}
		h.requestQuit()
	})
	return h.closeErr
}

func (h *Host) logf(format string, args ...any) {
	if h != nil && h.logger != nil {
		h.logger(format, args...)
	}
}

// IsPrimaryBusy reports whether another process won the normal named-pipe
// ownership gate while this host was starting.
func IsPrimaryBusy(err error) bool {
	return errors.Is(err, headless.ErrPipeBusy)
}
