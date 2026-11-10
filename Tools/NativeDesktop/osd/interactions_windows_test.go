//go:build windows

package osd

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

func TestInputLeaveIsScopedAndHoverReversesWithoutJump(t *testing.T) {
	p := newWindowPresenter(Options{})
	p.hoverFrom = []float64{0, 0}
	p.hover, p.hoverInput, p.hoverStarted = 1, 22, time.Now().Add(-hoverTime)
	if p.leaveInput(11) || p.hover != 1 {
		t.Fatal("stale leave erased a different input target")
	}
	if !p.leaveInput(22) || p.hover != -1 || p.hoverFrom[1] != 1 {
		t.Fatal("current leave did not fade from current highlight")
	}
}

func TestHoverFramesNeverAccumulatePaint(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	for _, dark := range []bool{true, false} {
		r, err := prepare(Request{Title: "PCController", Text: "OSD hover verification", Actions: []Action{{ID: "ok", Label: "Continue"}, {ID: "cancel", Label: "Cancel"}}}, 144, 1920, nil, dark)
		if err != nil {
			t.Fatal(err)
		}
		neutral := append([]byte(nil), r.paint(0, false, nil).Pix...)
		base := append([]byte(nil), r.base.Pix...)
		probe := r.hits[0].Min.Add(image.Pt(15, 12))
		previous := r.frame.RGBAAt(probe.X, probe.Y)
		for step := 1; step <= 12; step++ {
			frame := r.paint(0, false, []float64{float64(step) / 12, 0, float64(step) / 12})
			next := frame.RGBAAt(probe.X, probe.Y)
			if next.A != previous.A {
				t.Fatal("hover changed surface opacity", previous, next)
			}
			previous = next
		}
		if !bytes.Equal(r.paint(0, false, nil).Pix, neutral) || !bytes.Equal(base, r.base.Pix) {
			t.Fatal("hover accumulated overpaint")
		}
		if output := os.Getenv("PCCONTROLLER_OSD_INTERACTION_CAPTURE"); output != "" {
			if !filepath.IsAbs(output) {
				t.Fatal("capture directory must be absolute")
			}
			if err := os.MkdirAll(output, 0700); err != nil {
				t.Fatal(err)
			}
			for _, state := range []struct {
				name   string
				amount float64
			}{{"normal", 0}, {"mid", .5}, {"hover", 1}} {
				theme := "dark"
				if !dark {
					theme = "light"
				}
				f, err := os.Create(filepath.Join(output, "osd-"+theme+"-"+state.name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, r.paint(0, false, []float64{state.amount, 0, state.amount}))
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
		}
		r.close()
	}
}

func TestCloseGlyphInkIsCenteredAtEveryDPIScale(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	for _, dpi := range []int{96, 110, 120, 144, 168, 192, 240, 288, 384} {
		r, err := prepare(Request{Title: "PCController", Text: "Close alignment"}, dpi, 1920*dpi/96, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		ink := glyphInkBounds(r.closeGlyph)
		if r.closeRect.Dx() != r.closeRect.Dy() {
			t.Fatal("close target is not square", dpi, r.closeRect)
		}
		if ink.Empty() || ink != r.closeGlyph.Bounds() {
			t.Fatal("glyph still contains transparent padding", dpi, ink, r.closeGlyph.Bounds())
		}
		placed := ink.Add(r.closeGlyphAt)
		dx := (float64(placed.Min.X+placed.Max.X) - float64(r.closeRect.Min.X+r.closeRect.Max.X)) / 2
		dy := (float64(placed.Min.Y+placed.Max.Y) - float64(r.closeRect.Min.Y+r.closeRect.Max.Y)) / 2
		if math.Abs(dx) > .5 || math.Abs(dy) > .5 || !placed.In(r.closeRect) {
			t.Fatal("visible × not centered", dpi, placed, r.closeRect, dx, dy)
		}
		r.close()
	}
}

func TestOSDFontsUseMeasuredWeightsRatherThanRequestedSubstitutions(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, role := range []struct {
		name     string
		size     int
		semibold bool
	}{{"title", 24, true}, {"body", 21, false}, {"buttons", 21, false}, {"close", 30, false}} {
		font, err := newTextFont(role.size, role.semibold)
		if err != nil {
			t.Fatal(role.name, err)
		}
		dc := win.CreateCompatibleDC(0)
		if dc == 0 {
			win.DeleteObject(win.HGDIOBJ(font))
			t.Fatal("font audit DC")
		}
		old := win.SelectObject(dc, win.HGDIOBJ(font))
		var metrics win.TEXTMETRIC
		ok := win.GetTextMetrics(dc, &metrics)
		win.SelectObject(dc, old)
		win.DeleteDC(dc)
		win.DeleteObject(win.HGDIOBJ(font))
		if !ok {
			t.Fatal("cannot measure selected font", role.name)
		}
		t.Logf("%s actual weight=%d", role.name, metrics.TmWeight)
		if role.semibold {
			if metrics.TmWeight != win.FW_SEMIBOLD && metrics.TmWeight != win.FW_NORMAL {
				t.Fatal("title was substituted with unintended weight", metrics.TmWeight)
			}
		} else if metrics.TmWeight != win.FW_NORMAL {
			t.Fatal("regular role rendered with unintended weight", role.name, metrics.TmWeight)
		}
	}
}

func TestOSDLiveCloseSlidesBeforeHidingAndDoesNotRestart(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("opt-in source-owned input fixture")
	}
	var animate int32 = 1
	if win.SystemParametersInfo(0x1042, 0, unsafe.Pointer(&animate), 0) && animate == 0 {
		t.Skip("system reduced-motion policy")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	p := newWindowPresenter(Options{})
	defer p.Close()
	if _, err := p.Show(context.Background(), Request{ID: "close-fixture", Title: "PCController", Text: "Source-owned close transition", DurationMS: 4000}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(220 * time.Millisecond)
	h := win.HWND(p.hwnd.Load())
	var before win.RECT
	win.GetWindowRect(h, &before)
	var target *inputTarget
	inputWindows.Range(func(_, value any) bool {
		candidate := value.(*inputTarget)
		if candidate.owner == p {
			target = candidate
			return false
		}
		return true
	})
	if target == nil {
		t.Fatal("missing close input")
	}
	local := uintptr(uint16(target.rect.Dx()/2)) | uintptr(uint16(target.rect.Dy()/2))<<16
	// An UP without a matching DOWN must never activate the close target.
	win.SendMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	if !p.Status().Active {
		t.Fatal("unmatched release closed the overlay")
	}
	win.SendMessage(target.hwnd, win.WM_LBUTTONDOWN, 0, local)
	win.SendMessage(target.hwnd, win.WM_CANCELMODE, 0, 0)
	win.SendMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	if !p.Status().Active {
		t.Fatal("cancelled press closed the overlay")
	}
	started := time.Now()
	win.SendMessage(target.hwnd, win.WM_LBUTTONDOWN, 0, local)
	win.SendMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	initial := p.Status()
	if !initial.Active || !win.IsWindowVisible(h) {
		t.Fatal("close hid the surface immediately", initial)
	}
	if !win.IsWindowVisible(target.hwnd) {
		t.Fatal("fading close target lets a second click through to the desktop")
	}
	win.SendMessage(target.hwnd, win.WM_LBUTTONDOWN, 0, local)
	win.SendMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	if !p.Status().ExpiresAt.Equal(initial.ExpiresAt) {
		t.Fatal("double click restarted dismissal")
	}
	time.Sleep(70 * time.Millisecond)
	var during win.RECT
	win.GetWindowRect(h, &during)
	if !p.Status().Active || during.Top <= before.Top {
		t.Fatal("close did not slide down before hiding", before, during, p.Status())
	}
	if _, err = p.Dismiss(context.Background(), "close-fixture"); err != nil {
		t.Fatal(err)
	}
	if !p.Status().ExpiresAt.Equal(initial.ExpiresAt) {
		t.Fatal("repeat dismissal restarted exit")
	}
	deadline := time.Now().Add(time.Second)
	for p.Status().Active && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.Status().Active || p.Status().Dismissal != "dismissed" || time.Since(started) < 120*time.Millisecond || win.IsWindowVisible(h) {
		t.Fatal("close exit did not complete", p.Status(), time.Since(started))
	}
	if _, err = p.Show(context.Background(), Request{Text: "Reduced motion", ReduceMotion: true, DurationMS: 4000}); err != nil {
		t.Fatal(err)
	}
	inputWindows.Range(func(_, value any) bool {
		candidate := value.(*inputTarget)
		if candidate.owner == p {
			target = candidate
			return false
		}
		return true
	})
	win.SendMessage(target.hwnd, win.WM_LBUTTONDOWN, 0, local)
	win.SendMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	if p.Status().Active {
		t.Fatal("reduced-motion dismissal did not finish immediately")
	}
}
