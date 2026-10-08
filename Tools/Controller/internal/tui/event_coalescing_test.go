package tui

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
)

func TestWaitControlEventCoalescesStateFramesToNewest(t *testing.T) {
	events := make(chan control.Event, 2)
	events <- control.Event{Kind: "status_led.changed", Stream: control.EventStreamState, Text: "first"}
	events <- control.Event{Kind: "status_led.changed", Stream: control.EventStreamState, Text: "latest"}
	close(events)

	message := waitControlEvent(events)()
	event, ok := message.(runtimeEventMsg)
	if !ok || control.Event(event).Text != "latest" {
		t.Fatalf("message=%#v, want newest state frame", message)
	}
}

func TestWaitControlEventLetsActivityPreemptStatePresentationWindow(t *testing.T) {
	events := make(chan control.Event, 2)
	events <- control.Event{Kind: "status_led.changed", Stream: control.EventStreamState}
	events <- control.Event{Kind: "door.changed", Stream: control.EventStreamActivity, Text: "door opened"}

	started := time.Now()
	message := waitControlEvent(events)()
	event, ok := message.(runtimeEventMsg)
	if !ok || control.Event(event).Kind != "door.changed" {
		t.Fatalf("message=%#v, want activity event", message)
	}
	if elapsed := time.Since(started); elapsed >= 40*time.Millisecond {
		t.Fatalf("activity was delayed by state presentation window: %s", elapsed)
	}
}
