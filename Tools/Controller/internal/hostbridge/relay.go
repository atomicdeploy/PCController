package hostbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/ipcjson"
)

const (
	relayTraceMetadata       = "pccontroller.relay.trace"
	relayHopsMetadata        = "pccontroller.relay.hops"
	relayLimitMetadata       = "pccontroller.relay.limit"
	relayEventIDMetadata     = "event.id"
	relayEventKindMetadata   = "event.kind"
	relayEventSourceMetadata = "event.source"
	relaySeenLimit           = 4096
	relaySeenTTL             = 10 * time.Minute
)

type relayedEvent struct {
	event controller.Event
	trace ipcjson.RelayTrace
}

type relaySeenEntry struct {
	id string
	at time.Time
}

// relayTracker bounds both cyclic command paths and event fan-out. Entries are
// intentionally process-local: active cascades only need to remain unique for
// longer than their network lifetime, and a hard hop limit remains authoritative.
type relayTracker struct {
	mu       sync.Mutex
	seen     map[string]time.Time
	order    []relaySeenEntry
	accepted map[string]ipcjson.RelayTrace
	now      func() time.Time
}

func (tracker *relayTracker) fresh() (ipcjson.RelayTrace, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ipcjson.RelayTrace{}, fmt.Errorf("create relay trace: %w", err)
	}
	return ipcjson.RelayTrace{
		ID: hex.EncodeToString(raw[:]), Limit: ipcjson.DefaultRelayHopLimit,
	}, nil
}

// advance accepts one trace at most once on this host and consumes one hop.
// It does not inspect or modify authorization state.
func (tracker *relayTracker) advance(trace *ipcjson.RelayTrace) (ipcjson.RelayTrace, error) {
	return tracker.advanceTrace(trace, false)
}

// acceptIncoming consumes the relay hop before a remote notification is
// inserted into the local event stream. The one-shot reservation lets the
// event loop distinguish that accepted delivery from a replay of the same
// trace without trusting caller-controlled metadata.
func (tracker *relayTracker) acceptIncoming(
	trace *ipcjson.RelayTrace,
) (ipcjson.RelayTrace, error) {
	return tracker.advanceTrace(trace, true)
}

func (tracker *relayTracker) advanceTrace(
	trace *ipcjson.RelayTrace,
	reserve bool,
) (ipcjson.RelayTrace, error) {
	var value ipcjson.RelayTrace
	var err error
	if trace == nil {
		value, err = tracker.fresh()
		if err != nil {
			return ipcjson.RelayTrace{}, err
		}
	} else {
		value = *trace
	}
	value.ID = strings.ToLower(strings.TrimSpace(value.ID))
	if err := ipcjson.ValidateRelayTrace(value); err != nil {
		return ipcjson.RelayTrace{}, err
	}
	if value.Hops >= value.Limit {
		return ipcjson.RelayTrace{}, errors.New("relay hop limit reached")
	}

	now := time.Now()
	if tracker.now != nil {
		now = tracker.now()
	}
	tracker.mu.Lock()
	if tracker.seen == nil {
		tracker.seen = make(map[string]time.Time)
	}
	tracker.pruneLocked(now)
	if _, duplicate := tracker.seen[value.ID]; duplicate {
		tracker.mu.Unlock()
		return ipcjson.RelayTrace{}, errors.New("relay trace already crossed this host")
	}
	tracker.seen[value.ID] = now
	tracker.order = append(tracker.order, relaySeenEntry{id: value.ID, at: now})
	value.Hops++
	if reserve {
		if tracker.accepted == nil {
			tracker.accepted = make(map[string]ipcjson.RelayTrace)
		}
		tracker.accepted[value.ID] = value
	}
	tracker.pruneLocked(now)
	tracker.mu.Unlock()
	return value, nil
}

// consumeIncoming claims the one event that was accepted before publication.
// A second copy with the same trace is a replay and must take the ordinary
// duplicate path instead.
func (tracker *relayTracker) consumeIncoming(trace ipcjson.RelayTrace) bool {
	trace.ID = strings.ToLower(strings.TrimSpace(trace.ID))
	if ipcjson.ValidateRelayTrace(trace) != nil {
		return false
	}
	tracker.mu.Lock()
	accepted, ok := tracker.accepted[trace.ID]
	if ok && accepted == trace {
		delete(tracker.accepted, trace.ID)
	} else {
		ok = false
	}
	tracker.mu.Unlock()
	return ok
}

// cancelIncoming releases a reservation when local publication fails, so a
// reconnect can deliver the same upstream trace instead of losing it forever.
func (tracker *relayTracker) cancelIncoming(trace ipcjson.RelayTrace) {
	trace.ID = strings.ToLower(strings.TrimSpace(trace.ID))
	tracker.mu.Lock()
	if accepted, ok := tracker.accepted[trace.ID]; ok && accepted == trace {
		delete(tracker.accepted, trace.ID)
		delete(tracker.seen, trace.ID)
	}
	tracker.mu.Unlock()
}

func (tracker *relayTracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-relaySeenTTL)
	for len(tracker.order) != 0 {
		entry := tracker.order[0]
		if len(tracker.seen) <= relaySeenLimit && entry.at.After(cutoff) {
			break
		}
		tracker.order = tracker.order[1:]
		if recorded, ok := tracker.seen[entry.id]; ok && recorded.Equal(entry.at) {
			delete(tracker.seen, entry.id)
			delete(tracker.accepted, entry.id)
		}
	}
}

func relayMetadata(trace ipcjson.RelayTrace, event controller.Event) map[string]string {
	metadata := relayEventMetadata(event)
	metadata[relayTraceMetadata] = trace.ID
	metadata[relayHopsMetadata] = strconv.Itoa(int(trace.Hops))
	metadata[relayLimitMetadata] = strconv.Itoa(int(trace.Limit))
	return metadata
}

func relayEventMetadata(event controller.Event) map[string]string {
	eventID := strconv.FormatUint(event.ID, 10)
	eventKind := event.Kind
	eventSource := event.Source
	if value, ok := event.Metadata[relayEventIDMetadata]; ok {
		eventID = value
	}
	if value, ok := event.Metadata[relayEventKindMetadata]; ok {
		eventKind = value
	}
	if value, ok := event.Metadata[relayEventSourceMetadata]; ok {
		eventSource = value
	}
	return map[string]string{
		relayEventIDMetadata:     eventID,
		relayEventKindMetadata:   eventKind,
		relayEventSourceMetadata: eventSource,
	}
}

func relayEventKind(event controller.Event) string {
	if value := strings.TrimSpace(event.Metadata[relayEventKindMetadata]); value != "" {
		return value
	}
	return event.Kind
}

func relayEventKindForwardable(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind != "integration.error" && kind != "controller.error" &&
		!strings.HasPrefix(kind, "bridge.") &&
		!strings.HasPrefix(kind, "security.") &&
		!strings.HasPrefix(kind, "transport.")
}

func relayTraceFromMetadata(metadata map[string]string) (*ipcjson.RelayTrace, bool, error) {
	if len(metadata) == 0 {
		return nil, false, nil
	}
	id, haveID := metadata[relayTraceMetadata]
	hopsText, haveHops := metadata[relayHopsMetadata]
	limitText, haveLimit := metadata[relayLimitMetadata]
	if !haveID && !haveHops && !haveLimit {
		return nil, false, nil
	}
	if !haveID || !haveHops || !haveLimit {
		return nil, true, errors.New("relay event metadata is incomplete")
	}
	hops, err := strconv.ParseUint(strings.TrimSpace(hopsText), 10, 8)
	if err != nil {
		return nil, true, errors.New("relay event hops are invalid")
	}
	limit, err := strconv.ParseUint(strings.TrimSpace(limitText), 10, 8)
	if err != nil {
		return nil, true, errors.New("relay event hop limit is invalid")
	}
	trace := ipcjson.RelayTrace{ID: id, Hops: uint8(hops), Limit: uint8(limit)}
	if err := ipcjson.ValidateRelayTrace(trace); err != nil {
		return nil, true, err
	}
	return &trace, true, nil
}

func relayTraceFromEvent(event controller.Event) (*ipcjson.RelayTrace, bool, error) {
	return relayTraceFromMetadata(event.Metadata)
}

// remoteNotificationMessage preserves an existing relay trace, or starts a
// trace for a genuinely remote board/status notification. Legacy untraced
// bridge-message events remain locally visible but are not repeated.
func (manager *Manager) remoteNotificationMessage(
	transport string,
	method string,
	raw json.RawMessage,
) (controller.TextMessage, error) {
	messageType := "remote-event"
	if strings.EqualFold(strings.TrimSpace(method), "controller.status") {
		messageType = "remote-status"
	}
	text := string(raw)
	if len(text) > 4096 {
		text = text[:4096]
	}
	message := controller.TextMessage{
		Source: transport, Target: "host", Type: messageType, Text: text,
	}

	var event controller.Event
	if strings.EqualFold(strings.TrimSpace(method), "controller.event") {
		if err := json.Unmarshal(raw, &event); err != nil {
			return controller.TextMessage{}, fmt.Errorf("decode remote event: %w", err)
		}
		message.Metadata = relayEventMetadata(event)
		if !relayEventKindForwardable(relayEventKind(event)) {
			return message, nil
		}
		trace, present, err := relayTraceFromEvent(event)
		if err != nil {
			return controller.TextMessage{}, err
		}
		if !present && strings.EqualFold(event.Kind, "message") &&
			(strings.EqualFold(event.Source, "bridge") ||
				strings.EqualFold(event.Source, "websocket") ||
				strings.EqualFold(event.Source, "socket_io")) {
			return message, nil
		}
		advanced, err := manager.relay.acceptIncoming(trace)
		if err != nil {
			return controller.TextMessage{}, err
		}
		message.Metadata = relayMetadata(advanced, event)
		return message, nil
	}
	if !strings.EqualFold(strings.TrimSpace(method), "controller.status") {
		return message, nil
	}
	event = controller.Event{Kind: "controller.status", Source: transport}
	trace, err := manager.relay.acceptIncoming(nil)
	if err != nil {
		return controller.TextMessage{}, err
	}
	message.Metadata = relayMetadata(trace, event)
	return message, nil
}

func (manager *Manager) releaseRemoteNotification(message controller.TextMessage) {
	trace, present, err := relayTraceFromMetadata(message.Metadata)
	if err == nil && present && trace != nil {
		manager.relay.cancelIncoming(*trace)
	}
}
