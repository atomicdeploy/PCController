//go:build windows

// Package application composes the independently testable core, transports,
// and Windows shell into the primary PCControllerDesktop process.
package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/cli"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/localapi"
	"pccontroller.local/desktop/internal/sfx"
	"pccontroller.local/desktop/internal/ui"
	"pccontroller.local/desktop/internal/webui"
	"pccontroller.local/desktop/internal/winfx"
)

var (
	ErrAlreadyRunning = errors.New("PCControllerDesktop is already running")
	ErrClosed         = errors.New("PCControllerDesktop application is closed")
)

type primaryIPCServer interface {
	Close() error
}

var (
	listenPrimaryIPC = func(handler ipc.Handler) (primaryIPCServer, error) {
		return ipc.Listen(handler)
	}
	isPrimaryIPCRunning = ipc.IsRunning
	savePrimaryConfig   = config.Save
)

// readyIPCHandler lets the named pipe claim single-instance ownership before
// the Windows shell exists without acknowledging shell commands prematurely.
// Calls already connected to the pipe wait until hooks are installed or until
// their server-derived context is canceled during failed startup.
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

type App struct {
	configPath string
	logger     *log.Logger
	logFile    *os.File

	core       *appcore.Core
	dispatcher *commands.Dispatcher
	shell      *ui.Shell
	ipcServer  primaryIPCServer

	lifecycleMu sync.Mutex
	closed      bool
	closeOnce   sync.Once
	workers     sync.WaitGroup

	apiMu          sync.Mutex
	apiServer      *localapi.Server
	apiRevision    atomic.Uint64
	apiSettings    localAPISettings
	apiSettingsSet bool
}

type localAPISettings struct {
	enabled bool
	listen  string
	token   string
}

func New(options cli.Options) (*App, error) {
	configPath, err := config.ResolvePath(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(configPath)
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
	var startWithWindowsProbeErr error
	if executable, executableErr := os.Executable(); executableErr != nil {
		startWithWindowsProbeErr = fmt.Errorf(
			"locate executable: %w",
			executableErr,
		)
	} else {
		// This is a read-only reconciliation. The winning primary persists
		// the resulting configuration only after it owns the IPC pipe.
		cfg.Integration.StartWithWindows, startWithWindowsProbeErr =
			winfx.ReconcileStartWithWindowsSetting(
				executable,
				cfg.Integration.StartWithWindows,
			)
	}
	core, err := appcore.New(cfg)
	if err != nil {
		return nil, err
	}
	dispatcher, err := commands.New(core, configPath)
	if err != nil {
		return nil, err
	}

	// The named pipe is the race-free single-instance gate. The HWND property
	// is registered by the shell immediately afterward for low-latency
	// WM_COPYDATA command handoff.
	readyHandler := newReadyIPCHandler(dispatcher.Handler)
	ipcServer, err := listenPrimaryIPC(readyHandler.Handle)
	if err != nil {
		probeTimeout := options.Timeout
		if probeTimeout <= 0 || probeTimeout > 12*time.Second {
			probeTimeout = 12 * time.Second
		}
		probeCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		if isPrimaryIPCRunning(probeCtx) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	ipcTransferred := false
	defer func() {
		if !ipcTransferred {
			_ = ipcServer.Close()
		}
	}()

	// Only the process that owns the single-instance pipe may create or replace
	// the first-run configuration. A losing process loaded a different random
	// API token and must never persist it over the primary's token.
	if err := savePrimaryConfig(configPath, cfg); err != nil {
		return nil, err
	}

	logger, logFile, err := newLogger(configPath)
	if err != nil {
		return nil, err
	}
	if startWithWindowsProbeErr != nil {
		logger.Printf(
			"inspect start-with-Windows registration: %v",
			startWithWindowsProbeErr,
		)
	}
	if soundWarning != nil {
		logger.Printf(
			"selected custom sound is unavailable; repaired to bundled sounds: %v",
			soundWarning,
		)
	}
	logTransferred := false
	defer func() {
		if !logTransferred {
			_ = logFile.Close()
		}
	}()

	app := &App{
		configPath: configPath,
		logger:     logger,
		logFile:    logFile,
		core:       core,
		dispatcher: dispatcher,
		ipcServer:  ipcServer,
	}
	shell, err := ui.New(core, dispatcher, configPath, logger.Printf)
	if err != nil {
		return nil, err
	}
	app.shell = shell
	shell.SetConfigPreviewCallback(func(preview config.Config) {
		app.broadcastLocalAPIPreview(preview)
	})
	shell.SetConfigAppliedCallback(func(updated config.Config) {
		app.localAPIConfigApplied(updated)
	})
	dispatcher.SetHooks(shell.Hooks())
	readyHandler.MarkReady()
	if err := app.startLocalAPI(cfg); err != nil {
		logger.Printf("local API: %v", err)
	}
	ipcTransferred = true
	logTransferred = true
	return app, nil
}

func (a *App) Run(options cli.Options) (int, error) {
	if a == nil || a.isClosed() {
		return -1, ErrClosed
	}
	if err := a.core.Start(context.Background()); err != nil {
		return -1, err
	}
	defer a.core.Stop()
	defer a.Close()

	startupMode := startupWindowModeForOptions(options)
	if options.HasCommand() {
		request, err := options.Request(fmt.Sprintf("startup-%d", time.Now().UnixNano()))
		if err != nil {
			return -1, err
		}
		if a.startWorker() {
			go func() {
				defer a.workers.Done()
				ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
				defer cancel()
				if _, commandErr := a.dispatcher.Handler(ctx, request); commandErr != nil {
					a.logger.Printf("startup command %s: %v", request.Method, commandErr)
				}
			}()
		}
	}
	return a.shell.Run(startupMode), nil
}

func startupWindowModeForOptions(options cli.Options) ui.StartupWindowMode {
	if options.HasCommand() {
		switch options.Method {
		case "window.show", "settings.show", "diagnostics.show":
			return ui.StartupForceShow
		default:
			// Device/control commands may bootstrap a primary instance in the
			// background, but that one-shot launch intent must not overwrite
			// the user's remembered visibility or native show state.
			return ui.StartupForceHidden
		}
	}
	if options.StartMinimized {
		return ui.StartupForceHidden
	}
	return ui.StartupRemembered
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		a.lifecycleMu.Lock()
		a.closed = true
		a.lifecycleMu.Unlock()

		if a.ipcServer != nil {
			_ = a.ipcServer.Close()
			a.ipcServer = nil
		}

		// Waiting for apiMu ensures an in-flight restart observes closed before
		// it can install a replacement server.
		a.apiMu.Lock()
		apiServer := a.apiServer
		a.apiServer = nil
		a.apiMu.Unlock()
		if apiServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = apiServer.Close(ctx)
			cancel()
		}

		a.workers.Wait()
		if a.shell != nil {
			a.shell.Close()
			a.shell = nil
		}
		if a.core != nil {
			a.core.Stop()
		}
		if a.logFile != nil {
			_ = a.logFile.Close()
			a.logFile = nil
		}
	})
}

func (a *App) isClosed() bool {
	if a == nil {
		return true
	}
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	return a.closed
}

func (a *App) startWorker() bool {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.closed {
		return false
	}
	a.workers.Add(1)
	return true
}

func (a *App) startLocalAPI(cfg config.Config) error {
	a.apiMu.Lock()
	defer a.apiMu.Unlock()
	if a.isClosed() {
		return ErrClosed
	}
	a.apiSettings = localAPISettingsFrom(cfg)
	a.apiSettingsSet = true
	return a.startLocalAPILocked(cfg)
}

func (a *App) startLocalAPILocked(cfg config.Config) error {
	if !cfg.Integration.LocalAPIEnabled {
		return nil
	}
	server, err := localapi.NewWithOptions(localapi.Options{
		Listen:      cfg.Integration.LocalAPIListen,
		BearerToken: cfg.Integration.LocalAPIToken,
		Handler:     a.dispatcher.Handler,
		EventSource: a.core,
		AppHandler:  webui.Handler(),
		AudioAsset: func(cue string) (localapi.AudioAsset, error) {
			asset, resolveErr := a.shell.ResolveAudio(cue)
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
			imported, importErr := a.shell.ImportAudio(name, data)
			return localapi.ImportedAudio{
				ID:   imported.ID,
				Name: imported.Name,
			}, importErr
		},
		HotkeyCapture: a.shell.SetBrowserHotkeyCapture,
		HotkeyProbe:   a.shell.ProbeHotkey,
	})
	if err != nil {
		return err
	}
	a.apiServer = server
	a.logger.Printf("local API listening on http://%s", server.Addr)
	return nil
}

func (a *App) localAPIConfigApplied(cfg config.Config) {
	next := localAPISettingsFrom(cfg)
	a.apiMu.Lock()
	restart := !a.apiSettingsSet || a.apiSettings != next
	a.apiSettings = next
	a.apiSettingsSet = true
	server := a.apiServer
	a.apiMu.Unlock()

	if !restart {
		if server != nil {
			server.BroadcastConfig(cfg)
		}
		return
	}
	// Never synchronously shut down the HTTP server from inside one of its own
	// config PATCH handlers.
	a.scheduleLocalAPIRestart(cfg)
}

func (a *App) broadcastLocalAPIPreview(cfg config.Config) {
	if a == nil {
		return
	}
	// Preview originates on the native UI thread. A concurrent listener
	// rebind may hold apiMu while graceful HTTP shutdown completes; dropping
	// one transient preview is preferable to freezing the window. The next
	// preview or authoritative committed configuration reconciles every tab.
	if !a.apiMu.TryLock() {
		return
	}
	server := a.apiServer
	a.apiMu.Unlock()
	if server != nil {
		server.BroadcastConfigPreview(cfg)
	}
}

func localAPISettingsFrom(cfg config.Config) localAPISettings {
	return localAPISettings{
		enabled: cfg.Integration.LocalAPIEnabled,
		listen:  cfg.Integration.LocalAPIListen,
		token:   cfg.Integration.LocalAPIToken,
	}
}

func (a *App) scheduleLocalAPIRestart(cfg config.Config) {
	if !a.startWorker() {
		return
	}
	revision := a.apiRevision.Add(1)
	go func() {
		defer a.workers.Done()
		a.restartLocalAPI(cfg, revision)
	}()
}

func (a *App) restartLocalAPI(cfg config.Config, revision uint64) {
	a.apiMu.Lock()
	defer a.apiMu.Unlock()
	if revision != a.apiRevision.Load() || a.isClosed() {
		return
	}
	existing := a.apiServer
	a.apiServer = nil
	if existing != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := existing.Close(ctx); err != nil {
			a.logger.Printf("close local API: %v", err)
		}
		cancel()
	}
	if revision != a.apiRevision.Load() || a.isClosed() {
		return
	}
	if err := a.startLocalAPILocked(cfg); err != nil {
		a.logger.Printf("restart local API: %v", err)
	}
}

func newLogger(configPath string) (*log.Logger, *os.File, error) {
	logDir := filepath.Dir(configPath)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	logPath := filepath.Join(logDir, "PCControllerDesktop.log")
	if info, err := os.Stat(logPath); err == nil && info.Size() > 2<<20 {
		_ = os.Rename(logPath, logPath+".previous")
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open application log: %w", err)
	}
	return log.New(file, "", log.Ldate|log.Ltime|log.Lmicroseconds), file, nil
}
