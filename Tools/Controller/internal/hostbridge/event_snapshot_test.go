package hostbridge

import (
	"context"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestEventPresentationSharesOneFreshSnapshot(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(8))
	t.Cleanup(func() { _ = client.Shutdown() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := &Manager{client: client}
	manager.segmentScroll = newSegmentScrollPresenter(ctx, client, nil, nil)
	manager.statusLED = newStatusLEDArbiter(ctx, client, nil, nil)
	config := appconfig.Defaults()
	config.Integrations.Notifications.DoorRunningBeep = false
	snapshot := controller.Snapshot{Connected: true, HaveStatus: true, ConnectionGeneration: 7,
		Status: controller.Status{DoorOpen: true}, ProgramState: controller.ProgramStateSnapshot{Mode: control.ProgramRunning}}
	reads := 0
	observe := func() controller.Snapshot { reads++; return snapshot }
	manager.observeEventPresentation(config, controller.Event{Kind: "telemetry"}, observe)
	if reads != 1 || !manager.runningDoorWarning || manager.statusLED.snapshot.ConnectionGeneration != 7 || !manager.statusLED.snapshot.Status.DoorOpen {
		t.Fatal("presentation consumers did not share current observation")
	}
	snapshot.ConnectionGeneration = 8
	snapshot.Status.DoorOpen = false
	manager.observeEventPresentation(config, controller.Event{Kind: "connection"}, observe)
	if reads != 2 || manager.runningDoorWarning || manager.statusLED.snapshot.ConnectionGeneration != 8 || manager.statusLED.snapshot.Status.DoorOpen {
		t.Fatal("observation was cached across replacement events")
	}
}

func TestNotificationOnlyObservesForPhysicalDoorContext(t *testing.T) {
	config := appconfig.Defaults()
	config.Integrations.Notifications.ImportantKinds = []string{"door", "warning.door-open-running", "error"}
	config.Integrations.Notifications.DoorRunningToast = true
	reads := 0
	snapshot := controller.Snapshot{Connected: true, HaveStatus: true, Status: controller.Status{DoorOpen: true}}
	observe := func() controller.Snapshot { reads++; return snapshot }
	for _, event := range []controller.Event{{Kind: "telemetry"}, {Kind: "status_led.changed"}, {Kind: "pwm.changed"}, {Kind: "error", Text: "fixture failure"}, {Kind: "door", Source: "bridge"}} {
		_, _, err := notificationJobFromObservation(config, event, observe)
		if err != nil {
			t.Fatal(err)
		}
		if reads != 0 {
			t.Fatalf("non-contextual event %s expanded catalog", event.Kind)
		}
	}
	job, ok, err := notificationJobFromObservation(config, physicalDoorEvent(true), observe)
	if err != nil || !ok || reads != 1 || job.key != "door.opened" {
		t.Fatalf("physical door did not read current context: reads%d ok%v err%v", reads, ok, err)
	}
	// Notification delivery is after action dispatch; it must observe again,
	// not consume the earlier presentation snapshot after a state mutation.
	snapshot.ProgramState.Mode = control.ProgramRunning
	_, ok, err = notificationJobFromObservation(config, physicalDoorEvent(true), observe)
	if err != nil || ok || reads != 2 {
		t.Fatal("Running-door suppression used stale earlier context")
	}
}
