package hostbridge

import (
	"encoding/json"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/boardmirror"
	"pccontroller.local/controller/internal/fleet"
)

func TestMirrorPeerReportsActualBuzzerBackend(t *testing.T) {
	expected := appconfig.BuzzerRuntimeStatus{BackendEffective: "native", BackendError: "device denied"}
	manager := &Manager{status: Status{BuzzerRuntime: expected}}
	service := manager.remotePeerService()
	if service.BuzzerRuntimeStatus == nil || service.BuzzerRuntimeStatus() != expected {
		t.Fatal("peer service replaced actual buzzer backend state with configuration")
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
