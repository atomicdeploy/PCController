package hostbridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

func configuredKeyboardBinding(
	t *testing.T,
	key string,
) appconfig.KeyboardControlBinding {
	t.Helper()
	for _, binding := range appconfig.DefaultKeyboardControl().Bindings {
		if binding.Key == key {
			return binding
		}
	}
	t.Fatalf("default keyboard binding %s not found", key)
	return appconfig.KeyboardControlBinding{}
}

func TestKeyboardPlanningUsesTrueMotionDownAndUpEdges(t *testing.T) {
	binding := configuredKeyboardBinding(t, "K")
	down, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{Down: true})
	if err != nil || !execute || down.kind != "motion" || down.side != 1 ||
		down.motion != controller.RelayMotionUp {
		t.Fatalf("keydown=%+v execute=%v err=%v", down, execute, err)
	}
	up, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{Down: false})
	if err != nil || !execute || up.side != 1 || up.motion != controller.RelayMotionStop {
		t.Fatalf("keyup=%+v execute=%v err=%v", up, execute, err)
	}
}

func TestKeyboardPlanningCtrlSelectsAlternateAndSuppressesToggleKeyUp(t *testing.T) {
	binding := configuredKeyboardBinding(t, "1")
	primary, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{Down: true})
	if err != nil || !execute || primary.kind != "relay" ||
		primary.behavior != "toggle" || primary.relay != 1 {
		t.Fatalf("primary=%+v execute=%v err=%v", primary, execute, err)
	}
	_, execute, err = planKeyboardOperation(binding, hostui.KeyboardEvent{Down: false})
	if err != nil || execute {
		t.Fatalf("toggle keyup execute=%v err=%v", execute, err)
	}
	alternate, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{
		Down: true, Control: true,
	})
	if err != nil || !execute || alternate.behavior != "momentary" ||
		alternate.relay != 1 {
		t.Fatalf("Ctrl alternate=%+v execute=%v err=%v", alternate, execute, err)
	}
	alternate, execute, err = planKeyboardOperation(binding, hostui.KeyboardEvent{
		Down: false, Control: true,
	})
	if err != nil || !execute || alternate.down {
		t.Fatalf("Ctrl release=%+v execute=%v err=%v", alternate, execute, err)
	}
}

func TestKeyboardPlanningKeepsPWMWithinPurePlanningLayer(t *testing.T) {
	binding := configuredKeyboardBinding(t, "9")
	operation, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{Down: true})
	if err != nil || !execute || operation.kind != "pwm" ||
		operation.channel != 0 || operation.value != 4095 ||
		operation.releaseValue != 0 {
		t.Fatalf("PWM plan=%+v execute=%v err=%v", operation, execute, err)
	}
	// No controller Client exists in this test: planning can never actuate a
	// real or mock board by construction.
}

func TestCtrlMotionLatchIgnoresKeyUpAndUsesSecondPressAsStop(t *testing.T) {
	binding := configuredKeyboardBinding(t, "A")
	operation, execute, err := planKeyboardOperation(binding, hostui.KeyboardEvent{
		Down: true, Control: true,
	})
	if err != nil || !execute || operation.behavior != "latch" ||
		operation.side != 2 || operation.motion != controller.RelayMotionUp {
		t.Fatalf("Ctrl+A plan=%+v execute=%v err=%v", operation, execute, err)
	}
	_, execute, err = planKeyboardOperation(binding, hostui.KeyboardEvent{
		Down: false, Control: true,
	})
	if err != nil || execute {
		t.Fatalf("Ctrl+A keyup execute=%v err=%v", execute, err)
	}

	var applied []keyboardOperation
	manager := &Manager{
		keyboardLatches: make(map[string]keyboardLatch),
		keyboardActuator: func(_ context.Context, value keyboardOperation) error {
			applied = append(applied, value)
			return nil
		},
	}
	if err := manager.executeKeyboardOperation(context.Background(), binding.Name, operation); err != nil {
		t.Fatal(err)
	}
	if len(manager.keyboardLatches) != 1 || len(applied) != 1 ||
		applied[0].motion != controller.RelayMotionUp {
		t.Fatalf("first latch: map=%#v applied=%#v", manager.keyboardLatches, applied)
	}
	if err := manager.executeKeyboardOperation(context.Background(), binding.Name, operation); err != nil {
		t.Fatal(err)
	}
	if len(manager.keyboardLatches) != 0 || len(applied) != 2 ||
		applied[1].motion != controller.RelayMotionStop {
		t.Fatalf("second press: map=%#v applied=%#v", manager.keyboardLatches, applied)
	}
}

func TestOppositeMotionAndFailSafeCannotOrphanLatch(t *testing.T) {
	var applied []keyboardOperation
	manager := &Manager{
		keyboardLatches: make(map[string]keyboardLatch),
		keyboardActuator: func(_ context.Context, value keyboardOperation) error {
			applied = append(applied, value)
			return nil
		},
	}
	up := keyboardOperation{
		kind: "motion", behavior: "latch", down: true,
		side: 2, motion: controller.RelayMotionUp,
	}
	down := up
	down.motion = controller.RelayMotionDown
	if err := manager.executeKeyboardOperation(context.Background(), "side-b-up", up); err != nil {
		t.Fatal(err)
	}
	if err := manager.executeKeyboardOperation(context.Background(), "side-b-down", down); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 || applied[0].motion != controller.RelayMotionUp ||
		applied[1].motion != controller.RelayMotionStop ||
		applied[2].motion != controller.RelayMotionDown {
		t.Fatalf("opposite transition=%#v", applied)
	}
	if err := manager.releaseKeyboardLatches(context.Background(), "focus-lost"); err != nil {
		t.Fatal(err)
	}
	if len(manager.keyboardLatches) != 0 || len(applied) != 4 ||
		applied[3].motion != controller.RelayMotionStop {
		t.Fatalf("fail-safe release: map=%#v applied=%#v", manager.keyboardLatches, applied)
	}
}

func TestFailedLatchReleaseRemainsTrackedForReconnectRetry(t *testing.T) {
	stopFailed := true
	manager := &Manager{
		keyboardLatches: map[string]keyboardLatch{
			"motion:1": {
				binding: "side-a-up",
				operation: keyboardOperation{
					kind: "motion", behavior: "latch", down: true,
					side: 1, motion: controller.RelayMotionUp,
				},
			},
		},
		keyboardActuator: func(_ context.Context, value keyboardOperation) error {
			if stopFailed && value.motion == controller.RelayMotionStop {
				return errors.New("offline")
			}
			return nil
		},
	}
	if err := manager.releaseKeyboardLatches(context.Background(), "disconnect"); err == nil {
		t.Fatal("failed disconnect release unexpectedly succeeded")
	}
	if len(manager.keyboardLatches) != 1 {
		t.Fatal("failed release forgot latch instead of retaining it for reconnect")
	}
	stopFailed = false
	if err := manager.releaseKeyboardLatches(context.Background(), "reconnect"); err != nil {
		t.Fatal(err)
	}
	if len(manager.keyboardLatches) != 0 {
		t.Fatal("successful reconnect fail-safe did not clear latch")
	}
}

func TestLiveTelemetryRelinquishesExternallyStoppedOrReversedMotionLatch(t *testing.T) {
	now := time.Unix(100, 0)
	latch := keyboardLatch{
		binding: "side-b-up",
		operation: keyboardOperation{
			kind: "motion", behavior: "latch", down: true,
			side: 2, motion: controller.RelayMotionUp,
		},
		started: now.Add(-time.Second),
	}
	manager := &Manager{keyboardLatches: map[string]keyboardLatch{"motion:2": latch}}
	manager.observeKeyboardStatus(controller.Status{ActiveRelays: 1 << 3}, now)
	if len(manager.keyboardLatches) != 1 {
		t.Fatal("matching live Side B Up state unexpectedly relinquished latch")
	}
	manager.observeKeyboardStatus(controller.Status{ActiveRelays: 0}, now)
	if len(manager.keyboardLatches) != 0 {
		t.Fatal("external motion stop did not relinquish keyboard latch")
	}

	latch.started = now
	manager.keyboardLatches["motion:2"] = latch
	manager.observeKeyboardStatus(controller.Status{ActiveRelays: 0}, now.Add(100*time.Millisecond))
	if len(manager.keyboardLatches) != 1 {
		t.Fatal("settling telemetry prematurely relinquished a new latch")
	}
	latch.started = now.Add(-time.Second)
	manager.keyboardLatches["motion:2"] = latch
	manager.observeKeyboardStatus(controller.Status{ActiveRelays: 1<<2 | 1<<3}, now)
	if len(manager.keyboardLatches) != 0 {
		t.Fatal("externally reversed Side B motion did not relinquish Up latch")
	}
}

func TestKeyboardLifecycleCommandPersistsHostConfigAndStopsLatchWithoutBoard(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var applied []keyboardOperation
	manager := &Manager{
		store: store, ctx: context.Background(),
		keyboardLatches: map[string]keyboardLatch{
			"motion:1": {
				binding: "side-a-up",
				operation: keyboardOperation{
					kind: "motion", behavior: "latch", down: true,
					side: 1, motion: controller.RelayMotionUp,
				},
			},
		},
		keyboardActuator: func(_ context.Context, operation keyboardOperation) error {
			applied = append(applied, operation)
			return nil
		},
	}
	if output, err := manager.KeyboardCommand(context.Background(), []string{"stop"}); err != nil ||
		!strings.Contains(output, "released") {
		t.Fatalf("stop output=%q err=%v", output, err)
	}
	if len(applied) != 1 || applied[0].motion != controller.RelayMotionStop ||
		len(manager.keyboardLatches) != 0 {
		t.Fatalf("stop applied=%#v latches=%#v", applied, manager.keyboardLatches)
	}
	if _, err := manager.KeyboardCommand(context.Background(), []string{"enable"}); err != nil {
		t.Fatal(err)
	}
	if !store.Current().Integrations.Keyboard.Enabled {
		t.Fatal("keyboard enable was not persisted in PC-side config")
	}
	if _, err := manager.KeyboardCommand(context.Background(), []string{"disable"}); err != nil {
		t.Fatal(err)
	}
	if store.Current().Integrations.Keyboard.Enabled {
		t.Fatal("keyboard disable was not persisted in PC-side config")
	}
}
