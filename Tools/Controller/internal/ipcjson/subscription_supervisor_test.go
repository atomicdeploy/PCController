package ipcjson

import (
	"context"
	"fmt"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func TestPreservedStatusReplacementDoesNotGapOrderedEvents(t *testing.T) {
	client := controllerapi.New(controllerapi.Options{})
	defer client.Shutdown()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	written := make(chan wsNotification, 64)
	subscriptions := newWebSocketSubscriptions(ctx, client, func(value any) error {
		notification, ok := value.(wsNotification)
		if ok {
			written <- notification
		}
		return nil
	})
	defer subscriptions.stopAll()
	subscriptions.replace(wsSubscription{Topics: []string{"events"}})

	const events = 24
	expected := make(map[uint64]bool, events)
	for index := 0; index < events; index++ {
		if index == events/2 {
			subscriptions.replace(wsSubscription{
				Topics: []string{"status"}, Preserve: true,
				IntervalMS: appconfig.DefaultMeasurementRefreshMS,
			})
		}
		event := client.EmitHostActionEvent(
			"config", fmt.Sprintf("revision %d", index),
			"test", "config.changed", nil,
		)
		expected[event.ID] = true
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for len(expected) > 0 {
		select {
		case notification := <-written:
			if notification.Method != "controller.event" {
				continue
			}
			event, ok := notification.Params.(controllerapi.Event)
			if !ok {
				t.Fatalf("event notification params=%T", notification.Params)
			}
			if !expected[event.ID] {
				t.Fatalf("unexpected or duplicate event ID %d", event.ID)
			}
			delete(expected, event.ID)
		case <-deadline.C:
			t.Fatalf("preserved status replacement lost event IDs: %v", expected)
		}
	}
}
