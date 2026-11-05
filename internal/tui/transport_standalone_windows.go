//go:build windows

package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/eventeffects"
	"rayanlamp.local/app/internal/headless"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/localapi"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/sfx"
	"rayanlamp.local/app/internal/webui"
	"rayanlamp.local/app/internal/winfx"
)

// standaloneTransport is a headless RayanLamp primary, not an uncoordinated
// second client. It owns the normal per-user pipe and dispatcher while alive.
type standaloneTransport struct {
	runtime            *headless.Runtime
	commandCall        func(context.Context, ipc.Request) (any, error)
	events             <-chan model.Event
	unsubscribe        func()
	effects            *eventeffects.Service
	unsubscribeEffects func()
	quit               chan struct{}
	startWithWindows   *winfx.StartWithWindowsPersistence

	configApplyMu sync.Mutex
	apiMu         sync.Mutex
	apiOpMu       sync.Mutex
	api           standaloneAPIServer
	apiFactory    func(config.Config) (standaloneAPIServer, error)
	apiSettings   standaloneAPISettings
	apiErr        error
	apiRevision   atomic.Uint64

	lifecycleMu sync.Mutex
	workers     sync.WaitGroup
	closed      bool
	closeOnce   sync.Once
	quitOnce    sync.Once
	closeErr    error
}

type standaloneAPISettings struct {
	enabled bool
	listen  string
	token   string
}

type standaloneAPIServer interface {
	BrowserURL() string
	BroadcastConfig(config.Config)
	Close(context.Context) error
}

var (
	_ ConfigTransport  = (*standaloneTransport)(nil)
	_ CommandTransport = (*standaloneTransport)(nil)
)

func newStandaloneTransport(
	cfg config.Config,
	configPath string,
) (*standaloneTransport, error) {
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
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf(
			"initialize start-with-Windows persistence: locate executable: %w",
			err,
		)
	}
	// Reconcile by reading only. headless.New persists this effective value
	// after (and only after) it wins the primary IPC pipe.
	cfg.Integration.StartWithWindows, _ =
		winfx.ReconcileStartWithWindowsSetting(
			executable,
			cfg.Integration.StartWithWindows,
		)
	startWithWindows, err := winfx.NewStartWithWindowsPersistence(
		executable,
		cfg.Integration.StartWithWindows,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"initialize start-with-Windows persistence: %w",
			err,
		)
	}

	runtime, err := headless.New(headless.Options{
		ConfigPath: configPath,
		Config:     &cfg,
	})
	if err != nil {
		return nil, err
	}

	// Subscribe before Start so the first network snapshot cannot be missed.
	events, unsubscribe := runtime.Subscribe(32)
	effectEvents, unsubscribeEffects := runtime.Subscribe(32)
	effects, err := eventeffects.NewWindowsAt(configPath, cfg, effectEvents, nil)
	if err != nil {
		unsubscribeEffects()
		unsubscribe()
		_ = runtime.Close()
		return nil, fmt.Errorf("initialize standalone event effects: %w", err)
	}
	transport := &standaloneTransport{
		runtime:            runtime,
		events:             events,
		unsubscribe:        unsubscribe,
		effects:            effects,
		unsubscribeEffects: unsubscribeEffects,
		quit:               make(chan struct{}),
		startWithWindows:   startWithWindows,
		apiSettings:        standaloneAPISettingsFromConfig(cfg),
	}
	if err := runtime.SetHooks(commands.Hooks{
		OpenWebUI:    func() { transport.openDashboardRoute("") },
		CanOpenWebUI: transport.canOpenDashboard,
		Quit:         transport.signalQuit,
		PlayAudio: func(cue string, count int) error {
			return transport.effects.PlayCount(eventeffects.Cue(cue), count)
		},
		ImportAudioFile: func(path string) (commands.AudioImportResult, error) {
			imported, importErr := transport.effects.ImportAudioFile(path)
			return commands.AudioImportResult{
				ID:   imported.ID,
				Name: imported.Name,
			}, importErr
		},
		PrepareConfig: transport.effects.PrepareConfig,
		ConfigApplied: transport.applyConfig,
	}); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("install standalone command hooks: %w", err)
	}
	initialAPIRevision := transport.apiRevision.Load()
	// Runtime.Start admits named-pipe commands before it returns. Serialize
	// shell-side configuration effects across that publication boundary so a
	// racing config.patch cannot be overwritten by the startup snapshot.
	transport.configApplyMu.Lock()
	startErr := runtime.Start(context.Background())
	transport.configApplyMu.Unlock()
	if startErr != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("start standalone lamp core: %w", startErr)
	}
	if cfg.Integration.LocalAPIEnabled {
		transport.initializeAPI(cfg, initialAPIRevision)
		// A port conflict must not make lamp control unavailable, but it is
		// retained as serving status and a later settings change can retry it.
	}
	return transport, nil
}

func (*standaloneTransport) Mode() TransportMode { return ModeStandalone }
func (*standaloneTransport) Label() string       { return "headless primary · direct" }
func (transport *standaloneTransport) Events() <-chan model.Event {
	return transport.events
}
func (transport *standaloneTransport) QuitEvents() <-chan struct{} {
	if transport == nil {
		return nil
	}
	return transport.quit
}
func (transport *standaloneTransport) Status(context.Context) (model.Snapshot, error) {
	return transport.runtime.Snapshot(), nil
}

func (transport *standaloneTransport) ExecuteCommand(
	ctx context.Context,
	method string,
	params any,
) (json.RawMessage, error) {
	if transport == nil {
		return nil, fmt.Errorf("standalone transport is unavailable")
	}
	request, err := ipc.NewRequest(
		fmt.Sprintf("tui-direct-%d", time.Now().UnixNano()),
		method,
		params,
	)
	if err != nil {
		return nil, err
	}
	var result any
	if transport.commandCall != nil {
		result, err = transport.commandCall(ctx, request)
	} else if transport.runtime != nil {
		result, err = transport.runtime.Call(ctx, request)
	} else {
		return nil, fmt.Errorf("standalone transport is unavailable")
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode standalone command result: %w", err)
	}
	return json.RawMessage(data), nil
}

func (transport *standaloneTransport) GetConfig(
	ctx context.Context,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.get", nil)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("standalone", result)
}

func (transport *standaloneTransport) PatchConfig(
	ctx context.Context,
	patch any,
) (config.Config, error) {
	result, err := transport.ExecuteCommand(ctx, "config.patch", patch)
	if err != nil {
		return config.Config{}, err
	}
	return decodeConfigCommandResult("standalone", result)
}

func (transport *standaloneTransport) Execute(
	ctx context.Context,
	action Action,
) (model.Snapshot, error) {
	if err := validateAction(action); err != nil {
		return model.Snapshot{}, err
	}
	method := map[Action]string{
		ActionToggle:    "lamp.toggle",
		ActionOn:        "lamp.on",
		ActionOff:       "lamp.off",
		ActionRefresh:   "lamp.refresh",
		ActionReconnect: "reconnect",
	}[action]
	request, err := ipc.NewRequest(
		fmt.Sprintf("tui-direct-%d", time.Now().UnixNano()),
		method,
		nil,
	)
	if err != nil {
		return model.Snapshot{}, err
	}
	if _, err := transport.runtime.Call(ctx, request); err != nil {
		return transport.runtime.Snapshot(), err
	}
	return transport.runtime.Snapshot(), nil
}

func (transport *standaloneTransport) FetchDiagnostics(
	ctx context.Context,
	endpoint string,
) (DiagnosticResult, error) {
	request, err := newDiagnosticCommandRequest(
		fmt.Sprintf("tui-direct-%d", time.Now().UnixNano()),
		endpoint,
	)
	if err != nil {
		return DiagnosticResult{}, err
	}
	result, err := transport.runtime.Call(ctx, request)
	if err != nil {
		return DiagnosticResult{}, err
	}
	return decodeDirectDiagnosticResult(endpoint, result)
}

func (transport *standaloneTransport) TerminalStatus(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	var snapshot deviceterminal.Snapshot
	err := transport.callTerminal(ctx, "terminal.status", nil, &snapshot)
	return snapshot, err
}

func (transport *standaloneTransport) TerminalConnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	var snapshot deviceterminal.Snapshot
	err := transport.callTerminal(ctx, "terminal.connect", nil, &snapshot)
	return snapshot, err
}

func (transport *standaloneTransport) TerminalDisconnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	var snapshot deviceterminal.Snapshot
	err := transport.callTerminal(ctx, "terminal.disconnect", nil, &snapshot)
	return snapshot, err
}

func (transport *standaloneTransport) TerminalRead(
	ctx context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (deviceterminal.ReadResult, error) {
	if err := validateTerminalRead(limit, wait); err != nil {
		return deviceterminal.ReadResult{}, err
	}
	var result deviceterminal.ReadResult
	err := transport.callTerminal(ctx, "terminal.read", map[string]any{
		"after":   after,
		"limit":   limit,
		"wait_ms": int(wait.Milliseconds()),
	}, &result)
	return result, err
}

func (transport *standaloneTransport) TerminalWrite(
	ctx context.Context,
	data []byte,
) (deviceterminal.Snapshot, error) {
	if err := validateTerminalInput(data); err != nil {
		return deviceterminal.Snapshot{}, err
	}
	var snapshot deviceterminal.Snapshot
	err := transport.callTerminal(ctx, "terminal.write", map[string]string{
		"data_base64": base64.StdEncoding.EncodeToString(data),
	}, &snapshot)
	return snapshot, err
}

func (transport *standaloneTransport) callTerminal(
	ctx context.Context,
	method string,
	params any,
	target any,
) error {
	request, err := ipc.NewRequest(
		fmt.Sprintf("tui-direct-%d", time.Now().UnixNano()),
		method,
		params,
	)
	if err != nil {
		return err
	}
	result, err := transport.runtime.Call(ctx, request)
	if err != nil {
		return err
	}
	return decodeDirectTerminalResult(result, target)
}

func (transport *standaloneTransport) signalQuit() {
	if transport == nil || transport.quit == nil {
		return
	}
	transport.quitOnce.Do(func() {
		close(transport.quit)
	})
}

func (transport *standaloneTransport) applyConfig(cfg config.Config) {
	if transport == nil {
		return
	}
	transport.configApplyMu.Lock()
	defer transport.configApplyMu.Unlock()
	if transport.effects != nil {
		transport.effects.ApplyConfig(cfg)
	}
	if transport.startWithWindows != nil {
		_ = transport.startWithWindows.ApplySaved(
			cfg.Integration.StartWithWindows,
		)
	}

	next := standaloneAPISettingsFromConfig(cfg)
	transport.apiMu.Lock()
	restart := transport.apiSettings != next
	transport.apiSettings = next
	var revision uint64
	if restart {
		// Keep the generation and the settings it identifies in one critical
		// section. Publishers can then reject a listener that finished
		// starting after a newer configuration was applied.
		revision = transport.apiRevision.Add(1)
	}
	current := transport.api
	if current != nil {
		current.BroadcastConfig(cfg)
	}
	transport.apiMu.Unlock()
	if restart {
		transport.scheduleAPIReconfigure(cfg, revision)
	}
}

func standaloneAPISettingsFromConfig(cfg config.Config) standaloneAPISettings {
	return standaloneAPISettings{
		enabled: cfg.Integration.LocalAPIEnabled,
		listen:  cfg.Integration.LocalAPIListen,
		token:   cfg.Integration.LocalAPIToken,
	}
}

func (transport *standaloneTransport) startAPI(
	cfg config.Config,
) (standaloneAPIServer, error) {
	if !cfg.Integration.LocalAPIEnabled {
		return nil, nil
	}
	if transport.apiFactory != nil {
		return transport.apiFactory(cfg)
	}
	server, err := localapi.NewWithOptions(localapi.Options{
		Listen:      cfg.Integration.LocalAPIListen,
		BearerToken: cfg.Integration.LocalAPIToken,
		Handler:     transport.runtime.Call,
		EventSource: transport.runtime,
		AppHandler:  webui.Handler(),
		AudioAsset: func(cue string) (localapi.AudioAsset, error) {
			asset, resolveErr := transport.effects.ResolveAudio(cue)
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
			imported, importErr := transport.effects.ImportAudio(name, data)
			return localapi.ImportedAudio{
				ID:   imported.ID,
				Name: imported.Name,
			}, importErr
		},
		Capabilities: map[string]bool{
			"native_shell":   false,
			"window_control": false,
			"notifications":  false,
			"global_hotkeys": false,
			"hotkey_capture": false,
			"hotkey_probe":   false,
			"audio_sfx":      true,
		},
	})
	if err != nil {
		return nil, err
	}
	return server, nil
}

func (transport *standaloneTransport) initializeAPI(
	cfg config.Config,
	revision uint64,
) {
	transport.apiOpMu.Lock()
	defer transport.apiOpMu.Unlock()

	if !transport.apiGenerationCurrent(cfg, revision) {
		return
	}
	server, err := transport.startAPI(cfg)
	if transport.publishAPIResult(cfg, revision, server, err) {
		if err != nil {
			transport.closeDetachedAPI(server)
		}
		return
	}
	transport.closeDetachedAPI(server)
}

func (transport *standaloneTransport) scheduleAPIReconfigure(
	cfg config.Config,
	revision uint64,
) {
	transport.lifecycleMu.Lock()
	if transport.closed {
		transport.lifecycleMu.Unlock()
		return
	}
	transport.workers.Add(1)
	transport.lifecycleMu.Unlock()
	go func() {
		defer transport.workers.Done()
		transport.reconfigureAPI(cfg, revision)
	}()
}

func (transport *standaloneTransport) reconfigureAPI(
	cfg config.Config,
	revision uint64,
) {
	transport.apiOpMu.Lock()
	defer transport.apiOpMu.Unlock()

	existing, current := transport.detachAPIForGeneration(cfg, revision)
	if !current {
		return
	}
	transport.closeDetachedAPI(existing)
	if !transport.apiGenerationCurrent(cfg, revision) {
		return
	}
	if !cfg.Integration.LocalAPIEnabled {
		return
	}

	replacement, err := transport.startAPI(cfg)
	if transport.publishAPIResult(cfg, revision, replacement, err) {
		if err != nil {
			transport.closeDetachedAPI(replacement)
		}
		return
	}
	transport.closeDetachedAPI(replacement)
}

func (transport *standaloneTransport) apiGenerationCurrent(
	cfg config.Config,
	revision uint64,
) bool {
	transport.lifecycleMu.Lock()
	defer transport.lifecycleMu.Unlock()
	transport.apiMu.Lock()
	defer transport.apiMu.Unlock()
	return !transport.closed &&
		revision == transport.apiRevision.Load() &&
		transport.apiSettings == standaloneAPISettingsFromConfig(cfg)
}

func (transport *standaloneTransport) detachAPIForGeneration(
	cfg config.Config,
	revision uint64,
) (standaloneAPIServer, bool) {
	transport.lifecycleMu.Lock()
	defer transport.lifecycleMu.Unlock()
	transport.apiMu.Lock()
	defer transport.apiMu.Unlock()
	if transport.closed ||
		revision != transport.apiRevision.Load() ||
		transport.apiSettings != standaloneAPISettingsFromConfig(cfg) {
		return nil, false
	}
	existing := transport.api
	transport.api = nil
	// A previous bind error belongs to the listener generation being
	// replaced. Clear it before exposing the asynchronous "starting" state.
	transport.apiErr = nil
	return existing, true
}

func (transport *standaloneTransport) publishAPIResult(
	cfg config.Config,
	revision uint64,
	server standaloneAPIServer,
	err error,
) bool {
	transport.lifecycleMu.Lock()
	defer transport.lifecycleMu.Unlock()
	transport.apiMu.Lock()
	defer transport.apiMu.Unlock()
	if transport.closed ||
		revision != transport.apiRevision.Load() ||
		transport.apiSettings != standaloneAPISettingsFromConfig(cfg) ||
		transport.api != nil {
		return false
	}
	transport.apiErr = err
	if err == nil {
		transport.api = server
	}
	return true
}

func (*standaloneTransport) closeDetachedAPI(server standaloneAPIServer) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = server.Close(ctx)
	cancel()
}

func (transport *standaloneTransport) openDashboardRoute(route string) {
	if transport == nil {
		return
	}
	transport.apiMu.Lock()
	server := transport.api
	transport.apiMu.Unlock()
	if server == nil {
		return
	}
	target := strings.TrimSuffix(server.BrowserURL(), "/")
	route = strings.Trim(strings.TrimSpace(route), "/#")
	if route != "" {
		target += "/#/" + route
	}
	_ = winfx.OpenURL(target)
}

func (transport *standaloneTransport) canOpenDashboard() error {
	if transport == nil {
		return fmt.Errorf("standalone transport is unavailable")
	}
	transport.apiMu.Lock()
	defer transport.apiMu.Unlock()
	if transport.api != nil {
		return nil
	}
	if transport.apiErr != nil {
		return fmt.Errorf("local API/Web UI listener is unavailable: %w", transport.apiErr)
	}
	if !transport.apiSettings.enabled {
		return fmt.Errorf("local API/Web UI is disabled; enable Serving first")
	}
	return fmt.Errorf("local API/Web UI listener is still starting")
}

func (transport *standaloneTransport) ServingStatus(
	context.Context,
) (ServingStatus, error) {
	if transport == nil {
		return ServingStatus{}, fmt.Errorf("standalone transport is unavailable")
	}
	transport.apiMu.Lock()
	defer transport.apiMu.Unlock()
	status := ServingStatus{Enabled: transport.apiSettings.enabled}
	if transport.api == nil {
		if transport.apiErr != nil {
			status.Error = compactError(transport.apiErr)
		}
		return status, nil
	}
	status.Running = true
	status.URL = transport.api.BrowserURL()
	return status, nil
}

func (transport *standaloneTransport) Close() error {
	if transport == nil {
		return nil
	}
	transport.closeOnce.Do(func() {
		transport.signalQuit()
		transport.lifecycleMu.Lock()
		transport.closed = true
		transport.apiRevision.Add(1)
		transport.lifecycleMu.Unlock()

		transport.apiOpMu.Lock()
		transport.apiMu.Lock()
		server := transport.api
		transport.api = nil
		transport.apiMu.Unlock()
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := server.Close(ctx); transport.closeErr == nil {
				transport.closeErr = err
			}
			cancel()
		}
		transport.apiOpMu.Unlock()
		transport.workers.Wait()

		if transport.unsubscribe != nil {
			transport.unsubscribe()
		}
		if transport.unsubscribeEffects != nil {
			transport.unsubscribeEffects()
		}
		if transport.effects != nil {
			transport.effects.Close()
		}
		if err := transport.runtime.Close(); transport.closeErr == nil {
			transport.closeErr = err
		}
	})
	return transport.closeErr
}
