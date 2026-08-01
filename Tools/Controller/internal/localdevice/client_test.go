package localdevice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeBaseURLAllowsOnlyLocalRoots(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"127.0.0.1:8080":            "http://127.0.0.1:8080",
		"http://192.168.1.12/":      "http://192.168.1.12",
		"https://device.local:9443": "https://device.local:9443",
		"http://controller":         "http://controller",
		"http://[fe80::1%25eth0]":   "http://[fe80::1%25eth0]",
	}
	for input, expected := range valid {
		input, expected := input, expected
		t.Run(input, func(t *testing.T) {
			actual, err := NormalizeBaseURL(input)
			if err != nil || actual != expected {
				t.Fatalf("NormalizeBaseURL(%q)=%q, %v; want %q", input, actual, err, expected)
			}
		})
	}
	invalid := []string{
		"", "https://example.com", "http://8.8.8.8", "ftp://127.0.0.1",
		"http://user:pass@127.0.0.1", "http://127.0.0.1/path",
		"http://127.0.0.1?query=1", "http://127.0.0.1#fragment",
		"http://bad_name", "http://127.0.0.1:0", "http://127.0.0.1:70000",
	}
	for _, input := range invalid {
		if _, err := NormalizeBaseURL(input); !errors.Is(err, ErrInvalidBaseURL) {
			t.Errorf("NormalizeBaseURL(%q) error=%v", input, err)
		}
	}
}

func TestClientUsesFixedRoutesAndStrictJSONActions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 2, 11, 12, 13, 0, time.UTC)
	var mu sync.Mutex
	var received []Action
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(testCapabilities())
		case request.Method == http.MethodGet && request.URL.Path == SnapshotPath:
			_ = json.NewEncoder(writer).Encode(testSnapshot(4, PowerOn, now))
		case request.Method == http.MethodPost && request.URL.Path == ActionsPath:
			if request.Header.Get("Content-Type") != "application/json" ||
				request.Header.Get("Accept") != "application/json" {
				t.Errorf("JSON headers=%v", request.Header)
			}
			var action Action
			decoder := json.NewDecoder(io.LimitReader(request.Body, 2048))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&action); err != nil {
				t.Errorf("decode action: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			received = append(received, action)
			mu.Unlock()
			_ = json.NewEncoder(writer).Encode(ActionResult{
				Contract: ContractVersion, Accepted: true, Action: action.Type,
				CompletedAt: now, Snapshot: snapshotPointer(testSnapshot(5, PowerOff, now)),
			})
		default:
			http.Error(writer, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, ClientOptions{UserAgent: "PCController-Test/1"})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil || client.http.CheckRedirect == nil {
		t.Fatalf("client transport was not hardened: %#v", client.http.Transport)
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || capabilities.Contract != ContractVersion || capabilities.DeviceID != "pc-unit-7" {
		t.Fatalf("capabilities=%#v err=%v", capabilities, err)
	}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil || snapshot.Sequence != 4 || snapshot.Power != PowerOn {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	result, err := client.Action(context.Background(), Action{Type: ActionDisplayMessage, Message: "سلام PC"})
	if err != nil || result.Action != ActionDisplayMessage || result.Snapshot == nil || result.Snapshot.Power != PowerOff {
		t.Fatalf("action result=%#v err=%v", result, err)
	}
	mu.Lock()
	if len(received) != 1 || received[0].Message != "سلام PC" || received[0].Type != ActionDisplayMessage {
		t.Fatalf("received actions=%#v", received)
	}
	mu.Unlock()
	if !strings.HasSuffix(client.EventsURL(), EventsPath) || !strings.HasPrefix(client.EventsURL(), "ws://") {
		t.Fatalf("events URL=%q", client.EventsURL())
	}
}

func TestClientRejectsInvalidActionsBeforeNetwork(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []Action{
		{},
		{Type: ActionPowerOn, Message: "unexpected"},
		{Type: ActionDisplayMessage, Pulses: 1},
		{Type: ActionDisplayMessage, Message: strings.Repeat("x", maxMessageBytes+1)},
		{Type: ActionAlertPulse, Pulses: 0},
		{Type: ActionAlertPulse, Pulses: 11},
	}
	for _, action := range invalid {
		if _, err := client.Action(context.Background(), action); !errors.Is(err, ErrInvalidAction) {
			t.Errorf("Action(%#v) error=%v", action, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid actions reached network: %d", requests.Load())
	}
}

func TestClientRefusesRedirectsUnknownFieldsAndOversizedBodies(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case CapabilitiesPath:
			http.Redirect(writer, request, SnapshotPath, http.StatusTemporaryRedirect)
		case SnapshotPath:
			_, _ = io.WriteString(writer, `{"contract":"pccontroller.local-device/v1","device_id":"pc-unit-7","sequence":1,"power":"on","updated_at":"2026-08-02T11:12:13Z","secret":"blocked"}`)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, ClientOptions{BodyLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Capabilities(context.Background()); err == nil {
		t.Fatal("redirect was followed")
	} else {
		var statusError *HTTPStatusError
		if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusTemporaryRedirect {
			t.Fatalf("redirect error=%v", err)
		}
	}
	if _, err := client.Snapshot(context.Background()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("unknown field error=%v", err)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Repeat("x", 65))
	}))
	defer largeServer.Close()
	limited, err := NewClient(largeServer.URL, ClientOptions{BodyLimit: 64})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.Snapshot(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversized error=%v", err)
	}
}

type unsupportedRoundTripper struct{}

func (unsupportedRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func TestClientRejectsTransportThatCannotBeHardened(t *testing.T) {
	t.Parallel()
	_, err := NewClient("http://127.0.0.1", ClientOptions{
		HTTPClient: &http.Client{Transport: unsupportedRoundTripper{}},
	})
	if !errors.Is(err, ErrUnsupportedTransport) {
		t.Fatalf("error=%v", err)
	}
}

func testCapabilities() Capabilities {
	return Capabilities{
		Contract: ContractVersion,
		DeviceID: "pc-unit-7",
		Name:     "PCController local device",
		Model:    "LD-1",
		Firmware: "2026.08",
		Actions: []ActionType{
			ActionPowerOn, ActionPowerOff, ActionPowerToggle,
			ActionDisplayMessage, ActionAlertPulse,
		},
		Events: []EventType{EventSnapshotUpdated, EventActionCompleted, EventDeviceNotice},
	}
}

func testSnapshot(sequence uint64, power PowerState, updatedAt time.Time) Snapshot {
	return Snapshot{
		Contract: ContractVersion, DeviceID: "pc-unit-7", Sequence: sequence,
		Power: power, DisplayMessage: "private display text", AlertPulses: 2,
		UpdatedAt: updatedAt,
	}
}

func snapshotPointer(value Snapshot) *Snapshot { return &value }
