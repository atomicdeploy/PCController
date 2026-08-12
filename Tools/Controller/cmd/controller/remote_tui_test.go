package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestRemoteTUIPollEventsBacksOffAndRediscoversAfterTransportFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &remoteTUIIPC{
		events: make(chan control.Event, 1), done: make(chan struct{}), retry: 80 * time.Millisecond,
	}
	var calls atomic.Int32
	fourthCall := make(chan struct{})
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		call := calls.Add(1)
		if call == 1 {
			if method != "controller.event.latest" {
				t.Fatalf("first method=%q", method)
			}
			target.(*struct {
				ID uint64 `json:"id"`
			}).ID = 9
			return nil
		}
		if call == 2 && method != "controller.event.next" {
			t.Errorf("second method=%q, want event.next", method)
		}
		if (call == 3 || call == 4) && method != "controller.event.latest" {
			t.Errorf("call %d method=%q, want event.latest", call, method)
		}
		if call == 4 {
			close(fourthCall)
		}
		return errors.New("primary unavailable")
	}
	started := time.Now()
	go client.pollEvents(ctx)
	select {
	case <-fourthCall:
	case <-time.After(time.Second):
		t.Fatal("poller did not retry")
	}
	cancel()
	<-client.done
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("transport failure retried without backoff: %s", elapsed)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("RPC calls=%d, want one backoff then cursor rediscovery", got)
	}
}

func TestRemoteTUICommandEngineMirrorsPrimaryCatalog(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	primaryEngine := shell.New(10)
	if err := primaryEngine.Register(shell.Command{
		Name: "echo", Aliases: []string{"say"}, Usage: "echo VALUE", Summary: "test command",
		Run: func(_ context.Context, args []string) (string, error) {
			return shell.Join(args), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	serverContext, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	server, err := startPrimaryIPCAt(serverContext, "127.0.0.1:0", runtime, primaryEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client := newRemoteTUIIPC(context.Background(), server.listener.Addr().String(), "")
	defer client.Close()
	requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	remoteEngine, err := client.CommandEngine(requestContext)
	if err != nil {
		t.Fatal(err)
	}
	output, err := remoteEngine.Execute(requestContext, `say "hello remote board"`)
	if err != nil {
		t.Fatal(err)
	}
	if output != `"hello remote board"` {
		t.Fatalf("remote command output=%q", output)
	}
	if snapshot, err := client.Snapshot(requestContext); err != nil || snapshot.Connected {
		t.Fatalf("remote snapshot=%#v err=%v", snapshot, err)
	}
}

func TestRemoteControlEventPreservesTUIFields(t *testing.T) {
	value := remoteControlEvent(controllerapi.Event{
		ID: 7, Kind: "status", Stream: "activity", Text: "updated",
		Opcode: 0x81, Seq: 3, Payload: []byte{1, 2}, Source: "board",
		Metadata: map[string]string{"page": "events"}, RFCode: 0x1234,
	})
	if value.ID != 7 || value.Frame.Opcode != 0x81 || value.Frame.Seq != 3 ||
		value.Source != "board" || value.Metadata["page"] != "events" || value.RFCode != 0x1234 {
		t.Fatalf("converted event=%#v", value)
	}
}
