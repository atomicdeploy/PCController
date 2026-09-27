package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTextMessagePublishesBoundedMultiTargetEnvelope(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	event, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Targets: []string{"native", "web", "tui", "web"},
		Type: "operator.notice", Text: "Ready", Action: "app.page:events",
		Severity: "warning", Correlation: "commission-42", Delivery: "async",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != "message" || event.Lifecycle != "accepted" || event.Severity != "warning" ||
		event.Correlation != "commission-42" || event.Delivery != "async" ||
		!reflect.DeepEqual(event.Targets, []string{"native", "web", "tui"}) || event.Action != "app.page:events" {
		t.Fatalf("event=%+v", event)
	}
}

func TestMessageDeliveryOutcomeRetainsActionAndCorrelation(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	message, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Target: "native", Type: "operator.notice", Text: "Ready",
		Action: "app page events", Correlation: "commission-42", Delivery: "async",
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := client.EmitMessageDeliveryOutcome(message, "native", nil)
	if completed.Kind != "message.delivery" || completed.Lifecycle != "completed" ||
		completed.State != "delivered" || completed.Correlation != "commission-42" ||
		completed.Action != "app page events" || completed.Target != "native" ||
		completed.Metadata["message_event_id"] == "" {
		t.Fatalf("completed=%+v", completed)
	}
	failed := client.EmitMessageDeliveryOutcome(message, "native", errors.New("backend unavailable"))
	if failed.Lifecycle != "failed" || failed.State != "failed" ||
		failed.Severity != "error" || failed.Metadata["error"] != "backend unavailable" {
		t.Fatalf("failed=%+v", failed)
	}
}

func TestTextMessageRejectsUnknownTargetAndInvalidDelivery(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	for _, message := range []TextMessage{
		{Source: "ipc", Target: "unknown", Type: "operator.notice", Text: "x"},
		{Source: "ipc", Target: "web", Type: "operator.notice", Text: "x", Delivery: "later"},
	} {
		if _, err := client.SendTextMessage(context.Background(), message); err == nil {
			t.Fatalf("message %#v unexpectedly accepted", message)
		}
	}
}

func TestSynchronousMessageWaitsForEveryRequestedSurfaceOutcome(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	afterID := client.LatestEventID()
	acknowledged := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		message, err := client.NextEvent(ctx, afterID, "message")
		if err == nil {
			_, err = client.AcknowledgeMessageDelivery(message.ID, "web", "")
		}
		if err == nil {
			_, err = client.AcknowledgeMessageDelivery(message.ID, "tui", "")
		}
		acknowledged <- err
	}()

	outcome, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Targets: []string{"web", "tui"},
		Type: "operator.prompt", Text: "Inspect output 3",
		Action: "relay off", Correlation: "job-23", Delivery: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-acknowledged; err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != "message.delivery" || outcome.Lifecycle != "completed" ||
		outcome.State != "delivered" || outcome.Correlation != "job-23" ||
		outcome.Metadata["surfaces"] != "web,tui" || outcome.Metadata["message_event_id"] == "" {
		t.Fatalf("outcome=%+v", outcome)
	}
}

func TestSynchronousMessageReportsDisconnectedSurfaceInsteadOfCompleting(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	outcome, err := client.SendTextMessage(ctx, TextMessage{
		Source: "ipc", Target: "web", Type: "operator.prompt",
		Text: "No browser is connected", Delivery: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Lifecycle != "failed" || outcome.State != "failed" ||
		outcome.Metadata["unconfirmed_surfaces"] != "web" {
		t.Fatalf("outcome=%+v", outcome)
	}
}

func TestMessageDeliveryRejectsUnsupportedOrUntargetedSurface(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	message, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Target: "web", Type: "operator.notice",
		Text: "Web only", Delivery: "async",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AcknowledgeMessageDelivery(message.ID, "tui", ""); err == nil {
		t.Fatal("untargeted TUI delivery was accepted")
	}
	if _, err := client.AcknowledgeMessageDelivery(message.ID, "pager", ""); err == nil {
		t.Fatal("unsupported surface was accepted")
	}
}
