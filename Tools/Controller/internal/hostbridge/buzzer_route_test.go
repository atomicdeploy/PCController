package hostbridge

import (
	"context"
	"errors"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func testBuzzerRouteManager(snapshot *controller.Snapshot, execute func(bool) error) *Manager {
	return &Manager{
		ctx:             context.Background(),
		buzzerRouteWake: make(chan struct{}, 1),
		buzzerRouteSnapshot: func() controller.Snapshot {
			return *snapshot
		},
		buzzerRouteExecute: func(_ context.Context, silent bool) error {
			return execute(silent)
		},
	}
}

func TestBuzzerRouteAppliesAllFourPathsAndVerifies(t *testing.T) {
	for _, test := range []struct {
		path          string
		desiredSilent bool
	}{
		{appconfig.BuzzerPathBoard, false},
		{appconfig.BuzzerPathBoth, false},
		{appconfig.BuzzerPathHost, true},
		{appconfig.BuzzerPathNone, true},
	} {
		t.Run(test.path, func(t *testing.T) {
			snapshot := controller.Snapshot{HaveSettings: true}
			if !test.desiredSilent {
				snapshot.Settings.Flags = native.SettingsSilent
			}
			calls := 0
			manager := testBuzzerRouteManager(&snapshot, func(silent bool) error {
				calls++
				if silent != test.desiredSilent {
					t.Fatalf("silent=%t want %t", silent, test.desiredSilent)
				}
				if silent {
					snapshot.Settings.Flags |= native.SettingsSilent
				} else {
					snapshot.Settings.Flags &^= native.SettingsSilent
				}
				return nil
			})
			manager.observeBuzzerRoute(test.path, false)
			manager.applyBuzzerRoute()
			if calls != 1 || manager.buzzerRouteState != "verified" {
				t.Fatalf("calls=%d state=%q", calls, manager.buzzerRouteState)
			}
			manager.applyBuzzerRoute()
			if calls != 1 {
				t.Fatalf("verified route rewrote EEPROM: calls=%d", calls)
			}
		})
	}
}

func TestBuzzerRouteWaitsForSettingsAndNeverWritesWhenUnspecified(t *testing.T) {
	snapshot := controller.Snapshot{}
	calls := 0
	manager := testBuzzerRouteManager(&snapshot, func(bool) error { calls++; return nil })
	manager.observeBuzzerRoute("", false)
	manager.applyBuzzerRoute()
	if calls != 0 || manager.buzzerRouteState != "unspecified" {
		t.Fatalf("unspecified calls=%d state=%q", calls, manager.buzzerRouteState)
	}
	manager.observeBuzzerRoute(appconfig.BuzzerPathHost, false)
	manager.applyBuzzerRoute()
	if calls != 0 || manager.buzzerRouteState != "pending" {
		t.Fatalf("disconnected calls=%d state=%q", calls, manager.buzzerRouteState)
	}
	snapshot.HaveSettings = true
	manager.buzzerRouteExecute = func(_ context.Context, silent bool) error {
		calls++
		if silent {
			snapshot.Settings.Flags |= native.SettingsSilent
		}
		return nil
	}
	manager.observeBuzzerRoute(appconfig.BuzzerPathHost, true)
	manager.applyBuzzerRoute()
	if calls != 1 || manager.buzzerRouteState != "verified" {
		t.Fatalf("connected calls=%d state=%q", calls, manager.buzzerRouteState)
	}
}

func TestBuzzerRouteRetriesAreBoundedAndHotChangeResetsIntent(t *testing.T) {
	snapshot := controller.Snapshot{HaveSettings: true}
	calls := 0
	manager := testBuzzerRouteManager(&snapshot, func(bool) error {
		calls++
		return errors.New("write failed")
	})
	manager.observeBuzzerRoute(appconfig.BuzzerPathHost, false)
	for range 5 {
		manager.applyBuzzerRoute()
	}
	if calls != buzzerRouteMaximumAttempts || manager.buzzerRouteState != "error" {
		t.Fatalf("calls=%d state=%q", calls, manager.buzzerRouteState)
	}
	manager.buzzerRouteExecute = func(_ context.Context, silent bool) error {
		calls++
		if silent {
			snapshot.Settings.Flags |= native.SettingsSilent
		} else {
			snapshot.Settings.Flags &^= native.SettingsSilent
		}
		return nil
	}
	manager.observeBuzzerRoute(appconfig.BuzzerPathBoard, false)
	manager.applyBuzzerRoute()
	if calls != buzzerRouteMaximumAttempts || manager.buzzerRouteState != "verified" {
		t.Fatalf("already-matching hot change calls=%d state=%q", calls, manager.buzzerRouteState)
	}
	manager.observeBuzzerRoute(appconfig.BuzzerPathHost, false)
	manager.applyBuzzerRoute()
	if calls != buzzerRouteMaximumAttempts+1 || manager.buzzerRouteState != "verified" {
		t.Fatalf("new hot change calls=%d state=%q", calls, manager.buzzerRouteState)
	}
}
