package ipcjson

import (
	"testing"

	controllerapi "pccontroller.local/controller"
)

func TestNormalizeSubscriptionAcceptsBoundedStatePresentationCadence(t *testing.T) {
	value, err := normalizeSubscription(wsSubscription{
		Topics: []string{"state", "events"}, StateIntervalMS: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.StateIntervalMS != 100 {
		t.Fatalf("state_interval_ms=%d, want 100", value.StateIntervalMS)
	}
	if _, err := normalizeSubscription(wsSubscription{
		Topics: []string{"state"}, StateIntervalMS: 10,
	}); err == nil {
		t.Fatal("sub-frame state interval must be rejected")
	}
}

func TestHighRateStateEventClassificationLeavesEdgesImmediate(t *testing.T) {
	for _, kind := range []string{"status_led.changed", "pwm.changed", "animation.frame", "front_panel.segment"} {
		if !highRateStateEvent(controllerapi.Event{Kind: kind}) {
			t.Fatalf("%q must be presentation-rate limited", kind)
		}
	}
	for _, kind := range []string{"relay.changed", "door", "connection.changed", "settings.changed"} {
		if highRateStateEvent(controllerapi.Event{Kind: kind}) {
			t.Fatalf("%q must remain immediate", kind)
		}
	}
}
