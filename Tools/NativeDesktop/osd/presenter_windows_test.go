//go:build windows

package osd

import (
	"context"
	"fmt"
	"github.com/lxn/win"
	"image"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOSDLiveWindowUsesNativeDPI(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("opt-in source-owned window verification")
	}
	getWindowContext := osdUser.NewProc("GetWindowDpiAwarenessContext")
	getAwareness := osdUser.NewProc("GetAwarenessFromDpiAwarenessContext")
	getThreadContext := osdUser.NewProc("GetThreadDpiAwarenessContext")
	if getWindowContext.Find() != nil || getAwareness.Find() != nil || getThreadContext.Find() != nil {
		t.Skip("per-thread DPI contexts require Windows 10 1607")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callerBefore, _, _ := getThreadContext.Call()
	beforeAwareness, _, _ := getAwareness.Call(callerBefore)
	p := newWindowPresenter(Options{})
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := p.Show(ctx, Request{Title: "Native DPI verification", Text: "Rendering at the display's physical resolution", DurationMS: 1000}); err != nil {
		t.Fatal(err)
	}
	h := win.HWND(p.hwnd.Load())
	windowContext, _, _ := getWindowContext.Call(uintptr(h))
	awareness, _, _ := getAwareness.Call(windowContext)
	dpi := win.GetDpiForWindow(h)
	t.Logf("caller awareness=%d; OSD awareness=%d; OSD DPI=%d", beforeAwareness, awareness, dpi)
	if awareness != 2 {
		t.Fatalf("OSD is not per-monitor DPI-aware: %d (Windows would bitmap-scale it)", awareness)
	}
	callerAfter, _, _ := getThreadContext.Call()
	if callerAfter != callerBefore {
		t.Fatal("OSD changed the caller's DPI context")
	}
	restoreDPI, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restoreDPI()
	var rect win.RECT
	win.GetClientRect(h, &rect)
	expected := int32(540 * dpi / 96)
	if rect.Right > expected || rect.Right < expected-1 {
		t.Fatalf("OSD backing width is not native-scaled: got %d, expected %d", rect.Right, expected)
	}
}

func TestOSDDPIContextIsScopedAndRestored(t *testing.T) {
	getContext := osdUser.NewProc("GetThreadDpiAwarenessContext")
	getAwareness := osdUser.NewProc("GetAwarenessFromDpiAwarenessContext")
	if getContext.Find() != nil || getAwareness.Find() != nil {
		t.Skip("per-thread DPI contexts require Windows 10 1607")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	original, _, _ := setThreadDPIContext.Call(^uintptr(0)) // DPI-unaware caller.
	if original == 0 {
		t.Fatal("cannot establish DPI-unaware fixture")
	}
	defer setThreadDPIContext.Call(original)
	before, _, _ := getContext.Call()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := getContext.Call()
	awareness, _, _ := getAwareness.Call(current)
	restore()
	after, _, _ := getContext.Call()
	if awareness != 2 || after != before {
		t.Fatalf("DPI scope failed: native awareness=%d, restored=%t", awareness, after == before)
	}
}

func TestOSDLiveActionClickIsExplicitAndSingleShot(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("opt-in source-owned window verification")
	}
	// Match the window's coordinate space when calculating synthetic client input.
	// A DPI-unaware test caller would otherwise receive virtualized dimensions.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restoreDPI()
	clicked := make(chan string, 4)
	p := newWindowPresenter(Options{Activate: func(uri string) error { clicked <- uri; return nil }})
	defer p.Close()
	r := Request{Title: "OSD action verification", Text: "Source-owned input fixture", DurationMS: 2000, Actions: []Action{{Label: "Events", URI: "pccontroller://page/events"}, {Label: "Preferences", URI: "pccontroller://page/settings"}}}
	if _, err := p.Show(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	select {
	case uri := <-clicked:
		t.Fatal("action ran at submission", uri)
	default:
	}
	h := win.HWND(p.hwnd.Load())
	var rect win.RECT
	win.GetClientRect(h, &rect)
	scale := float64(win.GetDpiForWindow(h)) / 96
	cell := min(int(156*scale), (int(rect.Right)-int(48*scale)-int(8*scale))/2)
	x := int(rect.Right) - int(24*scale) - 2*cell - int(8*scale) + cell/2
	y := int(103 * scale)
	point := uintptr(uint16(x)) | uintptr(uint16(y))<<16
	if win.GetWindowLong(h, win.GWL_EXSTYLE)&0x20 == 0 {
		t.Fatal("visual plane is not cross-thread click-through")
	}
	clickInput := func() {
		var target *inputTarget
		inputWindows.Range(func(_, value any) bool {
			candidate := value.(*inputTarget)
			if candidate.owner == p && image.Pt(x, y).In(candidate.rect) {
				target = candidate
				return false
			}
			return true
		})
		if target == nil {
			t.Fatal("interactive target is missing")
		}
		if win.GetWindowLong(target.hwnd, win.GWL_EXSTYLE)&0x20 != 0 {
			t.Fatal("button target also passes clicks through")
		}
		local := uintptr(uint16(x-target.rect.Min.X)) | uintptr(uint16(y-target.rect.Min.Y))<<16
		win.PostMessage(target.hwnd, win.WM_MOUSEMOVE, 0, local)
		win.PostMessage(target.hwnd, win.WM_LBUTTONDOWN, 0, local)
		win.PostMessage(target.hwnd, win.WM_LBUTTONUP, 0, local)
	}
	clickInput()
	select {
	case uri := <-clicked:
		if uri != r.Actions[0].URI {
			t.Fatal(uri)
		}
	case <-time.After(time.Second):
		t.Fatal("action button did not activate")
	}
	win.PostMessage(h, win.WM_LBUTTONUP, 0, point)
	select {
	case uri := <-clicked:
		t.Fatal("duplicate activation", uri)
	case <-time.After(50 * time.Millisecond):
	}
	if p.Status().Active {
		t.Fatal("activated overlay remained on screen")
	}
	first := p.Status()
	r.ID = "script-choice"
	r.Actions = []Action{{ID: "yes", Label: "Continue"}, {ID: "no", Label: "Cancel"}}
	if _, err := p.Show(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	clickInput()
	deadline := time.Now().Add(time.Second)
	for p.Status().Active && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	choice := p.Status()
	if choice.Active || choice.Dismissal != "action" || choice.LastActionID != "yes" {
		t.Fatal(choice)
	}
	select {
	case uri := <-clicked:
		t.Fatal("script choice launched a URI", uri)
	default:
	}
	r.ID = "later-overlay"
	if _, err := p.Show(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if saved, ok := p.Result("script-choice"); !ok || saved.LastActionID != "yes" || saved.Dismissal != "action" {
		t.Fatal("lost choice receipt", saved, ok)
	}
	if saved, ok := p.Result(first.ID); !ok || saved.Dismissal != "action" {
		t.Fatal("lost earlier receipt", saved, ok)
	}
}

func TestOSDMultilineLayoutAndIndependentMarquee(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	r, err := prepare(Request{Title: "Heading", Text: "Short line\n" + strings.Repeat("Long line ", 40), Actions: []Action{{ID: "ok", Label: "OK"}}}, 144, 1920, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if len(r.bodies) != 2 || r.bodyRects[1].Min.Y-r.bodyRects[0].Min.Y != 36 || r.hits[0].Min.Y < r.bodyRects[1].Max.Y {
		t.Fatal("multiline body/actions overlap", r.bodyRects, r.hits)
	}
	offsets := r.offsets(2100*time.Millisecond, false)
	if len(offsets) != 3 || offsets[1] != 0 || offsets[2] <= 0 {
		t.Fatal("lines do not scroll independently", offsets)
	}
}

func TestOSDReceiptHistoryIsBounded(t *testing.T) {
	p := newWindowPresenter(Options{})
	for i := 0; i < 70; i++ {
		p.state = Status{ID: fmt.Sprint(i), Active: true}
		p.rememberLocked("expired")
	}
	if len(p.history) != 64 {
		t.Fatal(len(p.history))
	}
	if _, ok := p.Result("0"); ok {
		t.Fatal("old receipt not evicted")
	}
	if saved, ok := p.Result("68"); !ok || saved.Dismissal != "expired" {
		t.Fatal(saved, ok)
	}
}
