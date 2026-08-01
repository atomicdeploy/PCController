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
	})
	if err != nil {
		t.Fatal(err)
	}
	value := string(payload)
	for _, expected := range []string{"Door &lt;OPEN&gt;", "R5 &amp; R6", `activationType="protocol"`, "pccontroller://page/outputs"} {
		if !strings.Contains(value, expected) {
			t.Errorf("toast XML missing %q: %s", expected, value)
		}
	}
}

func TestToastRejectsUntrustedActionSchemes(t *testing.T) {
	_, err := buildToastXML(Notification{
		Title: "test", Actions: []NotificationAction{{Label: "run", URI: "file:///tmp/test"}},
	})
	if err == nil {
		t.Fatal("file action URI unexpectedly accepted")
	}
}

func TestImportantEventMappingSuppressesTelemetryAndAddsSafetyAction(t *testing.T) {
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
}
