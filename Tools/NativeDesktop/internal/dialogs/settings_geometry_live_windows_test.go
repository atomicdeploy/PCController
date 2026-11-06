//go:build windows

package dialogs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/design"
	"pccontroller.local/desktop/internal/winfx"
)

// This test owns every HWND it uses. It does not inject input or operate other
// applications. Screenshots are optional evidence, never renderer input.
func TestSettingsWindowGeometryLifecycle(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_WINDOW_TESTS") != "1" {
		t.Skip("set PCCONTROLLER_DESKTOP_WINDOW_TESTS=1 for native window integration")
	}
	for _, name := range []string{"owned", "ownerless", "persian"} {
		t.Run(name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			deactivate := activateSettingsTestManifest(t)
			defer deactivate()
			owner, err := walk.NewMainWindow()
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Dispose()
			if err := owner.SetBoundsPixels(walk.Rectangle{X: 100, Y: 100, Width: 420, Height: 420}); err != nil {
				t.Fatal(err)
			}
			var parent walk.Form = owner
			if name == "ownerless" {
				parent = nil
			}
			cfg := config.Default()
			cfg.UI.Animations = false // This fixture verifies settled fonts and geometry.
			cfg.UI.Locale = "en"
			if name == "persian" {
				cfg.UI.Locale = "fa"
			}
			var saved config.WindowBounds
			actions := SettingsActions{PersistWindowBounds: func(bounds config.WindowBounds) {
				saved = bounds
			}}
			session, err := newSettingsDialogSession(parent, cfg, actions, true)
			if err != nil {
				t.Fatal(err)
			}
			defer session.dialog.Dispose()
			// Raise only this fixture's own HWND for unobscured screen evidence.
			win.SetWindowPos(session.dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
			prepared := session.dialog.BoundsPixels()
			session.shown = true
			session.dialog.Show()
			pumpSettingsTestMessages(owner, 400*time.Millisecond)
			first := session.dialog.BoundsPixels()
			assertDialogPhysicalFonts(t, session.dialog, cfg.UI.Locale, 50)
			if first != prepared {
				t.Fatalf("first show replaced prepared bounds: before=%+v after=%+v", prepared, first)
			}
			logical, err := winfx.CaptureDialogPlacement(uintptr(session.dialog.Handle()))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("first show: pixels=%+v, logical=%+v, DPI=%d, configured=%+v", first, logical, session.dialog.DPI(), cfg.UI.SettingsBounds)
			captureSettingsTestWindow(t, session.dialog, name+"-first")
			if logical.Width < config.SettingsWindowMinWidth || logical.Height < config.SettingsWindowMinHeight {
				t.Fatalf("Settings collapsed to %+v", logical)
			}
			metrics, err := winfx.MonitorMetricsForRect(winfx.Rect{
				Left: int32(first.X), Top: int32(first.Y),
				Right: int32(first.X + first.Width), Bottom: int32(first.Y + first.Height),
			}, uint32(session.dialog.DPI()))
			if err != nil {
				t.Fatal(err)
			}
			if int(metrics.WorkArea.Width()) >= cfg.UI.SettingsBounds.Width*session.dialog.DPI()/96 &&
				int(metrics.WorkArea.Height()) >= cfg.UI.SettingsBounds.Height*session.dialog.DPI()/96 &&
				(logical.Width != cfg.UI.SettingsBounds.Width || logical.Height != cfg.UI.SettingsBounds.Height) {
				t.Fatalf("first show ignored configured logical size: got=%+v want=%+v", logical, cfg.UI.SettingsBounds)
			}

			resized := first
			resized.Width += 60
			resized.Height -= 20
			if err := session.dialog.SetBoundsPixels(resized); err != nil {
				t.Fatal(err)
			}
			pumpSettingsTestMessages(owner, 200*time.Millisecond)
			resized = session.dialog.BoundsPixels()
			session.dialog.Show()
			pumpSettingsTestMessages(owner, 150*time.Millisecond)
			if got := session.dialog.BoundsPixels(); got != resized {
				t.Fatalf("repeated Show changed resized bounds: got=%+v want=%+v", got, resized)
			}
			if !owner.Enabled() {
				t.Fatal("modeless Settings disabled its owner")
			}
			// First-show fixes must also survive subsequent layout passes.
			// Locale/font/RTL changes and every tab used to be tested only as
			// source strings, which could not detect a native size overwrite.
			for _, locale := range []string{"fa", "en"} {
				localized := cfg
				localized.UI.Locale = locale
				applySettingsLocalization(session, localized)
				pumpSettingsTestMessages(owner, 200*time.Millisecond)
				assertDialogPhysicalFonts(t, session.dialog, locale, 50)
				if got := session.dialog.BoundsPixels(); got != resized {
					t.Fatalf("live locale %s replaced user bounds: got=%+v want=%+v", locale, got, resized)
				}
				if locale == "fa" && name == "persian" {
					for index := 0; index < session.controls.tabs.Pages().Len(); index++ {
						_ = session.controls.tabs.SetCurrentIndex(index)
						pumpSettingsTestMessages(owner, 400*time.Millisecond)
						page := session.controls.tabs.Pages().At(index)
						t.Logf("Persian page %d: visible=%t bounds=%+v children=%d", index, page.Visible(), page.BoundsPixels(), page.Children().Len())
						if bounds := page.BoundsPixels(); bounds.Width < 100 || bounds.Height < 100 {
							t.Fatalf("Persian page %d collapsed: %+v", index, bounds)
						}
						captureSettingsTestWindow(t, session.dialog, "persian-tab-"+strconv.Itoa(index))
					}
				}
			}
			for index := 0; index < session.controls.tabs.Pages().Len(); index++ {
				if err := session.controls.tabs.SetCurrentIndex(index); err != nil {
					t.Fatal(err)
				}
				pumpSettingsTestMessages(owner, 100*time.Millisecond)
				if got := session.dialog.BoundsPixels(); got != resized {
					t.Fatalf("tab %d replaced user bounds: got=%+v want=%+v", index, got, resized)
				}
			}
			captureSettingsTestWindow(t, session.dialog, name+"-live-layout")
			session.dialog.Cancel()
			if saved.Width == 0 || saved.Height == 0 {
				t.Fatal("close did not persist Settings dimensions")
			}
			cfg.UI.SettingsBounds = saved
			reopened, err := newSettingsDialogSession(parent, cfg, actions, true)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.dialog.Dispose()
			win.SetWindowPos(reopened.dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
			reopened.shown = true
			reopened.dialog.Show()
			pumpSettingsTestMessages(owner, 400*time.Millisecond)
			got := reopened.dialog.BoundsPixels()
			// Integer logical dimensions can round by one physical pixel at
			// fractional scaling (e.g. 940px -> 627 DIP -> 941px at 150%).
			if got.Width < resized.Width-1 || got.Width > resized.Width+1 ||
				got.Height < resized.Height-1 || got.Height > resized.Height+1 {
				t.Fatalf("reopen lost resized dimensions: got=%+v want=%+v saved=%+v", got, resized, saved)
			}
			t.Logf("reopen: pixels=%+v, saved=%+v", got, saved)
			captureSettingsTestWindow(t, reopened.dialog, name+"-reopened")
		})
	}
}

// The canonical test executable deliberately has no product PE resources.
// Match the packaged Common Controls v6 manifest on this test's UI thread.
func activateSettingsTestManifest(t *testing.T) func() {
	t.Helper()
	dpiProc := windows.NewLazySystemDLL("user32.dll").NewProc("SetThreadDpiAwarenessContext")
	var previousDPI uintptr
	if dpiProc.Find() == nil {
		previousDPI, _, _ = dpiProc.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2
		if previousDPI == 0 {
			t.Fatal("enable per-monitor-v2 DPI context")
		}
	}
	_, source, _, _ := runtime.Caller(0)
	manifest, err := windows.UTF16PtrFromString(filepath.Join(filepath.Dir(source), "..", "..", "resources", "PCControllerDesktop.exe.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	activation := win.CreateActCtx(&win.ACTCTX{Source: manifest})
	if activation == win.HANDLE(^uintptr(0)) {
		t.Fatal("create product manifest activation context")
	}
	cookie, ok := win.ActivateActCtx(activation)
	if !ok {
		t.Fatal("activate product manifest")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	return func() {
		kernel.NewProc("DeactivateActCtx").Call(0, cookie)
		kernel.NewProc("ReleaseActCtx").Call(uintptr(activation))
		if previousDPI != 0 {
			dpiProc.Call(previousDPI)
		}
	}
}

func TestControllerSettingsRetainDonorEditorsWithoutDeviceRoutes(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_WINDOW_TESTS") != "1" {
		t.Skip("native window integration is opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deactivate := activateSettingsTestManifest(t)
	defer deactivate()
	originalTokens := design.Default
	design.Default = design.ControllerTokens()
	defer func() { design.Default = originalTokens }()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	cfg := config.Default()
	cfg.UI.Locale, cfg.UI.Theme, cfg.UI.Animations = "en", "dark", false
	cfg.UI.SettingsTab = 5 // Old six-tab donor preference must clamp safely.
	cfg.Device.BaseURL = "http://127.0.0.1:1"
	session, err := newSettingsDialogSession(owner, cfg, SettingsActions{ControllerMode: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.dialog.Dispose()
	session.shown = true
	session.dialog.Show()
	pumpSettingsTestMessages(owner, 300*time.Millisecond)
	c := session.controls
	assertDialogPhysicalFonts(t, session.dialog, cfg.UI.Locale, 50)
	if c.tabs.Pages().Len() != 5 || c.tabs.CurrentIndex() != 4 {
		t.Fatal("controller settings lost tabs or invalid persisted selection")
	}
	if c.baseURL != nil || c.localAPIListen != nil || c.hotkeyRecorders == nil || c.onScript == nil || c.soundSource == nil || c.startWithWindows == nil {
		t.Fatal("donor controls were not adapted to controller mode")
	}
	if session.dialog.Title() != "Desktop settings — PCController" {
		t.Fatal(session.dialog.Title())
	}
	_ = c.onScript.SetText(`C:\Automation\running.ps1`)
	c.automationEnabled.SetChecked(true)
	c.notificationsEnabled.SetChecked(true)
	c.sound.SetChecked(true)
	got, err := collectSettings(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Device != cfg.Device || got.Integration.LocalAPIToken != cfg.Integration.LocalAPIToken || got.Automation.OnScript != `C:\Automation\running.ps1` || !got.Notifications.Sound {
		t.Fatal("controller settings corrupted dormant settings or discarded drafts")
	}
	for index := 0; index < c.tabs.Pages().Len(); index++ {
		_ = c.tabs.SetCurrentIndex(index)
		pumpSettingsTestMessages(owner, 100*time.Millisecond)
		captureSettingsTestWindow(t, session.dialog, "controller-settings-"+strconv.Itoa(index))
	}
	_ = c.automationTimeout.SetText("invalid")
	if _, err := collectSettings(cfg, c); err == nil {
		t.Fatal("invalid numeric edit was silently accepted")
	}
	if c.onScript.Text() != `C:\Automation\running.ps1` {
		t.Fatal("validation discarded script draft")
	}
	session.dialog.Cancel()
	if !owner.Enabled() {
		t.Fatal("modeless dialog disabled its owner")
	}
}

func assertDialogPhysicalFonts(t *testing.T, dialog *walk.Dialog, locale string, minimum int) {
	t.Helper()
	checked := 0
	visit := func(window walk.Window) {
		font := window.Font()
		want := defaultUIFontFamily
		if locale == "fa" {
			want = winfx.PersianFontFamily(font.Bold())
		}
		if font.Family() != want {
			t.Fatalf("%T font family = %q, want %q", window, font.Family(), want)
		}
		handle := win.SendMessage(window.Handle(), win.WM_GETFONT, 0, 0)
		if handle != 0 {
			var physical win.LOGFONT
			if win.GetObject(win.HGDIOBJ(handle), unsafe.Sizeof(physical), unsafe.Pointer(&physical)) == 0 {
				t.Fatal("read actual HWND font metrics")
			}
			wantHeight := int32((font.PointSize()*dialog.DPI() + 36) / 72)
			height := physical.LfHeight
			if height < 0 {
				height = -height
			}
			if height < wantHeight-1 || height > wantHeight+1 {
				t.Fatalf("%T bitmap-scaled font: actual=%d expected=%d at DPI %d", window, height, wantHeight, dialog.DPI())
			}
			face, err := winfx.FontFaceName(handle)
			if err != nil || face != want {
				t.Fatalf("%T actually renders %q, want %q: %v", window, face, want, err)
			}
			checked++
		}
	}
	visit(dialog)
	walkDialogWidgets(dialog, func(widget walk.Widget) { visit(widget) })
	if checked < minimum {
		t.Fatalf("only %d native control fonts were verified", checked)
	}
	t.Logf("verified %d actual HWND font faces for locale %s", checked, locale)
}

func pumpSettingsTestMessages(owner *walk.MainWindow, duration time.Duration) {
	// Walk's loop also commits asynchronous layouts via RunSynchronized;
	// a plain PeekMessage loop cannot test real control rendering.
	timer := time.AfterFunc(duration, func() {
		owner.Synchronize(func() { win.PostQuitMessage(0) })
	})
	defer timer.Stop()
	owner.Run()
}

func captureSettingsTestWindow(t *testing.T, dialog *walk.Dialog, name string) {
	t.Helper()
	tool, directory := os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS")
	if tool == "" || directory == "" {
		return
	}
	win.SetWindowPos(dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	win.RedrawWindow(dialog.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
	_ = winfx.FlushComposition()
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(tool, strconv.Itoa(os.Getpid()),
		filepath.Join(directory, name+".png"), dialog.Title(), "screen", "12").CombinedOutput()
	if err != nil {
		t.Fatalf("screenshot: %v: %s", err, output)
	}
	t.Logf("%s", output)
}
