package controller

import (
	"context"
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
