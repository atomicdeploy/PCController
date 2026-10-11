package hostbridge

import (
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/boardmirror"
	"pccontroller.local/controller/internal/fleet"
	"pccontroller.local/controller/internal/native"
)

func TestMirrorPeriodicPanelReadsStopWhenSegmentPushExists(t *testing.T) {
	snapshot := controller.Snapshot{HaveFrontPanel: true, Hello: controller.Hello{Capabilities: native.CapabilitySegmentPush}}
	if mirrorReadPanel(snapshot, false, true, false) {
		t.Fatal("periodic display read shadows firmware push")
	}
	if !mirrorReadPanel(snapshot, true, false, false) {
		t.Fatal("explicit refresh was lost")
	}
	snapshot.HaveFrontPanel = false
	if !mirrorReadPanel(snapshot, false, false, false) {
		t.Fatal("initial exact snapshot was lost")
	}
	snapshot.HaveFrontPanel = true
	snapshot.Hello.Capabilities = 0
	if mirrorReadPanel(snapshot, false, false, false) || !mirrorReadPanel(snapshot, false, true, false) {
		t.Fatal("legacy fallback cadence is not bounded")
	}
	snapshot.Hello.Capabilities = native.CapabilitySegmentPush
	if !mirrorReadPanel(snapshot, false, true, true) {
		t.Fatal("opt-in LCD legacy readback unavailable")
	}
}

func TestMirrorPeerReportsActualBuzzerBackend(t *testing.T) {
	expected := appconfig.BuzzerRuntimeStatus{BackendEffective: "native", BackendError: "device denied"}
	manager := &Manager{status: Status{BuzzerRuntime: expected}}
	service := manager.remotePeerService()
	if service.BuzzerRuntimeStatus == nil || service.BuzzerRuntimeStatus() != expected {
		t.Fatal("peer service replaced actual buzzer backend state with configuration")
	}
}

func TestMirrorStaticRGBAllowsManualButNeverSafetyOrCues(t *testing.T) {
	for _, condition := range []byte{0xff, native.StatusConditionOff, native.StatusConditionReady, native.StatusConditionCustom, native.StatusConditionBluetoothOff} {
		if !mirrorStaticRGBAvailable(native.StatusLEDState{Condition: condition}) {
			t.Fatal("safe static color unavailable", condition)
		}
	}
	for _, condition := range []byte{native.StatusConditionBoot, native.StatusConditionLearning, native.StatusConditionHot, native.StatusConditionFault, native.StatusConditionRunning, native.StatusConditionDoorOpen, native.StatusConditionReset} {
		if mirrorStaticRGBAvailable(native.StatusLEDState{Condition: condition}) {
			t.Fatal("local safety/cue exposed as mirror base", condition)
		}
	}
	if mirrorStaticRGBAvailable(native.StatusLEDState{Condition: 0xff, Effect: native.StatusEffectBreathe}) {
		t.Fatal("animated physical result exposed as static base")
	}
}

func TestMirrorWriteRequiresMatchingIntentTargetOwnerAndGeneration(t *testing.T) {
	now := time.Now()
	command := fleet.MirrorCommand{Kind: "pwm", Value: 123}
	encoded, _ := json.Marshal(command)
	write := boardmirror.Write{Generation: 1, Command: command, Route: fleet.RouteEnvelope{RouteID: "0123456789abcdef0123456789abcdef", OperationID: "op", TraceID: "trace", OriginInstanceID: "owner", OriginBoardID: "one", Target: fleet.Destination{BoardID: "two"}, Coordinator: fleet.CoordinatorPosition{ID: "owner", Epoch: 1, Sequence: 1}, HopCount: 1, HopLimit: 1, Deadline: now.Add(time.Second), Intent: fleet.Intent{Domain: "pwm", Name: "mirror", Arguments: map[string]json.RawMessage{"command": encoded}}}}
	config := fleet.MirrorConfig{LocalBoardID: "two", ReceiveFrom: "owner", Receive: fleet.MirrorPolicy{Domains: []string{"pwm"}, PWMChannels: []int{0}}}
	if err := validateMirrorWrite(write, config, now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*boardmirror.Write){
		func(w *boardmirror.Write) { w.Generation = 0 }, func(w *boardmirror.Write) { w.Route.Target.BoardID = "other" }, func(w *boardmirror.Write) { w.Route.Coordinator.ID = "other" }, func(w *boardmirror.Write) { w.Route.OriginBoardID = "two" }, func(w *boardmirror.Write) { w.Command.Value++ }, func(w *boardmirror.Write) { w.Route.Deadline = now.Add(time.Minute) },
	} {
		copy := write
		mutate(&copy)
		if validateMirrorWrite(copy, config, now) == nil {
			t.Fatal("invalid write admitted")
		}
	}
}
