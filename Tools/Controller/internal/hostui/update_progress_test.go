package hostui

import (
	"strings"
	"testing"
	"time"
)

func TestUpdateProgressRequiresExplicitMeasuredPercentage(t *testing.T) {
	parsed := ParseUpdateProgress("update.receiving", "receiving", map[string]string{
		"progress_known": "true", "progress_percent": "40",
		"bytes_done": "2567782", "bytes_total": "6419456",
	}, time.Now())
	if parsed.BytesDone != 2567782 || parsed.BytesTotal != 6419456 {
		t.Fatalf("byte counters=%+v", parsed)
	}
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

func TestUpdateNotificationTitlesDescribeActualOperation(t *testing.T) {
	for _, test := range []struct{ kind, label string }{
		{"firmware", "Firmware update"},
		{"device-capture", "Board readback"},
		{"firmware-build", "Firmware build"},
		{"eeprom", "EEPROM update"},
		{"host", "Host update"},
		{"", "Operation"},
		{"future-operation", "Operation"},
	} {
		for _, state := range []string{"completed", "failed", "cancelled", "programming"} {
			t.Run(test.kind+"/"+state, func(t *testing.T) {
				tracker := UpdateNotificationTracker{}
				value := UpdateProgress{OperationID: "operation", Kind: test.kind, State: state, Stage: "flash write:verifying", Detail: "Original error detail"}
				notification, ok := tracker.Next(value)
				if !ok || !strings.HasPrefix(notification.Title, test.label) {
					t.Fatalf("incorrect operation identity: %+v", notification)
				}
				if state == "failed" && (!strings.Contains(notification.Body, "Last stage:") || strings.Contains(notification.Body, "Failed at") || !strings.Contains(notification.Body, value.Detail)) {
					t.Fatalf("failure misattributed to last cleanup stage or detail lost: %+v", notification)
				}
			})
		}
	}
}

func TestPeerHostUpdateMilestonesProduceBoundedToasts(t *testing.T) {
	tracker := UpdateNotificationTracker{}
	for _, test := range []struct {
		state, title, severity string
	}{
		{"queued", "Sending host update to peer", "info"},
		{"artifact-verified", "Peer verified host update", "info"},
		{"reconnecting", "Peer host is restarting", "info"},
		{"health-checking", "Peer host update is health-checking", "info"},
		{"completed", "Host update complete", "success"},
		{"outcome-uncertain", "Host update outcome uncertain", "warning"},
	} {
		value := UpdateProgress{
			OperationID: "peer-operation", Kind: "host", State: test.state,
			Stage: test.state, Detail: "peer update detail",
		}
		notification, ok := tracker.Next(value)
		if !ok || notification.Title != test.title || notification.Severity != test.severity {
			t.Fatalf("state=%s notification=%+v ok=%t", test.state, notification, ok)
		}
		if _, repeated := tracker.Next(value); repeated {
			t.Fatalf("state=%s repeated a peer update toast", test.state)
		}
	}
}
