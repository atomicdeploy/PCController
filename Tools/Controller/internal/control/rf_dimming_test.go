package control

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"testing"
)

func TestRFPWMRecallPersistenceNeverReentersPresentationProvider(t *testing.T) {
	// Do not start unrelated process-monitor goroutines in this lock-order fixture.
	runtime := &Runtime{}
	runtime.session, runtime.generation, runtime.pwmRecallGeneration = &link.Session{}, 7, 7
	runtime.port = ports.Info{Name: "COM3"}
	runtime.pwmRecall[0] = PWMRecallValue{Known: true, Raw: 239}
	config := appconfig.Defaults()
	identity := pwmProfileIdentity(pwmRecallOwnerSnapshot(runtime))
	config.BoardProfiles = map[string]appconfig.BoardProfile{identity: {Key: "cafe", Mode: appconfig.BoardModeOrdinaryRelays}}
	runtime.macroRunner = NewMacroRunner(runtime, nil, func() appconfig.Config {
		t.Fatal("recall persistence entered an effect/configuration provider")
		return config
	})
	updates := 0
	options := CommandOptions{HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: func(change func(*appconfig.Config) error) error {
		updates++
		return change(&config)
	}}
	if err := persistPWMRecall(runtime, options, 7, 0); err != nil {
		t.Fatal(err)
	}
	if config.BoardProfiles[identity].PWMRecall["pwm.0"] != 239 || updates != 1 {
		t.Fatal("confirmed recall was not persisted once")
	}
	if err := persistPWMRecall(runtime, options, 7, 0); err != nil || updates != 1 {
		t.Fatal("unchanged memory rewrote configuration", err)
	}
	if err := persistPWMRecall(runtime, options, 7, 16); err == nil {
		t.Fatal("out-of-range channel accepted")
	}
}

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
