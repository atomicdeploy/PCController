//go:build windows

package hostui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowsNotifierRunnerCanBeInjectedWithoutDisplayingToast(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests"}).(*windowsNotifier)
	called := false
	notifier.run = func(context.Context, string, ...string) error { called = true; return nil }
	if err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "No real toast"}); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if !called || status.Accepted != 1 || !status.Available {
		t.Fatalf("called=%t status=%#v", called, status)
	}
}

func TestWindowsNotifierSerializesNativeDelivery(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests"}).(*windowsNotifier)
	var active atomic.Int32
	var maximum atomic.Int32
	notifier.run = func(context.Context, string, ...string) error {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return nil
	}
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "bounded"}); err != nil {
				t.Errorf("notify: %v", err)
			}
		}()
	}
	wait.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent native deliveries=%d; want 1", got)
	}
	if status := notifier.Status(); status.Accepted != 8 {
		t.Fatalf("status=%#v", status)
	}
}
