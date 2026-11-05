//go:build windows

package ui

import (
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

// This integration fixture owns its shell and has no device/network lifecycle.
// It uses the app's own APIs and screenshots, never mouse/keyboard automation.
func TestShellChromeGeometryLifecycle(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_WINDOW_TESTS") != "1" {
		t.Skip("set PCCONTROLLER_DESKTOP_WINDOW_TESTS=1 for native window integration")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cfg := config.Default()
	cfg.Device.BaseURL = "http://127.0.0.1:1"
	cfg.UI.Locale = "en"
	cfg.UI.Theme = "dark"
	cfg.UI.ViewMode = config.ViewDashboard
	cfg.UI.WindowBounds = config.WindowBounds{X: 80, Y: 80, Width: 340, Height: 520}
	cfg.UI.Animations = false
	cfg.UI.AlwaysOnTop = true // Only the fixture window, for unobscured captures.
	cfg.UI.PrewarmTrayMenu = false
	cfg.UI.Backdrop = config.BackdropBlur
	cfg.Hotkeys = config.HotkeysConfig{}
	cfg.Notifications.Enabled = false
	cfg.Notifications.Sound = false
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
	// Exercise the production window/render/message setup without requiring
	// Explorer's notification area (unavailable in restricted test desktops).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shell := &Shell{core: core, dispatcher: dispatcher, configPath: path,
		cfg: core.Config(), snapshot: core.Snapshot(), ctx: ctx, cancel: cancel,
		eventDone: make(chan struct{}), log: t.Logf}
	shell.initializeLocalization()
	if err := shell.createWindow(); err != nil {
		t.Fatal(err)
	}
	if err := shell.loadArtwork(); err != nil {
		shell.dispose()
		t.Fatal(err)
	}
	shell.installEvents()
	defer shell.Close()

	pump := func() {
		timer := time.AfterFunc(350*time.Millisecond, func() {
			shell.mw.Synchronize(func() { win.PostQuitMessage(0) })
		})
		shell.mw.Run()
		timer.Stop()
	}
	check := func(label string, native bool) {
		t.Helper()
		var outer, client win.RECT
		win.GetWindowRect(shell.mw.Handle(), &outer)
		win.GetClientRect(shell.mw.Handle(), &client)
		exStyle := uint32(win.GetWindowLong(shell.mw.Handle(), win.GWL_EXSTYLE))
		if !native {
			style := uint32(win.GetWindowLong(shell.mw.Handle(), win.GWL_STYLE))
			if style&win.WS_THICKFRAME == 0 && exStyle&win.WS_EX_WINDOWEDGE != 0 {
				t.Fatalf("%s: frameless window retained a raised edge: exstyle=%#x", label, exStyle)
			}
			if client.Right-client.Left != outer.Right-outer.Left || client.Bottom-client.Top != outer.Bottom-outer.Top {
				t.Fatalf("%s: captionless client does not fill outer HWND: outer=%+v client=%+v", label, outer, client)
			}
		}
		if winfx.CurrentVersion().IsWindows11() {
			var color int32
			result, _, _ := windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute").Call(
				shell.HWND(), 34, uintptr(unsafe.Pointer(&color)), 4)
			want := int32(-2)
			if native {
				want = -1
			}
			if int32(result) >= 0 && color != want {
				t.Fatalf("%s: DWM border=%d want=%d HRESULT=%#x", label, color, want, result)
			}
			if int32(result) < 0 {
				t.Logf("%s: DWM border-color readback unsupported (%#x); using rectangle and screenshot evidence", label, uint32(result))
			}
		}
		t.Logf("%s: outer=%+v client=%+v exstyle=%#x", label, outer, client, exStyle)
	}
	capture := func(name string, checkFrame bool) {
		tool, directory := os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS")
		if tool == "" || directory == "" {
			return
		}
		win.SetWindowPos(shell.mw.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		win.RedrawWindow(shell.mw.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
		_ = winfx.FlushComposition()
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name+".png")
		output, err := exec.Command(tool, strconv.Itoa(os.Getpid()), path, shell.mw.Title(), "screen", "12").CombinedOutput()
		if err != nil {
			t.Fatalf("screenshot: %v: %s", err, output)
		}
		if checkFrame && screenshotHasWhiteFrame(readChromeScreenshot(t, path)) {
			t.Fatalf("%s: screenshot contains a bright outer frame: %s", name, path)
		}
	}
	if baseline := os.Getenv("PCCONTROLLER_DESKTOP_BORDER_BASELINE"); baseline != "" {
		if !screenshotHasWhiteFrame(readChromeScreenshot(t, baseline)) {
			t.Fatal("pixel regression gate failed to detect the known-bad white frame")
		}
	}
	for _, variant := range []struct{ name, backend, backdrop, title, border string }{
		{"auto-blur", "auto", "blur", "none", "auto"},
		{"auto-acrylic", "auto", "acrylic", "none", "auto"},
		{"auto-solid", "auto", "solid", "none", "auto"},
		{"native-solid", "auto", "solid", "native", "auto"},
		{"native-acrylic", "auto", "acrylic", "native", "auto"},
		{"custom-acrylic", "auto", "acrylic", "custom", "auto"},
		{"legacy-blur", "legacy", "blur", "none", "auto"},
		{"transparent-borderless", "legacy", "transparent", "none", "none"},
		{"transparent-to-blur", "auto", "blur", "none", "auto"},
		{"persian-dashboard", "auto", "acrylic", "none", "auto"},
		{"compact-acrylic", "auto", "acrylic", "none", "auto"},
		{"compact-blur", "auto", "blur", "none", "auto"},
	} {
		cfg.UI.Locale = "en"
		if variant.name == "persian-dashboard" {
			cfg.UI.Locale = "fa"
		}
		if variant.name == "compact-acrylic" || variant.name == "compact-blur" {
			cfg.UI.ViewMode = config.ViewCompact
			cfg.UI.WindowBounds.Width, cfg.UI.WindowBounds.Height = 176, 176
		}
		cfg.UI.RenderBackend, cfg.UI.Backdrop = variant.backend, variant.backdrop
		cfg.UI.TitleBarMode, cfg.UI.BorderMode = variant.title, variant.border
		shell.cfgMu.Lock()
		shell.cfg = cfg
		shell.cfgMu.Unlock()
		shell.applyConfigUI(cfg, false)
		shell.mw.Show()
		pump()
		if variant.backend == "auto" && shell.rendering.capability.Available &&
			(!shell.directXForegroundEnabled() || shell.rendering.presenter == nil || shell.rendering.failures != 0) {
			t.Fatalf("%s: GPU coverage silently fell back: %s", variant.name, shell.renderingSummary())
		}
		native := variant.title == config.TitleBarNative
		check(variant.name, native)
		// The final screenshot alone can miss a forbidden NC paint that a
		// subsequent child/GPU repaint covers. Check message ownership too.
		for _, message := range []uintptr{win.WM_NCPAINT, wmNCActivate} {
			_, handled := shell.windowMessage(uintptr(shell.mw.Handle()), message, 1, 0)
			if handled == native {
				t.Fatalf("%s: message %#x handled=%t; captionless chrome must own NC painting, native chrome must delegate", variant.name, message, handled)
			}
		}
		checkFrame := !native && variant.backdrop != config.BackdropTransparent
		capture(variant.name, checkFrame)
		// Both inactive and active notifications must leave the final policy
		// intact after the ordinary DefWindowProc chain has run.
		for _, active := range []uintptr{0, 1} {
			win.SendMessage(shell.mw.Handle(), wmNCActivate, active, 0)
			pump()
			check(variant.name+" activation", native)
		}
		shell.mw.Hide()
		shell.mw.Show()
		pump()
		check(variant.name+" reshow", native)
		capture(variant.name+"-reshow", checkFrame)
	}
}

func readChromeScreenshot(t *testing.T, path string) image.Image {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func screenshotHasWhiteFrame(img image.Image) bool {
	// The fixture's regular screen capture has a 12px exterior margin. Sample
	// inside the old 8px GDI frame, away from rounded corners and UI content.
	const margin = 12
	b := img.Bounds().Inset(margin)
	if b.Dx() < 40 || b.Dy() < 40 {
		return false
	}
	whiteSides := 0
	for side := 0; side < 4; side++ {
		white, total := 0, 0
		for fraction := 20; fraction < 80; fraction++ {
			for inset := 3; inset <= 5; inset++ {
				x, y := b.Min.X+b.Dx()*fraction/100, b.Min.Y+inset
				switch side {
				case 1:
					y = b.Max.Y - 1 - inset
				case 2:
					x, y = b.Min.X+inset, b.Min.Y+b.Dy()*fraction/100
				case 3:
					x, y = b.Max.X-1-inset, b.Min.Y+b.Dy()*fraction/100
				}
				r, g, bl, _ := img.At(x, y).RGBA()
				lo, hi := min(r, g, bl)>>8, max(r, g, bl)>>8
				if lo >= 200 && hi-lo <= 30 {
					white++
				}
				total++
			}
		}
		if white*10 > total*7 {
			whiteSides++
		}
	}
	return whiteSides >= 3
}
