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
		Source: "ipc", Targets: []string{"surface:desktop", "webui", "webui"},
		Type: "operator.notice", Text: "Ready", Action: "app.page:events",
		Severity: "warning", Correlation: "commission-42", Delivery: "async",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != "message" || event.Lifecycle != "accepted" || event.Severity != "warning" ||
		event.Correlation != "commission-42" || event.Delivery != "async" ||
		!reflect.DeepEqual(event.Targets, []string{"surface:desktop", "webui"}) || event.Action != "app.page:events" {
		t.Fatalf("event=%+v", event)
	}
}

func TestTextMessageRejectsUnknownTargetAndInvalidDelivery(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	for _, message := range []TextMessage{
		{Source: "ipc", Targets: []string{"bad target"}, Type: "operator.notice", Text: "x"},
		{Source: "ipc", Targets: []string{"webui"}, Type: "operator.notice", Text: "x", Delivery: "later"},
	} {
		if _, err := client.SendTextMessage(context.Background(), message); err == nil {
			t.Fatalf("message %#v unexpectedly accepted", message)
		}
	}
}

func TestTextMessageDisconnectedDeliveryMatchesTargets(t *testing.T) {
	client := New(Options{})
	defer client.Close()
	if _, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Targets: []string{"webui"}, Type: "operator.notice", Text: "host only",
	}); err != nil {
		t.Fatalf("host-only message while disconnected: %v", err)
	}
	if _, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Targets: []string{"lcd"}, Type: "operator.notice", Text: "board",
	}); err == nil || err.Error() != "message target requires a connected board" {
		t.Fatalf("explicit LCD delivery error = %v", err)
	}
	if _, err := client.SendTextMessage(context.Background(), TextMessage{
		Source: "ipc", Targets: []string{"all"}, Type: "operator.notice", Text: "live receivers",
	}); err != nil {
		t.Fatalf("all live receivers while disconnected: %v", err)
	}
}
