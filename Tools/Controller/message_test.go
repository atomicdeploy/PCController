package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
