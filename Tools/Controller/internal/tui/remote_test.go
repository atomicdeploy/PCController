package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestRemoteBackendKeepsFullModelAndRelaysPortOpen(t *testing.T) {
	localRuntime := control.New(control.Options{})
	defer localRuntime.Close()

	remoteSnapshot := RichPreviewSnapshot()
	remoteSnapshot.Port.Name = "REMOTE-COM4"
	remoteSnapshot.ConnectionState = "connected through IPC"
	called := make(chan []string, 1)
	engine := shell.New(10)
	if err := engine.Register(shell.Command{
		Name: "port", Usage: "port open|close", Summary: "manage the primary-owned serial port",
		Run: func(_ context.Context, args []string) (string, error) {
			called <- append([]string(nil), args...)
			return "remote port request accepted", nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	model := NewApplicationWithOptions(localRuntime, engine, Options{
		DisableWelcome: true,
		Remote: &RemoteBackend{
			Endpoint: "cafe-pc.local:8787", InitialSnapshot: remoteSnapshot,
		},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 42})
	model = updated.(Model)
	if rendered := model.View(); !strings.Contains(rendered, "REMOTE-COM4") ||
		!strings.Contains(rendered, "PCController") {
		t.Fatalf("remote full TUI did not render the normal dashboard: %q", rendered)
	}

	_, command, handled := model.openPort()
	if !handled || command == nil {
		t.Fatal("remote port open was not routed through the full TUI command path")
	}
	message := command()
	result, ok := message.(commandResultMsg)
	if !ok || result.err != nil || result.output != "remote port request accepted" {
		t.Fatalf("remote command result=%#v", message)
	}
	if args := <-called; len(args) != 1 || args[0] != "open" {
		t.Fatalf("remote port args=%q", args)
	}
	if localRuntime.Snapshot().Connected {
		t.Fatal("remote TUI opened the local serial runtime")
	}
}

func TestRemoteSnapshotFailureIsVisibleAndRecovers(t *testing.T) {
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	updated, _ := model.Update(remoteSnapshotResultMsg{err: context.DeadlineExceeded})
	model = updated.(Model)
	if model.snapshot().Connected || model.snapshot().ConnectionState != "remote IPC unavailable" {
		t.Fatalf("failed remote snapshot=%#v", model.snapshot())
	}

	recovered := RichPreviewSnapshot()
	recovered.Port.Name = "RECOVERED-COM4"
	updated, _ = model.Update(remoteSnapshotResultMsg{snapshot: recovered})
	model = updated.(Model)
	if !model.snapshot().Connected || model.snapshot().Port.Name != "RECOVERED-COM4" ||
		model.remoteSnapshotError != "" {
		t.Fatalf("recovered remote snapshot=%#v error=%q", model.snapshot(), model.remoteSnapshotError)
	}
}

func TestClosedRemoteEventStreamIsTerminalAndDoesNotResubscribe(t *testing.T) {
	events := make(chan control.Event)
	close(events)
	model := Model{
		remote:         &RemoteBackend{Events: events},
		remoteSnapshot: RichPreviewSnapshot(),
	}

	message := waitControlEvent(events)()
	if _, ok := message.(controlEventClosedMsg); !ok {
		t.Fatalf("closed stream message=%T; want controlEventClosedMsg", message)
	}
	updated, command := model.Update(message)
	model = updated.(Model)
	if !model.remoteEventsClosed {
		t.Fatal("closed remote event stream was not recorded as terminal")
	}
	if command != nil {
		t.Fatal("closed stream scheduled another command")
	}
	if len(model.logs) != 1 || !strings.Contains(model.logs[0], "event stream closed") {
		t.Fatalf("closed stream logs=%q", model.logs)
	}

	// A duplicate terminal notification remains inert and does not grow the
	// transcript or restart the closed-channel receive loop.
	updated, command = model.Update(controlEventClosedMsg{})
	model = updated.(Model)
	if command != nil || len(model.logs) != 1 {
		t.Fatalf("duplicate close command=%v logs=%q", command != nil, model.logs)
	}
}

func TestRemoteModelHasNoFillerReadyLogOrHostMutationHooks(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	model := NewApplicationWithOptions(runtime, shell.New(10), Options{
		DisableWelcome: true,
		Remote: &RemoteBackend{
			Endpoint: "cafe-pc.local:8787", InitialSnapshot: RichPreviewSnapshot(),
		},
	})
	if strings.Contains(strings.Join(model.logs, "\n"), "console ready") {
		t.Fatalf("remote transcript contains filler ready copy: %q", model.logs)
	}
	if model.saveHostIntegrations != nil || model.saveRF != nil {
		t.Fatal("remote model must not silently save remote-owned settings to local config")
	}

	model.page = PageAppSettings
	for _, row := range model.appSettingRows() {
		if (strings.HasPrefix(row.Key, "led.") || strings.HasPrefix(row.Key, peripheralNameSettingPrefix)) && row.Editable {
			t.Fatalf("remote-owned setting %q is misleadingly editable", row.Key)
		}
	}
	before := model.rfValue.DisplayRadix
	model.toggleRFRadix()
	if model.rfValue.DisplayRadix != before || !strings.Contains(model.notice, "unavailable") {
		t.Fatalf("remote RF mutation radix=%q notice=%q", model.rfValue.DisplayRadix, model.notice)
	}
}
