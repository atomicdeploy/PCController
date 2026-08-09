//go:build linux

package hostui

import (
	"context"
	"errors"
	"reflect"
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

func TestActiveLinuxGraphicalSessionRequiresPhysicalSeatUser(t *testing.T) {
	originalRun := linuxNotifyRun
	t.Cleanup(func() { linuxNotifyRun = originalRun })
	linuxNotifyRun = func(_ context.Context, _ []string, name string, arguments ...string) ([]byte, error) {
		if name != "loginctl" {
			return nil, errors.New("unexpected command")
		}
		if arguments[0] == "list-sessions" {
			return []byte("10 1000 remote seat0\n11 1001 nested seat1\n12 1000 asus seat0\n"), nil
		}
		switch arguments[1] {
		case "10":
			return []byte("Active=yes\nRemote=yes\nType=x11\nState=active\nSeat=seat0\nClass=user\nUser=1000\nName=remote\n"), nil
		case "11":
			return []byte("Active=yes\nRemote=no\nType=wayland\nState=active\nSeat=seat1\nClass=user\nUser=1001\nName=nested\n"), nil
		case "12":
			return []byte("Active=yes\nRemote=no\nType=wayland\nState=active\nSeat=seat0\nClass=user\nUser=1000\nName=asus\n"), nil
		default:
			return nil, errors.New("unknown session")
		}
	}

	session, err := activeLinuxGraphicalSession(context.Background())
	if err != nil || session.id != "12" || session.uid != 1000 || session.user != "asus" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
}

func TestLinuxNotificationIgnoresInheritedBusAndRoutesToPhysicalUser(t *testing.T) {
	originalRun, originalEUID := linuxNotifyRun, linuxNotifyEUID
	t.Cleanup(func() { linuxNotifyRun, linuxNotifyEUID = originalRun, originalEUID })
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/0/stale-bus")
	linuxNotifyEUID = func() int { return 0 }
	type invocation struct {
		name      string
		arguments []string
	}
	var delivered invocation
	linuxNotifyRun = func(_ context.Context, _ []string, name string, arguments ...string) ([]byte, error) {
		if name == "loginctl" && arguments[0] == "list-sessions" {
			return []byte("7 1000 asus seat0\n"), nil
		}
		if name == "loginctl" && arguments[0] == "show-session" {
			return []byte("Active=yes\nRemote=no\nType=wayland\nState=active\nSeat=seat0\nClass=user\nUser=1000\nName=asus\n"), nil
		}
		delivered = invocation{name: name, arguments: append([]string(nil), arguments...)}
		return nil, nil
	}

	err := deliverLinuxNotification(
		context.Background(), "/usr/bin/notify-send", "test.app", Notification{Title: "Ready"},
	)
	wantPrefix := []string{
		"-u", "asus", "--", "env", "XDG_RUNTIME_DIR=/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus", "/usr/bin/notify-send",
	}
	if err != nil || delivered.name != "runuser" || len(delivered.arguments) < len(wantPrefix) ||
		!reflect.DeepEqual(delivered.arguments[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("delivery=%+v err=%v", delivered, err)
	}
}
