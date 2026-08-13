//go:build windows

package hostui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestWindowsNotifier(t *testing.T) *windowsNotifier {
	t.Helper()
	logo := filepath.Join(t.TempDir(), ToastLogoFileName)
	if err := os.WriteFile(logo, append([]byte("\x89PNG\r\n\x1a\n"), 0), 0o600); err != nil {
		t.Fatal(err)
	}
	return newPlatformNotifier(NotifierOptions{AppID: "PCController.Tests", LogoPath: logo}).(*windowsNotifier)
}

func TestWindowsNotifierRunnerCanBeInjectedWithoutDisplayingToast(t *testing.T) {
	notifier := newTestWindowsNotifier(t)
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
	notifier := newTestWindowsNotifier(t)
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
	notifier := newTestWindowsNotifier(t)
	nativeErr := errors.New("WinRT unavailable")
	notifier.deliver = func(context.Context, []byte, string) error { return nativeErr }
	balloonCalled := false
	notifier.balloon = func(_ context.Context, notification Notification, reason error) error {
		balloonCalled = true
		if notification.Title != "Test" || !errors.Is(reason, nativeErr) {
			t.Fatalf("notification=%+v reason=%v", notification, reason)
		}
		return nil
	}
	if err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "Fallback"}); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if !balloonCalled || status.Accepted != 1 || status.Backend != "legacy-balloon" || !status.Degraded || !status.Branded {
		t.Fatalf("balloon=%t status=%+v", balloonCalled, status)
	}
	if status.LastFallback != nativeErr.Error() || status.LastError != "" {
		t.Fatalf("status=%+v", status)
	}
}

func TestWindowsNotifierReportsBothNativeAndFallbackFailures(t *testing.T) {
	notifier := newTestWindowsNotifier(t)
	notifier.deliver = func(context.Context, []byte, string) error { return errors.New("native failed") }
	notifier.balloon = func(context.Context, Notification, error) error { return errors.New("balloon failed") }
	notifier.dialog = func(context.Context, Notification, error) error { return errors.New("dialog failed") }
	err := notifier.Notify(context.Background(), Notification{Title: "Test", Body: "Failure"})
	if err == nil || !stringsContainAll(err.Error(), "native failed", "balloon failed", "dialog failed") {
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

func TestWindowsNotifierUsesBrandedBalloonWhenLogoIsUnavailable(t *testing.T) {
	notifier := newPlatformNotifier(NotifierOptions{
		AppID: "PCController.Tests", LogoPath: filepath.Join(t.TempDir(), "missing.png"),
	}).(*windowsNotifier)
	delivered := false
	notifier.deliver = func(context.Context, []byte, string) error { delivered = true; return nil }
	notifier.balloon = func(context.Context, Notification, error) error { return nil }
	if err := notifier.Notify(context.Background(), Notification{Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if delivered || !status.Branded || !status.Degraded || status.Backend != "legacy-balloon" {
		t.Fatalf("delivered=%t status=%+v", delivered, status)
	}
}

func TestWindowsNotifierFallsBackFromBalloonToTaskDialog(t *testing.T) {
	notifier := newTestWindowsNotifier(t)
	notifier.deliver = func(context.Context, []byte, string) error { return errors.New("native failed") }
	notifier.balloon = func(context.Context, Notification, error) error { return errors.New("balloon failed") }
	dialogCalled := false
	notifier.dialog = func(context.Context, Notification, error) error { dialogCalled = true; return nil }
	if err := notifier.Notify(context.Background(), Notification{Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if !dialogCalled || status.Branded || !status.Degraded || status.Backend != "task-dialog" {
		t.Fatalf("dialog=%t status=%+v", dialogCalled, status)
	}
}
