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
