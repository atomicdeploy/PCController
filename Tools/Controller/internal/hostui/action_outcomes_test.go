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
	}

	operation, err = coordinator.Ack(ActionAck{
		OperationID: operation.OperationID, InstanceID: "tui:one", State: ActionStateApplied,
	})
	if err != nil || operation.State != ActionStateQueued {
		t.Fatalf("first ack operation=%#v err=%v", operation, err)
	}
	operation, err = coordinator.Ack(ActionAck{
		OperationID: operation.OperationID, InstanceID: "web:one",
		State: ActionStateRejected, Reason: "browser_policy",
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
	coordinator := NewActionCoordinator(registry, func(AppAction) error { return nil })
	defer coordinator.Close()
	operation, err := coordinator.Submit(AppAction{
		Kind: "app.progress", Value: "warning 73", Target: "tui:one",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	foreign := ActionAck{
		OperationID: operation.OperationID, InstanceID: "tui:two", State: ActionStateApplied,
	}
	if _, err := coordinator.Ack(foreign); err == nil {
		t.Fatal("foreign target acknowledgement was accepted")
	}
	ack := ActionAck{
		OperationID: operation.OperationID, InstanceID: "tui:one", State: ActionStateApplied,
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
