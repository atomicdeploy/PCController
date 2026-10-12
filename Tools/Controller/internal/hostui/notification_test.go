package hostui

import (
	"strings"
	"testing"
)

func TestToastXMLIsEscapedAndActionsAreProtocolActivated(t *testing.T) {
	payload, err := buildToastXML(Notification{
		Title: "Door <OPEN>", Body: "R5 & R6 changed",
		LaunchURI: "pccontroller://page/events",
		Actions:   []NotificationAction{{Label: "Open & inspect", URI: "pccontroller://page/outputs"}},
	}, "file:///C:/Program%20Files/PCController/toast-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	value := string(payload)
	for _, expected := range []string{"Door &lt;OPEN&gt;", "R5 &amp; R6", `<toast launch="pccontroller://page/events" activationType="protocol">`, "pccontroller://page/outputs", `placement="appLogoOverride"`, `src="file:///C:/Program%20Files/PCController/toast-logo.png"`} {
		if !strings.Contains(value, expected) {
			t.Errorf("toast XML missing %q: %s", expected, value)
		}
	}
}

func TestToastRejectsUntrustedActionSchemes(t *testing.T) {
	_, err := buildToastXML(Notification{
		Title: "test", Actions: []NotificationAction{{Label: "run", URI: "file:///tmp/test"}},
	}, "")
	if err == nil {
		t.Fatal("file action URI unexpectedly accepted")
	}
}

func TestToastRejectsRemoteOrNonFileLogo(t *testing.T) {
	for _, logo := range []string{"https://example.invalid/logo.png", "file://server/share/logo.png"} {
		if _, err := buildToastXML(Notification{Title: "test"}, logo); err == nil {
			t.Fatalf("logo URI %q unexpectedly accepted", logo)
		}
	}
}

func TestImportantEventMappingSuppressesTelemetryAndAddsSafetyAction(t *testing.T) {
	t.Setenv("APP_TITLE", "")
	if _, ok := NotificationForImportantEvent(ImportantEvent{Kind: "telemetry", Message: "status"}); ok {
		t.Fatal("routine telemetry generated a notification")
	}
	notification, ok := NotificationForImportantEvent(ImportantEvent{Kind: "motion.fault", Message: "Side A timeout"})
	if !ok || len(notification.Actions) != 2 || notification.Actions[1].Label != "Stop outputs" {
		t.Fatalf("motion notification=%#v ok=%t", notification, ok)
	}
	warning, ok := NotificationForImportantEvent(ImportantEvent{
		Kind: "warning.door-open-running", Message: "door opened while Running",
	})
	if !ok || warning.Title != "PCController · Door open during operation" ||
		len(warning.Actions) != 2 || warning.Actions[1].Label != "Stop outputs" {
		t.Fatalf("door-running notification=%#v ok=%t", warning, ok)
	}
	custom, ok := NotificationForImportantEvent(ImportantEvent{
		Kind: "motion.fault", Message: "Side B timeout", AppTitle: "Workshop Controller",
	})
	if !ok || custom.Title != "Workshop Controller · MOTION.FAULT" {
		t.Fatalf("custom-title notification=%#v ok=%t", custom, ok)
	}
}

func TestImportantEventsSelectSemanticToastIcons(t *testing.T) {
	tests := []struct {
		kind    string
		message string
		want    string
	}{
		{kind: "hot", message: "temperature alert active", want: NotificationIconHot},
		{kind: "hot", message: "temperature alert cleared", want: NotificationIconTemperatureCleared},
		{kind: "warning.door-open-running", message: "door opened while Running", want: NotificationIconDoor},
		{kind: "motion.fault", message: "Side A timeout", want: NotificationIconMotion},
		{kind: "relay.changed", message: "R1 on", want: NotificationIconRelay},
		{kind: "rf.learn.ended", message: "learning ended", want: NotificationIconRadio},
		{kind: "error", message: "controller failed", want: NotificationIconError},
	}
	for _, test := range tests {
		notification, ok := NotificationForImportantEvent(ImportantEvent{Kind: test.kind, Message: test.message})
		if !ok || notification.Icon != test.want {
			t.Errorf("kind=%q message=%q icon=%q ok=%t; want %q", test.kind, test.message, notification.Icon, ok, test.want)
		}
	}
}

func TestMessageNotificationsSelectSeverityIcon(t *testing.T) {
	notification, err := NotificationForMessage(MessageNotification{ID: 1, Text: "maintenance required", Severity: "warning"})
	if err != nil {
		t.Fatal(err)
	}
	if notification.Icon != NotificationIconWarning {
		t.Fatalf("message icon=%q; want %q", notification.Icon, NotificationIconWarning)
	}
}
