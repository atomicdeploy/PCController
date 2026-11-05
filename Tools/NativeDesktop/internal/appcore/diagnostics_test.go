package appcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/lamp"
)

func TestDiagnosticsSubscriptionRetainsOnlyNewestWholeSnapshot(t *testing.T) {
	core, err := New(coreTestConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe := core.SubscribeDiagnostics(128)
	defer unsubscribe()
	if got := cap(updates); got != 1 {
		t.Fatalf("diagnostics subscription capacity = %d, want 1", got)
	}

	const publishes = 100
	for index := 0; index < publishes; index++ {
		core.diagnostics.recordSuccess(diagnostics.Parse(
			diagnostics.EndpointInfo,
			[]byte(fmt.Sprintf("hostname: lamp-%03d\n", index)),
			time.Unix(int64(index+1), 0).UTC(),
		))
	}
	if got := len(updates); got != 1 {
		t.Fatalf("queued diagnostics snapshots = %d, want 1", got)
	}
	snapshot := <-updates
	if got, want := snapshot.Device.Hostname, "lamp-099"; got != want {
		t.Fatalf("latest diagnostics hostname = %q, want %q", got, want)
	}
}

func TestRefreshDiagnosticsParsesAndCachesSharedSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case diagnostics.EndpointInfo:
			_, _ = writer.Write([]byte(
				"hostname: test-lamp\n" +
					"program usage: 25%\n" +
					"heap memory: 32768\n" +
					"uptime: 01:02:03\n",
			))
		case diagnostics.EndpointWiFiSignal:
			_, _ = writer.Write([]byte("-48\nExcellent\n"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe := core.SubscribeDiagnostics(16)
	defer unsubscribe()

	snapshot, err := core.RefreshDiagnostics(
		context.Background(),
		diagnostics.EndpointInfo,
		diagnostics.EndpointWiFiSignal,
		diagnostics.EndpointInfo,
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Device.Hostname != "test-lamp" ||
		snapshot.Device.ProgramUsagePercent == nil ||
		*snapshot.Device.ProgramUsagePercent != 25 ||
		snapshot.Device.RSSIDBm == nil || *snapshot.Device.RSSIDBm != -48 ||
		snapshot.Device.SignalQuality != "Excellent" {
		t.Fatalf("diagnostics snapshot = %+v", snapshot.Device)
	}
	if len(snapshot.Endpoints) != 2 {
		t.Fatalf("endpoint cache = %v", snapshot.Endpoints)
	}
	if snapshot.Endpoints[diagnostics.EndpointInfo].Document.Raw == "" {
		t.Fatal("in-process raw document was not retained")
	}

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	observed := false
	for !observed {
		select {
		case update := <-updates:
			_, observed = update.Endpoints[diagnostics.EndpointWiFiSignal]
		case <-timer.C:
			t.Fatal("diagnostics subscriber did not receive cached update")
		}
	}
}

func TestRefreshDiagnosticsRejectsRetiredPUAndQueryEndpoints(t *testing.T) {
	core, err := New(coreTestConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		"/datetime/get",
		"/pu",
		"/info?full=1",
		diagnostics.EndpointConfigAP,
		diagnostics.EndpointConfigST,
	} {
		_, fetchErr := core.RefreshDiagnostics(context.Background(), endpoint)
		if fetchErr == nil {
			t.Errorf("unsafe endpoint %q was accepted", endpoint)
		}
	}
}

func TestAutomaticDiagnosticsRefreshAfterDeviceReachable(t *testing.T) {
	var mu sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests[request.URL.Path]++
		mu.Unlock()
		switch request.URL.Path {
		case "/lamp/get":
			_, _ = writer.Write([]byte("ON"))
		case diagnostics.EndpointInfo:
			_, _ = writer.Write([]byte("hostname: automatic\nuptime: 00:00:01\n"))
		case diagnostics.EndpointUpdateInfo:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"hash":"abc","size":100,"free":200,"date":"2026-07-23 12:00:00"}`))
		case diagnostics.EndpointWiFiSignal:
			_, _ = writer.Write([]byte("-60\nNormal\n"))
		case diagnostics.EndpointWiFiStatus:
			_, _ = writer.Write([]byte("ap status: 1\nst status: 1\n"))
		case diagnostics.EndpointWiFiCountry:
			_, _ = writer.Write([]byte("country: IR\n"))
		case diagnostics.EndpointDescription:
			writer.Header().Set("Content-Type", "application/xml")
			_, _ = writer.Write([]byte(`<device><friendlyName>Automatic Lamp</friendlyName></device>`))
		case diagnostics.EndpointConfigST:
			_, _ = writer.Write([]byte(
				"ssid: Automatic Wi-Fi\n" +
					"pass: private-station-password\n" +
					"dhcp: enabled\n" +
					"ip: 192.168.1.44\n" +
					"netmask: 255.255.255.0\n" +
					"gw: 192.168.1.1\n" +
					"dns: 192.168.1.1\n",
			))
		case diagnostics.EndpointConfigAP:
			_, _ = writer.Write([]byte(
				"ssid: Automatic AP\n" +
					"pass: private-access-point-password\n" +
					"ip: 192.168.4.1\n" +
					"netmask: 255.255.255.0\n",
			))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := core.DiagnosticsSnapshot()
		if len(snapshot.Endpoints) == len(diagnostics.SafeEndpoints()) &&
			snapshot.Device.Hostname == "automatic" &&
			snapshot.Device.Description.FriendlyName == "Automatic Lamp" &&
			snapshot.Device.SSID == "Automatic Wi-Fi" &&
			snapshot.Device.WiFiConnected != nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	snapshot := core.DiagnosticsSnapshot()
	if len(snapshot.Endpoints) != len(diagnostics.SafeEndpoints()) {
		t.Fatalf("automatic endpoint cache = %d, want %d: %+v",
			len(snapshot.Endpoints), len(diagnostics.SafeEndpoints()), snapshot.Endpoints)
	}
	if snapshot.Device.WiFiConnected == nil || !*snapshot.Device.WiFiConnected ||
		snapshot.Device.SSID != "Automatic Wi-Fi" ||
		snapshot.Device.WiFiIP != "192.168.1.44" ||
		snapshot.Device.AccessPointSSID != "Automatic AP" {
		t.Fatalf("automatic network projection = %+v", snapshot.Device)
	}
	if _, exposed := snapshot.Endpoints[diagnostics.EndpointConfigST]; exposed {
		t.Fatal("station configuration source escaped into the public endpoint cache")
	}
	if _, exposed := snapshot.Endpoints[diagnostics.EndpointConfigAP]; exposed {
		t.Fatal("access-point configuration source escaped into the public endpoint cache")
	}
	serialized, marshalErr := json.Marshal(snapshot)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(serialized), "private-station-password") ||
		strings.Contains(string(serialized), "private-access-point-password") {
		t.Fatalf("serialized diagnostics leaked a Wi-Fi credential: %s", serialized)
	}
	core.diagnostics.mu.RLock()
	for endpoint, source := range core.diagnostics.sources {
		if endpoint != diagnostics.EndpointConfigST &&
			endpoint != diagnostics.EndpointConfigAP {
			continue
		}
		if strings.Contains(source.Document.Raw, "private-") {
			core.diagnostics.mu.RUnlock()
			t.Fatalf("%s retained an exact credential in memory", endpoint)
		}
		for _, field := range source.Document.Fields {
			if strings.Contains(field.Raw, "private-") {
				core.diagnostics.mu.RUnlock()
				t.Fatalf("%s retained an exact credential field", endpoint)
			}
		}
	}
	core.diagnostics.mu.RUnlock()
	mu.Lock()
	defer mu.Unlock()
	if requests["/datetime/get"] != 0 || requests["/pu"] != 0 {
		t.Fatalf("automatic refresh reached retired endpoints: %v", requests)
	}
	for _, endpoint := range diagnostics.PollEndpoints() {
		if requests[endpoint] != 1 {
			t.Errorf("automatic requests[%s] = %d, want 1", endpoint, requests[endpoint])
		}
	}
}

func TestDiagnosticFailureKeepsLastGoodDocument(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if fail.Load() {
			http.Error(writer, "offline", http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte("hostname: retained\n"))
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.RefreshDiagnostics(context.Background(), diagnostics.EndpointInfo); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	snapshot, err := core.RefreshDiagnostics(context.Background(), diagnostics.EndpointInfo)
	if err == nil {
		t.Fatal("failed diagnostic refresh returned nil error")
	}
	if snapshot.Device.Hostname != "retained" {
		t.Fatalf("last good projection was discarded: %+v", snapshot.Device)
	}
	result := snapshot.Endpoints[diagnostics.EndpointInfo]
	if result.Error == "" || result.LastSuccess.IsZero() {
		t.Fatalf("failed endpoint status = %+v", result)
	}
}

func TestApplyLampConfigResetsDiagnosticsDeviceGeneration(t *testing.T) {
	core, err := New(coreTestConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	core.diagnostics.recordSuccess(diagnostics.Parse(
		diagnostics.EndpointInfo,
		[]byte("hostname: stale\n"),
		time.Now(),
	))
	updated := core.Config()
	updated.Lamp.BaseURL = "http://127.0.0.1:2"
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	snapshot := core.DiagnosticsSnapshot()
	if snapshot.DeviceURL != "http://127.0.0.1:2" ||
		len(snapshot.Endpoints) != 0 || snapshot.Device.Hostname != "" {
		t.Fatalf("diagnostics generation was not reset: %+v", snapshot)
	}
}

func TestRefreshDiagnosticsNilContext(t *testing.T) {
	core, err := New(coreTestConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = core.RefreshDiagnostics(nil, diagnostics.EndpointInfo)
	if !errors.Is(err, lamp.ErrNilContext) {
		t.Fatalf("nil context error = %v", err)
	}
}
