package control

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"testing"
)

func TestRFPWMRollReachesHighThenWrapsWithoutOff(t *testing.T) {
	for _, channel := range []int{0, 1, 8} {
		config := appconfig.PWMChannel(appconfig.Defaults().UI, channel)
		for _, raw := range []uint16{0, 1, 239, 4094} {
			next, err := pwmActionRaw(raw, "roll", config)
			if err != nil || next <= raw {
				t.Fatalf("channel=%d raw=%d next=%d err=%v", channel, raw, next, err)
			}
		}
		next, err := pwmActionRaw(4095, "roll", config)
		if err != nil || next == 0 || next >= 4095 {
			t.Fatalf("wrap channel=%d raw=%d err=%v", channel, next, err)
		}
	}
}

func TestRFPWMPersistedRecallDoesNotOverrideNewReadback(t *testing.T) {
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 0)
	config.OnBehavior, config.PersistedRecallRaw = "restore", 239
	if got, _ := pwmActionTarget(0, "toggle", config, PWMRecallValue{}); got != 239 {
		t.Fatal(got)
	}
	if got, _ := pwmActionTarget(0, "toggle", config, PWMRecallValue{Known: true, Raw: 984}); got != 984 {
		t.Fatal(got)
	}
	if got, _ := pwmActionTarget(500, "toggle", config, PWMRecallValue{}); got != 0 {
		t.Fatal(got)
	}
	if appconfig.PWMRawFromPercent(100, config) != 4095 {
		t.Fatal("absolute 100 changed")
	}
}

func TestRFLearningReceptionCannotScheduleDelayedTap(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	runtime.rfLearnState.Active = true
	after := runtime.LatestEventID()
	runtime.observeRFGesture(native.DeviceEvent{Type: native.EventRFReceived, RFCode: 1, RFBits: 24, RFProtocol: 1})
	if runtime.LatestEventID() != after || len(runtime.rfGestures) != 0 {
		t.Fatal("learning scheduled a gesture")
	}
}
