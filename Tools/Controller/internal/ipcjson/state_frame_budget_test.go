package ipcjson

import (
	"testing"
	"time"

	controller "pccontroller.local/controller"
)

func TestStateFrameBudgetsDoNotDropSegmentOrBuzzerEdges(t *testing.T) {
	budget := make(stateFrameBudget)
	now := time.Unix(1700000000, 0)
	for _, kind := range []string{"status_led.changed", "front_panel.segment", "pwm.changed", "buzzer.note", "front_panel.segment"} {
		if !budget.allow(controller.Event{Kind: kind}, 16*time.Millisecond, now) {
			t.Fatalf("independent edge %s was lost", kind)
		}
	}
	if budget.allow(controller.Event{Kind: "status_led.changed"}, 16*time.Millisecond, now.Add(time.Millisecond)) {
		t.Fatal("RGB frame budget was not retained")
	}
	if !budget.allow(controller.Event{Kind: "status_led.changed"}, 16*time.Millisecond, now.Add(16*time.Millisecond)) {
		t.Fatal("RGB frame budget did not reopen")
	}
}
