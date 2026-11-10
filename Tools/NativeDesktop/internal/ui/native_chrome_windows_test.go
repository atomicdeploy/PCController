//go:build windows

package ui

import (
	"image"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/internal/config"
)

func TestCustomCaptionHitRegions(t *testing.T) {
	layout := surfaceLayout{titleMinimize: walk.Rectangle{X: -90, Y: 10, Width: 26, Height: 26},
		titleZoom:  walk.Rectangle{X: -64, Y: 10, Width: 26, Height: 26},
		titleClose: walk.Rectangle{X: -38, Y: 10, Width: 26, Height: 26},
		titleDrag:  walk.Rectangle{X: -100, Y: 0, Width: 600, Height: 40}}
	for _, test := range []struct{ x, y, want int }{
		{-80, 20, win.HTMINBUTTON}, {-55, 20, win.HTMAXBUTTON}, {-25, 20, win.HTCLOSE},
		{200, 20, win.HTCAPTION}, {200, 80, win.HTCLIENT}, {500, 20, win.HTCLIENT},
	} {
		if got := layout.captionHit(walk.Point{X: test.x, Y: test.y}); got != test.want {
			t.Errorf("caption hit (%d,%d) = %d, want %d", test.x, test.y, got, test.want)
		}
	}
	if got := (surfaceLayout{}).captionHit(walk.Point{}); got != win.HTCLIENT {
		t.Fatalf("empty/unpainted caption hit = %d", got)
	}
}

func chromeMessagePoint(x, y int32) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

// Real, test-owned HWNDs only: no global input injection, host, or board access.
func TestControllerNativeWindowCommandsAndSnapContract(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	cfg.UI.Animations = false // Own material animation; Windows retains its OS policy.
	for _, chrome := range []string{"macos", "windows"} {
		cfg.UI.ControllerChrome = chrome
		cfg.UI.TitleBarMode = config.TitleBarCustom
		s.cfgMu.Lock()
		s.cfg = cfg
		s.cfgMu.Unlock()
		s.applyConfigUI(cfg, false)
		s.mw.Show()
		pumpControllerFixture(s)
		canvas, err := s.surface.CreateCanvas()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.paint(canvas, s.surface.ClientBoundsPixels()); err != nil {
			t.Fatal(err)
		}
		canvas.Dispose()
		hwnd := s.mw.Handle()
		required := int32(win.WS_CAPTION | win.WS_SYSMENU | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX | win.WS_THICKFRAME)
		if style := win.GetWindowLong(hwnd, win.GWL_STYLE); style&required != required {
			t.Fatalf("%s: custom chrome lost Windows capabilities: %#x", chrome, style)
		}
		var outer, client win.RECT
		win.GetWindowRect(hwnd, &outer)
		win.GetClientRect(hwnd, &client)
		if outer.Right-outer.Left != client.Right || outer.Bottom-outer.Top != client.Bottom {
			t.Fatalf("%s: an unwanted native caption/frame became visible: outer=%+v client=%+v", chrome, outer, client)
		}
		for _, button := range []struct {
			rect walk.Rectangle
			hit  int
		}{
			{s.layout.titleMinimize, win.HTMINBUTTON}, {s.layout.titleZoom, win.HTMAXBUTTON},
			{s.layout.titleClose, win.HTCLOSE}, {s.layout.titleDrag, win.HTCAPTION},
		} {
			if button.rect.Width <= 0 {
				t.Fatal("missing painted caption region")
			}
			point := win.POINT{X: int32(button.rect.X + button.rect.Width/2), Y: int32(button.rect.Y + button.rect.Height/2)}
			win.ClientToScreen(s.surface.Handle(), &point)
			packed := chromeMessagePoint(point.X, point.Y)
			if got := int(win.SendMessage(hwnd, win.WM_NCHITTEST, 0, packed)); got != button.hit {
				t.Fatalf("%s: root caption hit = %d, want %d", chrome, got, button.hit)
			}
			for _, child := range []win.HWND{s.surface.Handle(), win.HWND(s.clientHWND)} {
				if got := int32(win.SendMessage(child, win.WM_NCHITTEST, 0, packed)); got != win.HTTRANSPARENT {
					t.Fatalf("%s: child intercepted caption hit: %d", chrome, got)
				}
			}
		}
		// Non-client maximize uses the existing captured release/cancel contract.
		clickZoom := func(cancel bool) {
			t.Helper()
			r := s.layout.titleZoom
			x, y := r.X+r.Width/2, r.Y+r.Height/2
			point := win.POINT{X: int32(x), Y: int32(y)}
			win.ClientToScreen(s.surface.Handle(), &point)
			win.SendMessage(hwnd, win.WM_NCLBUTTONDOWN, win.HTMAXBUTTON, chromeMessagePoint(point.X, point.Y))
			getCapture := windows.NewLazySystemDLL("user32.dll").NewProc("GetCapture")
			capture, _, _ := getCapture.Call()
			if win.HWND(capture) != s.surface.Handle() || s.buttonActivation.pointerAction != actionTitleZoom {
				t.Fatal("custom NC button did not arm/capture its release")
			}
			if cancel {
				x, y = 200, 200
			}
			win.SendMessage(s.surface.Handle(), win.WM_LBUTTONUP, 0, chromeMessagePoint(int32(x), int32(y)))
			pumpControllerFixture(s)
			capture, _, _ = getCapture.Call()
			if s.buttonActivation.pointerAction != actionNone || win.HWND(capture) == s.surface.Handle() {
				t.Fatal("caption activation/capture survived release")
			}
		}
		clickZoom(true)
		if win.IsZoomed(hwnd) {
			t.Fatal("release outside unexpectedly maximized")
		}
		clickZoom(false)
		if !win.IsZoomed(hwnd) {
			t.Fatal("native maximize command was not applied")
		}
		var info win.MONITORINFO
		info.CbSize = uint32(unsafe.Sizeof(info))
		win.GetMonitorInfo(win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST), &info)
		win.GetClientRect(hwnd, &client)
		origin := win.POINT{}
		win.ClientToScreen(hwnd, &origin)
		if origin.X < info.RcWork.Left || origin.Y < info.RcWork.Top ||
			origin.X+client.Right > info.RcWork.Right || origin.Y+client.Bottom > info.RcWork.Bottom {
			t.Fatalf("maximized client escapes monitor work area: origin=%+v client=%+v work=%+v", origin, client, info.RcWork)
		}
		// Transparent hit pixels must use the maximized client origin, not
		// the invisible resize frame's outer origin. No cursor movement needed.
		oldHit, oldBackdrop := s.hitSurface, s.cfg.UI.Backdrop
		s.cfg.UI.Backdrop = config.BackdropTransparent
		s.hitSurface = image.NewRGBA(image.Rect(0, 0, 128, 128))
		s.hitSurface.Pix[s.hitSurface.PixOffset(123, 123)+3] = 255
		pixel := win.POINT{X: 123, Y: 123}
		win.ClientToScreen(s.surface.Handle(), &pixel)
		if s.transparentPixelPassesThrough(chromeMessagePoint(pixel.X, pixel.Y)) ||
			!s.transparentPixelPassesThrough(chromeMessagePoint(pixel.X+1, pixel.Y)) {
			t.Fatal("maximized alpha hit test did not honor surface client coordinates")
		}
		s.hitSurface, s.cfg.UI.Backdrop = oldHit, oldBackdrop
		s.activateSurfaceAction(actionTitleZoom)
		pumpControllerFixture(s)
		if win.IsZoomed(hwnd) {
			t.Fatal("native restore command was not applied")
		}
		var restored win.RECT
		win.GetWindowRect(hwnd, &restored)
		if restored != outer {
			t.Fatalf("restored bounds changed: got=%+v want=%+v", restored, outer)
		}
		s.activateSurfaceAction(actionTitleMinimize)
		pumpControllerFixture(s)
		if !win.IsIconic(hwnd) {
			t.Fatal("native minimize command was not applied")
		}
		win.PostMessage(hwnd, win.WM_SYSCOMMAND, win.SC_RESTORE, 0)
		pumpControllerFixture(s)
		if win.IsIconic(hwnd) {
			t.Fatal("native restore from minimized failed")
		}
		// Caption double-click is owned by DefWindowProc, not an application
		// imitation of maximize. This is also the system caption drag surface.
		drag := s.layout.titleDrag
		caption := win.POINT{X: int32(drag.X + drag.Width/2), Y: int32(drag.Y + drag.Height/2)}
		win.ClientToScreen(s.surface.Handle(), &caption)
		win.SendMessage(hwnd, win.WM_NCLBUTTONDBLCLK, win.HTCAPTION, chromeMessagePoint(caption.X, caption.Y))
		pumpControllerFixture(s)
		if !win.IsZoomed(hwnd) {
			t.Fatal("system caption double-click did not maximize")
		}
		win.PostMessage(hwnd, win.WM_SYSCOMMAND, win.SC_RESTORE, 0)
		pumpControllerFixture(s)
		// Geometry/state settle before the compositor's restore transition ends.
		// Do not crop a screen capture around the final bounds mid-transition.
		timer := time.AfterFunc(600*time.Millisecond, func() { s.mw.Synchronize(func() { win.PostQuitMessage(0) }) })
		s.mw.Run()
		timer.Stop()
		captureControllerFixture(t, s, "native-window-commands-"+chrome)
		t.Logf("%s: native style, caption routing, cancel/release, minimize/maximize/restore, work-area bounds verified", chrome)
	}
}
