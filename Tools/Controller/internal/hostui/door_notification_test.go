package hostui

import (
	"strings"
	"testing"
)

func TestDoorTransitionNotificationHasStateSpecificDynamicDetails(t *testing.T) {
	opened := NotificationForDoorTransition(DoorNotification{
		Open: true, AppTitle: "Workshop Controller",
		Device: "USB Controller", Port: "COM7", ProgramState: "Idle",
	})
	if opened.Title != "Workshop Controller · Enclosure door opened" ||
		opened.Body != "USB Controller (COM7) reports the enclosure door is open. Program state: Idle." ||
		opened.Severity != "door" || len(opened.Actions) != 1 ||
		opened.Actions[0].URI != "pccontroller://page/events" {
		t.Fatalf("opened notification=%#v", opened)
	}
	closed := NotificationForDoorTransition(DoorNotification{
		Device: "COM7", Port: "COM7", ProgramState: "Running",
	})
	if closed.Title != "PCController · Enclosure door closed" ||
		closed.Body != "COM7 reports the enclosure door is closed. Program state: Running." ||
		strings.Contains(closed.Body, "COM7 (COM7)") {
		t.Fatalf("closed notification=%#v", closed)
	}
}

func TestRunningDoorNotificationIsOneActionableSafetyPresentation(t *testing.T) {
	warning := NotificationForDoorTransition(DoorNotification{
		Open: true, Running: true, Device: "CAFE", Port: "COM3",
		ProgramState: "Running",
	})
	if warning.Title != "PCController · Door open during operation" ||
		warning.Severity != "warning" ||
		warning.Body != "CAFE (COM3) reports the enclosure door is open. Program state: Running. Stop outputs if this was not expected." ||
		len(warning.Actions) != 2 || warning.Actions[1].Label != "Stop outputs" ||
		warning.Actions[1].URI != "pccontroller://command/relay%20off" {
		t.Fatalf("running-door notification=%#v", warning)
	}
}
