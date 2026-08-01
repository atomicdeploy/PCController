//go:build windows

package hostui

import (
	"context"
	"testing"
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
