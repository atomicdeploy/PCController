package hostui

import (
	"testing"
	"time"
)

func liveActionInstance(t *testing.T, registry *InstanceRegistry, id, surface, capabilities string) {
	t.Helper()
	values := map[string]string{}
	if capabilities != "" {
		values[ActionCapabilitiesKey] = capabilities
	}
	if _, err := registry.Upsert(AppInstance{
		ID: id, Surface: surface, State: "active", LeaseSeconds: 45, Values: values,
	}); err != nil {
		t.Fatal(err)
	}
}

func actionDeliveryID(t *testing.T, deliveries []AppAction, instanceID string) string {
	t.Helper()
	for _, delivery := range deliveries {
		if delivery.Target == instanceID {
			value := delivery.Metadata[ActionDeliveryIDKey]
			if value == "" {
				t.Fatalf("delivery for %s has no delivery id: %#v", instanceID, delivery)
			}
			return value
		}
	}
	t.Fatalf("no delivery for %s in %#v", instanceID, deliveries)
	return ""
}

func TestActionCoordinatorFreezesExactTargetsAndTracksAcks(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:one", "tui", TUIActionCapabilities)
	liveActionInstance(t, registry, "web:one", "webui", WebActionCapabilities)
	liveActionInstance(t, registry, "host:bridge", "bridge", "")
	var deliveries []AppAction
	coordinator := NewActionCoordinator(registry, func(action AppAction) error {
		deliveries = append(deliveries, action)
		return nil
	})
	defer coordinator.Close()
	var changes []ActionOutcomeChange
	coordinator.SetObserver(func(change ActionOutcomeChange) { changes = append(changes, change) })

	operation, err := coordinator.Submit(AppAction{
		Kind: "app.title", Value: "Bench", Source: "test", Target: "*",
		OperationID: "operation-one",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != ActionStateQueued || len(operation.Targets) != 2 || len(deliveries) != 2 {
		t.Fatalf("operation=%#v deliveries=%#v", operation, deliveries)
	}
	for index, delivery := range deliveries {
		if delivery.OperationID != operation.OperationID || delivery.Target != operation.Targets[index].InstanceID {
			t.Fatalf("delivery[%d]=%#v target=%#v", index, delivery, operation.Targets[index])
		}
		if expiresAt, parseErr := time.Parse(time.RFC3339Nano, delivery.Metadata[ActionExpiresAtKey]); parseErr != nil || !expiresAt.Equal(operation.ExpiresAt) {
			t.Fatalf("delivery[%d] expiry=%q err=%v want=%s", index, delivery.Metadata[ActionExpiresAtKey], parseErr, operation.ExpiresAt)
		}
	}

	operation, err = coordinator.Ack(ActionAck{
		OperationID: operation.OperationID, DeliveryID: actionDeliveryID(t, deliveries, "tui:one"),
		InstanceID: "tui:one", State: ActionStateApplied,
	})
	if err != nil || operation.State != ActionStateQueued {
		t.Fatalf("first ack operation=%#v err=%v", operation, err)
	}
	operation, err = coordinator.Ack(ActionAck{
		OperationID: operation.OperationID, DeliveryID: actionDeliveryID(t, deliveries, "web:one"),
		InstanceID: "web:one",
		State:      ActionStateRejected, Reason: "browser_policy",
	})
	if err != nil || operation.State != ActionStatePartial {
		t.Fatalf("second ack operation=%#v err=%v", operation, err)
	}
	if len(changes) != 4 || changes[2].State != ActionStateApplied || changes[3].State != ActionStateRejected {
		t.Fatalf("changes=%#v", changes)
	}
}

func TestActionCoordinatorDeduplicatesAndRejectsConflictingOperationID(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:one", "tui", TUIActionCapabilities)
	published := 0
	coordinator := NewActionCoordinator(registry, func(AppAction) error { published++; return nil })
	defer coordinator.Close()
	action := AppAction{
		Kind: "app.progress", Value: "normal 42", Target: "tui:one",
		OperationID: "dedupe-one",
	}
	first, err := coordinator.Submit(action, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.Submit(action, time.Second)
	if err != nil || second.OperationID != first.OperationID || published != 1 {
		t.Fatalf("second=%#v published=%d err=%v", second, published, err)
	}
	action.Value = "normal 43"
	if _, err := coordinator.Submit(action, time.Second); err == nil {
		t.Fatal("conflicting operation id was accepted")
	}
}

func TestActionCoordinatorRejectsCallerOwnedDeliveryMetadata(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:one", "tui", TUIActionCapabilities)
	published := 0
	coordinator := NewActionCoordinator(registry, func(AppAction) error { published++; return nil })
	defer coordinator.Close()

	for _, key := range []string{ActionDeliveryIDKey, ActionExpiresAtKey} {
		t.Run(key, func(t *testing.T) {
			_, err := coordinator.Submit(AppAction{
				Kind: "app.title", Value: "Bench", Target: "tui:one",
				Metadata: map[string]string{key: "caller-owned"},
			}, time.Second)
			if err == nil {
				t.Fatalf("caller-supplied %s was accepted", key)
			}
		})
	}
	if published != 0 {
		t.Fatalf("published=%d after rejected coordinator metadata", published)
	}
}

func TestActionCoordinatorRejectsSubMillisecondTimeout(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:one", "tui", TUIActionCapabilities)
	published := 0
	coordinator := NewActionCoordinator(registry, func(AppAction) error { published++; return nil })
	defer coordinator.Close()

	_, err := coordinator.Submit(AppAction{
		Kind: "app.title", Value: "Bench", Target: "tui:one",
	}, time.Millisecond-time.Nanosecond)
	if err == nil {
		t.Fatal("sub-millisecond timeout was accepted")
	}
	if published != 0 {
		t.Fatalf("published=%d after rejected timeout", published)
	}
}

func TestActionCoordinatorReportsUnknownUnsupportedAndTimedOutTargets(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "web:one", "webui", WebActionCapabilities)
	liveActionInstance(t, registry, "tui:legacy", "tui", "")
	coordinator := NewActionCoordinator(registry, func(AppAction) error { return nil })
	defer coordinator.Close()

	unknown, err := coordinator.Submit(AppAction{
		Kind: "app.title", Value: "Bench", Target: "web:offline",
	}, time.Second)
	if err != nil || unknown.State != ActionStateRejected || unknown.Reason != "target_not_live" || len(unknown.Targets) != 0 {
		t.Fatalf("unknown=%#v err=%v", unknown, err)
	}
	unsupported, err := coordinator.Submit(AppAction{
		Kind: "app.osc", Value: "9;4;1;42", Target: "web:one",
	}, time.Second)
	if err != nil || unsupported.State != ActionStateRejected ||
		len(unsupported.Targets) != 1 || unsupported.Targets[0].Reason != "capability_not_advertised" {
		t.Fatalf("unsupported=%#v err=%v", unsupported, err)
	}
	legacy, err := coordinator.Submit(AppAction{
		Kind: "app.title", Value: "Bench", Target: "tui:legacy",
	}, time.Second)
	if err != nil || legacy.State != ActionStateQueued {
		t.Fatalf("legacy=%#v err=%v", legacy, err)
	}
	coordinator.timeout(legacy.OperationID)
	legacy, err = coordinator.Outcome(legacy.OperationID)
	if err != nil || legacy.State != ActionStateTimeout || legacy.Targets[0].Reason != "ack_timeout" {
		t.Fatalf("timed out legacy=%#v err=%v", legacy, err)
	}
}

func TestActionCoordinatorAckIsExactAndIdempotent(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:one", "tui", TUIActionCapabilities)
	liveActionInstance(t, registry, "tui:two", "tui", TUIActionCapabilities)
	var deliveries []AppAction
	coordinator := NewActionCoordinator(registry, func(action AppAction) error {
		deliveries = append(deliveries, action)
		return nil
	})
	defer coordinator.Close()
	operation, err := coordinator.Submit(AppAction{
		Kind: "app.progress", Value: "warning 73", Target: "tui:one",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	foreign := ActionAck{
		OperationID: operation.OperationID, DeliveryID: actionDeliveryID(t, deliveries, "tui:one"),
		InstanceID: "tui:two", State: ActionStateApplied,
	}
	if _, err := coordinator.Ack(foreign); err == nil {
		t.Fatal("foreign target acknowledgement was accepted")
	}
	forged := foreign
	forged.InstanceID = "tui:one"
	forged.DeliveryID = "forged-delivery"
	if _, err := coordinator.Ack(forged); err == nil {
		t.Fatal("forged delivery acknowledgement was accepted")
	}
	ack := ActionAck{
		OperationID: operation.OperationID, DeliveryID: actionDeliveryID(t, deliveries, "tui:one"),
		InstanceID: "tui:one", State: ActionStateApplied,
	}
	first, err := coordinator.Ack(ack)
	if err != nil || first.State != ActionStateApplied {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := coordinator.Ack(ack)
	if err != nil || second.State != ActionStateApplied {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	ack.State = ActionStateRejected
	if _, err := coordinator.Ack(ack); err == nil {
		t.Fatal("conflicting terminal acknowledgement was accepted")
	}
}

func TestActionCoordinatorAcceptsInFlightAckAfterLeaseDeparture(t *testing.T) {
	registry := NewInstanceRegistry()
	liveActionInstance(t, registry, "tui:departing", "tui", TUIActionCapabilities)
	var deliveries []AppAction
	coordinator := NewActionCoordinator(registry, func(action AppAction) error {
		deliveries = append(deliveries, action)
		return nil
	})
	defer coordinator.Close()
	operation, err := coordinator.Submit(AppAction{
		Kind: "app.title", Value: "Still in flight", Target: "tui:departing",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Remove("tui:departing") {
		t.Fatal("target lease was not removed")
	}
	operation, err = coordinator.Ack(ActionAck{
		OperationID: operation.OperationID, DeliveryID: actionDeliveryID(t, deliveries, "tui:departing"),
		InstanceID: "tui:departing", State: ActionStateApplied,
	})
	if err != nil || operation.State != ActionStateApplied {
		t.Fatalf("in-flight departure ack operation=%#v err=%v", operation, err)
	}
}

func TestActionCapabilitiesAreBoundedAndCanonical(t *testing.T) {
	value, err := ActionCapabilities("app.title", "app.page", "app.title")
	if err != nil || value != "app.page,app.title" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if _, err := ActionCapabilities("future.unbounded"); err == nil {
		t.Fatal("unknown capability was accepted")
	}
	registry := NewInstanceRegistry()
	stored, err := registry.Upsert(AppInstance{
		ID: "web:canonical", Surface: "webui", State: "active",
		Values: map[string]string{ActionCapabilitiesKey: "app.title, app.page,app.title"},
	})
	if err != nil || stored.Values[ActionCapabilitiesKey] != "app.page,app.title" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if _, err := registry.Upsert(AppInstance{
		ID: "web:unbounded", Surface: "webui", State: "active",
		Values: map[string]string{ActionCapabilitiesKey: "app.title,future.unbounded"},
	}); err == nil {
		t.Fatal("registry accepted an unknown app action capability")
	}
}
