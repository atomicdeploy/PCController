package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/hostmenu"
)

func TestLegacyPanelAnimatorReloadCancelAndDisconnect(t *testing.T) {
	var mu sync.Mutex
	frames := make([]string, 0, 8)
	disconnected := false
	animator := newLegacyPanelAnimator(func(snapshot hostmenu.Snapshot) error {
		mu.Lock()
		defer mu.Unlock()
		if disconnected {
			return errors.New("device disconnected")
		}
		frames = append(frames, snapshot.MenuID+":"+snapshot.Panel.Segments)
		return nil
	})
	animator.minimumPeriod = 0
	animator.period = 5 * time.Millisecond
	first := hostmenu.Snapshot{MenuID: "first", Panel: hostmenu.Panel{Segments: "ONE ", Blink: true, EditVisual: "blink"}}
	animator.Start(first)
	waitForLegacyFrames(t, &mu, &frames, 2)

	second := hostmenu.Snapshot{MenuID: "second", Panel: hostmenu.Panel{Segments: "TWO ", EditVisual: "edit-dim"}}
	animator.Start(second)
	waitForLegacyFrames(t, &mu, &frames, 4)
	mu.Lock()
	reloadIndex := len(frames) - 2
	for _, frame := range frames[reloadIndex:] {
		if len(frame) < len("second:") || frame[:len("second:")] != "second:" {
			mu.Unlock()
			t.Fatalf("stale animation frame survived reload: %q in %v", frame, frames)
		}
	}
	disconnected = true
	mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	animator.Stop()
	mu.Lock()
	count := len(frames)
	mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(frames) != count {
		t.Fatalf("animation emitted after disconnect/cancel: before=%d after=%d", count, len(frames))
	}
}

func TestLegacyPanelVisualVariants(t *testing.T) {
	panel := hostmenu.Panel{Segments: "EDIT", EditVisual: "edit-dim"}
	if got := legacyPanelSegments(panel, false); got != "----" {
		t.Fatalf("edit-dim fallback=%q", got)
	}
	panel.EditVisual, panel.Blink = "blink", true
	if got := legacyPanelSegments(panel, false); got != "    " {
		t.Fatalf("blink fallback=%q", got)
	}
	if !legacyPanelAnimated(panel) {
		t.Fatal("blink panel was not recognized as animated")
	}
}

func waitForLegacyFrames(t *testing.T, mu *sync.Mutex, frames *[]string, count int) {
	t.Helper()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		ready := len(*frames) >= count
		mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("animation produced %d frames, need %d", len(*frames), count)
}
