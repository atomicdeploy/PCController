package hostui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// KeyboardBinding identifies one unmodified physical key observed by the
// low-level host keyboard hook. Action semantics stay in host configuration.
type KeyboardBinding struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type KeyboardEvent struct {
	Binding  KeyboardBinding `json:"binding"`
	Down     bool            `json:"down"`
	Control  bool            `json:"control"`
	FailSafe bool            `json:"fail_safe,omitempty"`
	Reason   string          `json:"reason"`
	At       time.Time       `json:"at"`
}

func keyboardFailSafeEvent(reason string, at time.Time) KeyboardEvent {
	if strings.TrimSpace(reason) == "" {
		reason = "fail-safe"
	}
	return KeyboardEvent{FailSafe: true, Reason: reason, At: at}
}

type KeyboardStatus struct {
	Supported  bool              `json:"supported"`
	Running    bool              `json:"running"`
	Bindings   []KeyboardBinding `json:"bindings,omitempty"`
	ActiveKeys int               `json:"active_keys"`
	LastError  string            `json:"last_error,omitempty"`
	LastEvent  *KeyboardEvent    `json:"last_event,omitempty"`
}

// KeyboardRegistrar uses true down/up events. ReleaseAll is synchronous so a
// caller can issue fail-safe motion stops before replacing or closing a hook.
type KeyboardRegistrar interface {
	Start(context.Context, []KeyboardBinding, func(KeyboardEvent) error) error
	ReleaseAll(reason string) error
	Stop(reason string) error
	Status() KeyboardStatus
}

func NewKeyboardRegistrar() KeyboardRegistrar { return newPlatformKeyboardRegistrar() }

// ParseKeyboardKey resolves a configurable non-modifier key without the
// RegisterHotKey requirement that ordinary keys have Ctrl/Alt modifiers.
func ParseKeyboardKey(value string) (uint32, string, error) {
	key := strings.ToUpper(strings.TrimSpace(value))
	if key == "CTRL" || key == "CONTROL" || key == "SHIFT" || key == "ALT" ||
		key == "WIN" || key == "WINDOWS" {
		return 0, "", fmt.Errorf("modifier %q cannot be a keyboard-control key", value)
	}
	return parseVirtualKey(key)
}

func validateKeyboardBindings(bindings []KeyboardBinding) ([]uint32, error) {
	if len(bindings) == 0 || len(bindings) > 32 {
		return nil, errors.New("keyboard control requires 1..32 bindings")
	}
	keys := make([]uint32, len(bindings))
	names := make(map[string]bool)
	virtualKeys := make(map[uint32]bool)
	for index, binding := range bindings {
		name := strings.ToLower(strings.TrimSpace(binding.Name))
		if name == "" || names[name] {
			return nil, fmt.Errorf("keyboard binding %d has an empty or duplicate name", index)
		}
		names[name] = true
		key, _, err := ParseKeyboardKey(binding.Key)
		if err != nil {
			return nil, fmt.Errorf("keyboard binding %q: %w", binding.Name, err)
		}
		if virtualKeys[key] {
			return nil, fmt.Errorf("keyboard binding %q duplicates key %s", binding.Name, binding.Key)
		}
		virtualKeys[key] = true
		keys[index] = key
	}
	return keys, nil
}

type keyboardPress struct {
	binding KeyboardBinding
	control bool
}

// keyboardState is platform-neutral, making repeat suppression and fail-safe
// release behavior testable without installing a hook or touching the board.
type keyboardState struct {
	mu      sync.Mutex
	byKey   map[uint32]KeyboardBinding
	pressed map[uint32]keyboardPress
	blocked map[uint32]bool
}

func newKeyboardState(bindings []KeyboardBinding, keys []uint32) *keyboardState {
	state := &keyboardState{
		byKey:   make(map[uint32]KeyboardBinding, len(bindings)),
		pressed: make(map[uint32]keyboardPress), blocked: make(map[uint32]bool),
	}
	for index, binding := range bindings {
		state.byKey[keys[index]] = binding
	}
	return state
}

func (state *keyboardState) blockUntilUp(key uint32) {
	state.mu.Lock()
	state.blocked[key] = true
	state.mu.Unlock()
}

func (state *keyboardState) handle(
	key uint32,
	down bool,
	control bool,
	focused bool,
	at time.Time,
) []KeyboardEvent {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !focused {
		events := state.releaseAllLocked("focus-lost", at)
		if down {
			state.blocked[key] = true
		} else {
			delete(state.blocked, key)
		}
		return events
	}
	if !down {
		delete(state.blocked, key)
		press, ok := state.pressed[key]
		if !ok {
			return nil
		}
		delete(state.pressed, key)
		return []KeyboardEvent{{
			Binding: press.binding, Down: false, Control: press.control,
			Reason: "release", At: at,
		}}
	}
	if state.blocked[key] {
		return nil
	}
	if _, repeated := state.pressed[key]; repeated {
		return nil
	}
	binding, ok := state.byKey[key]
	if !ok {
		return nil
	}
	state.pressed[key] = keyboardPress{binding: binding, control: control}
	return []KeyboardEvent{{
		Binding: binding, Down: true, Control: control,
		Reason: "press", At: at,
	}}
}

func (state *keyboardState) releaseAll(reason string, at time.Time) []KeyboardEvent {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.releaseAllLocked(reason, at)
}

func (state *keyboardState) releaseAllLocked(reason string, at time.Time) []KeyboardEvent {
	if strings.TrimSpace(reason) == "" {
		reason = "fail-safe"
	}
	keys := make([]uint32, 0, len(state.pressed))
	for key := range state.pressed {
		keys = append(keys, key)
	}
	// At most 32 entries exist. Stable key order keeps stop/audit ordering
	// deterministic across shutdown and configuration reloads.
	for left := 0; left < len(keys); left++ {
		for right := left + 1; right < len(keys); right++ {
			if keys[right] < keys[left] {
				keys[left], keys[right] = keys[right], keys[left]
			}
		}
	}
	events := make([]KeyboardEvent, 0, len(keys))
	for _, key := range keys {
		press := state.pressed[key]
		events = append(events, KeyboardEvent{
			Binding: press.binding, Down: false, Control: press.control,
			Reason: reason, At: at,
		})
		state.blocked[key] = true
		delete(state.pressed, key)
	}
	return events
}

func (state *keyboardState) active() int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return len(state.pressed)
}
