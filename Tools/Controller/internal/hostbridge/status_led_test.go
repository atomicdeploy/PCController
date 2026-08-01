package hostbridge

import (
	"context"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func TestStatusLEDStatePriority(t *testing.T) {
	policy := appconfig.DefaultStatusLEDPolicy()
	now := time.Now()
	snapshot := controller.Snapshot{
		Connected: true, HaveStatus: true,
		ProgramState: controller.ProgramStateSnapshot{Mode: controller.ProgramIdle},
	}
	assertStatusLEDState(t, policy, snapshot, time.Time{}, now, statusLEDBTOff)
	snapshot.Status.BluetoothState = 1
	assertStatusLEDState(t, policy, snapshot, time.Time{}, now, statusLEDBTConnected)
	snapshot.Status.BluetoothState = 2
	assertStatusLEDState(t, policy, snapshot, time.Time{}, now, statusLEDBTSearching)
	snapshot.ProgramState.Mode = controller.ProgramRunning
	assertStatusLEDState(t, policy, snapshot, time.Time{}, now, statusLEDRunning)
	assertStatusLEDState(t, policy, snapshot, now.Add(time.Second), now, statusLEDRF)
	snapshot.Status.DoorOpen = true
	assertStatusLEDState(t, policy, snapshot, now.Add(time.Second), now, statusLEDDoorWarning)
	snapshot.Status.TLEDCenti = policy.HotThresholdCentiC
	assertStatusLEDState(t, policy, snapshot, now.Add(time.Second), now, statusLEDHot)
	snapshot.Connected = false
	assertStatusLEDState(t, policy, snapshot, now.Add(time.Second), now, statusLEDOffline)
}

func TestStatusLEDVisualsAreSmoothAndBounded(t *testing.T) {
	policy := appconfig.DefaultStatusLEDPolicy()
	start := statusLEDVisualFrame(policy.Hot, 0)
	middle := statusLEDVisualFrame(
		policy.Hot, time.Duration(policy.Hot.PeriodMS/2)*time.Millisecond,
	)
	end := statusLEDVisualFrame(
		policy.Hot, time.Duration(policy.Hot.PeriodMS)*time.Millisecond,
	)
	if start.brightness != policy.Hot.MinimumBrightness ||
		middle.brightness != policy.Hot.Brightness || start != end {
		t.Fatalf("unexpected breathe cycle: start=%#v middle=%#v end=%#v", start, middle, end)
	}
	from := statusLEDFrame{red: 255, brightness: 255}
	to := statusLEDFrame{blue: 255, brightness: 128}
	mid := interpolateStatusLEDFrame(from, to, smoothStep(0.5))
	if mid.red == 0 || mid.blue == 0 || mid.brightness <= 128 || mid.brightness >= 255 {
		t.Fatalf("transition did not blend both endpoints: %#v", mid)
	}
}

func TestStatusLEDDoorEdgeCueExpiresAndRestoresBaseWithEasing(t *testing.T) {
	policy := appconfig.DefaultStatusLEDPolicy()
	now := time.Unix(123, 0)
	snapshot := controller.Snapshot{
		Connected: true, HaveStatus: true,
		ProgramState: controller.ProgramStateSnapshot{Mode: controller.ProgramIdle},
	}
	snapshot.Status.BluetoothState = 1
	snapshot.Status.DoorOpen = true
	cueUntil := now.Add(time.Duration(policy.DoorCueHoldMS) * time.Millisecond)

	state, visual := selectStatusLEDState(
		policy, snapshot, time.Time{}, cueUntil, true, now,
	)
	if state != statusLEDDoorOpened || visual != policy.DoorOpened {
		t.Fatalf("open edge state=%q visual=%#v", state, visual)
	}
	state, visual = selectStatusLEDState(
		policy, snapshot, time.Time{}, cueUntil, true, cueUntil,
	)
	if state != statusLEDBTConnected || visual != policy.BluetoothAudioConnected {
		t.Fatalf("expired cue did not restore base: state=%q visual=%#v", state, visual)
	}

	from := statusLEDVisualFrame(policy.DoorOpened, 0)
	to := statusLEDVisualFrame(policy.BluetoothAudioConnected, 0)
	duration := time.Duration(policy.TransitionMS) * time.Millisecond
	start := easeStatusLEDTransition(from, to, 0, duration)
	middle := easeStatusLEDTransition(from, to, duration/2, duration)
	end := easeStatusLEDTransition(from, to, duration, duration)
	if start != from || end != to || middle == from || middle == to {
		t.Fatalf(
			"door cue restore was abrupt: start=%#v middle=%#v end=%#v",
			start, middle, end,
		)
	}

	snapshot.Status.DoorOpen = false
	state, visual = selectStatusLEDState(
		policy, snapshot, time.Time{}, cueUntil, false, now,
	)
	if state != statusLEDDoorClosed || visual != policy.DoorClosed ||
		visual == policy.DoorOpened {
		t.Fatalf("close edge is not visually distinct: state=%q visual=%#v", state, visual)
	}
}

func TestStatusLEDDoorObservationUsesConfiguredHoldAndDirection(t *testing.T) {
	policy := appconfig.DefaultStatusLEDPolicy()
	policy.DoorCueHoldMS = 2750
	arbiter := newStatusLEDArbiter(context.Background(), nil, nil, nil)
	snapshot := controller.Snapshot{Connected: true, HaveStatus: true}
	arbiter.Observe(policy, snapshot, controller.Event{Kind: "config"})

	snapshot.Status.DoorOpen = true
	before := time.Now()
	arbiter.Observe(policy, snapshot, controller.Event{Kind: "door"})
	_, _, _, cueUntil, cueOpen, _ := arbiter.currentObservation()
	remaining := cueUntil.Sub(before)
	if !cueOpen || remaining < 2750*time.Millisecond || remaining > 2850*time.Millisecond {
		t.Fatalf("open cue duration/direction mismatch: open=%t remaining=%s", cueOpen, remaining)
	}

	snapshot.Status.DoorOpen = false
	arbiter.Observe(policy, snapshot, controller.Event{Kind: "door"})
	_, _, _, cueUntil, cueOpen, _ = arbiter.currentObservation()
	if cueOpen || time.Until(cueUntil) < 2650*time.Millisecond {
		t.Fatalf("close cue duration/direction mismatch: open=%t until=%s", cueOpen, cueUntil)
	}
}

func TestStatusLEDRunningDoorOpenRemainsPersistentCritical(t *testing.T) {
	policy := appconfig.DefaultStatusLEDPolicy()
	now := time.Unix(456, 0)
	snapshot := controller.Snapshot{
		Connected: true, HaveStatus: true,
		ProgramState: controller.ProgramStateSnapshot{Mode: controller.ProgramRunning},
	}
	snapshot.Status.DoorOpen = true

	for _, observedAt := range []time.Time{now, now.Add(24 * time.Hour)} {
		state, visual := selectStatusLEDState(
			policy, snapshot, time.Time{}, now.Add(time.Second), false, observedAt,
		)
		if state != statusLEDDoorWarning || visual != policy.RunningDoorOpen ||
			visual.Color.Red != 255 || visual.Color.Green != 0 || visual.Color.Blue != 0 {
			t.Fatalf("Running+door-open lost critical red priority: state=%q visual=%#v", state, visual)
		}
	}
	target := &statusLEDTargetRecorder{}
	criticalFrame := statusLEDVisualFrame(policy.RunningDoorOpen, 0)
	if err := sendStatusLEDFrame(
		context.Background(), target, statusLEDDoorWarning, criticalFrame,
	); err != nil {
		t.Fatal(err)
	}
	if target.direct != 1 || target.base != 0 {
		t.Fatalf("critical door warning did not preempt overlays: %#v", target)
	}
	if err := sendStatusLEDFrame(
		context.Background(), target, statusLEDDoorOpened,
		statusLEDVisualFrame(policy.DoorOpened, 0),
	); err != nil {
		t.Fatal(err)
	}
	if target.direct != 1 || target.base != 1 {
		t.Fatalf("ordinary door cue unexpectedly cancelled overlays: %#v", target)
	}
}

func assertStatusLEDState(
	t *testing.T,
	policy appconfig.StatusLEDPolicy,
	snapshot controller.Snapshot,
	rfUntil, now time.Time,
	want string,
) {
	t.Helper()
	got, _ := selectStatusLEDState(
		policy, snapshot, rfUntil, time.Time{}, false, now,
	)
	if got != want {
		t.Fatalf("status LED state=%q, want %q", got, want)
	}
}

type statusLEDTargetRecorder struct {
	base   int
	direct int
}

func (target *statusLEDTargetRecorder) SetStatusRGBBase(
	context.Context,
	byte, byte, byte, byte,
) error {
	target.base++
	return nil
}

func (target *statusLEDTargetRecorder) SetStatusRGB(
	context.Context,
	byte, byte, byte, byte,
) error {
	target.direct++
	return nil
}
