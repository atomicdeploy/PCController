//go:build windows

package hostui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowsNotifierRunnerCanBeInjectedWithoutDisplayingToast(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests"}).(*windowsNotifier)
	called := false
	notifier.deliver = func(context.Context, []byte, string) error { called = true; return nil }
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
	notifier.deliver = func(context.Context, []byte, string) error {
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

func TestWindowsNotifierUsesBoundedNativeFallback(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests"}).(*windowsNotifier)
	nativeErr := errors.New("WinRT unavailable")
	notifier.deliver = func(context.Context, []byte, string) error { return nativeErr }
	fallbackCalled := false
	notifier.fallback = func(_ context.Context, notification Notification, reason error) error {
		fallbackCalled = true
		if notification.Title != "Test" || !errors.Is(reason, nativeErr) {
			t.Fatalf("notification=%+v reason=%v", notification, reason)
		}
		return nil
	}
	if err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "Fallback"}); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if !fallbackCalled || status.Accepted != 1 || status.Backend != "task-dialog" || !status.Degraded {
		t.Fatalf("fallback=%t status=%+v", fallbackCalled, status)
	}
	if status.LastFallback != nativeErr.Error() || status.LastError != "" {
		t.Fatalf("status=%+v", status)
	}
}

func TestWindowsNotifierReportsBothNativeAndFallbackFailures(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests"}).(*windowsNotifier)
	notifier.deliver = func(context.Context, []byte, string) error { return errors.New("native failed") }
	notifier.fallback = func(context.Context, Notification, error) error { return errors.New("fallback failed") }
	err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "Failure"})
	if err == nil || !stringsContainAll(err.Error(), "native failed", "fallback failed") {
		t.Fatalf("Notify error=%v", err)
	}
	if status := notifier.Status(); status.Available || status.Accepted != 0 || status.LastError == "" {
		t.Fatalf("status=%+v", status)
	}
}

func stringsContainAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
