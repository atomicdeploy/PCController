package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/sessionsnapshot"
	"pccontroller.local/controller/internal/shell"
)

func TestPrimaryIPCExplicitlyUsesAlphaAuthorizationMode(t *testing.T) {
	t.Setenv("PCCONTROLLER_DATA_DIR", filepath.Join(t.TempDir(), "host-data"))
	previous := currentPrimaryEndpoint()
	defer primaryEndpoint.Store(previous)

	store, err := appconfig.Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Update(func(config *appconfig.Config) error {
		config.IPC.AuthToken = "configured-but-unused-alpha-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	configurePrimaryIPC(store.CurrentRuntime())

	runtime := control.New(control.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(
		ctx, "127.0.0.1:0", runtime, shell.New(4), store,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	response, err := http.Get("http://" + server.listener.Addr().String() + "/api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("X-PCController-Authentication") != "disabled" {
		t.Fatalf("primary status=%d headers=%v", response.StatusCode, response.Header)
	}
}

func TestHostUpdateEventRequestsSecondaryConsoleExit(t *testing.T) {
	if !eventRequestsSecondaryExit(controllerapi.Event{
		Kind: "update.staged", Metadata: map[string]string{"kind": "host"},
	}) {
		t.Fatal("host update did not request secondary console exit")
	}
	for _, event := range []controllerapi.Event{
		{Kind: "update.staged", Metadata: map[string]string{"kind": "firmware"}},
		{Kind: "update.completed", Metadata: map[string]string{"kind": "host"}},
		{Kind: "status_led.changed", Metadata: map[string]string{"kind": "host"}},
	} {
		if eventRequestsSecondaryExit(event) {
			t.Fatalf("unrelated event requested secondary exit: %#v", event)
		}
	}
}

func TestPrimaryIPCClaimsOwnershipAndRoutesCommands(t *testing.T) {
	runtime := control.New(control.Options{})
	engine := shell.New(10)
	if err := engine.Register(shell.Command{
		Name: "echo", Usage: "echo VALUE", Summary: "test command",
		Run: func(_ context.Context, args []string) (string, error) {
			return shell.Join(args), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(
		ctx,
		"127.0.0.1:0",
		runtime,
		engine,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	address := server.listener.Addr().String()

	requestContext, requestCancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer requestCancel()
	output, err := executeThroughPrimaryAt(
		requestContext,
		address,
		joinControllerCommand([]string{"echo", "hello world"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if output != `"hello world"` {
		t.Fatalf("forwarded output = %q", output)
	}

	second, err := startPrimaryIPCAt(
		requestContext,
		address,
		runtime,
		engine,
	)
	if second != nil {
		_ = second.Close()
		t.Fatal("second process unexpectedly claimed primary IPC")
	}
	if !errors.Is(err, errPrimaryAlreadyRunning) {
		t.Fatalf("second claim error = %v", err)
	}
}

func TestPrimaryClosePersistsAndPublishesDiagnosticSnapshotOnce(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	engine := shell.New(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(ctx, "127.0.0.1:0", runtime, engine)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state", "last-session.json")
	server.sessionSnapshot = newHostSessionRecorderAt(
		path,
		server.client,
		func() sessionsnapshot.HostIdentity {
			return sessionsnapshot.HostIdentity{
				Title: "Controller", Role: "primary-host", SourceHash: "test-source",
			}
		},
	)
	afterID := runtime.LatestEventID()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	firstContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := sessionsnapshot.Read(path)
	if err != nil || !stored.Exists || stored.Snapshot == nil {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if stored.Snapshot.Host.SourceHash != "test-source" || stored.Snapshot.Complete ||
		len(stored.Snapshot.Errors) != 3 {
		t.Fatalf("unexpected offline diagnostic snapshot: %#v", stored.Snapshot)
	}
	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	event, err := runtime.WaitEvent(waitContext, afterID, "diagnostic.snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if event.Lifecycle != "saved" || event.State != "partial" ||
		event.Metadata["path"] != path || event.Metadata["complete"] != "false" ||
		event.Metadata["sha256"] == "" {
		t.Fatalf("snapshot event=%#v", event)
	}
	eventID := runtime.LatestEventID()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	secondContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstContent) != string(secondContent) || runtime.LatestEventID() != eventID {
		t.Fatal("duplicate Close rewrote or republished the session snapshot")
	}
}

func TestPrimaryAppPagePreservesTUIDeliveryAndFansOutRuntimeEvent(t *testing.T) {
	runtime := control.New(control.Options{})
	engine := shell.New(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(ctx, "127.0.0.1:0", runtime, engine)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	actions := server.AppActions()
	afterID := runtime.LatestEventID()
	if err := server.actions.Publish(hostui.AppAction{
		Kind: "app.page", Value: "events", Source: "global-hotkey", Target: "webui",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case action := <-actions:
		if action.Kind != "app.page" || action.Value != "events" || action.Target != "webui" {
			t.Fatalf("TUI action=%#v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("app.page did not reach the TUI queue")
	}

	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	first, err := runtime.WaitEvent(waitContext, afterID, "app.page")
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.WaitEvent(waitContext, afterID, "app.page")
	if err != nil {
		t.Fatal(err)
	}
	for index, event := range []control.Event{first, second} {
		if event.ID == 0 || event.Action != "navigate" || event.Source != "global-hotkey" ||
			event.Metadata["page"] != "events" || event.Metadata["value"] != "events" ||
			event.Metadata["target_instance"] != "webui" {
			t.Fatalf("subscriber %d event=%#v", index+1, event)
		}
	}

	// A subscribed TUI queue remains bounded without interrupting the
	// observer-backed browser event stream.
	for index := 0; index < cap(actions); index++ {
		if err := server.actions.Publish(hostui.AppAction{
			Kind: "app.page", Value: "events", Source: "global-hotkey",
		}); err != nil {
			t.Fatalf("fill TUI queue at %d: %v", index, err)
		}
	}
	overflowCursor := runtime.LatestEventID()
	if err := server.actions.Publish(hostui.AppAction{
		Kind: "app.page", Value: "settings", Source: "global-hotkey",
	}); err == nil {
		t.Fatal("expected the full TUI queue to report an error")
	}
	overflowEvent, err := runtime.WaitEvent(waitContext, overflowCursor, "app.page")
	if err != nil {
		t.Fatal(err)
	}
	if overflowEvent.Metadata["page"] != "settings" || overflowEvent.Action != "navigate" {
		t.Fatalf("overflow browser event=%#v", overflowEvent)
	}
}

func TestPrimaryCoordinatesThreeTUIInstancePagesAndMirrorsMetadata(t *testing.T) {
	runtime := control.New(control.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(ctx, "127.0.0.1:0", runtime, shell.New(4))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	actions := server.AppActions()
	makeInstance := func(id, epoch, page, revision string) hostui.AppInstance {
		return hostui.AppInstance{
			ID: id, Surface: "tui", Page: page, State: "active", LeaseSeconds: 45,
			Values: map[string]string{
				hostui.NavigationSyncKey:  hostui.NavigationSyncFollow,
				hostui.NavigationGroupKey: hostui.DefaultNavigationGroup,
				hostui.NavigationEpochKey: epoch, hostui.NavigationRevisionKey: revision,
			},
		}
	}
	one := makeInstance("tui:one", "11111111111111111111111111111111", "dashboard", "1")
	two := makeInstance("tui:two", "22222222222222222222222222222222", "controls", "1")
	three := makeInstance("tui:three", "33333333333333333333333333333333", "settings", "1")
	if _, err := server.instances.Upsert(one); err != nil {
		t.Fatal(err)
	}
	if _, err := server.instances.Upsert(two); err != nil {
		t.Fatal(err)
	}
	if action := <-actions; action.Target != two.ID || action.Value != "dashboard" ||
		action.Metadata[hostui.NavigationRevisionKey] != "1" {
		t.Fatalf("second follower catch-up=%#v", action)
	}
	if _, err := server.instances.Upsert(three); err != nil {
		t.Fatal(err)
	}
	if action := <-actions; action.Target != three.ID || action.Value != "dashboard" {
		t.Fatalf("third follower catch-up=%#v", action)
	}

	afterID := runtime.LatestEventID()
	one.Page, one.Values[hostui.NavigationRevisionKey] = "events", "2"
	if _, err := server.instances.Upsert(one); err != nil {
		t.Fatal(err)
	}
	for _, wantTarget := range []string{three.ID, two.ID} {
		if action := <-actions; action.Target != wantTarget || action.Value != "events" ||
			action.Metadata[hostui.NavigationSourceKey] != one.ID ||
			action.Metadata[hostui.NavigationRevisionKey] != "2" {
			t.Fatalf("fanout action target=%q action=%#v", wantTarget, action)
		}
	}
	waitContext, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	event, err := runtime.WaitEvent(waitContext, afterID, "app.page")
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata[hostui.NavigationSyncKey] != hostui.NavigationSyncGroupUpdate ||
		event.Metadata[hostui.NavigationSourceKey] != one.ID ||
		event.Metadata["target_instance"] == "" {
		t.Fatalf("mirrored synchronization metadata=%#v", event)
	}
}

func TestTerminalAppActionFansOutWithoutInterpretingOSC(t *testing.T) {
	runtime := control.New(control.Options{})
	engine := shell.New(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAt(ctx, "127.0.0.1:0", runtime, engine)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_ = server.AppActions()
	afterID := runtime.LatestEventID()
	if err := server.actions.Publish(hostui.AppAction{
		Kind: "app.progress", Value: "normal 42", Source: "ipc", Target: "tui",
	}); err != nil {
		t.Fatal(err)
	}
	waitContext, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	event, err := runtime.WaitEvent(waitContext, afterID, "app.progress")
	if err != nil {
		t.Fatal(err)
	}
	if event.Action != "progress" || event.Metadata["value"] != "normal 42" ||
		event.Metadata["target_instance"] != "tui" {
		t.Fatalf("terminal app event=%#v", event)
	}
}
