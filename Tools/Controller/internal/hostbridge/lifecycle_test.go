package hostbridge

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestLifecycleInstructionUsesConfiguredSafetyPolicy(t *testing.T) {
	policy := appconfig.LifecycleSafety{
		SessionLock:     appconfig.LifecycleActionAllOff,
		Suspend:         appconfig.LifecycleActionStopMotion,
		RefreshOnResume: true,
	}
	for _, test := range []struct {
		kind, action string
		refresh      bool
	}{
		{LifecycleSessionLocked, appconfig.LifecycleActionAllOff, false},
		{LifecyclePowerSuspending, appconfig.LifecycleActionStopMotion, false},
		{LifecycleSessionUnlocked, "", true},
		{LifecyclePowerResumed, "", true},
		{"network.changed", "", false},
	} {
		action, refresh := lifecycleInstruction(test.kind, policy)
		if action != test.action || refresh != test.refresh {
			t.Errorf("instruction %q=(%q,%t), want (%q,%t)", test.kind, action, refresh, test.action, test.refresh)
		}
	}
}

func TestApplyLifecycleActionGatesDisconnectedAndPropagatesErrors(t *testing.T) {
	var actions []string
	actuator := func(_ context.Context, action string) error {
		actions = append(actions, action)
		if action == appconfig.LifecycleActionAllOff {
			return errors.New("transport closed")
		}
		return nil
	}
	if err := applyLifecycleAction(context.Background(), appconfig.LifecycleActionStopMotion, false, actuator); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("disconnected actions=%v", actions)
	}
	if err := applyLifecycleAction(context.Background(), appconfig.LifecycleActionStopMotion, true, actuator); err != nil {
		t.Fatal(err)
	}
	if err := applyLifecycleAction(context.Background(), appconfig.LifecycleActionLeave, true, actuator); err != nil {
		t.Fatal(err)
	}
	if err := applyLifecycleAction(context.Background(), appconfig.LifecycleActionAllOff, true, actuator); err == nil {
		t.Fatal("expected actuator error")
	}
	if !reflect.DeepEqual(actions, []string{appconfig.LifecycleActionStopMotion, appconfig.LifecycleActionAllOff}) {
		t.Fatalf("actions=%v", actions)
	}
}
