//go:build windows

package ui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"rayanlamp.local/app/internal/appcore"
	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/presentation"
)

// Opt-in, app-owned window: no input injection, transport, tray, or board core.
// Reports CPU paint + presentation submission, not monitor scanout FPS.
func newControllerWindowFixture(t *testing.T) (*Shell, config.Config) {
	t.Helper()
	if os.Getenv("RAYANLAMP_WINDOW_TESTS") != "1" {
		t.Skip("set RAYANLAMP_WINDOW_TESTS=1 for native window integration")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	// Match the packaged PerMonitorV2 manifest without changing other threads.
	dpiProc := windows.NewLazySystemDLL("user32.dll").NewProc("SetThreadDpiAwarenessContext")
	if dpiProc.Find() == nil {
		previous, _, _ := dpiProc.Call(^uintptr(3))
		if previous != 0 {
			t.Cleanup(func() { dpiProc.Call(previous) })
		}
	}
	panicHandler := walk.App().Panicking().Attach(func(err error) { t.Errorf("native callback: %v\n%s", err, debug.Stack()) })
	t.Cleanup(func() { walk.App().Panicking().Detach(panicHandler) })
	// Stable-path test executables do not embed the product's PE manifest.
	// Activate its Common Controls v6 dependency for this test's UI thread.
	_, source, _, _ := runtime.Caller(0)
	manifest, err := windows.UTF16PtrFromString(filepath.Join(filepath.Dir(source), "..", "..", "resources", "RayanLamp.exe.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	activation := win.CreateActCtx(&win.ACTCTX{Source: manifest})
	if activation == win.HANDLE(^uintptr(0)) {
		t.Fatal("create product manifest activation context")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	t.Cleanup(func() { kernel.NewProc("ReleaseActCtx").Call(uintptr(activation)) })
	cookie, ok := win.ActivateActCtx(activation)
	if !ok {
		t.Fatal("activate product manifest")
	}
	t.Cleanup(func() { kernel.NewProc("DeactivateActCtx").Call(0, cookie) })
	configureControllerTokens()
	cfg := config.Default()
	cfg.UI.ViewMode = config.ViewDashboard
	cfg.Lamp.BaseURL = "http://127.0.0.1:1"
	cfg.UI.Locale, cfg.UI.Theme = "en", "dark"
	cfg.UI.ViewMode, cfg.UI.Backdrop = config.ViewDashboard, config.BackdropAcrylic
	cfg.UI.WindowBounds = config.WindowBounds{X: 80, Y: 80, Width: 640, Height: 720}
	cfg.UI.PrewarmTrayMenu = false
	cfg.Hotkeys = config.HotkeysConfig{}
	cfg.Notifications.Enabled, cfg.Notifications.Sound = false, false
	cfg.Integration.LocalAPIEnabled = false
	core, err := appcore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	dispatcher, err := commands.New(core, path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	shell := &Shell{core: core, dispatcher: dispatcher, configPath: path,
		controller:      &presentation.Options{Title: "PCController animation fixture"},
		controllerState: presentation.Snapshot{Preview: true, HavePanel: true, Segments: [4]byte{0x3f, 0x06, 0x5b, 0x4f}, Brightness: 7},
		cfg:             cfg, snapshot: core.Snapshot(), ctx: ctx, cancel: cancel,
		eventDone: make(chan struct{}), log: t.Logf}
	shell.initializeLocalization()
	if err := shell.createWindow(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	// Test binaries lack the APP PE resource; use the source icon for this
	// fixture without changing the packaged application's resource path.
	icon, err := walk.NewIconFromFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "Controller", "winres", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	shell.appIcon = icon
	shell.settingsIcon = icon
	if err := shell.mw.SetIcon(icon); err != nil {
		t.Fatal(err)
	}
	shell.installEvents()
	return shell, cfg
}

func TestControllerGlassHoverPresentation(t *testing.T) {
	shell, cfg := newControllerWindowFixture(t)
	for _, backend := range []string{"auto", "legacy"} {
		cfg.UI.RenderBackend = backend
		shell.cfgMu.Lock()
		shell.cfg = cfg
		shell.cfgMu.Unlock()
		shell.applyConfigUI(cfg, false)
		shell.mw.Show()
		timer := time.AfterFunc(200*time.Millisecond, func() { shell.mw.Synchronize(func() { win.PostQuitMessage(0) }) })
		shell.mw.Run()
		timer.Stop()
		canvas, err := shell.surface.CreateCanvas()
		if err != nil {
			t.Fatal(err)
		}
		paint := func() {
			t.Helper()
			if err := shell.paint(canvas, shell.surface.ClientBoundsPixels()); err != nil {
				t.Fatal(err)
			}
		}
		paint()
		if len(shell.layout.controllerButtons) == 0 {
			t.Fatal("fixture has no glass buttons")
		}
		button := shell.layout.controllerButtons[0]
		for _, candidate := range shell.layout.controllerButtons {
			if candidate.rect.Width*candidate.rect.Height > button.rect.Width*button.rect.Height {
				button = candidate
			}
		}
		const frames = 30
		started := time.Now()
		for i := 0; i < frames; i++ {
			shell.hoverLevels[button.action] = float64(i%15) / 14
			shell.visualPointer = walk.Point{X: button.rect.X + 10 + i, Y: button.rect.Y + 12}
			paint()
		}
		t.Logf("%s: full glass hover paint/present %.2f ms/frame over %d frames (button %dx%d); %s", backend, float64(time.Since(started).Microseconds())/1000/frames, frames, button.rect.Width, button.rect.Height, shell.renderingSummary())
		canvas.Dispose()
		if backend == "auto" && shell.rendering.capability.Available && (!shell.directXForegroundEnabled() || shell.rendering.failures != 0) {
			t.Fatal("GPU fixture fell back unexpectedly")
		}
	}
}
