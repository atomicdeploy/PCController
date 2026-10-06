package control

import (
	"pccontroller.local/controller/internal/appconfig"
	"testing"
)

func TestRFGestureMatchingIncludesBitsAndNormalizesDouble(t *testing.T) {
	code := uint32(42)
	match := appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "double"}
	event := Event{Kind: "rf.gesture", Source: "rf", RFCode: code, RFBits: 24, RFProtocol: 1, Gesture: "double-click"}
	if !automationMatches(match, event) {
		t.Fatal("double-click alias did not match")
	}
	event.RFBits = 32
	if automationMatches(match, event) {
		t.Fatal("different bit width matched")
	}
	event.RFBits = 24
	event.RFProtocol = 2
	if automationMatches(match, event) {
		t.Fatal("different protocol matched")
	}
}
