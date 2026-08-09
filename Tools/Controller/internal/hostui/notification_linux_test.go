//go:build linux

package hostui

import (
	"context"
	"errors"
	"testing"
)

func TestLinuxNotifierTracksAcceptedAndDegradedDelivery(t *testing.T) {
	var delivered Notification
	notifier := &linuxNotifier{
		appID: "test.app", tool: "notify-send",
		status: NotificationStatus{Supported: true, Available: true, Backend: "notify-send"},
		deliver: func(_ context.Context, _, _ string, notification Notification) error {
			delivered = notification
			return nil
		},
		gate: make(chan struct{}, 1),
	}
	notification := Notification{Title: "Door open", Body: "Check input", LaunchURI: "pccontroller://page/events"}
	if err := notifier.Notify(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	status := notifier.Status()
	if delivered.Title != notification.Title || status.Accepted != 1 || !status.Available || !status.Degraded {
		t.Fatalf("delivered=%+v status=%+v", delivered, status)
	}
}

func TestLinuxNotifierRetainsDeliveryFailure(t *testing.T) {
	notifier := &linuxNotifier{
		appID: "test.app", tool: "notify-send",
		status: NotificationStatus{Supported: true, Available: true, Backend: "notify-send"},
		deliver: func(context.Context, string, string, Notification) error {
			return errors.New("session bus unavailable")
		},
		gate: make(chan struct{}, 1),
	}
	if err := notifier.Notify(context.Background(), Notification{Title: "Failure"}); err == nil {
		t.Fatal("delivery failure was ignored")
	}
	if status := notifier.Status(); status.Available || status.LastError == "" {
		t.Fatalf("status=%+v", status)
	}
}

func TestParseLoginctlProperties(t *testing.T) {
	values := parseLoginctlProperties("Active=yes\nRemote=no\nType=wayland\nState=active\n")
	if values["Active"] != "yes" || values["Type"] != "wayland" {
		t.Fatalf("properties=%#v", values)
	}
}
