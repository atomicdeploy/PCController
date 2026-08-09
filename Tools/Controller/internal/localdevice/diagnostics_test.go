package localdevice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInspectionDocumentsContainOnlySafeKnownFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
	capability := InspectCapabilityDocument(testCapabilities())
	if len(capability.Actions) != 5 || capability.Actions[0] != ActionAlertPulse {
		t.Fatalf("sorted capability inspection=%#v", capability)
	}
	snapshot := testSnapshot(12, PowerOn, now)
	snapshot.DisplayMessage = "sensitive screen content"
	inspection := InspectSnapshotDocument(snapshot)
	if !inspection.DisplayMessagePresent || inspection.DisplayMessageBytes != len([]byte(snapshot.DisplayMessage)) {
		t.Fatalf("snapshot inspection=%#v", inspection)
	}
	encoded, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), snapshot.DisplayMessage) || strings.Contains(string(encoded), "display_message\"") {
		t.Fatalf("display content leaked into diagnostic: %s", encoded)
	}
}

func TestClientInspectAllowsOnlyNamedSafeResources(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case CapabilitiesPath:
			_ = json.NewEncoder(writer).Encode(testCapabilities())
		case SnapshotPath:
			_ = json.NewEncoder(writer).Encode(testSnapshot(3, PowerOff, now))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := client.Inspect(context.Background(), InspectCapabilities)
	if _, ok := capabilities.(CapabilityInspection); err != nil || !ok {
		t.Fatalf("capability inspection=%T %#v err=%v", capabilities, capabilities, err)
	}
	snapshot, err := client.Inspect(context.Background(), InspectSnapshot)
	if _, ok := snapshot.(SnapshotInspection); err != nil || !ok {
		t.Fatalf("snapshot inspection=%T %#v err=%v", snapshot, snapshot, err)
	}
	for _, resource := range []string{"/", "raw", "config", CapabilitiesPath} {
		if _, err := client.Inspect(context.Background(), resource); !errors.Is(err, ErrUnsupportedInspection) {
			t.Errorf("Inspect(%q) error=%v", resource, err)
		}
	}
}
