//go:build windows

package ui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"pccontroller.local/desktop/presentation"
)

func TestControllerMotionReleasesPendingOwnedPress(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.controllerState.Connected = true
	s.controllerState.Generation = 7
	s.controllerState.Seats = []presentation.Seat{{Key: "seat.a", Name: "Seat A", Side: 1, Known: true, State: "Stopped", StopAction: "seat.a.stop"}}
	ack := make(chan struct{})
	released := make(chan struct{}, 4)
	var mu sync.Mutex
	starts, stops, renews := 0, 0, 0
	s.controller.MotionHold = func(ctx context.Context, phase string, lease presentation.MotionHoldLease, _ string) (presentation.MotionHoldLease, error) {
		mu.Lock()
		if lease.Generation != 7 || lease.Side != 1 || lease.ID == "" {
			t.Error("motion request lost press owner")
		}
		switch phase {
		case "start":
			starts++
		case "release":
			stops++
		case "renew":
			renews++
		}
		mu.Unlock()
		if phase == "start" {
			select {
			case <-ack:
			case <-ctx.Done():
				return lease, ctx.Err()
			}
		}
		if phase == "release" {
			released <- struct{}{}
		}
		return lease, nil
	}
	s.mw.Show()
	watchdog := time.AfterFunc(8*time.Second, func() { s.mw.Synchronize(func() { t.Error("motion fixture timed out"); win.PostQuitMessage(0) }) })
	defer watchdog.Stop()
	s.openControllerMotionObserved(func(dialog *walk.Dialog) {
		up := controllerFixtureButton(dialog, "Hold Up")
		down := controllerFixtureButton(dialog, "Hold Down")
		if up == nil || down == nil || !up.Enabled() || up.Size().Width != down.Size().Width {
			t.Error("motion controls missing or uneven")
		}
		time.AfterFunc(40*time.Millisecond, func() {
			dialog.Synchronize(func() {
				win.SendMessage(up.Handle(), win.WM_LBUTTONDOWN, win.MK_LBUTTON, uintptr(10|10<<16))
				time.AfterFunc(40*time.Millisecond, func() {
					dialog.Synchronize(func() { win.SendMessage(up.Handle(), win.WM_LBUTTONUP, 0, uintptr(10|10<<16)) })
				})
			})
		})
		go func() {
			select {
			case <-released:
				close(ack)
			case <-time.After(3 * time.Second):
				t.Error("release waited for delayed start acknowledgement")
				close(ack)
			}
			time.AfterFunc(80*time.Millisecond, func() { dialog.Synchronize(func() { dialog.Cancel() }) })
		}()
	})
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || stops < 1 || renews != 0 {
		t.Fatalf("pending press lifecycle: start=%d stop=%d renew=%d", starts, stops, renews)
	}
}
