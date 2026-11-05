package winfx

import (
	"fmt"
	"strings"
)

const recorderPrompt = "Press shortcut..."

// RecorderAction describes one state-machine transition. Native controls can
// use it without interpreting key names or rebuilding a chord themselves.
type RecorderAction uint8

const (
	RecorderIgnored RecorderAction = iota
	RecorderStarted
	RecorderRecording
	RecorderCompleted
	RecorderCancelled
	RecorderCleared
	RecorderRejected
)

// RecorderUpdate is an immutable view of the recorder after an input event.
// Chord is always empty or canonical input accepted by ParseHotkey. Display is
// transient while recording and otherwise equals Chord.
type RecorderUpdate struct {
	Action    RecorderAction
	Display   string
	Chord     string
	Modifiers uint32
}

// ChordRecorder converts native virtual-key down/up events into canonical
// global-hotkey strings. Its zero value is idle and ready for Start.
type ChordRecorder struct {
	active       bool
	original     string
	chord        string
	display      string
	modifierDown [256]bool
}

// Start begins a new recording transaction. A non-empty current chord is
// canonicalized first and will be restored by Cancel. Invalid current text
// leaves the recorder unchanged.
func (r *ChordRecorder) Start(current string) (RecorderUpdate, error) {
	canonical := ""
	if strings.TrimSpace(current) != "" {
		hotkey, err := ParseHotkey(current)
		if err != nil {
			return r.snapshot(RecorderRejected), err
		}
		canonical = hotkey.String()
	}

	*r = ChordRecorder{
		active:   true,
		original: canonical,
		chord:    canonical,
		display:  recorderPrompt,
	}
	return r.snapshot(RecorderStarted), nil
}

// Cancel restores the chord supplied to Start.
func (r *ChordRecorder) Cancel() RecorderUpdate {
	if r == nil || !r.active {
		return r.snapshot(RecorderIgnored)
	}
	r.active = false
	r.chord = r.original
	r.display = r.original
	r.clearModifiers()
	return r.snapshot(RecorderCancelled)
}

// Clear completes recording with no chord. It is also safe to use as an
// explicit Clear-button action while the recorder is idle.
func (r *ChordRecorder) Clear() RecorderUpdate {
	if r == nil {
		return RecorderUpdate{Action: RecorderCleared}
	}
	r.active = false
	r.original = ""
	r.chord = ""
	r.display = ""
	r.clearModifiers()
	return r.snapshot(RecorderCleared)
}

// KeyDown consumes a native virtual-key press. Modifier presses update the
// live preview but never complete a chord. Escape cancels. Unmodified
// Backspace/Delete clear; with modifiers they remain recordable hotkeys.
func (r *ChordRecorder) KeyDown(virtualKey uint32) RecorderUpdate {
	if r == nil || !r.active {
		return r.snapshot(RecorderIgnored)
	}

	if _, modifier := recorderModifier(virtualKey); modifier {
		r.setModifierDown(virtualKey, true)
		r.display = recorderModifierDisplay(r.Modifiers())
		return r.snapshot(RecorderRecording)
	}

	if virtualKey == 0x1B { // VK_ESCAPE
		return r.Cancel()
	}

	modifiers := r.Modifiers()
	if modifiers == 0 && (virtualKey == 0x08 || virtualKey == 0x2E) {
		// VK_BACK and VK_DELETE are ergonomic clear gestures when they are not
		// part of a real modified chord.
		return r.Clear()
	}

	keyName, supported := recorderKeyName(virtualKey)
	if !supported {
		return r.snapshot(RecorderRejected)
	}
	candidate := Hotkey{
		Modifiers: modifiers,
		Key:       virtualKey,
		KeyName:   keyName,
	}
	// Keep this boundary defensive: every emitted value must remain compatible
	// if the parser's accepted-key grammar changes later.
	parsed, err := ParseHotkey(candidate.String())
	if err != nil || parsed.Key != candidate.Key ||
		parsed.Modifiers != candidate.Modifiers {
		return r.snapshot(RecorderRejected)
	}

	r.active = false
	r.chord = parsed.String()
	r.original = r.chord
	r.display = r.chord
	r.clearModifiers()
	return r.snapshot(RecorderCompleted)
}

// KeyUp updates live modifier state. Releasing a non-modifier is ignored.
func (r *ChordRecorder) KeyUp(virtualKey uint32) RecorderUpdate {
	if r == nil || !r.active {
		return r.snapshot(RecorderIgnored)
	}
	if _, modifier := recorderModifier(virtualKey); !modifier {
		return r.snapshot(RecorderIgnored)
	}
	r.setModifierDown(virtualKey, false)
	r.display = recorderModifierDisplay(r.Modifiers())
	return r.snapshot(RecorderRecording)
}

func (r *ChordRecorder) Active() bool {
	return r != nil && r.active
}

func (r *ChordRecorder) Display() string {
	if r == nil {
		return ""
	}
	return r.display
}

// Chord returns the last committed chord, or the transaction's original chord
// while recording. It never returns transient modifier-only display text.
func (r *ChordRecorder) Chord() string {
	if r == nil {
		return ""
	}
	return r.chord
}

// Modifiers reports the normalized live modifier mask. Left/right variants
// collapse to one canonical Ctrl, Alt, Shift, or Win flag.
func (r *ChordRecorder) Modifiers() uint32 {
	if r == nil {
		return 0
	}
	var modifiers uint32
	for virtualKey, down := range r.modifierDown {
		if !down {
			continue
		}
		if modifier, ok := recorderModifier(uint32(virtualKey)); ok {
			modifiers |= modifier
		}
	}
	return modifiers
}

func (r *ChordRecorder) snapshot(action RecorderAction) RecorderUpdate {
	if r == nil {
		return RecorderUpdate{Action: action}
	}
	return RecorderUpdate{
		Action:    action,
		Display:   r.display,
		Chord:     r.chord,
		Modifiers: r.Modifiers(),
	}
}

func (r *ChordRecorder) setModifierDown(virtualKey uint32, down bool) {
	if virtualKey < uint32(len(r.modifierDown)) {
		r.modifierDown[virtualKey] = down
	}
}

func (r *ChordRecorder) clearModifiers() {
	if r != nil {
		r.modifierDown = [256]bool{}
	}
}

func recorderModifier(virtualKey uint32) (uint32, bool) {
	switch virtualKey {
	case 0x11, 0xA2, 0xA3: // VK_CONTROL, VK_LCONTROL, VK_RCONTROL
		return ModControl, true
	case 0x12, 0xA4, 0xA5: // VK_MENU, VK_LMENU, VK_RMENU
		return ModAlt, true
	case 0x10, 0xA0, 0xA1: // VK_SHIFT, VK_LSHIFT, VK_RSHIFT
		return ModShift, true
	case 0x5B, 0x5C: // VK_LWIN, VK_RWIN
		return ModWin, true
	default:
		return 0, false
	}
}

func recorderModifierDisplay(modifiers uint32) string {
	if modifiers == 0 {
		return recorderPrompt
	}
	return (Hotkey{Modifiers: modifiers}).String() + "+..."
}

func recorderKeyName(virtualKey uint32) (string, bool) {
	if virtualKey >= 'A' && virtualKey <= 'Z' ||
		virtualKey >= '0' && virtualKey <= '9' {
		return string(rune(virtualKey)), true
	}
	if virtualKey >= 0x70 && virtualKey <= 0x87 {
		return fmt.Sprintf("F%d", virtualKey-0x70+1), true
	}
	if virtualKey >= 0x60 && virtualKey <= 0x69 {
		return fmt.Sprintf("Num%d", virtualKey-0x60), true
	}

	names := map[uint32]string{
		0x08: "Backspace",
		0x09: "Tab",
		0x0D: "Enter",
		0x13: "Pause",
		0x14: "CapsLock",
		0x20: "Space",
		0x21: "PageUp",
		0x22: "PageDown",
		0x23: "End",
		0x24: "Home",
		0x25: "Left",
		0x26: "Up",
		0x27: "Right",
		0x28: "Down",
		0x2C: "PrintScreen",
		0x2D: "Insert",
		0x2E: "Delete",
		0x5D: "Apps",
		0x5F: "Sleep",
		0x6A: "Multiply",
		0x6B: "Add",
		0x6C: "Separator",
		0x6D: "Subtract",
		0x6E: "Decimal",
		0x6F: "Divide",
		0x90: "NumLock",
		0x91: "ScrollLock",
		0xA6: "BrowserBack",
		0xA7: "BrowserForward",
		0xA8: "BrowserRefresh",
		0xA9: "BrowserStop",
		0xAA: "BrowserSearch",
		0xAB: "BrowserFavorites",
		0xAC: "BrowserHome",
		0xAD: "VolumeMute",
		0xAE: "VolumeDown",
		0xAF: "VolumeUp",
		0xB0: "MediaNext",
		0xB1: "MediaPrevious",
		0xB2: "MediaStop",
		0xB3: "MediaPlayPause",
		0xB4: "LaunchMail",
		0xB5: "LaunchMediaSelect",
		0xB6: "LaunchApp1",
		0xB7: "LaunchApp2",
		0xBA: "Semicolon",
		0xBB: "Equal",
		0xBC: "Comma",
		0xBD: "Minus",
		0xBE: "Period",
		0xBF: "Slash",
		0xC0: "Backtick",
		0xDB: "LeftBracket",
		0xDC: "Backslash",
		0xDD: "RightBracket",
		0xDE: "Quote",
	}
	name, ok := names[virtualKey]
	return name, ok
}
