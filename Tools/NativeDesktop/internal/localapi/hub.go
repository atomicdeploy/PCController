package localapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/model"
)

const (
	streamEventSubscriberBuffer       = 64
	streamDiagnosticsSubscriberBuffer = 1
	streamClientBuffer                = 16
	maxStreamClients                  = 32
)

// EventSource is the application state surface needed by the local browser
// bridge. appcore.Core and the headless runtime both satisfy this contract.
type EventSource interface {
	Snapshot() model.Snapshot
	Config() config.Config
	Subscribe(buffer int) (<-chan model.Event, func())
}

// DiagnosticsSource is discovered independently so REST-only/custom event
// sources remain compatible while appcore.Core gains the richer live surface.
type DiagnosticsSource interface {
	DiagnosticsSnapshot() diagnostics.Snapshot
	RefreshDiagnostics(context.Context, ...string) (diagnostics.Snapshot, error)
	SubscribeDiagnostics(buffer int) (<-chan diagnostics.Snapshot, func())
}

// StreamEnvelope is the living, server-to-client WebSocket protocol. Every
// state-bearing event includes an authoritative Snapshot, so reconnecting
// clients never need an event replay log to recover.
type StreamEnvelope struct {
	Sequence      uint64                `json:"sequence"`
	Revision      uint64                `json:"revision,omitempty"`
	Type          string                `json:"type"`
	SentAt        time.Time             `json:"sent_at"`
	Snapshot      *model.Snapshot       `json:"snapshot,omitempty"`
	PreviousState model.LampState       `json:"previous_state,omitempty"`
	InitialSync   bool                  `json:"initial_sync,omitempty"`
	Title         string                `json:"title,omitempty"`
	Message       string                `json:"message,omitempty"`
	Icon          int                   `json:"icon,omitempty"`
	BeepCount     int                   `json:"beep_count,omitempty"`
	FromRemote    bool                  `json:"from_remote,omitempty"`
	Transient     bool                  `json:"transient,omitempty"`
	Config        *PublicConfig         `json:"config,omitempty"`
	Diagnostics   *diagnostics.Snapshot `json:"diagnostics,omitempty"`
}

type streamClient struct {
	// Keep authoritative envelopes, not their serialized representation, in
	// the bounded queue. Large diagnostics JSON is therefore materialized
	// only by a socket writer which is ready to send it, never retained for
	// every pending update in a stalled client's queue.
	send chan StreamEnvelope
}

type eventHub struct {
	mu             sync.Mutex
	clients        map[*streamClient]struct{}
	sequence       uint64
	configRevision uint64
	snapshot       model.Snapshot
	config         PublicConfig
	diagnostics    diagnostics.Snapshot
	hasDiagnostics bool
	closed         bool

	stop                   chan struct{}
	done                   chan struct{}
	unsubscribe            func()
	unsubscribeDiagnostics func()
}

func newEventHub(source EventSource) *eventHub {
	if source == nil {
		return nil
	}
	events, unsubscribe := source.Subscribe(streamEventSubscriberBuffer)
	var diagnosticsEvents <-chan diagnostics.Snapshot
	var unsubscribeDiagnostics func()
	var diagnosticsSnapshot diagnostics.Snapshot
	diagnosticsSource, hasDiagnostics := source.(DiagnosticsSource)
	if hasDiagnostics {
		diagnosticsSnapshot = diagnosticsSource.DiagnosticsSnapshot()
		diagnosticsEvents, unsubscribeDiagnostics =
			diagnosticsSource.SubscribeDiagnostics(
				streamDiagnosticsSubscriberBuffer,
			)
	}
	hub := &eventHub{
		clients:                make(map[*streamClient]struct{}),
		configRevision:         1,
		snapshot:               source.Snapshot(),
		config:                 redactConfig(source.Config()),
		diagnostics:            diagnosticsSnapshot,
		hasDiagnostics:         hasDiagnostics,
		stop:                   make(chan struct{}),
		done:                   make(chan struct{}),
		unsubscribe:            unsubscribe,
		unsubscribeDiagnostics: unsubscribeDiagnostics,
	}
	go hub.run(events, diagnosticsEvents)
	return hub
}

func (h *eventHub) run(
	events <-chan model.Event,
	diagnosticsEvents <-chan diagnostics.Snapshot,
) {
	defer close(h.done)
	for {
		if events == nil && diagnosticsEvents == nil {
			<-h.stop
			return
		}
		select {
		case <-h.stop:
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			h.broadcastEvent(event)
		case snapshot, ok := <-diagnosticsEvents:
			if !ok {
				diagnosticsEvents = nil
				continue
			}
			h.broadcastDiagnostics(snapshot)
		}
	}
}

func (h *eventHub) broadcastEvent(event model.Event) {
	if h == nil {
		return
	}
	snapshot := event.Snapshot
	envelope := StreamEnvelope{
		SentAt:        time.Now().UTC(),
		Snapshot:      &snapshot,
		PreviousState: event.PreviousState,
		InitialSync:   event.InitialSync,
		FromRemote:    event.FromRemote,
	}
	switch event.Kind {
	case model.EventSnapshot:
		envelope.Type = "snapshot"
	case model.EventMessage:
		envelope.Type = "message"
		envelope.Title = event.Title
		envelope.Message = event.Message
		envelope.Icon = event.Icon
	case model.EventBeep:
		envelope.Type = "beep"
		envelope.BeepCount = event.BeepCount
	default:
		// Raw/unrelated device traffic is intentionally not exposed to browser
		// clients. It may contain messages for another device/project.
		return
	}
	h.broadcast(envelope, func() {
		h.snapshot = snapshot
	})
}

func (h *eventHub) broadcastConfig(cfg config.Config) {
	if h == nil {
		return
	}
	public := redactConfig(cfg)
	h.broadcast(StreamEnvelope{
		Type:   "config",
		SentAt: time.Now().UTC(),
		Config: &public,
	}, func() {
		h.config = public
	})
}

func (h *eventHub) broadcastConfigPreview(cfg config.Config) {
	if h == nil {
		return
	}
	public := redactConfig(cfg)
	h.broadcast(StreamEnvelope{
		Type:      "config",
		SentAt:    time.Now().UTC(),
		Transient: true,
		Config:    &public,
	}, func() {
		h.config = public
	})
}

func (h *eventHub) broadcastDiagnostics(snapshot diagnostics.Snapshot) {
	if h == nil {
		return
	}
	snapshot = snapshot.Clone()
	h.broadcast(StreamEnvelope{
		Type:        "diagnostics",
		SentAt:      time.Now().UTC(),
		Diagnostics: &snapshot,
	}, func() {
		h.diagnostics = snapshot
		h.hasDiagnostics = true
	})
}

func (h *eventHub) broadcast(envelope StreamEnvelope, update func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if update != nil {
		update()
	}
	h.sequence++
	envelope.Sequence = h.sequence
	if envelope.Type == "config" {
		if !envelope.Transient {
			h.configRevision++
		}
		envelope.Revision = h.configRevision
	}
	// With no dashboard connected there is no wire representation to build.
	// register constructs an authoritative hello from the state above, so
	// skipping serialization and fan-out is lossless.
	if len(h.clients) == 0 {
		return
	}
	for client := range h.clients {
		select {
		case client.send <- envelope:
		default:
			// A stalled browser must not delay the lamp/network event loop.
			// Disconnect it; a reconnect receives an authoritative hello.
			delete(h.clients, client)
			retireStreamClient(client)
		}
	}
}

func (h *eventHub) register() (*streamClient, error) {
	if h == nil {
		return nil, errors.New("localapi: event stream is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errors.New("localapi: event stream is closed")
	}
	if len(h.clients) >= maxStreamClients {
		return nil, errors.New("localapi: too many event-stream clients")
	}
	client := &streamClient{
		send: make(chan StreamEnvelope, streamClientBuffer),
	}
	snapshot := h.snapshot
	public := h.config
	var diagnosticsSnapshot *diagnostics.Snapshot
	if h.hasDiagnostics {
		value := h.diagnostics.Clone()
		diagnosticsSnapshot = &value
	}
	client.send <- StreamEnvelope{
		Sequence:    h.sequence,
		Revision:    h.configRevision,
		Type:        "hello",
		SentAt:      time.Now().UTC(),
		Snapshot:    &snapshot,
		Config:      &public,
		Diagnostics: diagnosticsSnapshot,
	}
	h.clients[client] = struct{}{}
	return client, nil
}

func (h *eventHub) unregister(client *streamClient) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[client]; !ok {
		return
	}
	delete(h.clients, client)
	retireStreamClient(client)
}

func (h *eventHub) current() (
	uint64,
	uint64,
	model.Snapshot,
	PublicConfig,
	*diagnostics.Snapshot,
) {
	if h == nil {
		return 0, 0, model.Snapshot{}, PublicConfig{}, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var diagnosticsSnapshot *diagnostics.Snapshot
	if h.hasDiagnostics {
		value := h.diagnostics.Clone()
		diagnosticsSnapshot = &value
	}
	return h.sequence, h.configRevision, h.snapshot, h.config, diagnosticsSnapshot
}

func (h *eventHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.stop)
	if h.unsubscribe != nil {
		h.unsubscribe()
		h.unsubscribe = nil
	}
	if h.unsubscribeDiagnostics != nil {
		h.unsubscribeDiagnostics()
		h.unsubscribeDiagnostics = nil
	}
	for client := range h.clients {
		delete(h.clients, client)
		retireStreamClient(client)
	}
	h.mu.Unlock()
	<-h.done
}

// retireStreamClient releases every queued snapshot before closing the sole
// hub-owned send channel. The WebSocket reader may drain concurrently, but no
// producer can race this function because every hub send occurs under h.mu.
// This prevents a timed-out socket from serializing stale diagnostics after it
// has already been evicted.
func retireStreamClient(client *streamClient) {
	if client == nil || client.send == nil {
		return
	}
	for {
		select {
		case _, ok := <-client.send:
			if !ok {
				return
			}
		default:
			close(client.send)
			return
		}
	}
}
