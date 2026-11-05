package appcore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"pccontroller.local/desktop/compatibility"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/device"
	"pccontroller.local/desktop/internal/model"
)

func coreTestConfig(baseURL string) config.Config {
	cfg := config.Default()
	cfg.Device.BaseURL = baseURL
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	cfg.Device.RequestTimeout = config.Duration(2 * time.Second)
	return cfg
}

func TestStartPublishesInitialHTTPState(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != compatibility.StateEndpoint("get") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"state":"ON"}`))
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := core.Subscribe(8)
	defer unsubscribe()
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()

	event := waitForState(t, events, model.StateOn)
	if event.PreviousState != model.StateUnknown || !event.InitialSync {
		t.Fatalf("initial event = %#v", event)
	}
	if event.Snapshot.LastSource != "http" || !event.Snapshot.HTTPReachable {
		t.Fatalf("snapshot = %#v", event.Snapshot)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestStopCancelsAndJoinsNetworkRun(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		close(exited)
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("device request did not start")
	}

	stopped := make(chan struct{})
	go func() {
		core.Stop()
		close(stopped)
	}()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("request context was not canceled")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not join the network goroutine")
	}
	snapshot := core.Snapshot()
	if snapshot.State != model.StateUnknown || snapshot.LastError != "" {
		t.Fatalf("canceled generation changed state: %#v", snapshot)
	}

	// Stop is intentionally idempotent.
	core.Stop()
}

func TestApplyNonDeviceConfigPreservesConnectionAndState(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("ON"))
	}))
	defer server.Close()

	core, err := New(coreTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := core.Subscribe(8)
	defer unsubscribe()
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	_ = waitForState(t, events, model.StateOn)

	updated := core.Config()
	updated.UI.AlwaysOnTop = !updated.UI.AlwaysOnTop
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	if snapshot := core.Snapshot(); snapshot.State != model.StateOn {
		t.Fatalf("state reset for UI-only config: %#v", snapshot)
	}
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != 1 {
		t.Fatalf("UI-only config restarted device network; requests = %d", requests.Load())
	}
}

func TestApplyNonDeviceConfigDoesNotConstructDeviceClient(t *testing.T) {
	core, err := New(coreTestConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	var constructions atomic.Int32
	core.clientFactory = func(cfg config.Config) (*device.Client, error) {
		constructions.Add(1)
		return newDeviceClient(cfg)
	}

	updated := core.Config()
	updated.UI.AlwaysOnTop = !updated.UI.AlwaysOnTop
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	if got := constructions.Load(); got != 0 {
		t.Fatalf("UI-only ApplyConfig constructed %d device clients", got)
	}

	transport := &coreCloseIdleTransport{}
	trackedClient, err := device.New(
		core.Config().Device.BaseURL,
		device.WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}
	core.configMu.Lock()
	core.client = trackedClient
	core.configMu.Unlock()

	updated = core.Config()
	updated.Device.BaseURL = "http://127.0.0.1:2"
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	if got := constructions.Load(); got != 1 {
		t.Fatalf("device ApplyConfig constructed %d clients, want 1", got)
	}
	if got := transport.closeCount(); got != 1 {
		t.Fatalf("replaced client idle closes = %d, want 1", got)
	}
}

func TestNetworkLifecycleClosesIdleHTTPConnectionsAfterJoin(t *testing.T) {
	cfg := coreTestConfig("http://127.0.0.1:1")
	core, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	transport := &coreCloseIdleTransport{}
	client, err := device.New(
		cfg.Device.BaseURL,
		device.WithHTTPClient(&http.Client{
			Timeout:   cfg.Device.RequestTimeout.Value(),
			Transport: transport,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	core.configMu.Lock()
	core.client = client
	core.configMu.Unlock()

	events, unsubscribe := core.Subscribe(8)
	defer unsubscribe()
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, events, model.StateOn)

	if err := core.Reconnect(); err != nil {
		t.Fatal(err)
	}
	if got := transport.closeCount(); got != 1 {
		t.Fatalf("Reconnect idle closes = %d, want 1", got)
	}
	core.Stop()
	if got := transport.closeCount(); got != 2 {
		t.Fatalf("Stop idle closes = %d, want 2", got)
	}
	core.Stop()
	if got := transport.closeCount(); got != 2 {
		t.Fatalf("idempotent Stop idle closes = %d, want 2", got)
	}
}

func TestApplyDeviceConfigResetsThenUsesNewGeneration(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ON"))
	}))
	defer first.Close()

	releaseSecond := make(chan struct{})
	secondEntered := make(chan struct{})
	var secondOnce sync.Once
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondOnce.Do(func() { close(secondEntered) })
		select {
		case <-releaseSecond:
			_, _ = w.Write([]byte("OFF"))
		case <-r.Context().Done():
		}
	}))
	defer second.Close()

	core, err := New(coreTestConfig(first.URL))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := core.Subscribe(16)
	defer unsubscribe()
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	_ = waitForState(t, events, model.StateOn)

	core.stateMu.Lock()
	core.state.LastError = "stale"
	core.state.PendingCommand = "on"
	core.state.LastEvent = "old"
	core.stateMu.Unlock()

	core.lifecycleMu.Lock()
	oldGeneration := core.generation
	core.lifecycleMu.Unlock()

	updated := core.Config()
	updated.Device.BaseURL = second.URL
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	reset := waitForState(t, events, model.StateUnknown)
	if reset.PreviousState != model.StateOn {
		t.Fatalf("reset previous state = %s, want on", reset.PreviousState)
	}
	if reset.Snapshot.DeviceURL != second.URL || reset.Snapshot.LastError != "" ||
		reset.Snapshot.PendingCommand != "" || reset.Snapshot.LastEvent != "" {
		t.Fatalf("reset retained stale state: %#v", reset.Snapshot)
	}

	// A callback that passed through the old socket after replacement must not
	// mark the new device online or overwrite its state.
	core.handleConnectionForGeneration(oldGeneration, device.ConnectionStatus{
		Phase:  device.ConnectionOnline,
		Online: true,
		At:     time.Now().UTC(),
	})
	core.handleDeviceEventForGeneration(oldGeneration, model.Event{
		Kind:     model.EventSnapshot,
		Snapshot: model.Snapshot{State: model.StateOn},
	})
	if snapshot := core.Snapshot(); snapshot.State != model.StateUnknown || snapshot.WebSocketOnline {
		t.Fatalf("stale generation changed replacement state: %#v", snapshot)
	}

	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement device request did not start")
	}
	close(releaseSecond)
	final := waitForState(t, events, model.StateOff)
	if !final.InitialSync || final.Snapshot.DeviceURL != second.URL {
		t.Fatalf("replacement event = %#v", final)
	}
}

func TestConfigCopiesDiagnosticSlices(t *testing.T) {
	cfg := coreTestConfig("http://127.0.0.1:1")
	original := cfg.Diagnostics.SafeEndpoints[0]
	core, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Diagnostics.SafeEndpoints[0] = "/mutated-input"
	first := core.Config()
	if first.Diagnostics.SafeEndpoints[0] != original {
		t.Fatalf("input mutation reached core: %#v", first.Diagnostics.SafeEndpoints)
	}
	first.Diagnostics.SafeEndpoints[0] = "/mutated-output"
	if got := core.Config().Diagnostics.SafeEndpoints[0]; got != original {
		t.Fatalf("returned config aliases core storage: %q", got)
	}

	updated := core.Config()
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatal(err)
	}
	updated.Diagnostics.SafeEndpoints[0] = "/after-apply"
	if got := core.Config().Diagnostics.SafeEndpoints[0]; got != original {
		t.Fatalf("applied config aliases caller storage: %q", got)
	}
}

func waitForState(t *testing.T, events <-chan model.Event, state model.OutputState) model.Event {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == model.EventSnapshot && event.Snapshot.State == state {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for state %s", state)
		}
	}
}

type coreCloseIdleTransport struct {
	mu     sync.Mutex
	closes int
}

func (*coreCloseIdleTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ON")),
		Request:    request,
	}, nil
}

func (t *coreCloseIdleTransport) CloseIdleConnections() {
	t.mu.Lock()
	t.closes++
	t.mu.Unlock()
}

func (t *coreCloseIdleTransport) closeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closes
}
