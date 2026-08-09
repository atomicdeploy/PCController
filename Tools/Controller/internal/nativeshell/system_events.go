package nativeshell

import "sync"

// SystemEventKind is the closed set of host lifecycle changes surfaced by the
// optional native shell. It is deliberately independent from Win32 message
// numbers so callers never need platform constants or unsafe values.
type SystemEventKind string

const (
	SystemEventSessionLocked   SystemEventKind = "session.locked"
	SystemEventSessionUnlocked SystemEventKind = "session.unlocked"
	SystemEventPowerSuspending SystemEventKind = "power.suspending"
	SystemEventPowerResumed    SystemEventKind = "power.resumed"
	SystemEventNetworkChanged  SystemEventKind = "network.changed"
)

// SystemEvent is the narrow native-shell callback payload. NetworkSignature is
// a one-way digest of normalized interface state; it permits reliable deduping
// without exposing adapter names, addresses, or hardware identifiers.
type SystemEvent struct {
	Kind             SystemEventKind
	NetworkSignature string
	InterfaceCount   int
	AddressCount     int
}

func (event SystemEvent) domain() string {
	switch event.Kind {
	case SystemEventSessionLocked, SystemEventSessionUnlocked:
		return "session"
	case SystemEventPowerSuspending, SystemEventPowerResumed:
		return "power"
	case SystemEventNetworkChanged:
		return "network"
	default:
		return ""
	}
}

// systemEventState collapses duplicate Windows notifications while retaining
// genuine transitions such as locked -> unlocked -> locked. Network events
// include a digest, so a later address-only change is not lost when adapter
// and address counts remain the same.
type systemEventState struct {
	last map[string]SystemEvent
}

func (state *systemEventState) accept(event SystemEvent) bool {
	domain := event.domain()
	if domain == "" {
		return false
	}
	if state.last == nil {
		state.last = make(map[string]SystemEvent, 3)
	}
	if previous, ok := state.last[domain]; ok && previous == event {
		return false
	}
	state.last[domain] = event
	return true
}

type systemEventEmitter struct {
	mu       sync.Mutex
	state    systemEventState
	callback func(SystemEvent)
}

func (emitter *systemEventEmitter) emit(event SystemEvent) bool {
	if emitter == nil || emitter.callback == nil {
		return false
	}
	emitter.mu.Lock()
	accepted := emitter.state.accept(event)
	callback := emitter.callback
	emitter.mu.Unlock()
	if !accepted {
		return false
	}
	callback(event)
	return true
}
