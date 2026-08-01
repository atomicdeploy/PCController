package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/localdevice"
)

var _ interface {
	Status() any
	Action(context.Context, string, string, int) (any, error)
	Inspect(context.Context, string) (any, error)
	Close()
} = (*localDeviceHost)(nil)

func TestLocalDeviceHostTracksConfigAndExposesOnlyV1Operations(t *testing.T) {
	now := time.Date(2026, 8, 2, 17, 0, 0, 0, time.UTC)
	var snapshotReads atomic.Int32
	var actionWrites atomic.Int32
	var mu sync.Mutex
	var lastAction localdevice.Action
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == localdevice.CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(localdevice.Capabilities{
				Contract: localdevice.ContractVersion, DeviceID: "host-test-1",
				Name: "PCController companion", Model: "LD-1", Firmware: "2026.08",
				Actions: []localdevice.ActionType{
					localdevice.ActionPowerOn, localdevice.ActionPowerOff,
					localdevice.ActionPowerToggle, localdevice.ActionDisplayMessage,
					localdevice.ActionAlertPulse,
				},
				Events: []localdevice.EventType{localdevice.EventSnapshotUpdated},
			})
		case request.Method == http.MethodGet && request.URL.Path == localdevice.SnapshotPath:
			sequence := uint64(snapshotReads.Add(1))
			_ = json.NewEncoder(writer).Encode(localdevice.Snapshot{
				Contract: localdevice.ContractVersion, DeviceID: "host-test-1",
				Sequence: sequence, Power: localdevice.PowerOff,
				DisplayMessage: "must stay out of status and inspection", UpdatedAt: now.Add(time.Duration(sequence) * time.Second),
			})
		case request.Method == http.MethodPost && request.URL.Path == localdevice.ActionsPath:
			actionWrites.Add(1)
			var action localdevice.Action
			if err := json.NewDecoder(request.Body).Decode(&action); err != nil {
				t.Errorf("decode action: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			lastAction = action
			mu.Unlock()
			_ = json.NewEncoder(writer).Encode(localdevice.ActionResult{
				Contract: localdevice.ContractVersion, Accepted: true, Action: action.Type,
				CompletedAt: now.Add(time.Minute),
				Snapshot: &localdevice.Snapshot{
					Contract: localdevice.ContractVersion, DeviceID: "host-test-1",
					Sequence: 50, Power: localdevice.PowerOn, UpdatedAt: now.Add(time.Minute),
				},
			})
		case request.Method == http.MethodGet && request.URL.Path == localdevice.EventsPath:
			connection, err := websocket.Accept(writer, request, nil)
			if err != nil {
				return
			}
			defer connection.CloseNow()
			_, _, _ = connection.Read(context.Background())
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	store, err := appconfig.Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Integrations.LocalDevice = appconfig.LocalDevice{Enabled: true, BaseURL: server.URL}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	host := startLocalDeviceHost(context.Background(), nil, store)
	defer host.Close()
	waitForHostCondition(t, 2*time.Second, func() bool {
		status := host.Status().(localDeviceBrowserSnapshot)
		return status.HTTPReachable && status.HaveCapabilities && status.DeviceID == "host-test-1"
	})

	statusValue, err := host.Action(context.Background(), string(localdevice.ActionDisplayMessage), "سلام", 0)
	if err != nil {
		t.Fatal(err)
	}
	status := statusValue.(localDeviceBrowserSnapshot)
	if status.Power != localdevice.PowerOn || status.Phase == "disabled" {
		t.Fatalf("action status=%#v", status)
	}
	mu.Lock()
	if lastAction.Type != localdevice.ActionDisplayMessage || lastAction.Message != "سلام" {
		t.Fatalf("last action=%#v", lastAction)
	}
	mu.Unlock()

	beforeWrites := actionWrites.Load()
	beforeReads := snapshotReads.Load()
	if _, err := host.Action(context.Background(), "passive.refresh", "", 0); err != nil {
		t.Fatal(err)
	}
	if actionWrites.Load() != beforeWrites || snapshotReads.Load() <= beforeReads {
		t.Fatalf("passive refresh writes=%d->%d reads=%d->%d", beforeWrites, actionWrites.Load(), beforeReads, snapshotReads.Load())
	}
	if _, err := host.Action(context.Background(), "on", "", 0); !errors.Is(err, localdevice.ErrInvalidAction) {
		t.Fatalf("legacy-shaped action error=%v", err)
	}
	if actionWrites.Load() != beforeWrites {
		t.Fatal("invalid action reached the upstream device")
	}

	capability, err := host.Inspect(context.Background(), localdevice.InspectCapabilities)
	if _, ok := capability.(localdevice.CapabilityInspection); err != nil || !ok {
		t.Fatalf("capability=%T %#v err=%v", capability, capability, err)
	}
	inspection, err := host.Inspect(context.Background(), localdevice.InspectSnapshot)
	if snapshot, ok := inspection.(localdevice.SnapshotInspection); err != nil || !ok || !snapshot.DisplayMessagePresent {
		t.Fatalf("snapshot inspection=%T %#v err=%v", inspection, inspection, err)
	}
	if _, err := host.Inspect(context.Background(), "raw"); !errors.Is(err, localdevice.ErrUnsupportedInspection) {
		t.Fatalf("unsafe inspection error=%v", err)
	}

	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Integrations.LocalDevice.Enabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	waitForHostCondition(t, time.Second, func() bool {
		return host.Status().(localDeviceBrowserSnapshot).Phase == "disabled"
	})
}

func waitForHostCondition(t *testing.T, timeout time.Duration, condition func() bool) {
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
