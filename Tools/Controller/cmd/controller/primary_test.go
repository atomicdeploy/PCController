package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/sessionsnapshot"
	"pccontroller.local/controller/internal/shell"
)

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
		Kind: "app.page", Value: "events", Source: "global-hotkey",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case action := <-actions:
		if action.Kind != "app.page" || action.Value != "events" {
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
			event.Metadata["page"] != "events" || event.Metadata["value"] != "events" {
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
