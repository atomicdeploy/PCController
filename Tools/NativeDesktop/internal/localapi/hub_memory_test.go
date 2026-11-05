package localapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/diagnostics"
	"pccontroller.local/desktop/internal/model"
)

type hubBufferSource struct {
	events            chan model.Event
	diagnosticEvents  chan diagnostics.Snapshot
	eventBuffer       int
	diagnosticsBuffer int
	cfg               config.Config
	snapshot          model.Snapshot
	diagnostics       diagnostics.Snapshot
}

func newHubBufferSource() *hubBufferSource {
	cfg := config.Default()
	return &hubBufferSource{
		cfg: cfg,
		snapshot: model.Snapshot{
			State:     model.StateOff,
			DeviceURL: cfg.Device.BaseURL,
		},
		diagnostics: diagnostics.NewSnapshot(cfg.Device.BaseURL),
	}
}

func (source *hubBufferSource) Snapshot() model.Snapshot {
	return source.snapshot
}

func (source *hubBufferSource) Config() config.Config {
	return source.cfg
}

func (source *hubBufferSource) Subscribe(
	buffer int,
) (<-chan model.Event, func()) {
	source.eventBuffer = buffer
	source.events = make(chan model.Event, buffer)
	return source.events, func() {}
}

func (source *hubBufferSource) DiagnosticsSnapshot() diagnostics.Snapshot {
	return source.diagnostics.Clone()
}

func (source *hubBufferSource) RefreshDiagnostics(
	context.Context,
	...string,
) (diagnostics.Snapshot, error) {
	return source.diagnostics.Clone(), nil
}

func (source *hubBufferSource) SubscribeDiagnostics(
	buffer int,
) (<-chan diagnostics.Snapshot, func()) {
	source.diagnosticsBuffer = buffer
	source.diagnosticEvents = make(chan diagnostics.Snapshot, buffer)
	return source.diagnosticEvents, func() {}
}

func TestEventHubUsesLatestDiagnosticsSubscriptionAndCompactEnvelopeQueue(
	t *testing.T,
) {
	source := newHubBufferSource()
	hub := newEventHub(source)
	t.Cleanup(hub.close)

	if source.eventBuffer != streamEventSubscriberBuffer {
		t.Fatalf(
			"event subscription buffer = %d, want %d",
			source.eventBuffer,
			streamEventSubscriberBuffer,
		)
	}
	if source.diagnosticsBuffer != streamDiagnosticsSubscriberBuffer {
		t.Fatalf(
			"diagnostics subscription buffer = %d, want latest-only",
			source.diagnosticsBuffer,
		)
	}
	if streamDiagnosticsSubscriberBuffer != 1 {
		t.Fatalf(
			"latest-only diagnostics buffer constant = %d, want 1",
			streamDiagnosticsSubscriberBuffer,
		)
	}

	client, err := hub.register()
	if err != nil {
		t.Fatal(err)
	}
	defer hub.unregister(client)
	if got := cap(client.send); got != streamClientBuffer {
		t.Fatalf(
			"stream client queue capacity = %d, want %d",
			got,
			streamClientBuffer,
		)
	}
	if streamClientBuffer != 16 {
		t.Fatalf(
			"stream client queue constant = %d, want 16",
			streamClientBuffer,
		)
	}
	hello := <-client.send
	if hello.Type != "hello" || hello.Snapshot == nil {
		t.Fatalf("initial envelope = %#v", hello)
	}

	large := diagnostics.NewSnapshot(source.cfg.Device.BaseURL)
	large.Endpoints["/info"] = diagnostics.EndpointResult{
		Document: diagnostics.Document{
			Endpoint: "/info",
			SafeRaw:  strings.Repeat("diagnostic-", 1<<16),
		},
	}
	hub.broadcastDiagnostics(large)
	envelope := <-client.send
	if envelope.Type != "diagnostics" ||
		envelope.Diagnostics == nil ||
		envelope.Diagnostics.Endpoints["/info"].Document.SafeRaw == "" {
		t.Fatalf("diagnostics envelope = %#v", envelope)
	}

	// The queue retains a typed, shared immutable snapshot. Serialization is
	// deferred until a socket is ready to write, while the wire document stays
	// byte-for-byte compatible JSON.
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"type":"diagnostics"`) ||
		!strings.Contains(string(wire), `"diagnostics":`) {
		t.Fatalf("serialized diagnostics envelope = %s", wire)
	}
}

func TestEventHubWithoutClientsRetainsOnlyAuthoritativeState(t *testing.T) {
	hub := &eventHub{
		clients:        make(map[*streamClient]struct{}),
		configRevision: 1,
	}
	snapshot := diagnostics.NewSnapshot("http://pccontroller-desktop.local")
	snapshot.Endpoints["/info"] = diagnostics.EndpointResult{
		Document: diagnostics.Document{
			Endpoint: "/info",
			SafeRaw:  strings.Repeat("x", 1<<20),
		},
	}

	hub.broadcastDiagnostics(snapshot)
	if hub.sequence != 1 || !hub.hasDiagnostics {
		t.Fatalf(
			"authoritative state was not advanced: sequence=%d diagnostics=%v",
			hub.sequence,
			hub.hasDiagnostics,
		)
	}
	if got := hub.diagnostics.Endpoints["/info"].Document.SafeRaw; got == "" {
		t.Fatal("latest diagnostics state was not retained")
	}
	if len(hub.clients) != 0 {
		t.Fatal("a no-client update created a stream client")
	}
}

func TestEventHubOverflowDropsQueuedSnapshotsImmediately(t *testing.T) {
	hub := &eventHub{
		clients:        make(map[*streamClient]struct{}),
		configRevision: 1,
		snapshot: model.Snapshot{
			State: model.StateOff,
		},
		config: redactConfig(config.Default()),
	}
	client, err := hub.register()
	if err != nil {
		t.Fatal(err)
	}
	<-client.send // authoritative hello

	for index := 0; index < streamClientBuffer; index++ {
		hub.broadcastEvent(model.Event{
			Kind: model.EventSnapshot,
			Snapshot: model.Snapshot{
				State:            model.StateOff,
				ReconnectAttempt: index + 1,
			},
		})
	}
	if got := len(client.send); got != streamClientBuffer {
		t.Fatalf("queued envelopes = %d, want %d", got, streamClientBuffer)
	}

	hub.broadcastEvent(model.Event{
		Kind: model.EventSnapshot,
		Snapshot: model.Snapshot{
			State:            model.StateOn,
			ReconnectAttempt: streamClientBuffer + 1,
		},
	})
	if _, registered := hub.clients[client]; registered {
		t.Fatal("overflowing client remained registered")
	}
	if _, ok := <-client.send; ok {
		t.Fatal("evicted client retained stale queued diagnostics/events")
	}
	if hub.snapshot.State != model.StateOn {
		t.Fatal("overflow prevented authoritative state from advancing")
	}
}
