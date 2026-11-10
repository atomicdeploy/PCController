//go:build windows

package osd

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/win"
)

func TestOSDLiveStackAndReplaceKeepIndependentReceipts(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("opt-in source-owned windows")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	attentionCalls := 0
	m := newPresenter(Options{Attention: func(r Request) AttentionStatus {
		attentionCalls++
		if r.Sound != "success" || !r.FlashTaskbar {
			t.Fatal(r)
		}
		return AttentionStatus{Sound: "requested", Taskbar: "no-window"}
	}}).(*manager)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before := win.GetForegroundWindow()
	for _, id := range []string{"stack-one", "stack-two"} {
		if _, err = m.Show(ctx, Request{ID: id, Title: "Stack verification", Text: "Source-owned overlay", Behavior: "stack", DurationMS: 4000, Sound: "success", FlashTaskbar: true}); err != nil {
			t.Fatal(err)
		}
	}
	status := m.Status()
	if status.ActiveCount != 2 || len(status.Items) != 2 {
		t.Fatal(status)
	}
	if attentionCalls != 2 || status.Attention == nil || status.Attention.Sound != "requested" {
		t.Fatal(attentionCalls, status)
	}
	if _, err = m.Show(ctx, Request{Text: "invalid", Sound: "file:///unsafe.wav"}); err == nil {
		t.Fatal("invalid sound accepted")
	}
	if attentionCalls != 2 {
		t.Fatal("failed admission replayed attention")
	}
	m.mu.Lock()
	var rects [2]win.RECT
	for i, e := range m.active {
		win.GetWindowRect(win.HWND(e.window.hwnd.Load()), &rects[i])
	}
	m.mu.Unlock()
	if rects[1].Bottom > rects[0].Top {
		t.Fatal("stacked windows overlap", rects)
	}
	if win.GetForegroundWindow() != before {
		t.Fatal("stack stole focus")
	}
	if _, err = m.Dismiss(ctx, "stack-one"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for m.Status().ActiveCount != 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s, ok := m.Result("stack-one"); !ok || s.Active || s.Dismissal != "dismissed" || s.Attention == nil || s.Attention.Taskbar != "no-window" {
		t.Fatal(s, ok)
	}
	if _, err = m.Show(ctx, Request{ID: "replacement", Text: "Replacement", DurationMS: 1000}); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.ActiveCount != 1 || s.ID != "replacement" {
		t.Fatal(s)
	}
	if s, ok := m.Result("stack-two"); !ok || s.Active || s.Dismissal != "replaced" {
		t.Fatal(s, ok)
	}
	if attentionCalls != 2 {
		t.Fatal("status, layout, dismissal or silent replacement replayed attention")
	}
}

func TestOSDBackgroundAlphaAndStackGeometry(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	var alpha []byte
	for _, background := range []string{"current", "transparent", "acrylic"} {
		r, e := prepare(Request{Text: "Text", Background: background}, 144, 1920, nil, true)
		if e != nil {
			t.Fatal(e)
		}
		alpha = append(alpha, r.base.RGBAAt(30, 120).A)
		r.close()
	}
	if alpha[0] == 0 || alpha[1] != 0 || alpha[2] == 0 || alpha[2] >= alpha[0] {
		t.Fatal("background alpha modes are indistinguishable", alpha)
	}
	if stackOffset("center", 0, 330, 150, 15) != -90 || stackOffset("center", 165, 330, 165, 15) != 83 || stackOffset("bottom-center", 165, 330, 165, 15) != 165 {
		t.Fatal("stack anchor math changed")
	}
}
