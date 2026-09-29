package hostui

import (
	"strings"
	"testing"
	"time"
)

func TestUpdateProgressRequiresExplicitMeasuredPercentage(t *testing.T) {
	for _, known := range []string{"", "false", "true"} {
		value := ParseUpdateProgress("update.programming", "preparing", map[string]string{"stage": "flash write:writing", "progress_known": known, "progress_percent": "40"}, time.Now())
		want := 3
		if known == "true" {
			want = 1
		}
		if value.Terminal().State != want {
			t.Fatalf("known=%q terminal=%+v", known, value.Terminal())
		}
	}
	for _, state := range []string{"completed", "failed", "cancelled", "idle", "staged", "downloaded"} {
		value := UpdateProgress{State: state, Known: true, Percent: 40}
		progress := value.Terminal()
		if value.Active() || progress.Percent != 0 || progress.State == 1 || progress.State == 3 {
			t.Fatalf("terminal state retained activity: %s %+v", state, progress)
		}
	}
}

func TestUpdateNotificationsCoalesceMeasuredTicksAndMinorStages(t *testing.T) {
	tracker := UpdateNotificationTracker{}
	value := UpdateProgress{OperationID: "upload-one", Kind: "firmware", State: "programming", Stage: "flash", Detail: "Writing"}
	if _, ok := tracker.Next(value); !ok {
		t.Fatal("flash milestone absent")
	}
	for index := 0; index < 100; index++ {
		value.Stage, value.Percent = "flash write:writing", index
		if _, ok := tracker.Next(value); ok {
			t.Fatal("percentage tick created another toast")
		}
	}
	value.Stage = "reconnect"
	if _, ok := tracker.Next(value); ok {
		t.Fatal("minor stage created toast")
	}
	value.State, value.Detail = "failed", "Handshake timed out. Reconnect the USB cable."
	notification, ok := tracker.Next(value)
	if !ok || !strings.Contains(notification.Body, "reconnect") || notification.Severity != "error" {
		t.Fatalf("missing actionable failure: %+v", notification)
	}
	if !strings.HasSuffix(notification.LaunchURI, "/updates") {
		t.Fatalf("notification targets an unknown page: %s", notification.LaunchURI)
	}
	if _, ok := tracker.Next(value); ok {
		t.Fatal("failure repeated")
	}
	value.OperationID = "upload-two"
	if _, ok := tracker.Next(value); !ok {
		t.Fatal("new operation failure suppressed")
	}
}
