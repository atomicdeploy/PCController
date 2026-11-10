//go:build windows

package ui

import (
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/config"
)

func TestControllerCustomFrameOwnsActivationPaint(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	for _, title := range []string{config.TitleBarNone, config.TitleBarCustom, config.TitleBarNative} {
		cfg.UI.TitleBarMode = title
		s.cfg = cfg
		for _, message := range []uintptr{win.WM_NCPAINT, wmNCUAHDrawCaption, wmNCUAHDrawFrame} {
			_, handled := s.windowMessage(uintptr(s.mw.Handle()), message, 1, 0)
			if handled != (title != config.TitleBarNative) {
				t.Fatalf("title=%s message=%#x: native paint handled=%v", title, message, handled)
			}
		}
		if title != config.TitleBarNative {
			for _, active := range []uintptr{0, 1} {
				result, handled := s.windowMessage(uintptr(s.mw.Handle()), wmNCActivate, active, 0)
				if !handled || result != 1 {
					t.Fatal("custom activation must allow focus change without default frame painting")
				}
			}
		}
	}
}

// Sample the actual visible pixels synchronously, without PrintWindow or a
// forced client repaint that could conceal a transient classic frame.
func activationFrameIsWhite(t *testing.T, hwnd win.HWND) bool {
	t.Helper()
	var bounds win.RECT
	if !win.GetWindowRect(hwnd, &bounds) {
		t.Fatal("read activation fixture bounds")
	}
	dc := win.GetDC(0)
	if dc == 0 {
		t.Fatal("acquire screen DC for activation fixture")
	}
	defer win.ReleaseDC(0, dc)
	// One screen readback, then sample CPU/GDI memory. Per-pixel reads from
	// the desktop can each wait for the compositor and stall the UI thread.
	width, height := bounds.Right-bounds.Left, bounds.Bottom-bounds.Top
	memory := win.CreateCompatibleDC(dc)
	if memory == 0 {
		t.Fatal("create activation capture DC")
	}
	defer win.DeleteDC(memory)
	bitmap := win.CreateCompatibleBitmap(dc, width, height)
	if bitmap == 0 {
		t.Fatal("create activation capture bitmap")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(memory, win.HGDIOBJ(bitmap))
	defer win.SelectObject(memory, previous)
	if !win.BitBlt(memory, 0, 0, width, height, dc, bounds.Left, bounds.Top, win.SRCCOPY) {
		t.Fatal("capture activation fixture pixels")
	}
	whiteSides := 0
	for side := 0; side < 4; side++ {
		white, total := 0, 0
		for fraction := int32(20); fraction < 80; fraction++ {
			for inset := int32(3); inset <= 5; inset++ {
				x, y := bounds.Left+(bounds.Right-bounds.Left)*fraction/100, bounds.Top+inset
				switch side {
				case 1:
					y = bounds.Bottom - 1 - inset
				case 2:
					x, y = bounds.Left+inset, bounds.Top+(bounds.Bottom-bounds.Top)*fraction/100
				case 3:
					x, y = bounds.Right-1-inset, bounds.Top+(bounds.Bottom-bounds.Top)*fraction/100
				}
				color := win.GetPixel(memory, x-bounds.Left, y-bounds.Top)
				r, g, b := color&255, (color>>8)&255, (color>>16)&255
				if min(r, g, b) >= 200 && max(r, g, b)-min(r, g, b) <= 30 {
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

func TestControllerActivationDoesNotFlashClassicBorder(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	peer, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Dispose()
	if err := peer.SetLayout(walk.NewVBoxLayout()); err != nil {
		t.Fatal(err)
	}
	peer.SetTitle("PCController activation peer fixture")
	peer.SetBoundsPixels(walk.Rectangle{X: 780, Y: 80, Width: 300, Height: 220})
	peer.Show()
	for _, backend := range []string{"auto", "legacy"} {
		cfg.UI.TitleBarMode, cfg.UI.RenderBackend = config.TitleBarCustom, backend
		s.cfg = cfg
		s.applyConfigUI(cfg, false)
		s.mw.Show()
		win.SetWindowPos(s.mw.Handle(), win.HWND_TOPMOST, 80, 80, 640, 720, win.SWP_NOACTIVATE)
		pumpControllerFixture(s)
		canvas, err := s.surface.CreateCanvas()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.paint(canvas, s.surface.ClientBoundsPixels()); err != nil {
			t.Fatal(err)
		}
		canvas.Dispose()
		for cycle := 0; cycle < 3; cycle++ {
			for _, active := range []win.HWND{s.mw.Handle(), peer.Handle()} {
				win.SetActiveWindow(active)
				if win.GetActiveWindow() != active {
					t.Fatal("custom paint guard blocked the native focus transition")
				}
				for _, message := range []uint32{wmNCUAHDrawCaption, wmNCUAHDrawFrame} {
					win.SendMessage(s.mw.Handle(), message, 1, 0)
				}
				if activationFrameIsWhite(t, s.mw.Handle()) {
					t.Fatalf("%s: classic border at activation before repaint", backend)
				}
				pumpControllerFixture(s)
				if activationFrameIsWhite(t, s.mw.Handle()) {
					t.Fatalf("%s: classic border at activation after dispatch", backend)
				}
			}
		}
		// Still own frame drawing even if the ordinary posted cleanup is delayed.
		for _, active := range []uintptr{0, 1} {
			win.SendMessage(s.mw.Handle(), wmNCActivate, active, 0)
			time.Sleep(75 * time.Millisecond)
			if activationFrameIsWhite(t, s.mw.Handle()) {
				t.Fatal("classic border while deferred cleanup is blocked")
			}
		}
		captureControllerFixture(t, s, "activation-no-classic-frame-"+backend)
	}
}
