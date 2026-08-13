package hostbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestAuthenticatedPeerBuzzerEventStaysStructuredAndLoopSafe(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()
	raw, _ := json.Marshal(controller.Event{
		ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board",
		Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"},
	})
	if !manager.ingestPeerEvent("cafe-pc", raw) {
		t.Fatal("valid peer event was not accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, "buzzer.note")
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata["bridge.ingress"] != "cafe-pc" ||
		event.Metadata["bridge.original_source"] != "board" ||
		event.Source != "bridge" {
		t.Fatalf("peer provenance=%#v source=%q", event.Metadata, event.Source)
	}
	if bridgeEventForwardable(controller.Event{Kind: event.Kind, Metadata: event.Metadata}) {
		t.Fatal("ingressed event could be forwarded into a bridge cycle")
	}
	config := appconfig.DefaultBuzzerMirror()
	config.Enabled, config.NativeEnabled = true, true
	if job, ok := buzzerMirrorJobFor(config, controller.Event{Kind: event.Kind, Metadata: event.Metadata}); !ok || job.frequencyHz != 880 || job.durationMS != 125 {
		t.Fatalf("mirrored job=%+v ok=%t", job, ok)
	}
}

func TestBuzzerMirrorJobRequiresOptInAndValidBoardNote(t *testing.T) {
	config := appconfig.DefaultBuzzerMirror()
	event := controller.Event{Kind: "buzzer.note", Metadata: map[string]string{
		"frequency_hz": "440", "duration_ms": "220", "muted": "false",
	}}
	if _, ok := buzzerMirrorJobFor(config, event); ok {
		t.Fatal("disabled mirror accepted a note")
	}
	config.Enabled = true
	config.NativeEnabled = true
	config.DriverDirectory = `C:\optional\winring0`
	job, ok := buzzerMirrorJobFor(config, event)
	if !ok || job.frequencyHz != 440 || job.durationMS != 220 {
		t.Fatalf("job=%+v ok=%t", job, ok)
	}
	event.Metadata["muted"] = "true"
	if _, ok := buzzerMirrorJobFor(config, event); !ok {
		t.Fatal("board-silent note did not reach the independently enabled host path")
	}
}

func TestNativeBuzzerFailuresAreStateTransitionsNotPerNoteLogSpam(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()

	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	first := runtime.LatestEventID()
	if first != after+1 || manager.Status().BuzzerNativeState != "failed" {
		t.Fatalf("first failure id=%d status=%#v", first, manager.Status())
	}
	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	if runtime.LatestEventID() != first {
		t.Fatal("identical per-note failure emitted another activity event")
	}
	manager.recordNativeBuzzerResult(nil)
	if runtime.LatestEventID() != first+1 || manager.Status().BuzzerNativeState != "ready" {
		t.Fatalf("recovery id=%d status=%#v", runtime.LatestEventID(), manager.Status())
	}
}
