//go:build windows

// Package ui owns the Windows main window, tray presence, animation, global
// hotkeys, notifications, and the small amount of HWND message integration
// needed by PCControllerDesktop.
package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/assets"
	"pccontroller.local/desktop/internal/animation"
	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/automation"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/dialogs"
	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/notify"
	"pccontroller.local/desktop/internal/sfx"
	"pccontroller.local/desktop/internal/shellintegration"
	"pccontroller.local/desktop/internal/traymenu"
	"pccontroller.local/desktop/internal/vector"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/internal/wmipc"
	"pccontroller.local/desktop/presentation"
)

const (
	subclassID        = 0x524C414D
	glassSubclassID   = subclassID + 1
	surfaceSubclassID = subclassID + 2

	hotkeyToggle int32 = 0x5241
	hotkeyOn     int32 = 0x5242
	hotkeyOff    int32 = 0x5243
	hotkeyShow   int32 = 0x5244

	wmSettingChange               = 0x001A
	wmQueryEndSession             = 0x0011
	wmEndSession                  = 0x0016
	wmCancelMode                  = 0x001F
	wmSystemColorChange           = 0x0015
	wmActivate                    = 0x0006
	wmEraseBackground             = 0x0014
	wmShowWindow                  = 0x0018
	wmTimer                       = 0x0113
	wmHotkey                      = 0x0312
	wmThemeChanged                = 0x031A
	wmDWMCompositionChanged       = 0x031E
	wmDWMColorizationColorChanged = 0x0320
	wmSize                        = 0x0005
	wmNCCalcSize                  = 0x0083
	wmNCHitTest                   = 0x0084
	wmNCActivate                  = 0x0086
	wmNCUAHDrawCaption            = 0x00AE // themed non-client painting bypasses WM_NCPAINT
	wmNCUAHDrawFrame              = 0x00AF
	wmDisplayChange               = 0x007E
	wmContextMenu                 = 0x007B
	wmSizing                      = 0x0214
	wmMoving                      = 0x0216
	wmEnterSizeMove               = 0x0231
	wmExitSizeMove                = 0x0232
	wmPowerBroadcast              = 0x0218
	wmReapplyWindowIcons          = 0x8524 // WM_APP + "R$"
	wmCaptureWindowPlacement      = 0x8525 // deferred native-state settle
	wmReapplyAppearance           = 0x8526 // after native theme processing
	wmReassertWindowChrome        = 0x8528 // 0x8527 is winfx.WMSystemNetworkChange
	sizeRestored                  = 0
	sizeMinimized                 = 1
	sizeMaximized                 = 2
	visualTimerID                 = 0x524C
	windowFadeTimerID             = 0x524D
	iconSmall                     = 0
	iconBig                       = 1
	iconSmall2                    = 2
)

var (
	uiCopyMemory       = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlMoveMemory")
	uiGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").
				NewProc("GetAsyncKeyState")
)

type Logger func(format string, args ...any)

type audioJob struct {
	cue   sfx.Cue
	count int
}

type Shell struct {
	controllerOwner             *Shell
	controllerWidgetID          string
	controllerWidgets           map[string]*Shell
	controllerChartRasters      map[int]*controllerChartRaster
	controllerHistory           []controllerTelemetryPoint
	controller                  *presentation.Options
	controllerState             presentation.Snapshot
	controllerBusy              bool
	controllerIconFonts         map[int]*walk.Font
	controllerChannels          bool
	controllerPreferences       bool
	controllerPage              int
	controllerActionIDs         map[actionID]string
	controllerChannelHits       []controllerChannelHit
	controllerGesture           controllerChannelGesture
	controllerGestureGeneration uint64
	controllerInline            *controllerInlineEdit
	controllerPendingPWM        controllerChannelGesture
	controllerTrayStatus        string
	controllerTrayIcons         map[string]walk.Image
	controllerMinimumHeight     int
	controllerSegmentLevels     [4][8]float64
	controllerSegmentTargets    [4][8]float64
	controllerSegmentsReady     bool
	controllerNotifications     chan controllerNotification
	core                        *appcore.Core
	dispatcher                  *commands.Dispatcher
	configPath                  string
	log                         Logger

	mw                   *walk.MainWindow
	surface              *walk.CustomWidget
	tray                 *walk.NotifyIcon
	settings             *dialogs.SettingsWindow
	diagnostics          *walk.Dialog
	diagnosticsSyncTheme func(config.Config)
	clientHWND           uintptr

	ctx    context.Context
	cancel context.CancelFunc

	cfgMu sync.RWMutex
	cfg   config.Config

	localeMu             sync.RWMutex
	localizer            i18n.Localizer
	fontFamily           string
	persianFontAvailable bool

	snapshot                   model.Snapshot
	layout                     surfaceLayout
	hover                      actionID
	hoverLevels                [128]float64
	pressLevels                [128]float64
	buttonActivation           buttonActivationState
	visualPhase                float64
	visualTick                 time.Time
	visualPointer              walk.Point
	visualPointerDirty         bool
	visualTimerRunning         bool
	trackingMouse              bool
	foregroundRecovery         *foregroundRecovery
	glassFrame                 *image.RGBA
	glassCache                 glassCompositionCache
	hitSurface                 *image.RGBA
	presentedSurface           *image.RGBA
	dark                       bool
	highContrast               bool
	motionEnabled              bool
	backdrop                   string
	materialRequested          string
	materialApplying           bool
	appearanceReapplyPosted    bool
	windowChromeReassertPosted bool
	nativeTitleBar             bool
	borderMode                 string
	perPixelOverlay            bool
	rendering                  renderingState
	alphaRegionActive          bool
	alphaRegionShape           uint64
	taskbarInitialized         bool
	taskbarVisible             bool
	taskbarIntegration         *winfx.Taskbar
	taskbarButtonIcons         taskbarButtonIcons
	taskbarSuspended           bool
	hotkeyCaptureActive        bool
	nativeHotkeyCapture        bool
	browserHotkeyCapture       bool

	titleFont   *walk.Font
	stateFont   *walk.Font
	bodyFont    *walk.Font
	buttonFont  *walk.Font
	captionFont *walk.Font
	// Walk creates every font with CLEARTYPE_QUALITY. Subpixel ClearType
	// cannot be represented by one alpha channel in UpdateLayeredWindow, so
	// layered text uses private grayscale-antialiased HFONTs cached here.
	antialiasFonts   map[surfaceFontKey]win.HFONT
	surfaceTextMode  surfaceTextMode
	surfaceTextRuns  []surfaceTextRun
	surfaceTextMasks map[surfaceTextMaskKey]*image.Alpha

	animation             *animation.Animation
	animationPlayer       animation.Player
	animationBitmaps      map[int]*walk.Bitmap
	animationBitmapSize   int
	animationCacheEpoch   atomic.Uint64
	animationCachePrepare bool
	animationCacheSize    int
	currentAnimationFrame int
	animationActive       bool
	animationEpoch        atomic.Uint64
	deviceOnSVG           []byte
	deviceOffSVG          []byte
	vectorOnBitmap        *walk.Bitmap
	vectorOffBitmap       *walk.Bitmap
	vectorDeviceSize      int
	artworkSources        map[artworkID]image.Image
	artworkBitmaps        map[artworkBitmapKey]artworkBitmapEntry
	artworkCacheTick      uint64
	dragSnap              magneticDragSession

	appIcon         walk.Image
	onIcon          walk.Image
	offIcon         walk.Image
	unknownIcon     walk.Image
	connectingIcon  walk.Image
	errorIcon       walk.Image
	settingsIcon    walk.Image
	diagnosticsIcon walk.Image

	notifier   *notify.Manager
	automation *automation.Runner
	audio      *sfx.Manager
	audioJobs  chan audioJob
	audioDone  chan struct{}

	systemEvents                *winfx.SystemEvents
	systemRecovery              *systemRecoveryCoordinator
	startWithWindowsPersistence *winfx.StartWithWindowsPersistence

	unsubscribe func()
	eventDone   chan struct{}
	quitting    atomic.Bool
	closed      atomic.Bool

	windowFade             windowFadeState
	windowFadeBackend      windowFadeBackend
	windowOpacitySession   *winfx.UniformWindowOpacitySession
	windowFadeTimerRunning bool
	windowShownOnce        bool
	windowCloseAllowed     bool
	exitTransitionStarted  bool
	windowSessionTracking  atomic.Bool
	menuArtworkPrewarm     sync.Once

	onConfigApplied func(config.Config)
	onConfigPreview func(config.Config)
}

func New(
	core *appcore.Core,
	dispatcher *commands.Dispatcher,
	configPath string,
	logger Logger,
) (*Shell, error) {
	return newShell(core, dispatcher, configPath, logger, nil)
}

func newShell(core *appcore.Core, dispatcher *commands.Dispatcher, configPath string, logger Logger, controller *presentation.Options) (*Shell, error) {
	if core == nil || dispatcher == nil {
		return nil, errors.New("ui: core and dispatcher are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	shell := &Shell{
		controller: controller,
		core:       core,
		dispatcher: dispatcher,
		configPath: configPath,
		log:        logger,
		cfg:        core.Config(),
		snapshot:   core.Snapshot(),
		ctx:        ctx,
		cancel:     cancel,
		eventDone:  make(chan struct{}),
		audioJobs:  make(chan audioJob, 64),
		audioDone:  make(chan struct{}),
	}
	shell.initializeLocalization()
	sounds, soundWarning := sfx.NewManager(sfx.Options{
		ConfigPath:    configPath,
		Notifications: shell.cfg.Notifications,
	})
	if sounds == nil {
		cancel()
		return nil, fmt.Errorf("initialize shared sound effects: %w", soundWarning)
	}
	shell.audio = sounds
	if soundWarning != nil {
		shell.logf(
			"selected custom sound is unavailable; using bundled sounds: %v",
			soundWarning,
		)
	}
	go shell.runAudio()
	if controller == nil {
		if executable, executableErr := os.Executable(); executableErr != nil {
			shell.logf(
				"initialize start-with-Windows persistence: locate executable: %v",
				executableErr,
			)
		} else {
			persistence, persistenceErr := winfx.NewStartWithWindowsPersistence(
				executable,
				shell.cfg.Integration.StartWithWindows,
			)
			if persistenceErr != nil {
				shell.logf(
					"initialize start-with-Windows persistence: %v",
					persistenceErr,
				)
			} else {
				shell.startWithWindowsPersistence = persistence
			}
		}
	}
	if controller != nil {
		if executable, err := os.Executable(); err == nil {
			persistence, err := winfx.NewNamedStartWithWindowsPersistence("PCController", executable, shell.cfg.Integration.StartWithWindows, "gui")
			if err != nil {
				shell.logf("initialize PCController startup preference: %v", err)
			} else {
				shell.startWithWindowsPersistence = persistence
			}
		}
	}
	if err := shell.createWindow(); err != nil {
		cancel()
		shell.dispose()
		return nil, err
	}
	// Start immutable menu artwork preparation as soon as the owner HWND has a
	// monitor DPI. It now runs while the rest of the shell and notification
	// icon are being constructed, before either menu entry point is usable.
	if shell.cfg.UI.PrewarmTrayMenu {
		shell.prewarmAppMenuArtwork()
	}
	if err := shell.loadArtwork(); err != nil {
		cancel()
		shell.dispose()
		return nil, err
	}
	if err := shell.createTray(); err != nil {
		cancel()
		shell.dispose()
		return nil, err
	}
	if err := shell.createNotifications(); err != nil {
		shell.logf("notification initialization: %v", err)
	}

	if controller != nil {
		shell.controllerNotifications = make(chan controllerNotification, 32)
		go shell.runControllerNotifications()
		shell.automation = automation.NewController(shell.cfg.Automation, func(message string) { shell.logf("%s", message) })
	} else {
		shell.automation = automation.New(shell.cfg.Automation, func(message string) { shell.logf("%s", message) })
	}
	if controller == nil {
		shell.systemRecovery = newSystemRecoveryCoordinator(
			defaultSystemRecoveryDelay,
			shell.core.Snapshot,
			shell.core.Reconnect,
			func() {
				shell.handleSystemAutomation(
					automation.SystemEventNetworkChange,
				)
			},
			shell.logf,
		)
	}
	shell.installEvents()
	shell.applyConfigUI(shell.cfg, false)
	shell.updateTray()
	shell.updateTaskbarIntegration()
	shell.surface.Invalidate()
	// Prepare the first foreground while the top-level window is still hidden.
	// This creates and seeds the DirectComposition swap chain (or the legacy
	// layered DIB) before ShowWindow can expose an uninitialized frame.
	win.UpdateWindow(shell.surface.Handle())
	// Ignore the synthetic WM_MOVE/WM_SIZE traffic produced while Walk builds
	// and styles the hidden HWND. From this point onward those messages describe
	// real session state and may safely update the remembered placement.
	shell.windowSessionTracking.Store(true)
	return shell, nil
}

func (s *Shell) HWND() uintptr {
	if s == nil || s.mw == nil {
		return 0
	}
	return uintptr(s.mw.Handle())
}

func (s *Shell) SetConfigAppliedCallback(callback func(config.Config)) {
	s.onConfigApplied = callback
}

// SetConfigPreviewCallback observes every configuration applied to the live
// native shell, including transactional edits in the modeless Settings
// window. Persistence and listener restarts remain exclusive to
// ConfigApplied.
func (s *Shell) SetConfigPreviewCallback(callback func(config.Config)) {
	s.onConfigPreview = callback
}

func (s *Shell) Hooks() commands.Hooks {
	return commands.Hooks{
		ShowWindow:      s.Show,
		HideWindow:      s.Hide,
		OpenSettings:    s.OpenSettings,
		OpenDiagnostics: s.OpenDiagnostics,
		OpenWebUI:       s.OpenWebUI,
		Quit:            s.Quit,
		Notify: func(title, message string) error {
			return s.ShowNotification(title, message, true)
		},
		PlayAudio: func(cue string, count int) error {
			return s.QueueAudio(cue, count)
		},
		ImportAudioFile: func(path string) (commands.AudioImportResult, error) {
			imported, err := s.ImportAudioFile(path)
			return commands.AudioImportResult{
				ID:   imported.ID,
				Name: imported.Name,
			}, err
		},
		PrepareConfig: s.prepareConfig,
		ConfigApplied: func(cfg config.Config) {
			s.ApplyConfig(cfg)
			s.applySavedWindowsIntegration(cfg)
			if s.mw != nil {
				s.mw.Synchronize(func() {
					if settings := s.settings; settings != nil &&
						!settings.IsDisposed() {
						settings.SyncConfig(cfg)
					}
					if syncTheme := s.diagnosticsSyncTheme; syncTheme != nil {
						syncTheme(cfg)
					}
				})
			}
			if callback := s.onConfigApplied; callback != nil {
				callback(cfg)
			}
		},
	}
}

func (s *Shell) prepareConfig(cfg config.Config) (config.Config, error) {
	if s == nil || s.audio == nil {
		return cfg, nil
	}
	effective, warning := s.audio.PreflightNotifications(cfg.Notifications)
	cfg.Notifications = effective
	if warning != nil {
		s.logf(
			"selected custom sound is unavailable; repaired to bundled sounds: %v",
			warning,
		)
	}
	return cfg, nil
}

func (s *Shell) Run(startupMode StartupWindowMode) int {
	if s == nil || s.mw == nil {
		return -1
	}
	action := ResolveStartupWindowAction(startupMode, s.Config().UI)
	switch action {
	case StartupShowNormal:
		s.showRememberedWindow(config.WindowStateNormal)
	case StartupShowMinimized:
		s.showRememberedWindow(config.WindowStateMinimized)
	case StartupShowMaximized:
		s.showRememberedWindow(config.WindowStateMaximized)
	}
	if startupMode == StartupForceShow && action != StartupHide {
		s.setRememberedWindowSession(true, windowStateForStartupAction(action))
		s.persistWindowPlacement()
	}
	exitCode := s.mw.Run()
	s.Close()
	return exitCode
}

func (s *Shell) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.quitting.Store(true)
	for _, widget := range s.controllerWidgets {
		widget.Close()
	}
	s.controllerWidgets = nil
	// Widget HWNDs borrow the root's immutable product icon.
	if s.controllerOwner != nil {
		s.appIcon = nil
	}
	s.closeControllerInline()
	s.disposeControllerChartRasters()
	for _, font := range s.controllerIconFonts {
		font.Dispose()
	}
	s.controllerIconFonts = nil
	s.cancelButtonActivation()
	s.cleanupWindowFade()
	// The exit fade ends with an internal SW_HIDE. Flush the placement captured
	// before that implementation detail instead of recapturing and incorrectly
	// remembering every clean exit as user-hidden.
	s.persistWindowPlacement()
	s.animationEpoch.Add(1)
	s.cancel()
	s.animationPlayer.Stop()
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
	s.closeWindowBoundIntegrations()
	if s.systemRecovery != nil {
		s.systemRecovery.Close()
		s.systemRecovery = nil
	}
	if s.automation != nil {
		s.automation.Close()
	}
	select {
	case <-s.eventDone:
	case <-time.After(2 * time.Second):
	}
	if s.notifier != nil {
		if err := s.notifier.Close(); err != nil {
			s.logf("close notification manager: %v", err)
		}
		s.notifier = nil
	}
	s.unregisterHotkeys()
	_ = winfx.RemoveWindowSubclass(s.HWND(), subclassID)
	_ = winfx.RemoveWindowSubclass(s.clientHWND, glassSubclassID)
	if s.surface != nil {
		win.KillTimer(s.surface.Handle(), visualTimerID)
		_ = winfx.RemoveWindowSubclass(uintptr(s.surface.Handle()), surfaceSubclassID)
	}
	wmipc.UnregisterPrimary(s.HWND())
	s.dispose()
}

func (s *Shell) Config() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Shell) ApplyConfig(cfg config.Config) {
	if s == nil || s.mw == nil || s.quitting.Load() || s.closed.Load() {
		return
	}
	if s.controller != nil {
		cfg = controllerViewConfig(s.Config(), cfg)
	}
	cfg, _ = s.prepareConfig(cfg)
	s.cfgMu.Lock()
	if s.quitting.Load() || s.closed.Load() {
		s.cfgMu.Unlock()
		return
	}
	// X/Y and show state belong to the live shell, not to an appearance preview
	// or a potentially stale modeless Settings baseline. Width/height remain
	// previewable because changing compact/dashboard mode intentionally resizes
	// the current HWND.
	cfg.UI.WindowBounds.X = s.cfg.UI.WindowBounds.X
	cfg.UI.WindowBounds.Y = s.cfg.UI.WindowBounds.Y
	cfg.UI.WindowVisible = s.cfg.UI.WindowVisible
	cfg.UI.WindowState = s.cfg.UI.WindowState
	if s.audio != nil {
		if err := s.audio.ApplyConfig(cfg.Notifications); err != nil {
			s.cfgMu.Unlock()
			s.logf("apply audible cue configuration: %v", err)
			return
		}
	}
	s.cfg = cfg
	s.cfgMu.Unlock()
	if callback := s.onConfigPreview; callback != nil {
		callback(cfg)
	}
	if s.automation != nil {
		s.automation.ApplyConfig(cfg.Automation)
	}
	s.mw.Synchronize(func() {
		if s.quitting.Load() || s.closed.Load() {
			return
		}
		s.applyConfigUI(cfg, true)
		s.syncControllerWidgetAppearance()
	})
}

func (s *Shell) Show() {
	if s == nil || s.mw == nil {
		return
	}
	s.mw.Synchronize(func() {
		if s.quitting.Load() || s.closed.Load() {
			return
		}
		state := s.rememberedWindowState()
		// "Show" means make the window usable now. A window remembered as
		// minimized starts minimized on an ordinary launch, but an explicit
		// tray/IPC/CLI Show restores it to its normal rectangle.
		if state == config.WindowStateMinimized {
			state = config.WindowStateNormal
		}
		s.setRememberedWindowSession(true, state)
		s.showWindowWithState(state, true, true)
		s.persistWindowPlacement()
	})
}

func (s *Shell) Hide() {
	if s == nil || s.mw == nil {
		return
	}
	s.mw.Synchronize(func() {
		s.cancelButtonActivation()
		s.observeNativeWindowPlacement()
		s.setRememberedWindowVisibility(false)
		s.persistWindowPlacement()
		s.mw.SetVisible(false)
		s.cancelEntranceFadeWhileHidden()
		s.releaseHiddenRenderResources()
	})
}

func (s *Shell) Quit() {
	if s == nil || s.mw == nil {
		return
	}
	if s.controllerOwner != nil {
		s.Hide()
		return
	}
	if !s.quitting.CompareAndSwap(false, true) {
		return
	}
	s.mw.Synchronize(func() {
		s.beginExitTransition()
	})
}

func (s *Shell) ShowNotification(title, message string, audible bool) error {
	cfg := s.Config()
	if audible && cfg.Notifications.Sound {
		if err := s.QueueAudio(string(sfx.CueMessage), 1); err != nil {
			s.logf("queue notification sound: %v", err)
		}
	}
	if s.controller != nil {
		if !cfg.Notifications.Enabled || s.controller.Notify == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		return s.controller.Notify(ctx, title, message, cfg.Notifications.Delivery)
	}
	if !cfg.Notifications.Enabled || s.notifier == nil {
		return nil
	}
	// Visual delivery stays silent because the shared cue above is also used
	// by the Web UI. This avoids a second OS-selected toast sound.
	return s.notifier.Show(title, message, false)
}

func (s *Shell) createWindow() error {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return fmt.Errorf("create main window: %w", err)
	}
	s.mw = mw
	nativeTitleBar := effectiveTitleBarMode(s.cfg.UI) == config.TitleBarNative
	if err := winfx.ConfigureWindowChrome(
		uintptr(mw.Handle()),
		nativeTitleBar,
		s.cfg.UI.BorderMode,
	); err != nil {
		return fmt.Errorf("configure main window chrome: %w", err)
	}
	s.nativeTitleBar = nativeTitleBar
	s.borderMode = s.cfg.UI.BorderMode
	perPixelOverlay := perPixelForegroundForUI(s.cfg.UI)
	if err := winfx.SetPerPixelOverlay(
		uintptr(mw.Handle()),
		perPixelOverlay,
	); err != nil {
		return fmt.Errorf("create layered glass window: %w", err)
	}
	s.perPixelOverlay = perPixelOverlay
	if err := mw.SetTitle(s.tr(i18n.MsgAppName)); err != nil {
		return err
	}
	mw.ToolBar().SetVisible(false)
	mw.StatusBar().SetVisible(false)

	layout := walk.NewVBoxLayout()
	if err := layout.SetMargins(walk.Margins{}); err != nil {
		return err
	}
	if err := layout.SetSpacing(0); err != nil {
		return err
	}
	if err := mw.SetLayout(layout); err != nil {
		return err
	}

	surface, err := walk.NewCustomWidgetPixels(mw, win.WS_TABSTOP, s.paint)
	if err != nil {
		return fmt.Errorf("create main surface: %w", err)
	}
	s.surface = surface
	surface.SetPaintMode(surfacePaintMode(s.cfg.UI, s.cfg.UI.Backdrop))
	surface.SetInvalidatesOnResize(true)
	surface.MouseMove().Attach(s.onMouseMove)
	surface.MouseDown().Attach(s.onMouseDown)
	surface.MouseUp().Attach(s.onMouseUp)
	surface.KeyDown().Attach(s.onKeyDown)
	surface.KeyUp().Attach(s.onKeyUp)
	mw.KeyDown().Attach(s.onKeyDown)
	mw.KeyUp().Attach(s.onKeyUp)
	mw.Closing().Attach(s.onClosing)

	client := mw.AsContainerBase()
	s.clientHWND = uintptr(client.Handle())
	client.KeyDown().Attach(s.onKeyDown)
	client.KeyUp().Attach(s.onKeyUp)
	if err := winfx.MakeTransparentChild(s.clientHWND); err != nil {
		return fmt.Errorf("make client container transparent: %w", err)
	}
	if err := winfx.SetWindowSubclass(
		s.clientHWND,
		glassSubclassID,
		func(_, message, _, lParam uintptr) (uintptr, bool) {
			switch uint32(message) {
			case win.WM_ERASEBKGND:
				return 1, true
			case win.WM_KILLFOCUS:
				s.cancelKeyboardActivation()
			case wmNCHitTest:
				if s.customCaptionHit(lParam) != win.HTCLIENT {
					return ^uintptr(0), true // route non-client input to the root HWND
				}
				if s.transparentPixelPassesThrough(lParam) {
					return ^uintptr(0), true // HTTRANSPARENT
				}
				if windowResizeBorderEnabled(s.Config().UI) {
					if _, hit := resizeHitTest(s.mw.Handle(), lParam); hit {
						return ^uintptr(0), true // HTTRANSPARENT
					}
				}
			}
			return 0, false
		},
	); err != nil {
		return fmt.Errorf("subclass client container: %w", err)
	}
	if err := winfx.SetWindowSubclass(
		uintptr(surface.Handle()),
		surfaceSubclassID,
		func(_, message, wParam, lParam uintptr) (uintptr, bool) {
			if uint32(message) == wmTimer && wParam == visualTimerID {
				if !s.advanceVisualAnimation() {
					win.KillTimer(surface.Handle(), visualTimerID)
					s.visualTimerRunning = false
				}
				return 0, true
			}
			switch uint32(message) {
			case win.WM_MOUSELEAVE:
				s.onMouseLeave()
				return 0, true
			case win.WM_CAPTURECHANGED:
				s.deferPointerCaptureCancellation()
				token := s.controllerGesture.token
				s.mw.Synchronize(func() {
					s.cancelControllerGestureToken(token)
				})
			case win.WM_CANCELMODE:
				s.cancelButtonActivation()
			case win.WM_KILLFOCUS:
				s.cancelKeyboardActivation()
			}
			if uint32(message) == wmNCHitTest {
				if s.customCaptionHit(lParam) != win.HTCLIENT {
					return ^uintptr(0), true // route non-client input to the root HWND
				}
				if s.transparentPixelPassesThrough(lParam) {
					return ^uintptr(0), true // HTTRANSPARENT
				}
				if windowResizeBorderEnabled(s.Config().UI) {
					if _, hit := resizeHitTest(s.mw.Handle(), lParam); hit {
						return ^uintptr(0), true // HTTRANSPARENT
					}
				}
			}
			return 0, false
		},
	); err != nil {
		return fmt.Errorf("subclass foreground surface: %w", err)
	}

	bounds := reconcileRememberedWindowBounds(s.cfg.UI.WindowBounds)
	s.cfg.UI.WindowBounds = bounds
	windowSize := viewSizePixels(s.cfg.UI)
	minimum, maximum := viewMinMaxPixels(s.cfg.UI.ViewMode)
	if s.controller != nil {
		minimum.Width = 560
		minimum.Height = 550
		if s.cfg.UI.ViewMode == config.ViewCompact {
			minimum = walk.Size{Width: 340, Height: 330}
		}
		if s.controllerWidgetID != "" {
			minimum = s.controllerWidgetMinimum()
		}
	}
	if err := mw.SetMinMaxSizePixels(minimum, maximum); err != nil {
		return err
	}
	if bounds.X != -1 && bounds.Y != -1 {
		if err := mw.SetBoundsPixels(walk.Rectangle{
			X: bounds.X, Y: bounds.Y,
			Width: windowSize.Width, Height: windowSize.Height,
		}); err != nil {
			return err
		}
	} else if err := mw.SetSizePixels(windowSize); err != nil {
		return err
	}
	if err := s.applyConfiguredWindowCorners(s.cfg.UI); err != nil {
		return fmt.Errorf("apply main window corners: %w", err)
	}

	if err := s.replaceMainFonts(s.preferredFontFamily()); err != nil {
		return err
	}
	return nil
}

func viewSizePixels(ui config.UIConfig) walk.Size {
	return walk.Size{
		Width:  ui.WindowBounds.Width,
		Height: ui.WindowBounds.Height,
	}
}

func viewMinMaxPixels(mode string) (walk.Size, walk.Size) {
	if strings.EqualFold(mode, config.ViewDashboard) {
		return walk.Size{
				Width:  config.DashboardWindowMinWidth,
				Height: config.DashboardWindowMinHeight,
			}, walk.Size{
				Width:  config.DashboardWindowMaxWidth,
				Height: config.DashboardWindowMaxHeight,
			}
	}
	return walk.Size{
			Width:  config.CompactWindowMinWidth,
			Height: config.CompactWindowMinHeight,
		}, walk.Size{
			Width:  config.CompactWindowMaxWidth,
			Height: config.CompactWindowMaxHeight,
		}
}

func viewDefaultSizePixels(mode string) walk.Size {
	if strings.EqualFold(mode, config.ViewDashboard) {
		return walk.Size{
			Width:  config.DashboardWindowWidth,
			Height: config.DashboardWindowHeight,
		}
	}
	return walk.Size{
		Width:  config.CompactWindowWidth,
		Height: config.CompactWindowHeight,
	}
}

func viewCornerRadius(mode string) int {
	if strings.EqualFold(mode, config.ViewDashboard) {
		return 24
	}
	return 18
}

type windowIconMessenger func(
	hwnd win.HWND,
	message uint32,
	wParam uintptr,
	lParam uintptr,
) uintptr

func queryTaskbarIcon(
	send windowIconMessenger,
	hwnd win.HWND,
) uintptr {
	if send == nil || hwnd == 0 {
		return 0
	}
	return send(hwnd, win.WM_GETICON, iconBig, 0)
}

func applyTaskbarIcon(
	send windowIconMessenger,
	hwnd win.HWND,
	icon uintptr,
) bool {
	if send == nil || hwnd == 0 || icon == 0 {
		return false
	}
	send(hwnd, win.WM_SETICON, iconBig, icon)
	return true
}

func applySplitWindowIcons(
	setStateIcon func() error,
	queryStateBigIcon func() uintptr,
	restoreProductIcon func() error,
	applyStateBigIcon func(uintptr),
) error {
	// Walk's FormBase retains the last image passed to SetIcon and reapplies it
	// on WM_DPICHANGED. Materialize the live state icon only long enough to
	// obtain its DPI-correct ICON_BIG, then leave the product icon as Walk's
	// canonical image and override just the taskbar / Alt+Tab slot.
	if err := setStateIcon(); err != nil {
		return err
	}
	stateBigIcon := queryStateBigIcon()
	if err := restoreProductIcon(); err != nil {
		return err
	}
	if stateBigIcon != 0 {
		applyStateBigIcon(stateBigIcon)
	}
	return nil
}

func (s *Shell) applyWindowStateIcon(icon walk.Image) error {
	if s == nil || s.mw == nil || s.appIcon == nil || icon == nil {
		return nil
	}
	return applySplitWindowIcons(
		func() error { return s.mw.SetIcon(icon) },
		func() uintptr {
			return queryTaskbarIcon(win.SendMessage, s.mw.Handle())
		},
		func() error { return s.mw.SetIcon(s.appIcon) },
		func(stateBigIcon uintptr) {
			applyTaskbarIcon(
				win.SendMessage,
				s.mw.Handle(),
				stateBigIcon,
			)
		},
	)
}

func (s *Shell) restoreCaptionIcon(redrawFrame bool) {
	if s == nil || s.mw == nil || s.appIcon == nil {
		return
	}
	// Preserve the current live taskbar icon while restoring both product-icon
	// sizes through Walk. This also restores Walk's retained FormBase.icon, so
	// a future WM_DPICHANGED cannot replace the caption with a device-state icon.
	stateBigIcon := queryTaskbarIcon(win.SendMessage, s.mw.Handle())
	if err := s.mw.SetIcon(s.appIcon); err != nil {
		s.logf("restore product window icon: %v", err)
		return
	}
	applyTaskbarIcon(win.SendMessage, s.mw.Handle(), stateBigIcon)
	if redrawFrame && s.nativeTitleBar {
		win.RedrawWindow(
			s.mw.Handle(),
			nil,
			0,
			win.RDW_INVALIDATE|
				win.RDW_NOERASE|
				win.RDW_NOCHILDREN|
				win.RDW_UPDATENOW|
				win.RDW_FRAME,
		)
	}
}

func (s *Shell) loadArtwork() error {
	return s.loadArtworkWithProductIcon(func() (walk.Image, error) {
		if s.controller != nil {
			return walk.NewIconFromResource("APP")
		}
		return newProductIconImage()
	})
}

// Keep PE resource acquisition separate so native test executables without the
// packaged icon group can use the same shared artwork/rendering lifecycle.
func (s *Shell) loadArtworkWithProductIcon(load func() (walk.Image, error)) error {
	var err error
	s.appIcon, err = load()
	if err != nil {
		return err
	}
	if err := s.mw.SetIcon(s.appIcon); err != nil {
		return err
	}
	if s.controller != nil {
		s.controllerTrayIcons = make(map[string]walk.Image, 4)
		for _, state := range []string{"connected", "reconnecting", "paused", "offline"} {
			icon, err := walk.NewIconFromResource("TRAY_" + strings.ToUpper(state))
			if err != nil {
				return fmt.Errorf("load controller %s tray icon: %w", state, err)
			}
			s.controllerTrayIcons[state] = icon
		}
	}

	iconTargets := []struct {
		path   string
		target *walk.Image
	}{
		{"icons/device-on.ico", &s.onIcon},
		{"icons/device-off.ico", &s.offIcon},
		{"icons/device-unknown.ico", &s.unknownIcon},
		{"icons/device-connecting.ico", &s.connectingIcon},
		{"icons/device-error.ico", &s.errorIcon},
		{"icons/menu-settings.ico", &s.settingsIcon},
		{"icons/menu-diagnostics.ico", &s.diagnosticsIcon},
	}
	for _, item := range iconTargets {
		data, readErr := assets.Files.ReadFile(item.path)
		if readErr != nil {
			return fmt.Errorf("read %s icon pack: %w", item.path, readErr)
		}
		icon, iconErr := newPackedIconImage(data)
		if iconErr != nil {
			return fmt.Errorf("create %s icon: %w", item.path, iconErr)
		}
		*item.target = icon
	}

	s.artworkSources = make(map[artworkID]image.Image)
	for _, item := range []struct {
		path string
		id   artworkID
	}{
		{"icons/menu-settings.png", artworkSettings},
		{"icons/menu-diagnostics.png", artworkDiagnostics},
		{"icons/menu-reconnect.png", artworkReconnect},
		{"icons/menu-exit.png", artworkClose},
	} {
		decoded, decodeErr := embeddedPNG(item.path)
		if decodeErr != nil {
			return decodeErr
		}
		s.artworkSources[item.id] = decoded
	}
	if s.controller != nil {
		s.currentAnimationFrame = -1
		return nil
	}
	if s.deviceOnSVG, err = assets.Files.ReadFile("device-on.svg"); err != nil {
		return fmt.Errorf("read embedded device-on.svg: %w", err)
	}
	if s.deviceOffSVG, err = assets.Files.ReadFile("device-off.svg"); err != nil {
		return fmt.Errorf("read embedded device-off.svg: %w", err)
	}

	s.currentAnimationFrame = -1
	if s.cfg.UI.Animations {
		if err := s.ensureAnimationFrameSources(); err != nil {
			return err
		}
	}
	return nil
}

func decodeEmbeddedDeviceAnimation() (*animation.Animation, error) {
	apngFile, err := assets.Files.Open("device-transition.apng")
	if err != nil {
		return nil, fmt.Errorf("read device transition APNG: %w", err)
	}
	defer apngFile.Close()
	decoded, err := animation.DecodeReader(apngFile)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

var loadDeviceAnimation = decodeEmbeddedDeviceAnimation

func (s *Shell) ensureAnimationFrameSources() error {
	if s == nil {
		return errors.New("ui: animation shell is unavailable")
	}
	if s.animation != nil && s.animation.HasFrames() {
		return nil
	}
	cancelIdleMemoryRelease()
	decoded, err := loadDeviceAnimation()
	if err != nil {
		return err
	}
	s.animation = decoded
	return nil
}

func (s *Shell) setAnimationResourceEnabled(enabled bool) error {
	if s == nil {
		return nil
	}
	if !enabled {
		s.animationEpoch.Add(1)
		s.animationPlayer.Stop()
		s.animationActive = false
		s.currentAnimationFrame = -1
		s.disposeAnimationBitmaps()
		releasedBytes := residentAnimationBytes(s.animation)
		if s.animation != nil {
			s.animation.ReleaseFrames()
		}
		s.animation = nil
		scheduleIdleMemoryRelease(releasedBytes)
		return nil
	}
	if s.animation != nil &&
		(s.animation.HasFrames() || s.animationCacheComplete()) {
		return nil
	}
	return s.ensureAnimationFrameSources()
}

func (s *Shell) ensureVectorArtwork(size int) error {
	if size < 1 {
		return errors.New("ui: invalid vector device size")
	}
	if s.vectorDeviceSize == size && s.vectorOnBitmap != nil && s.vectorOffBitmap != nil {
		return nil
	}

	onImage, err := vector.RenderSVG(s.deviceOnSVG, size, size)
	if err != nil {
		return fmt.Errorf("render on-state SVG: %w", err)
	}
	offImage, err := vector.RenderSVG(s.deviceOffSVG, size, size)
	if err != nil {
		return fmt.Errorf("render off-state SVG: %w", err)
	}
	onBitmap, err := walk.NewBitmapFromImageForDPI(onImage, s.surface.DPI())
	if err != nil {
		return fmt.Errorf("create on-state vector bitmap: %w", err)
	}
	offBitmap, err := walk.NewBitmapFromImageForDPI(offImage, s.surface.DPI())
	if err != nil {
		onBitmap.Dispose()
		return fmt.Errorf("create off-state vector bitmap: %w", err)
	}
	if s.vectorOnBitmap != nil {
		s.vectorOnBitmap.Dispose()
	}
	if s.vectorOffBitmap != nil {
		s.vectorOffBitmap.Dispose()
	}
	s.vectorOnBitmap = onBitmap
	s.vectorOffBitmap = offBitmap
	s.vectorDeviceSize = size
	return nil
}

func (s *Shell) createTray() error {
	tray, err := walk.NewNotifyIcon(s.mw)
	if err != nil {
		return fmt.Errorf("create notification-area icon: %w", err)
	}
	s.tray = tray
	initialIcon := s.connectingIcon
	if s.controller != nil {
		initialIcon = s.controllerTrayIcons["offline"]
		if initialIcon == nil {
			initialIcon = s.appIcon
		}
	}
	if err := tray.SetIcon(initialIcon); err != nil {
		return err
	}
	initialTooltip := fmt.Sprintf(
		s.tr(i18n.MsgTrayTooltip),
		s.tr(i18n.MsgStateConnecting),
		s.tr(i18n.MsgConnectionReconnecting),
		s.tr(i18n.MsgTrayLeftClickHint),
	)
	if s.controller != nil {
		initialTooltip = s.controller.Title + " — Offline"
	}
	if err := tray.SetToolTip(initialTooltip); err != nil {
		return err
	}
	tray.MouseDown().Attach(func(_, _ int, button walk.MouseButton) {
		if trayToggleOnPress(s.controller != nil, button) {
			s.invoke("device.toggle", nil)
		}
	})
	tray.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if trayMenuOnRelease(s.controller != nil, button) {
			s.showAppMenuAt(int32(x), int32(y))
		}
	})
	tray.MessageClicked().Attach(s.Show)
	if err := tray.SetVisible(true); err != nil {
		return err
	}
	return nil
}

func (s *Shell) createNotifications() error {
	if s.controller != nil {
		return nil
	} // PCController owns notification identity/actions.
	cacheDir := filepath.Join(filepath.Dir(s.configPath), "cache")
	allowToastRegistration, registrationErr :=
		shellintegration.IsCurrentExecutableRegistered()
	if registrationErr != nil {
		s.logf(
			"inspect Windows shell registration for toast delivery: %v",
			registrationErr,
		)
	}
	manager, err := notify.New(
		cacheDir,
		notify.Mode(s.cfg.Notifications.Delivery),
		func(title, message string) error {
			if s.tray == nil || s.mw == nil {
				return errors.New("tray notification unavailable")
			}
			s.mw.Synchronize(func() {
				_ = s.tray.ShowCustom(title, message, s.appIcon)
			})
			return nil
		},
		func(action string) {
			switch strings.ToLower(strings.TrimSpace(action)) {
			case "on":
				s.invoke("device.on", nil)
			case "off":
				s.invoke("device.off", nil)
			default:
				s.Show()
			}
		},
		notify.Options{
			AllowPersistentRegistration: allowToastRegistration,
			Locale:                      i18n.Locale(s.cfg.UI.Locale),
		},
	)
	if err != nil {
		return err
	}
	s.notifier = manager
	return nil
}

func (s *Shell) installEvents() {
	events, unsubscribe := s.core.Subscribe(32)
	s.unsubscribe = unsubscribe
	go func() {
		defer close(s.eventDone)
		for {
			select {
			case <-s.ctx.Done():
				return
			case event := <-events:
				if s.automation != nil {
					s.automation.Handle(event)
				}
				if s.mw != nil {
					s.mw.Synchronize(func() { s.applyEvent(event) })
				}
			}
		}
	}()

	if err := winfx.SetWindowSubclass(s.HWND(), subclassID, s.windowMessage); err != nil {
		s.logf("window message integration: %v", err)
	}
	if err := s.initializeTaskbarIntegration(); err != nil {
		s.logf("taskbar integration: %v", err)
	}
	systemEvents, systemEventErr := winfx.RegisterSystemEvents(s.HWND())
	s.systemEvents = systemEvents
	if systemEventErr != nil {
		s.logf("Windows system-event integration: %v", systemEventErr)
	}
	// Recalculate the frame now that WM_NCCALCSIZE is installed. This keeps
	// WS_THICKFRAME's native sizing behavior without reserving a second,
	// visible non-client border inside the per-pixel window.
	win.SetWindowPos(
		s.mw.Handle(),
		0,
		0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|
			win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED,
	)
	if err := winfx.ReassertWindowChrome(s.HWND(), s.nativeTitleBar); err != nil {
		s.logf("restore main window frame policy: %v", err)
	}
	if s.controller == nil {
		if err := wmipc.RegisterPrimary(s.HWND()); err != nil {
			s.logf("WM IPC registration: %v", err)
		}
	}
}

func (s *Shell) applyEvent(event model.Event) {
	previous := s.snapshot.State
	previousPending := s.snapshot.PendingCommand
	s.snapshot = event.Snapshot
	if previousPending != s.snapshot.PendingCommand {
		s.startVisualAnimation()
	}
	switch event.Kind {
	case model.EventSnapshot:
		s.updateAnimation(previous, event.Snapshot.State, event.InitialSync)
		s.updateTray()
		cfg := s.Config()
		changed := event.PreviousState != event.Snapshot.State &&
			event.Snapshot.State != model.StateUnknown
		if changed && (!event.InitialSync || cfg.Notifications.InitialSync) {
			if cfg.Notifications.Sound {
				cue := sfx.CueOff
				if event.Snapshot.State == model.StateOn {
					cue = sfx.CueOn
				}
				if err := s.QueueAudio(string(cue), 1); err != nil {
					s.logf("queue %s cue: %v", cue, err)
				}
			}
			if cfg.Notifications.Enabled && cfg.Notifications.StateChanges {
				go func() {
					message := s.tr(i18n.MsgNotificationDeviceTurnedOff)
					if event.Snapshot.State == model.StateOn {
						message = s.tr(i18n.MsgNotificationDeviceTurnedOn)
					}
					_ = s.ShowNotification(
						s.tr(i18n.MsgAppName),
						message,
						false,
					)
				}()
			}
		}
	case model.EventMessage:
		cfg := s.Config()
		if cfg.Notifications.Sound {
			if err := s.QueueAudio(string(sfx.CueMessage), 1); err != nil {
				s.logf("queue message cue: %v", err)
			}
		}
		if cfg.Notifications.Enabled && cfg.Notifications.CustomMessage {
			title := event.Title
			if title == "" {
				title = s.tr(i18n.MsgAppName)
			}
			go func() { _ = s.ShowNotification(title, event.Message, false) }()
		}
	case model.EventBeep:
		if err := s.QueueAudio(string(sfx.CueMessage), event.BeepCount); err != nil {
			s.logf("queue beep cues: %v", err)
		}
	}
	s.updateTaskbarIntegration()
	if s.surface != nil {
		s.surface.Invalidate()
	}
}

func (s *Shell) updateAnimation(previous, next model.OutputState, initial bool) {
	activeAnimation := s.animation
	frameCount := activeAnimation.FrameCount()
	if next == model.StateUnknown || frameCount == 0 {
		s.animationEpoch.Add(1)
		s.animationPlayer.Stop()
		s.animationActive = false
		s.currentAnimationFrame = -1
		return
	}
	// Connection-health, pending-command, HTTP, and WebSocket echo snapshots
	// can repeat the same device state. They must not cancel a real transition
	// that is already playing toward that state.
	if previous == next {
		return
	}
	epoch := s.animationEpoch.Add(1)
	target := 0
	if next == model.StateOn {
		target = frameCount - 1
	}
	if initial || !s.motionEnabled || previous == model.StateUnknown {
		s.animationPlayer.Stop()
		s.animationActive = false
		s.currentAnimationFrame = -1
		return
	}
	forward := next == model.StateOn
	s.animationActive = true
	s.currentAnimationFrame = 0
	if !forward {
		s.currentAnimationFrame = frameCount - 1
	}
	s.animationPlayer.PlayTransition(s.ctx, activeAnimation, forward, func(index int, _ image.Image) {
		if index < 0 || index >= frameCount || s.mw == nil ||
			s.closed.Load() || s.animationEpoch.Load() != epoch {
			return
		}
		s.mw.Synchronize(func() {
			if s.closed.Load() || s.surface == nil || s.animationEpoch.Load() != epoch {
				return
			}
			s.currentAnimationFrame = index
			if index == target {
				// The final steady state is rendered from SVG at the exact
				// device-pixel size; APNG frames exist only while transitioning.
				s.animationActive = false
				s.prepareAnimationCacheAsync(
					s.animation,
					s.animationBitmapSize,
				)
			}
			s.surface.Invalidate()
		})
	})
}

func (s *Shell) applyConfigUI(cfg config.Config, reportErrors bool) {
	if s.windowFade.Active() {
		if s.windowFade.closing {
			// The process is already committed to closing. Persisted config can
			// still update outside the UI, but changing HWND styles mid-fade
			// would invalidate the selected opacity backend.
			return
		}
		s.completeEntranceFade()
	}
	// Live settings can change the layout, chrome, or visibility while an
	// input gesture is armed. A native control cancels rather than activating
	// a stale hit target after that mutation.
	s.cancelButtonActivation()
	s.resetDragSnap()
	s.applyLocaleUI(cfg.UI.Locale)
	if cfg.UI.PrewarmTrayMenu {
		s.prewarmAppMenuArtwork()
	} else {
		// This runs on the owner UI thread. Retire a previously cached popup
		// immediately when the compatibility setting is disabled.
		traymenu.DiscardCached(s.HWND())
	}
	previousBackdrop := s.backdrop
	resetMaterial := s.materialRequested == "" ||
		s.materialRequested != cfg.UI.Backdrop
	chromeChanged := false
	nativeTitleBar := effectiveTitleBarMode(cfg.UI) == config.TitleBarNative
	if nativeTitleBar != s.nativeTitleBar ||
		!strings.EqualFold(cfg.UI.BorderMode, s.borderMode) {
		resetMaterial = true
		if err := winfx.ConfigureWindowChrome(
			s.HWND(),
			nativeTitleBar,
			cfg.UI.BorderMode,
		); err != nil {
			s.logf("window chrome: %v", err)
			if reportErrors {
				s.showError(s.tr(i18n.MsgErrorWindowChrome), err)
			}
		} else {
			s.nativeTitleBar = nativeTitleBar
			s.borderMode = cfg.UI.BorderMode
			chromeChanged = true
		}
	}
	if err := s.applyViewMode(cfg.UI); err != nil {
		s.logf("view mode: %v", err)
		if reportErrors {
			s.showError(s.tr(i18n.MsgErrorResizeWindow), err)
		}
	}
	appearance, appearanceErr := winfx.ResolveAppearance(cfg.UI.Theme)
	if appearanceErr != nil {
		s.logf("resolve Windows appearance: %v", appearanceErr)
	}
	s.dark = appearance.Dark
	s.highContrast = appearance.HighContrast
	if err := s.setAnimationResourceEnabled(cfg.UI.Animations); err != nil {
		s.logf("load device transition animation: %v", err)
		if reportErrors {
			s.showError(s.tr(i18n.MsgErrorUpdateSettings), err)
		}
	}
	s.setMotionPolicy(cfg.UI.Animations && appearance.MotionEnabled)
	// Taskbar visibility mutates GWL_EXSTYLE. Do it before WS_EX_LAYERED and
	// DWM material setup so the final style transition cannot invalidate the
	// just-created composition visual.
	if !s.taskbarInitialized || s.taskbarVisible != cfg.UI.ShowTaskbar {
		if !cfg.UI.ShowTaskbar && s.taskbarIntegration != nil {
			_ = s.taskbarIntegration.SetProgress(
				winfx.TaskbarProgressNone,
				0,
				0,
			)
			s.taskbarIntegration.NotifyButtonRemoved()
		}
		s.setTaskbarVisibility(cfg.UI.ShowTaskbar)
		s.taskbarInitialized = true
		s.taskbarVisible = cfg.UI.ShowTaskbar
		resetMaterial = true
		chromeChanged = true
		s.updateTaskbarIntegration()
	}
	directXEnabled := s.configureRenderingBackend(cfg.UI)
	perPixelOverlay := perPixelForegroundForUI(cfg.UI) && !directXEnabled
	if perPixelOverlay != s.perPixelOverlay {
		resetMaterial = true
		if err := winfx.SetPerPixelOverlay(
			s.HWND(),
			perPixelOverlay,
		); err != nil {
			s.logf("layered window: %v", err)
			if directXEnabled {
				s.noteDirectXStyleFailure(err)
			}
		} else {
			s.perPixelOverlay = perPixelOverlay
			chromeChanged = true
		}
	}
	applyMaterial := winfx.Refresh
	if resetMaterial {
		applyMaterial = winfx.Apply
	}
	actual, backdropErr := applyMaterial(
		s.HWND(),
		cfg.UI.Backdrop,
		s.dark,
		nativeTitleBar,
	)
	s.backdrop = actual
	s.materialRequested = cfg.UI.Backdrop
	if s.surface != nil {
		s.surface.SetPaintMode(surfacePaintMode(cfg.UI, actual))
	}
	if backdropErr != nil {
		s.logf("backdrop: %v", backdropErr)
	}
	if err := s.applyConfiguredWindowCorners(cfg.UI); err != nil {
		s.logf("window corners: %v", err)
	}
	if immediateBackdropCommitRequired(previousBackdrop, actual) {
		if err := s.commitBackdropTransition(nativeTitleBar); err != nil {
			s.logf("commit backdrop transition: %v", err)
		}
	}
	if chromeChanged {
		// Frame/style changes can make USER32 re-query or temporarily omit the
		// caption's small icon. Reassert the stable product icon after the last
		// transition while leaving ICON_BIG available for live taskbar state.
		s.restoreCaptionIcon(true)
	}
	s.setAlwaysOnTop(cfg.UI.AlwaysOnTop)
	s.unregisterHotkeys()
	if !s.hotkeyCaptureActive {
		if hotkeyErr := s.registerHotkeys(cfg.Hotkeys); hotkeyErr != nil {
			s.logf("global hotkeys: %v", hotkeyErr)
			if reportErrors {
				s.showError(s.tr(i18n.MsgErrorHotkeyUnavailable), hotkeyErr)
			}
		}
	}
	if s.notifier != nil {
		mode := notify.Mode(cfg.Notifications.Delivery)
		if !cfg.Notifications.Enabled {
			mode = notify.ModeOff
		}
		s.notifier.SetMode(mode)
		s.notifier.SetLocale(i18n.Locale(cfg.UI.Locale))
	}
	s.surface.Invalidate()
	s.updateTray()
	s.updateTaskbarIntegration()
}

func (s *Shell) applySavedWindowsIntegration(cfg config.Config) {
	if s == nil || s.startWithWindowsPersistence == nil {
		return
	}
	if err := s.startWithWindowsPersistence.ApplySaved(
		cfg.Integration.StartWithWindows,
	); err != nil {
		s.logf("start with Windows: %v", err)
	}
}

func (s *Shell) applyViewMode(ui config.UIConfig) error {
	if s == nil || s.mw == nil {
		return errors.New("main window is unavailable")
	}
	minimum, maximum := viewMinMaxPixels(ui.ViewMode)
	if s.controller != nil {
		minimum.Width = 560
		minimum.Height = 550
		if ui.ViewMode == config.ViewCompact {
			minimum = walk.Size{Width: 340, Height: 330}
		}
		if s.controllerWidgetID != "" {
			minimum = s.controllerWidgetMinimum()
		}
	}
	current := s.mw.BoundsPixels()
	next := liveViewBounds(current, ui)
	if err := s.mw.SetMinMaxSizePixels(minimum, maximum); err != nil {
		return err
	}
	if current != next {
		if err := s.mw.SetBoundsPixels(next); err != nil {
			return err
		}
	}
	if err := s.applyConfiguredWindowCorners(ui); err != nil {
		return err
	}
	return nil
}

// liveViewBounds applies a previewed view size without replaying persisted
// coordinates. WindowBounds.X/Y are startup placement, not live appearance
// state: restoring them on every settings preview used to teleport the window
// to its pre-dialog (and sometimes pre-snap) location.
func liveViewBounds(current walk.Rectangle, ui config.UIConfig) walk.Rectangle {
	size := viewSizePixels(ui)
	return walk.Rectangle{
		X: current.X, Y: current.Y,
		Width: size.Width, Height: size.Height,
	}
}

func (s *Shell) registerHotkeys(cfg config.HotkeysConfig) error {
	if s.controllerOwner != nil {
		return nil
	}
	var failures []error
	for _, item := range []struct {
		id    int32
		chord string
		name  string
	}{
		{hotkeyToggle, cfg.Toggle, "toggle"},
		{hotkeyOn, cfg.On, "on"},
		{hotkeyOff, cfg.Off, "off"},
		{hotkeyShow, cfg.Show, "show"},
	} {
		if strings.TrimSpace(item.chord) == "" {
			continue
		}
		if _, err := winfx.RegisterHotkeyChord(s.HWND(), item.id, item.chord); err != nil {
			failures = append(failures, fmt.Errorf("%s (%s): %w", item.name, item.chord, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Shell) unregisterHotkeys() {
	for _, id := range []int32{hotkeyToggle, hotkeyOn, hotkeyOff, hotkeyShow} {
		_ = winfx.UnregisterHotKey(s.HWND(), id)
	}
}

// setHotkeyCapture suspends RegisterHotKey without altering or broadcasting
// configuration. That keeps browser tabs synchronized to the real bindings
// while the native recorder owns the keyboard.
func (s *Shell) setHotkeyCapture(active bool) {
	if s == nil || s.mw == nil || s.nativeHotkeyCapture == active {
		return
	}
	s.nativeHotkeyCapture = active
	_ = s.updateHotkeyCaptureState(true)
}

// SetBrowserHotkeyCapture is the UI-thread bridge used by the authenticated
// browser recorder lease. Native and browser capture are tracked separately so
// ending either one cannot accidentally restore shortcuts still owned by the
// other recorder.
func (s *Shell) SetBrowserHotkeyCapture(
	ctx context.Context,
	active bool,
) error {
	if s == nil || s.mw == nil || s.closed.Load() {
		return errors.New("main window is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := make(chan error, 1)
	var gate atomic.Int32
	s.mw.Synchronize(func() {
		if !gate.CompareAndSwap(0, 1) {
			return
		}
		if s.closed.Load() || s.mw == nil || s.mw.IsDisposed() {
			result <- errors.New("main window is unavailable")
			return
		}
		if active && s.nativeHotkeyCapture {
			result <- errors.New("the native hotkey recorder is already active")
			return
		}
		s.browserHotkeyCapture = active
		err := s.updateHotkeyCaptureState(false)
		if !active && err != nil {
			// The capture transition itself completed. A best-effort
			// RegisterHotKey restore failure is a runtime binding issue, not
			// an unapplied lease release; reporting nil lets the coordinator
			// retire the lease instead of keeping a false logical owner.
			result <- nil
			return
		}
		result <- err
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		if gate.CompareAndSwap(0, 2) {
			return ctx.Err()
		}
		// The UI callback already owns the operation. Waiting for its tiny
		// critical section avoids reporting a failed lease while leaving the
		// browser capture source applied behind the coordinator's back.
		return <-result
	}
}

// ProbeHotkey asks Windows whether a completed browser-recorded chord can be
// registered. The browser capture lease keeps this app's existing bindings
// released while the temporary registration is tested.
func (s *Shell) ProbeHotkey(ctx context.Context, chord string) error {
	if s == nil || s.mw == nil || s.closed.Load() {
		return errors.New("main window is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := make(chan error, 1)
	var gate atomic.Int32
	s.mw.Synchronize(func() {
		if !gate.CompareAndSwap(0, 1) {
			return
		}
		if s.closed.Load() || s.mw == nil || s.mw.IsDisposed() {
			result <- errors.New("main window is unavailable")
			return
		}
		if !s.browserHotkeyCapture {
			result <- errors.New("browser hotkey capture is not active")
			return
		}
		const browserProbeID int32 = 0x5201
		if _, err := winfx.RegisterHotkeyChord(
			s.HWND(),
			browserProbeID,
			chord,
		); err != nil {
			result <- err
			return
		}
		result <- winfx.UnregisterHotKey(s.HWND(), browserProbeID)
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		if gate.CompareAndSwap(0, 2) {
			return ctx.Err()
		}
		return <-result
	}
}

func (s *Shell) updateHotkeyCaptureState(reportErrors bool) error {
	if s == nil || s.mw == nil {
		return errors.New("main window is unavailable")
	}
	active := s.nativeHotkeyCapture || s.browserHotkeyCapture
	if s.hotkeyCaptureActive == active {
		return nil
	}
	s.hotkeyCaptureActive = active
	s.unregisterHotkeys()
	if active {
		return nil
	}
	if err := s.registerHotkeys(s.Config().Hotkeys); err != nil {
		s.logf("restore global hotkeys after capture: %v", err)
		if reportErrors {
			s.showError(s.tr(i18n.MsgErrorHotkeyUnavailable), err)
		}
		return err
	}
	return nil
}

func (s *Shell) windowMessage(hwnd, message, wParam, lParam uintptr) (uintptr, bool) {
	if s.taskbarIntegration != nil &&
		uint32(message) == s.taskbarIntegration.CreatedMessage() {
		s.handleTaskbarButtonCreated()
		return 0, true
	}
	if s.controller == nil && uint32(message) == wmipc.MessageID() {
		request, err := wmipc.Decode(lParam)
		if err != nil {
			s.logf("rejected WM IPC: %v", err)
			return 0, true
		}
		go func() {
			ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
			defer cancel()
			if _, commandErr := s.dispatcher.Handler(ctx, request); commandErr != nil {
				s.logf("WM IPC %s: %v", request.Method, commandErr)
			}
		}()
		return 1, true
	}
	if shellAppearanceMessage(uint32(message)) {
		// Our subclass runs before Walk and DefWindowProc. Defer the final
		// palette/DWM write until native caption processing has completed.
		s.scheduleAppearanceReapply()
		return 0, false
	}
	switch uint32(message) {
	case win.WM_COMMAND:
		if s.controller != nil {
			if action, ok := controllerTaskbarAction(wParam); ok {
				s.activateControllerAction(action)
				return 0, true
			}
			return 0, false
		}
		if method, ok := taskbarCommandMethod(wParam); ok {
			s.invoke(method, nil)
			return 0, true
		}
		return 0, false
	case wmQueryEndSession:
		s.observeNativeWindowPlacement()
		s.persistWindowPlacement()
		return 1, true
	case wmEndSession:
		if wParam != 0 {
			s.observeNativeWindowPlacement()
			s.persistWindowPlacement()
		}
		return 0, false
	case wmContextMenu:
		x := win.GET_X_LPARAM(lParam)
		y := win.GET_Y_LPARAM(lParam)
		if x == -1 && y == -1 {
			var bounds win.RECT
			if win.GetWindowRect(win.HWND(hwnd), &bounds) {
				x = bounds.Left + (bounds.Right-bounds.Left)/2
				y = bounds.Top + (bounds.Bottom-bounds.Top)/2
			} else {
				var cursor win.POINT
				if win.GetCursorPos(&cursor) {
					x, y = cursor.X, cursor.Y
				} else {
					x, y = 0, 0
				}
			}
		}
		s.showAppMenuAt(x, y)
		return 0, true
	case wmTimer:
		if wParam == windowFadeTimerID {
			s.advanceWindowFade()
			return 0, true
		}
		return 0, false
	case wmReapplyWindowIcons:
		// Walk has completed its WM_DPICHANGED processing and restored the
		// canonical product icon. Reapply only the live ICON_BIG role.
		s.updateWindowStateIcon()
		if err := s.rebuildTaskbarButtonIcons(); err != nil {
			s.logf("refresh taskbar thumbnail icons: %v", err)
		}
		s.updateTaskbarIntegration()
		return 0, true
	case wmCaptureWindowPlacement:
		s.observeNativeWindowPlacement()
		s.persistWindowPlacement()
		return 0, true
	case wmReapplyAppearance:
		// The originating system message has now passed through Walk and
		// DefWindowProc. Reassert DWM and custom-client appearance last so a
		// native caption cannot overwrite the requested app theme.
		s.appearanceReapplyPosted = false
		s.reapplyComposition()
		return 0, true
	case wmReassertWindowChrome:
		// WM_ACTIVATE/WM_NCACTIVATE/WM_SHOWWINDOW reach this subclass before
		// DefWindowProc. This posted pass runs afterward, when DWM has finished
		// rebuilding its active/inactive non-client visual.
		s.windowChromeReassertPosted = false
		native := effectiveTitleBarMode(s.Config().UI) == config.TitleBarNative
		if err := winfx.ReassertWindowChrome(s.HWND(), native); err != nil {
			s.logf("reassert main window frame policy: %v", err)
		}
		return 0, true
	case win.WM_DPICHANGED:
		// Our subclass runs before Walk. Post instead of applying immediately,
		// otherwise FormBase.WndProc would overwrite ICON_BIG afterwards.
		win.PostMessage(win.HWND(hwnd), wmReapplyWindowIcons, 0, 0)
		return 0, false
	case wmNCCalcSize:
		if effectiveTitleBarMode(s.Config().UI) != config.TitleBarNative {
			s.constrainMaximizedClient(win.HWND(hwnd), lParam)
			return 0, true
		}
		return 0, false
	case win.WM_NCPAINT, wmNCUAHDrawCaption, wmNCUAHDrawFrame:
		if effectiveTitleBarMode(s.Config().UI) != config.TitleBarNative {
			// WS_THICKFRAME is retained for native resizing. USER32 can
			// synthesize WS_EX_WINDOWEDGE from it even after SetWindowLong
			// clears that bit. A captionless HWND has no non-client pixels:
			// allowing the default GDI frame paint draws a thick white ring
			// over the DirectComposition foreground.
			return 0, true
		}
		return 0, false
	case wmEraseBackground:
		// The top-level pixels come exclusively from UpdateLayeredWindow.
		// Painting BLACKNESS here can cover descendants during a move/layout
		// pass and was the source of the transient black rectangle.
		return 1, true
	case wmNCHitTest:
		ui := s.Config().UI
		if effectiveTitleBarMode(ui) == config.TitleBarNative {
			return 0, false
		}
		if windowResizeBorderEnabled(ui) && !win.IsZoomed(win.HWND(hwnd)) {
			if result, hit := resizeHitTest(win.HWND(hwnd), lParam); hit {
				return uintptr(result), true
			}
		}
		if s.transparentPixelPassesThrough(lParam) {
			return ^uintptr(0), true // HTTRANSPARENT
		}
		// Do not let DefWindowProc discover invisible standard caption buttons
		// behind our custom chrome. Only the painted hit regions are non-client.
		return uintptr(s.customCaptionHit(lParam)), true
	case win.WM_NCMOUSEMOVE:
		if s.customCaptionHit(lParam) != win.HTCLIENT {
			if point, ok := s.captionClientPoint(lParam); ok {
				s.onMouseMove(point.X, point.Y, 0)
				event := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})),
					DwFlags: win.TME_LEAVE | win.TME_NONCLIENT, HwndTrack: win.HWND(hwnd)}
				win.TrackMouseEvent(&event)
			}
		}
		return 0, false // USER32 handles the Windows 11 maximize-hover Snap menu
	case win.WM_NCMOUSELEAVE:
		s.onMouseLeave()
		return 0, false
	case win.WM_NCLBUTTONDOWN, win.WM_NCLBUTTONDBLCLK:
		if captionButtonHit(int(wParam)) && s.customCaptionHit(lParam) == int(wParam) {
			if point, ok := s.captionClientPoint(lParam); ok {
				// Reuse the ordinary release/cancel state machine. Capturing the
				// surface makes release-outside and capture loss behave identically.
				win.SetCapture(s.surface.Handle())
				s.onMouseDown(point.X, point.Y, walk.LeftButton)
				return 0, true
			}
		}
		return 0, false // HTCAPTION uses the native move/restore/Snap loop
	case wmHotkey:
		if s.hotkeyCaptureActive {
			// A message already queued before UnregisterHotKey must not trigger
			// an action while the user is learning that same chord.
			return 0, true
		}
		if s.controller != nil {
			switch int32(wParam) {
			case hotkeyToggle:
				s.activateControllerAction(actionCompact)
			case hotkeyOn:
				s.activateControllerAction(actionChannels)
				s.Show()
			case hotkeyOff:
				s.activateControllerAction(actionConsole)
			case hotkeyShow:
				s.Show()
			}
			return 0, true
		}
		switch int32(wParam) {
		case hotkeyToggle:
			s.invoke("device.toggle", nil)
		case hotkeyOn:
			s.invoke("device.on", nil)
		case hotkeyOff:
			s.invoke("device.off", nil)
		case hotkeyShow:
			s.Show()
		}
		return 0, true
	case wmEnterSizeMove:
		s.resetDragSnap()
		return 0, false
	case wmCancelMode:
		s.cancelButtonActivation()
		s.resetDragSnap()
		return 0, false
	case wmSize:
		stateChanged := false
		switch wParam {
		case sizeMinimized:
			stateChanged = s.observeWindowPlacementState(
				config.WindowStateMinimized,
				false,
			)
		case sizeMaximized:
			stateChanged = s.observeWindowPlacementState(
				config.WindowStateMaximized,
				false,
			)
		case sizeRestored:
			stateChanged = s.observeWindowPlacementState(
				config.WindowStateNormal,
				false,
			)
		}
		if wParam == sizeRestored && stateChanged {
			// During maximize/restore transitions Windows can clear IsZoomed
			// before GetWindowRect stops reporting the maximized rectangle.
			// Capture on a posted message after the native move/size sequence.
			win.PostMessage(
				win.HWND(hwnd),
				wmCaptureWindowPlacement,
				0,
				0,
			)
		} else if stateChanged {
			s.persistWindowPlacement()
		}
		if wParam == sizeMinimized {
			s.cancelButtonActivation()
		} else {
			cfg := s.Config()
			width := int(uint16(lParam & 0xffff))
			height := int(uint16((lParam >> 16) & 0xffff))
			cornerErr := s.applyConfiguredWindowCornersForSize(
				cfg.UI,
				width,
				height,
			)
			if cornerErr != nil {
				s.logf(
					"resize window corners %dx%d: %v",
					width,
					height,
					cornerErr,
				)
			}
			if s.surface != nil {
				s.surface.Invalidate()
			}
		}
		return 0, false
	case wmMoving:
		cfg := s.Config()
		if !cfg.UI.MagneticSnap || lParam == 0 {
			s.resetDragSnap()
			return 0, false
		}
		var target winfx.Rect
		uiCopyMemory.Call(
			uintptr(unsafe.Pointer(&target)),
			lParam,
			unsafe.Sizeof(target),
		)
		var cursor win.POINT
		if !win.GetCursorPos(&cursor) {
			s.resetDragSnap()
			return 0, false
		}
		dpi := uint32(win.GetDpiForWindow(win.HWND(hwnd)))
		controlDown := controlModifierDown()
		workArea := winfx.Rect{}
		if !controlDown {
			metrics, metricsErr := monitorMetricsForMovingRect(
				target,
				dpi,
				winfx.MonitorMetricsForRect,
			)
			if metricsErr != nil {
				s.resetDragSnap()
				return 0, false
			}
			workArea = metrics.WorkArea
			dpi = metrics.DPI
		}
		target, _, handled := s.dragSnap.Apply(
			target,
			snapCursorPoint{X: cursor.X, Y: cursor.Y},
			workArea,
			dpi,
			cfg.UI.MagneticSnapDistance,
			controlDown,
			s.controllerSnapNeighbours()...,
		)
		if handled {
			uiCopyMemory.Call(
				lParam,
				uintptr(unsafe.Pointer(&target)),
				unsafe.Sizeof(target),
			)
		}
		return boolResult(handled), handled
	case wmExitSizeMove:
		s.resetDragSnap()
		s.saveWindowBounds()
		return 0, false
	case wmActivate:
		s.scheduleWindowChromeReassert()
		if uint16(wParam&0xffff) == win.WA_INACTIVE {
			s.cancelButtonActivation()
		}
		return 0, false
	case wmNCActivate:
		s.scheduleWindowChromeReassert()
		if effectiveTitleBarMode(s.Config().UI) != config.TitleBarNative {
			if win.IsIconic(win.HWND(hwnd)) {
				// Windows owns minimized/icon activation bookkeeping. Avoid its
				// frame repaint while retaining normal minimize/restore behavior.
				return win.DefWindowProc(win.HWND(hwnd), uint32(message), wParam, ^uintptr(0)), true
			}
			// WS_CAPTION is retained for OS transitions/Snap, not for drawing.
			// DefWindowProc can paint a classic/themed caption synchronously on
			// activation, before a posted cleanup or client repaint can run.
			// TRUE allows the focus transition; our compositor owns all pixels.
			if s.surface != nil {
				s.surface.Invalidate()
			}
			return 1, true
		}
		return 0, false
	case win.WM_KILLFOCUS:
		s.cancelKeyboardActivation()
		return 0, false
	case wmShowWindow:
		if wParam != 0 {
			s.resetDragSnap()
			s.scheduleWindowChromeReassert()
		} else {
			s.cancelButtonActivation()
		}
		return 0, false
	case winfx.WMWTSSessionChange:
		switch uint32(wParam) {
		case winfx.WTSSessionLock:
			s.handleSystemAutomation(automation.SystemEventLock)
		case winfx.WTSSessionUnlock:
			s.handleSystemAutomation(automation.SystemEventUnlock)
			if s.systemRecovery != nil {
				s.systemRecovery.Unlock()
			}
		}
		return 0, true
	case winfx.WMSystemNetworkChange:
		if s.systemRecovery != nil {
			s.systemRecovery.NetworkChanged()
		} else if s.controller != nil && s.controllerOwner == nil {
			s.handleSystemAutomation(automation.SystemEventNetworkChange)
		}
		return 0, true
	case wmPowerBroadcast:
		switch uint32(wParam) {
		case winfx.PBTAPMSuspend:
			s.taskbarSuspended = true
			s.updateTaskbarIntegration()
			s.handleSystemAutomation(automation.SystemEventSuspend)
			if s.systemRecovery != nil {
				s.systemRecovery.Suspend()
			}
		case winfx.PBTAPMResumeAutomatic:
			s.taskbarSuspended = false
			s.updateTaskbarIntegration()
			s.handleSystemAutomation(automation.SystemEventResume)
			if s.systemRecovery != nil {
				s.systemRecovery.Resume()
			}
		case winfx.PBTAPMResumeSuspend:
			// PBT_APMRESUMEAUTOMATIC is always the recovery trigger. A user
			// resume normally emits this second event immediately afterward,
			// and must not start another reconnect or automation script.
		default:
			return 0, false
		}
		return 1, true
	}
	return 0, false
}

func (s *Shell) handleSystemAutomation(event automation.SystemEvent) {
	if s == nil || s.automation == nil || s.core == nil {
		return
	}
	if s.controller != nil {
		s.automation.HandleSystemEvent(event, controllerAutomationSnapshot(s.controllerState))
	} else {
		s.automation.HandleSystemEvent(event, s.core.Snapshot())
	}
}

func shellAppearanceMessage(message uint32) bool {
	switch message {
	case wmSettingChange, wmSystemColorChange, wmThemeChanged,
		wmDWMCompositionChanged, wmDWMColorizationColorChanged,
		wmDisplayChange:
		return true
	default:
		return false
	}
}

func (s *Shell) scheduleWindowChromeReassert() {
	if s == nil || s.mw == nil || s.mw.IsDisposed() || s.closed.Load() ||
		s.windowChromeReassertPosted {
		return
	}
	// These notifications arrive before DefWindowProc. Coalesce them into a
	// single UI-thread pass after native frame processing; synchronously
	// changing the frame here would be overwritten and could recurse.
	s.windowChromeReassertPosted = true
	if win.PostMessage(s.mw.Handle(), wmReassertWindowChrome, 0, 0) == 0 {
		s.windowChromeReassertPosted = false
	}
}

func (s *Shell) scheduleAppearanceReapply() {
	if s == nil || s.mw == nil || s.materialApplying ||
		s.appearanceReapplyPosted {
		return
	}
	s.appearanceReapplyPosted = true
	if win.PostMessage(
		s.mw.Handle(),
		wmReapplyAppearance,
		0,
		0,
	) == 0 {
		s.appearanceReapplyPosted = false
		// Teardown can reject posting. A synchronous refresh is still safer
		// than leaving the old palette/DWM attributes installed.
		s.reapplyComposition()
	}
}

func (s *Shell) resetDragSnap() {
	s.dragSnap.Reset()
}

func controlModifierDown() bool {
	raw, _, _ := uiGetAsyncKeyState.Call(uintptr(win.VK_CONTROL))
	return asyncKeyStatePressed(raw)
}

func asyncKeyStatePressed(raw uintptr) bool {
	// GetAsyncKeyState's high bit is the current physical state. Its low bit
	// only means "pressed since the previous call" and is intentionally
	// ignored because it is shared and unreliable under preemptive Windows.
	return uint16(raw)&0x8000 != 0
}

func (s *Shell) reapplyComposition() {
	if s == nil || s.mw == nil || s.materialApplying {
		return
	}
	s.materialApplying = true
	defer func() { s.materialApplying = false }()

	cfg := s.Config()
	appearance, appearanceErr := winfx.ResolveAppearance(cfg.UI.Theme)
	if appearanceErr != nil {
		s.logf("refresh Windows appearance: %v", appearanceErr)
	}
	s.dark = appearance.Dark
	s.highContrast = appearance.HighContrast
	s.setMotionPolicy(cfg.UI.Animations && appearance.MotionEnabled)
	actual, err := winfx.Refresh(
		s.HWND(),
		cfg.UI.Backdrop,
		s.dark,
		effectiveTitleBarMode(cfg.UI) == config.TitleBarNative,
	)
	s.backdrop = actual
	if s.surface != nil {
		s.surface.SetPaintMode(surfacePaintMode(cfg.UI, actual))
	}
	if err != nil {
		s.logf("reapply backdrop: %v", err)
	}
	if err := s.applyConfiguredWindowCorners(cfg.UI); err != nil {
		s.logf("reapply window corners: %v", err)
	}
	if s.surface != nil {
		s.surface.Invalidate()
	}
}

func (s *Shell) setMotionPolicy(enabled bool) {
	if s == nil {
		return
	}
	s.motionEnabled = enabled
	if enabled {
		return
	}
	s.animationEpoch.Add(1)
	s.animationPlayer.Stop()
	s.animationActive = false
	s.currentAnimationFrame = -1
	s.settleVisualAppearance()
	if s.windowFade.Active() {
		if s.windowFade.closing {
			s.completeWindowExit()
		} else {
			s.completeEntranceFade()
		}
	}
}

func (s *Shell) onMouseMove(x, y int, _ walk.MouseButton) {
	if s.controller != nil && s.moveControllerChannel(x, y) {
		return
	}
	if !s.trackingMouse && s.surface != nil {
		event := win.TRACKMOUSEEVENT{
			CbSize:    uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})),
			DwFlags:   win.TME_LEAVE,
			HwndTrack: s.surface.Handle(),
		}
		s.trackingMouse = win.TrackMouseEvent(&event)
	}
	action := s.layout.hit(x, y)
	pressChanged := s.buttonActivation.pointerMove(action)
	pointerChanged := s.visualPointer.X != x || s.visualPointer.Y != y
	s.visualPointer = walk.Point{X: x, Y: y}
	if action == s.hover {
		if pressChanged || (pointerChanged && action != actionNone) {
			s.visualPointerDirty = true
			s.startVisualAnimation()
		}
		return
	}
	s.hover = action
	if s.controller != nil {
		_ = s.surface.SetToolTipText(s.controllerActionHint(action))
	}
	s.startVisualAnimation()
	if action == actionNone {
		s.surface.SetCursor(walk.CursorArrow())
	} else {
		s.surface.SetCursor(walk.CursorHand())
	}
	s.surface.Invalidate()
}

func (s *Shell) onMouseLeave() {
	if s == nil || s.surface == nil {
		return
	}
	s.trackingMouse = false
	if s.controller != nil {
		_ = s.surface.SetToolTipText("")
	}
	pressChanged := s.buttonActivation.pointerMove(actionNone)
	hoverChanged := s.hover != actionNone
	if hoverChanged {
		s.hover = actionNone
		s.surface.SetCursor(walk.CursorArrow())
	}
	if pressChanged || hoverChanged {
		s.startVisualAnimation()
		s.surface.Invalidate()
	}
}

func (s *Shell) startVisualAnimation() {
	if s == nil || s.surface == nil {
		return
	}
	if !s.motionEnabled {
		s.settleVisualAppearance()
		s.surface.Invalidate()
		return
	}
	if s.visualTick.IsZero() {
		s.visualTick = time.Now()
	}
	if !s.visualTimerRunning {
		s.visualTimerRunning =
			win.SetTimer(s.surface.Handle(), visualTimerID, 16, 0) != 0
		if !s.visualTimerRunning {
			s.surface.Invalidate()
		}
	}
}

func (s *Shell) settleVisualAppearance() {
	if s == nil {
		return
	}
	if s.controller != nil {
		s.controllerSegmentLevels = s.controllerSegmentTargets
	}
	if s.visualTimerRunning && s.surface != nil {
		win.KillTimer(s.surface.Handle(), visualTimerID)
		s.visualTimerRunning = false
	}
	for index := 1; index < len(s.hoverLevels); index++ {
		s.hoverLevels[index] = 0
		if actionID(index) == s.hover {
			s.hoverLevels[index] = 1
		}
		s.pressLevels[index] = 0
		if s.buttonActivation.pressTarget(actionID(index)) {
			s.pressLevels[index] = 1
		}
	}
	s.visualPhase = 0
	s.visualTick = time.Time{}
	s.visualPointerDirty = false
}

func (s *Shell) advanceVisualAnimation() bool {
	if s == nil || s.surface == nil {
		return false
	}
	if !s.motionEnabled {
		s.settleVisualAppearance()
		s.surface.Invalidate()
		return false
	}
	now := time.Now()
	elapsed := visualFrameElapsed(s.visualTick, now)
	s.visualTick = now

	step := elapsed.Seconds() / 0.14
	if step > 1 {
		step = 1
	}
	active := false
	if s.controller != nil {
		active = s.advanceControllerSegments(elapsed)
	}
	for index := 1; index < len(s.hoverLevels); index++ {
		target := 0.0
		if actionID(index) == s.hover {
			target = 1
		}
		value := s.hoverLevels[index]
		switch {
		case value < target:
			value += step
			if value > target {
				value = target
			}
		case value > target:
			value -= step
			if value < target {
				value = target
			}
		}
		s.hoverLevels[index] = value
		if value != target {
			active = true
		}

		var pressActive bool
		s.pressLevels[index], pressActive = advanceButtonPressLevel(
			s.pressLevels[index],
			s.buttonActivation.pressTarget(actionID(index)),
			elapsed,
		)
		if pressActive {
			active = true
		}
	}
	if s.snapshot.PendingCommand != "" {
		s.visualPhase += elapsed.Seconds() / 1.1
		s.visualPhase -= float64(int64(s.visualPhase))
		active = true
	}
	s.visualPointerDirty = false
	s.surface.Invalidate()
	if !active {
		s.visualTick = time.Time{}
	}
	return active
}

func (s *Shell) hoverProgress(action actionID) float64 {
	if action <= actionNone || int(action) >= len(s.hoverLevels) {
		return 0
	}
	value := s.hoverLevels[action]
	return value * value * (3 - 2*value)
}

func (s *Shell) pressProgress(action actionID) float64 {
	if action <= actionNone || int(action) >= len(s.pressLevels) {
		return 0
	}
	value := s.pressLevels[action]
	return value * value * (3 - 2*value)
}

func absInt32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func resizeHitTest(hwnd win.HWND, lParam uintptr) (int, bool) {
	var bounds win.RECT
	if hwnd == 0 || !win.GetWindowRect(hwnd, &bounds) {
		return 0, false
	}
	x := int32(int16(uint16(lParam & 0xffff)))
	y := int32(int16(uint16((lParam >> 16) & 0xffff)))
	// PerMonitorV2 window/message coordinates share the same physical space.
	// Honor the message point (including signed coordinates on left monitors),
	// not a newer cursor position that may belong to a different hit-test query.
	dpi := int32(win.GetDpiForWindow(hwnd))
	if dpi < 96 {
		dpi = 96
	}
	border := int32(8) * dpi / 96
	if border < 6 {
		border = 6
	}
	left := x >= bounds.Left && x < bounds.Left+border
	right := x < bounds.Right && x >= bounds.Right-border
	top := y >= bounds.Top && y < bounds.Top+border
	bottom := y < bounds.Bottom && y >= bounds.Bottom-border
	switch {
	case top && left:
		return win.HTTOPLEFT, true
	case top && right:
		return win.HTTOPRIGHT, true
	case bottom && left:
		return win.HTBOTTOMLEFT, true
	case bottom && right:
		return win.HTBOTTOMRIGHT, true
	case left:
		return win.HTLEFT, true
	case right:
		return win.HTRIGHT, true
	case top:
		return win.HTTOP, true
	case bottom:
		return win.HTBOTTOM, true
	default:
		return 0, false
	}
}

func (s *Shell) onMouseDown(x, y int, button walk.MouseButton) {
	if button == walk.RightButton {
		// DefWindowProc emits WM_CONTEXTMENU after the button is released.
		// The top-level subclass routes that message through the same styled
		// presenter as the notification-area icon, including native caption
		// right-clicks and the keyboard context-menu gesture.
		return
	}
	if button != walk.LeftButton {
		return
	}
	if s.controller != nil && s.beginControllerChannel(x, y) {
		return
	}
	// A pointer gesture takes ownership of the custom surface just as moving
	// focus to another native button would. Do not let a held shortcut later
	// dispatch a second, unrelated action on key-up.
	s.cancelKeyboardActivation()
	action := s.layout.hit(x, y)
	if releaseActivatedAction(action) {
		if !s.buttonActivation.pointerDown(action) {
			return
		}
		// Native buttons enter their pushed state synchronously on button-down.
		// The action itself remains armed until a matching button-up.
		s.pressLevels[action] = 1
		s.startVisualAnimation()
		s.surface.Invalidate()
		return
	}
	switch action {
	case actionDevice:
		s.dragDeviceOrToggle()
	default:
		if err := winfx.BeginWindowDrag(s.HWND()); err != nil {
			s.logf("begin borderless drag: %v", err)
		}
	}
}

func (s *Shell) onMouseUp(x, y int, button walk.MouseButton) {
	if s == nil || button != walk.LeftButton {
		return
	}
	if s.controller != nil && s.endControllerChannel(x, y) {
		return
	}
	armed, activate := s.buttonActivation.pointerUp(s.layout.hit(x, y))
	if armed == actionNone {
		return
	}
	s.startVisualAnimation()
	if s.surface != nil {
		s.surface.Invalidate()
	}
	if activate {
		s.activateSurfaceAction(armed)
	}
}

func (s *Shell) cancelPointerActivation() {
	if s == nil {
		return
	}
	s.controllerGesture = controllerChannelGesture{}
	if armed := s.buttonActivation.cancelPointer(); armed != actionNone {
		s.startVisualAnimation()
		if s.surface != nil {
			s.surface.Invalidate()
		}
	}
}

func (s *Shell) deferPointerCaptureCancellation() {
	if s == nil {
		return
	}
	token := s.buttonActivation.pointerToken()
	if token == 0 {
		return
	}
	if s.mw == nil {
		s.cancelPointerActivation()
		return
	}
	// Walk releases capture immediately before publishing MouseUp. Deferring
	// lets the normal MouseUp handler consume the armed action first; if
	// capture was stolen, no MouseUp arrives here and the queued check cancels.
	// The token prevents a delayed callback from canceling a newer gesture.
	s.mw.Synchronize(func() {
		if armed := s.buttonActivation.cancelPointerToken(token); armed != actionNone {
			s.startVisualAnimation()
			if s.surface != nil {
				s.surface.Invalidate()
			}
		}
	})
}

func (s *Shell) cancelKeyboardActivation() {
	if s == nil {
		return
	}
	if armed := s.buttonActivation.cancelKeyboard(); armed != actionNone {
		s.startVisualAnimation()
		if s.surface != nil {
			s.surface.Invalidate()
		}
	}
}

func (s *Shell) cancelButtonActivation() {
	s.cancelPointerActivation()
	s.cancelKeyboardActivation()
}

func (s *Shell) dragDeviceOrToggle() {
	if s == nil || s.mw == nil {
		return
	}
	before := s.mw.BoundsPixels()
	if err := winfx.BeginWindowDrag(s.HWND()); err != nil {
		s.logf("begin device drag: %v", err)
		return
	}
	after := s.mw.BoundsPixels()
	if absInt32(int32(after.X-before.X)) <= 2 &&
		absInt32(int32(after.Y-before.Y)) <= 2 {
		s.invoke("device.toggle", nil)
	}
}

func (s *Shell) onKeyDown(key walk.Key) {
	if key == walk.KeyEscape {
		if s.controllerInline != nil {
			if !s.controllerBusy {
				s.closeControllerInline()
			}
			return
		}
		if s.controllerGesture.key != "" {
			s.cancelPointerActivation()
			if s.surface != nil {
				s.surface.Invalidate()
			}
			return
		}
		s.cancelButtonActivation()
		if s.Config().UI.CloseToTray {
			s.Hide()
		}
		return
	}
	if s.controllerInline != nil {
		return
	}
	action := keyboardActivationAction(key)
	if s.controller != nil {
		action = controllerKeyboardAction(key)
		if action >= actionKey1 && action <= actionKey4 && !s.controllerState.CanNavigate {
			return
		}
	}
	if !s.buttonActivation.keyDown(key, action) {
		return
	}
	if action > actionNone && int(action) < len(s.pressLevels) {
		s.pressLevels[action] = 1
	}
	s.startVisualAnimation()
	if s.surface != nil {
		s.surface.Invalidate()
	}
}

func (s *Shell) onKeyUp(key walk.Key) {
	action, activate := s.buttonActivation.keyUp(key)
	if action == actionNone {
		return
	}
	s.startVisualAnimation()
	if s.surface != nil {
		s.surface.Invalidate()
	}
	if activate {
		s.activateSurfaceAction(action)
	}
}

func (s *Shell) activateSurfaceAction(action actionID) {
	if s.controller != nil && s.activateControllerAction(action) {
		return
	}
	switch action {
	case actionToggle:
		s.invoke("device.toggle", nil)
	case actionOn:
		s.invoke("device.on", nil)
	case actionOff:
		s.invoke("device.off", nil)
	case actionSettings:
		s.OpenSettings()
	case actionDiagnostics:
		s.OpenDiagnostics()
	case actionReconnect:
		s.invoke("reconnect", nil)
	case actionTitleMinimize:
		win.PostMessage(s.mw.Handle(), win.WM_SYSCOMMAND, win.SC_MINIMIZE, 0)
	case actionTitleZoom:
		if win.IsZoomed(s.mw.Handle()) {
			win.PostMessage(s.mw.Handle(), win.WM_SYSCOMMAND, win.SC_RESTORE, 0)
		} else {
			win.PostMessage(s.mw.Handle(), win.WM_SYSCOMMAND, win.SC_MAXIMIZE, 0)
		}
	case actionTitleClose:
		_ = s.mw.Close()
	}
}

func (s *Shell) onClosing(canceled *bool, _ walk.CloseReason) {
	if s.windowCloseAllowed {
		s.closeWindowBoundIntegrations()
		return
	}
	if !s.quitting.Load() && s.Config().UI.CloseToTray {
		*canceled = true
		s.Hide()
		return
	}
	s.quitting.Store(true)
	if !s.canAnimateWindowExit() {
		s.saveWindowBounds()
		s.windowCloseAllowed = true
		s.closeWindowBoundIntegrations()
		return
	}
	*canceled = true
	s.beginExitTransition()
}

func (s *Shell) closeWindowBoundIntegrations() {
	if s == nil {
		return
	}
	if s.systemEvents != nil {
		if err := s.systemEvents.Close(); err != nil {
			s.logf("close Windows system-event integration: %v", err)
		}
		s.systemEvents = nil
	}
	s.closeTaskbarIntegration()
}

func (s *Shell) invoke(method string, params any) {
	if s.controller != nil {
		s.invokeController(method)
		return
	}
	request, err := ipc.NewRequest(fmt.Sprintf("ui-%d", time.Now().UnixNano()), method, params)
	if err != nil {
		s.showError(s.tr(i18n.MsgErrorRunCommand), err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
		defer cancel()
		if _, commandErr := s.dispatcher.Handler(ctx, request); commandErr != nil {
			_ = s.QueueAudio(string(sfx.CueError), 1)
			s.showError(s.tr(i18n.MsgErrorCommandFailed), commandErr)
		}
	}()
}

func (s *Shell) showError(instruction string, err error) {
	if err == nil || s.mw == nil || s.quitting.Load() || s.closed.Load() {
		return
	}
	s.mw.Synchronize(func() {
		if s.quitting.Load() || s.closed.Load() {
			return
		}
		_, _ = winfx.TaskDialog(winfx.TaskDialogOptions{
			Parent:          s.HWND(),
			Title:           s.tr(i18n.MsgAppName),
			MainInstruction: instruction,
			Content:         err.Error(),
			Icon:            winfx.DialogIconError,
			Buttons:         winfx.DialogButtonOK,
			AllowCancel:     true,
		})
	})
}

// QueueAudio validates and serializes one explicit sound sequence without
// blocking the UI, HTTP, named-pipe, or WM_COPYDATA caller.
func (s *Shell) QueueAudio(cue string, count int) error {
	if s == nil || s.audio == nil {
		return errors.New("shared sound effects are unavailable")
	}
	normalized := sfx.Cue(strings.ToLower(strings.TrimSpace(cue)))
	switch normalized {
	case sfx.CueOn, sfx.CueOff, sfx.CueMessage, sfx.CueError:
	default:
		return fmt.Errorf("unknown sound cue %q", cue)
	}
	if count < 1 || count > 10 {
		return errors.New("sound count must be between 1 and 10")
	}
	select {
	case <-s.ctx.Done():
		return errors.New("shared sound effects are closed")
	case s.audioJobs <- audioJob{cue: normalized, count: count}:
		return nil
	default:
		return errors.New("shared sound effect queue is full")
	}
}

func (s *Shell) runAudio() {
	defer close(s.audioDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.audioJobs:
			for index := 0; index < job.count; index++ {
				if err := s.audio.Play(job.cue); err != nil {
					s.logf("play %s cue: %v", job.cue, err)
				}
				if index+1 >= job.count {
					continue
				}
				timer := time.NewTimer(220 * time.Millisecond)
				select {
				case <-s.ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return
				case <-timer.C:
				}
			}
		}
	}
}

// ResolveAudio exposes a defensive copy of the configured shared asset.
func (s *Shell) ResolveAudio(cue string) (sfx.Asset, error) {
	if s == nil || s.audio == nil {
		return sfx.Asset{}, errors.New("shared sound effects are unavailable")
	}
	return s.audio.Resolve(sfx.Cue(cue))
}

// ImportAudio validates and stores one custom sound in the shared asset store.
func (s *Shell) ImportAudio(name string, wave []byte) (sfx.ImportedAsset, error) {
	if s == nil || s.audio == nil {
		return sfx.ImportedAsset{}, errors.New("shared sound effects are unavailable")
	}
	return s.audio.Import(name, wave)
}

// ImportAudioFile is the native Settings file-picker adapter.
func (s *Shell) ImportAudioFile(path string) (sfx.ImportedAsset, error) {
	if s == nil || s.audio == nil {
		return sfx.ImportedAsset{}, errors.New("shared sound effects are unavailable")
	}
	return s.audio.ImportFile(path)
}

func (s *Shell) updateTray() {
	if s.controller != nil {
		s.updateControllerTray()
		return
	}
	if s.tray == nil {
		return
	}
	snapshot := s.snapshot
	icon := s.connectingIcon
	status := s.tr(i18n.MsgStateConnecting)
	switch {
	case snapshot.State == model.StateOn:
		icon, status = s.onIcon, s.tr(i18n.MsgTrayStateOn)
	case snapshot.State == model.StateOff:
		icon, status = s.offIcon, s.tr(i18n.MsgTrayStateOff)
	case snapshot.LastError != "":
		icon, status = s.errorIcon, s.tr(i18n.MsgTrayConnectionError)
	case snapshot.State == model.StateUnknown && snapshot.ReconnectAttempt == 0:
		icon, status = s.connectingIcon, s.tr(i18n.MsgStateConnecting)
	default:
		icon, status = s.unknownIcon, s.tr(i18n.MsgTrayStateUnknown)
	}
	_ = s.tray.SetIcon(icon)
	if err := s.applyWindowStateIcon(icon); err != nil {
		s.logf("update taskbar icon: %v", err)
	}
	connection := s.tr(i18n.MsgConnectionReconnecting)
	if snapshot.WebSocketOnline {
		connection = s.tr(i18n.MsgConnectionLive)
	} else if snapshot.HTTPReachable {
		connection = s.tr(i18n.MsgConnectionHTTPFallback)
	}
	_ = s.tray.SetToolTip(fmt.Sprintf(
		s.tr(i18n.MsgTrayTooltip),
		status,
		connection,
		s.tr(i18n.MsgTrayLeftClickHint),
	))
}

func (s *Shell) updateWindowStateIcon() {
	if s.controller != nil {
		_ = s.applyWindowStateIcon(s.appIcon)
		return
	}
	if s == nil {
		return
	}
	icon := s.connectingIcon
	switch {
	case s.snapshot.State == model.StateOn:
		icon = s.onIcon
	case s.snapshot.State == model.StateOff:
		icon = s.offIcon
	case s.snapshot.LastError != "":
		icon = s.errorIcon
	case s.snapshot.State == model.StateUnknown &&
		s.snapshot.ReconnectAttempt == 0:
		icon = s.connectingIcon
	default:
		icon = s.unknownIcon
	}
	if err := s.applyWindowStateIcon(icon); err != nil {
		s.logf("reapply taskbar icon: %v", err)
	}
}

func (s *Shell) setAlwaysOnTop(enabled bool) {
	insertAfter := win.HWND_NOTOPMOST
	if enabled {
		insertAfter = win.HWND_TOPMOST
	}
	win.SetWindowPos(
		s.mw.Handle(), insertAfter, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE,
	)
}

func (s *Shell) setTaskbarVisibility(show bool) {
	hwnd := s.mw.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_EXSTYLE)
	if show {
		style |= win.WS_EX_APPWINDOW
		style &^= win.WS_EX_TOOLWINDOW
	} else {
		style |= win.WS_EX_TOOLWINDOW
		style &^= win.WS_EX_APPWINDOW
	}
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, style)
}

func (s *Shell) saveWindowBounds() {
	if s == nil || s.mw == nil {
		return
	}
	s.observeNativeWindowPlacement()
	s.persistWindowPlacement()
}

func (s *Shell) dispose() {
	// DirectComposition targets and swap-chain resources must be detached
	// while their child HWND is still alive.
	s.closeRenderingBackend()
	s.disposeForegroundRecovery()
	// Owned popup HWNDs must be released before their owner and notification
	// icon disappear. This is a no-op when no tray window has been retained.
	traymenu.DiscardCached(s.HWND())
	if s.settings != nil {
		s.settings.Dispose()
		s.settings = nil
	}
	if s.diagnostics != nil && !s.diagnostics.IsDisposed() {
		s.diagnostics.Dispose()
		s.diagnostics = nil
	}
	if s.tray != nil {
		_ = s.tray.SetVisible(false)
		_ = s.tray.Dispose()
		s.tray = nil
	}
	s.disposeAnimationBitmaps()
	if s.animation != nil {
		s.animation.ReleaseFrames()
		s.animation = nil
	}
	s.disposeArtworkBitmaps()
	s.artworkSources = nil
	if s.vectorOnBitmap != nil {
		s.vectorOnBitmap.Dispose()
		s.vectorOnBitmap = nil
	}
	if s.vectorOffBitmap != nil {
		s.vectorOffBitmap.Dispose()
		s.vectorOffBitmap = nil
	}
	for _, icon := range s.controllerTrayIcons {
		if icon != nil {
			icon.Dispose()
		}
	}
	s.controllerTrayIcons = nil
	for _, icon := range []walk.Image{
		s.appIcon, s.onIcon, s.offIcon, s.unknownIcon, s.connectingIcon, s.errorIcon,
		s.settingsIcon, s.diagnosticsIcon,
	} {
		if icon != nil {
			icon.Dispose()
		}
	}
	for _, font := range []*walk.Font{
		s.titleFont, s.stateFont, s.bodyFont, s.buttonFont, s.captionFont,
	} {
		if font != nil {
			font.Dispose()
		}
	}
	s.disposeSurfaceFonts()
	if s.mw != nil {
		s.mw.Dispose()
		s.mw = nil
	}
}

func (s *Shell) logf(format string, args ...any) {
	if s.log != nil {
		s.log(format, args...)
	}
}

func embeddedPNG(path string) (image.Image, error) {
	data, err := assets.Files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", path, err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode embedded %s: %w", path, err)
	}
	return decoded, nil
}

func boolResult(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}
