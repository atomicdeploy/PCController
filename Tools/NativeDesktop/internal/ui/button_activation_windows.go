//go:build windows

package ui

import (
	"time"

	"github.com/lxn/walk"
)

// buttonActivationState gives the single custom-rendered surface the same
// arm/press/release contract that USER32 supplies to an ordinary BUTTON HWND.
// Rendering remains separate: pressTarget is consumed by the visual animator.
type buttonActivationState struct {
	pointerAction     actionID
	pointerInside     bool
	pointerGeneration uint64
	keyboardKey       walk.Key
	keyboardAction    actionID
}

func releaseActivatedAction(action actionID) bool {
	if action >= actionKey1 && action < 128 {
		return true
	}
	switch action {
	case actionToggle, actionOn, actionOff,
		actionSettings, actionDiagnostics, actionReconnect,
		actionTitleMinimize, actionTitleZoom, actionTitleClose:
		return true
	default:
		return false
	}
}

func keyboardActivationAction(key walk.Key) actionID {
	switch key {
	case walk.KeySpace, walk.KeyReturn:
		return actionToggle
	case walk.KeyO:
		return actionOn
	case walk.KeyF:
		return actionOff
	case walk.KeyF5:
		return actionReconnect
	case walk.KeyF1:
		return actionDiagnostics
	case walk.KeyF2:
		return actionSettings
	default:
		return actionNone
	}
}

func (s *buttonActivationState) pointerDown(action actionID) bool {
	if s == nil || !releaseActivatedAction(action) ||
		s.pointerAction != actionNone {
		return false
	}
	s.pointerAction = action
	s.pointerInside = true
	s.pointerGeneration++
	return true
}

func (s *buttonActivationState) pointerMove(over actionID) bool {
	if s == nil || s.pointerAction == actionNone {
		return false
	}
	inside := over == s.pointerAction
	if inside == s.pointerInside {
		return false
	}
	s.pointerInside = inside
	return true
}

// pointerUp returns the originally armed action and whether it should fire.
// Releasing over a different control never transfers activation.
func (s *buttonActivationState) pointerUp(over actionID) (actionID, bool) {
	if s == nil || s.pointerAction == actionNone {
		return actionNone, false
	}
	armed := s.pointerAction
	// WM_LBUTTONUP coordinates are authoritative. Mouse-move messages may be
	// coalesced, so the cached hover state can legitimately lag the release.
	activate := over == armed
	s.pointerAction = actionNone
	s.pointerInside = false
	s.pointerGeneration++
	return armed, activate
}

func (s *buttonActivationState) cancelPointer() actionID {
	if s == nil {
		return actionNone
	}
	armed := s.pointerAction
	s.pointerAction = actionNone
	s.pointerInside = false
	if armed != actionNone {
		s.pointerGeneration++
	}
	return armed
}

func (s *buttonActivationState) pointerToken() uint64 {
	if s == nil || s.pointerAction == actionNone {
		return 0
	}
	return s.pointerGeneration
}

// cancelPointerToken rejects a capture-loss callback queued for an earlier
// gesture. This matters because Walk posts synchronized callbacks: a fast
// second button-down can arrive before the first callback is drained.
func (s *buttonActivationState) cancelPointerToken(token uint64) actionID {
	if s == nil || token == 0 || s.pointerGeneration != token {
		return actionNone
	}
	return s.cancelPointer()
}

func (s *buttonActivationState) keyDown(key walk.Key, action actionID) bool {
	if s == nil || !releaseActivatedAction(action) ||
		s.keyboardAction != actionNone || s.pointerAction != actionNone {
		// Ignore auto-repeat and a second activation key while one is held.
		return false
	}
	s.keyboardKey = key
	s.keyboardAction = action
	return true
}

func (s *buttonActivationState) keyUp(key walk.Key) (actionID, bool) {
	if s == nil || s.keyboardAction == actionNone || key != s.keyboardKey {
		return actionNone, false
	}
	action := s.keyboardAction
	s.keyboardKey = 0
	s.keyboardAction = actionNone
	return action, true
}

func (s *buttonActivationState) cancelKeyboard() actionID {
	if s == nil {
		return actionNone
	}
	action := s.keyboardAction
	s.keyboardKey = 0
	s.keyboardAction = actionNone
	return action
}

func (s *buttonActivationState) pressTarget(action actionID) bool {
	if s == nil || action == actionNone {
		return false
	}
	return (s.pointerAction == action && s.pointerInside) ||
		s.keyboardAction == action
}

func advanceButtonPressLevel(
	value float64,
	pressed bool,
	elapsed time.Duration,
) (float64, bool) {
	target := 0.0
	duration := 140 * time.Millisecond
	if pressed {
		target = 1
		duration = 55 * time.Millisecond
	}
	step := elapsed.Seconds() / duration.Seconds()
	if step < 0 {
		step = 0
	} else if step > 1 {
		step = 1
	}
	switch {
	case value < target:
		value += step
		if value > target {
			value = target
		}
	case value > target:
		value -= step
		if value < target {
			value = target
		}
	}
	return value, value != target
}
