package hostbridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func TestEnabledTextMappingExecutesAllowlistedCommandOnly(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.TextMappings = []appconfig.TextMapping{{
			Name: "trusted-door", Enabled: true,
			Source: "ipc", Target: "host", Type: "door-command",
			Contains: "open", Command: "mark",
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	engine := shell.New(8)
	called := make(chan struct{}, 1)
	if err := engine.Register(shell.Command{
		Name: "mark", Usage: "mark", Summary: "test mapping",
		Run: func(context.Context, []string) (string, error) {
			called <- struct{}{}
			return "marked", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := controller.AttachSharedRuntime(runtime, engine)
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, client, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		manager.Close()
	}()
	_, err = client.SendTextMessage(ctx, controller.TextMessage{
		Source: "ipc", Target: "host", Type: "door-command",
		Text: "door open", Action: "this descriptive text is never executed",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("enabled source/target/type text mapping did not execute")
	}
}

func TestConfiguredToastActionsBecomeActionableProtocolURIs(t *testing.T) {
	notification, err := configuredNotificationActions(
		hostui.Notification{Title: "Door", Body: "opened"},
		[]appconfig.NotificationAction{
			{ID: "events", Label: "Show timeline", Command: "app page events"},
			{ID: "stop", Label: "Stop outputs", Command: "relay off"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(notification.Actions) != 2 ||
		notification.Actions[0].Label != "Show timeline" ||
		notification.Actions[0].URI != "pccontroller://page/events" ||
		notification.Actions[1].URI != "pccontroller://command/relay%20off" {
		t.Fatalf("actions=%#v", notification.Actions)
	}
}

func TestRunningDoorWarningRequiresConnectedLiveDoorAndExplicitRunningState(t *testing.T) {
	snapshot := controller.Snapshot{
		Connected: true, HaveStatus: true,
		Status:       controller.Status{DoorOpen: true},
		ProgramState: controller.ProgramStateSnapshot{Mode: control.ProgramRunning},
	}
	if !runningDoorCondition(snapshot) {
		t.Fatal("connected Running state with an open door did not warn")
	}
	for name, mutate := range map[string]func(*controller.Snapshot){
		"idle": func(value *controller.Snapshot) { value.ProgramState.Mode = control.ProgramIdle },
		"closed": func(value *controller.Snapshot) { value.Status.DoorOpen = false },
		"offline": func(value *controller.Snapshot) { value.Connected = false },
		"no-status": func(value *controller.Snapshot) { value.HaveStatus = false },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := snapshot
			mutate(&candidate)
			if runningDoorCondition(candidate) {
				t.Fatalf("%s state incorrectly warned: %#v", name, candidate)
			}
		})
	}
}
