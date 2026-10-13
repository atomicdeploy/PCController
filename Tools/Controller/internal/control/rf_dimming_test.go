package control

import (
	"errors"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"testing"
	"time"
)

func expiredRFState(t *testing.T, runtime *Runtime, key rfGestureKey) *rfGestureState {
	t.Helper()
	runtime.rfMu.Lock()
	defer runtime.rfMu.Unlock()
	state := runtime.rfGestures[key]
	if state == nil {
		t.Fatal("RF state missing")
	}
	state.lastSeen = time.Now().Add(-rfReleaseAfter - time.Millisecond)
	return state
}

func TestRFPWMRollGuardFollowsLiveHoldNotOldPacketDeadline(t *testing.T) {
	key := rfGestureKey{code: 1, bits: 24, protocol: 1}
	state := &rfGestureState{held: true, lastSeen: time.Now()}
	runtime := &Runtime{rfGestures: map[rfGestureKey]*rfGestureState{key: state}}
	guard := runtime.rfRollWriteGuard(Event{RFCode: 1, RFBits: 24, RFProtocol: 1})
	if err := guard(); err != nil {
		t.Fatal(err)
	}
	state.lastSeen = time.Now().Add(-rfReleaseAfter - time.Millisecond)
	if !errors.Is(guard(), errRFRollReleased) {
		t.Fatal("stale hold allowed a write")
	}
	state.lastSeen = time.Now()
	if err := guard(); err != nil {
		t.Fatal("fresh packet did not extend the hold", err)
	}
	runtime.rfGestures[key] = &rfGestureState{held: true, lastSeen: time.Now()}
	if !errors.Is(guard(), errRFRollReleased) {
		t.Fatal("old hold wrote into a replacement press")
	}
}

func TestRFHoldObsoleteReleaseCallbackCannotEndFreshReception(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	key := rfGestureKey{code: 1, bits: 24, protocol: 1}
	state := &rfGestureState{held: true, lastSeen: time.Now(), event: native.DeviceEvent{RFCode: 1, RFBits: 24, RFProtocol: 1}}
	runtime.rfMu.Lock()
	if runtime.rfGestures == nil {
		runtime.rfGestures = make(map[rfGestureKey]*rfGestureState)
	}
	runtime.rfGestures[key] = state
	runtime.rfMu.Unlock()
	after := runtime.LatestEventID()
	runtime.finishRFGesture(key, state)
	if runtime.LatestEventID() != after {
		t.Fatal("obsolete callback emitted a release")
	}
	runtime.rfMu.Lock()
	current := runtime.rfGestures[key]
	state.timer.Stop()
	runtime.rfMu.Unlock()
	if current != state {
		t.Fatal("fresh hold was removed")
	}
	runtime.finishRFGesture(key, expiredRFState(t, runtime, key))
	up := waitTestEvent(t, runtime, after, "rf.gesture")
	end := waitTestEvent(t, runtime, up.ID, "rf.gesture")
	if up.Gesture != "up" || end.Gesture != "hold-end" {
		t.Fatal(up.Gesture, end.Gesture)
	}
}

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
