package localdevice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestManagerOwnsPassiveRefreshTypedActionsAndJSONEvents(t *testing.T) {
	now := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)
	var snapshotSequence atomic.Uint64
	snapshotSequence.Store(1)
	var eventConnections atomic.Int32
	var clientMessages atomic.Int32
	var actionRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(testCapabilities())
		case request.Method == http.MethodGet && request.URL.Path == SnapshotPath:
			sequence := snapshotSequence.Add(1)
			_ = json.NewEncoder(writer).Encode(testSnapshot(sequence, PowerOff, now.Add(time.Duration(sequence)*time.Second)))
		case request.Method == http.MethodPost && request.URL.Path == ActionsPath:
			actionRequests.Add(1)
			var action Action
			if err := json.NewDecoder(request.Body).Decode(&action); err != nil {
				t.Errorf("decode action: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			sequence := snapshotSequence.Add(1)
			_ = json.NewEncoder(writer).Encode(ActionResult{
				Contract: ContractID, Accepted: true, Action: action.Type,
				CompletedAt: now.Add(time.Duration(sequence) * time.Second),
				Snapshot:    snapshotPointer(testSnapshot(sequence, PowerOn, now.Add(time.Duration(sequence)*time.Second))),
			})
		case request.Method == http.MethodGet && request.URL.Path == EventsPath:
			connection, err := websocket.Accept(writer, request, nil)
			if err != nil {
				t.Errorf("accept event connection: %v", err)
				return
			}
			defer connection.CloseNow()
			connectionIndex := eventConnections.Add(1)
			sequence := uint64(100 + connectionIndex)
			event := Event{
				Contract: ContractID, Type: EventSnapshotUpdated, Sequence: sequence,
				At:       now.Add(time.Duration(sequence) * time.Second),
				Snapshot: snapshotPointer(testSnapshot(sequence, PowerOn, now.Add(time.Duration(sequence)*time.Second))),
			}
			encoded, _ := json.Marshal(event)
			if err := connection.Write(request.Context(), websocket.MessageText, encoded); err != nil {
				return
			}
			readContext, cancel := context.WithTimeout(request.Context(), 80*time.Millisecond)
			defer cancel()
			if _, _, err := connection.Read(readContext); err == nil {
				clientMessages.Add(1)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	manager, err := NewManager(context.Background(), ManagerConfig{
		BaseURL: server.URL, EnableEvents: true,
		ReconnectMin: 10 * time.Millisecond, ReconnectMax: 20 * time.Millisecond,
	}, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	waitForCondition(t, 2*time.Second, func() bool {
		status := manager.Snapshot()
		return status.HaveCapabilities && status.HaveDeviceSnapshot && status.LastEventSequence >= 101
	})
	select {
	case event := <-manager.Events():
		if event.Event.Type != EventSnapshotUpdated || event.Event.Snapshot == nil {
			t.Fatalf("manager event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("validated JSON event was not delivered")
	}
	if clientMessages.Load() != 0 {
		t.Fatalf("manager sent %d unsolicited event messages", clientMessages.Load())
	}

	result, err := manager.Action(context.Background(), Action{Type: ActionAlertPulse, Pulses: 3})
	if err != nil || result.Action != ActionAlertPulse || actionRequests.Load() != 1 {
		t.Fatalf("action result=%#v requests=%d err=%v", result, actionRequests.Load(), err)
	}
	beforeRefresh := snapshotSequence.Load()
	if _, err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshotSequence.Load() <= beforeRefresh {
		t.Fatal("passive refresh did not use the snapshot GET")
	}
	inspection, err := manager.Inspect(context.Background(), InspectSnapshot)
	if _, ok := inspection.(SnapshotInspection); err != nil || !ok {
		t.Fatalf("inspection=%T %#v err=%v", inspection, inspection, err)
	}
	waitForCondition(t, time.Second, func() bool { return eventConnections.Load() >= 2 })
}

func TestManagerRejectsResultsFromSupersededGeneration(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	releaseOldRequest := make(chan struct{})
	oldServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(testCapabilities())
		case SnapshotPath:
			_ = json.NewEncoder(writer).Encode(testSnapshot(1, PowerOff, now))
		case ActionsPath:
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-request.Context().Done():
			case <-releaseOldRequest:
			}
		}
	}))
	defer oldServer.Close()
	defer func() {
		select {
		case <-releaseOldRequest:
		default:
			close(releaseOldRequest)
		}
	}()

	newServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case CapabilitiesPath:
			value := testCapabilities()
			value.DeviceID = "pc-unit-new"
			_ = json.NewEncoder(writer).Encode(value)
		case SnapshotPath:
			value := testSnapshot(20, PowerOn, now.Add(time.Minute))
			value.DeviceID = "pc-unit-new"
			_ = json.NewEncoder(writer).Encode(value)
		case ActionsPath:
			var action Action
			_ = json.NewDecoder(request.Body).Decode(&action)
			value := testSnapshot(21, PowerOn, now.Add(time.Minute))
			value.DeviceID = "pc-unit-new"
			_ = json.NewEncoder(writer).Encode(ActionResult{
				Contract: ContractID, Accepted: true, Action: action.Type,
				CompletedAt: now.Add(time.Minute), Snapshot: &value,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer newServer.Close()

	manager, err := NewManager(context.Background(), ManagerConfig{BaseURL: oldServer.URL}, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	operationDone := make(chan error, 1)
	go func() {
		_, operationErr := manager.Action(context.Background(), Action{Type: ActionPowerOn})
		operationDone <- operationErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old-generation action did not start")
	}
	if err := manager.Update(ManagerConfig{BaseURL: newServer.URL}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operationDone:
		if !errors.Is(err, ErrGenerationChanged) {
			t.Fatalf("stale action error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("old-generation action did not cancel")
	}
	close(releaseOldRequest)
	waitForCondition(t, 2*time.Second, func() bool {
		status := manager.Snapshot()
		return status.ConfigurationVersion == 2 && status.Device.DeviceID == "pc-unit-new"
	})
	if _, err := manager.Action(context.Background(), Action{Type: ActionPowerToggle}); err != nil {
		t.Fatalf("new-generation action: %v", err)
	}
}

func TestManagerDropsOutOfOrderEvents(t *testing.T) {
	now := time.Date(2026, 8, 2, 16, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(testCapabilities())
		case SnapshotPath:
			_ = json.NewEncoder(writer).Encode(testSnapshot(1, PowerOff, now))
		case EventsPath:
			connection, err := websocket.Accept(writer, request, nil)
			if err != nil {
				return
			}
			defer connection.CloseNow()
			for _, sequence := range []uint64{20, 19} {
				event := Event{
					Contract: ContractID, Type: EventDeviceNotice,
					Sequence: sequence, At: now.Add(time.Duration(sequence) * time.Second), Notice: "ordered",
				}
				encoded, _ := json.Marshal(event)
				_ = connection.Write(request.Context(), websocket.MessageText, encoded)
			}
			_ = connection.Close(websocket.StatusNormalClosure, "test sequence complete")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	manager, err := NewManager(context.Background(), ManagerConfig{BaseURL: server.URL, EnableEvents: true}, ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	select {
	case event := <-manager.Events():
		if event.Event.Sequence != 20 {
			t.Fatalf("first event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("ordered event was not delivered")
	}
	time.Sleep(30 * time.Millisecond)
	select {
	case event := <-manager.Events():
		t.Fatalf("out-of-order event was delivered: %#v", event)
	default:
	}
	if status := manager.Snapshot(); status.LastEventSequence != 20 {
		t.Fatalf("last sequence=%d", status.LastEventSequence)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}
