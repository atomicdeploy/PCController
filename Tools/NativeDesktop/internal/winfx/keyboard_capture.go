package winfx

const (
	keyboardMessageKeyDown    uint32 = 0x0100
	keyboardMessageKeyUp      uint32 = 0x0101
	keyboardMessageSysKeyDown uint32 = 0x0104
	keyboardMessageSysKeyUp   uint32 = 0x0105

	keyboardFlagExtended        uint32 = 0x01
	keyboardFlagLowerILInjected uint32 = 0x02
	keyboardFlagInjected        uint32 = 0x10
	keyboardFlagAltDown         uint32 = 0x20
	keyboardFlagUp              uint32 = 0x80
)

// KeyboardCaptureEvent is the complete low-level Windows keyboard event
// delivered while a recording capture is active.
//
// VirtualKey normalizes generic Shift, Control, and Alt values to their
// left/right variants. RawVirtualKey preserves the exact KBDLLHOOKSTRUCT
// value. Injected events are included by default so the Windows on-screen
// keyboard, accessibility tools, and UI automation can record shortcuts.
type KeyboardCaptureEvent struct {
	Message                uint32
	VirtualKey             uint32
	RawVirtualKey          uint32
	ScanCode               uint32
	Flags                  uint32
	Time                   uint32
	ExtraInfo              uintptr
	KeyDown                bool
	KeyUp                  bool
	System                 bool
	Extended               bool
	Injected               bool
	LowerIntegrityInjected bool
	AltDown                bool
	Repeat                 bool
}

// KeyboardCaptureOptions controls the recording hook. The zero value is the
// recommended recorder policy: both physical and injected input is captured
// and suppressed.
type KeyboardCaptureOptions struct {
	// IgnoreInjected passes injected input to the rest of Windows without
	// recording or suppressing it. Leave false for accessibility support.
	IgnoreInjected bool
}

// KeyboardCaptureHandler runs synchronously on the thread that installed the
// capture. It should update in-memory/UI state quickly and must not block.
type KeyboardCaptureHandler func(KeyboardCaptureEvent)

// KeyboardModifierState is a left/right snapshot suitable for seeding a
// ChordRecorder when the user was already holding modifiers before pressing a
// Record button.
type KeyboardModifierState struct {
	LeftControl  bool
	RightControl bool
	LeftAlt      bool
	RightAlt     bool
	LeftShift    bool
	RightShift   bool
	LeftWin      bool
	RightWin     bool
}

// VirtualKeys returns every held modifier as canonical left/right Windows
// virtual-key values, in the same Ctrl, Alt, Shift, Win order used by Hotkey.
func (s KeyboardModifierState) VirtualKeys() []uint32 {
	keys := make([]uint32, 0, 8)
	for _, entry := range []struct {
		down bool
		key  uint32
	}{
		{s.LeftControl, 0xA2},
		{s.RightControl, 0xA3},
		{s.LeftAlt, 0xA4},
		{s.RightAlt, 0xA5},
		{s.LeftShift, 0xA0},
		{s.RightShift, 0xA1},
		{s.LeftWin, 0x5B},
		{s.RightWin, 0x5C},
	} {
		if entry.down {
			keys = append(keys, entry.key)
		}
	}
	return keys
}

// Modifiers returns the normalized RegisterHotKey modifier mask.
func (s KeyboardModifierState) Modifiers() uint32 {
	var modifiers uint32
	if s.LeftControl || s.RightControl {
		modifiers |= ModControl
	}
	if s.LeftAlt || s.RightAlt {
		modifiers |= ModAlt
	}
	if s.LeftShift || s.RightShift {
		modifiers |= ModShift
	}
	if s.LeftWin || s.RightWin {
		modifiers |= ModWin
	}
	return modifiers
}

type lowLevelKeyboardInput struct {
	VirtualKey uint32
	ScanCode   uint32
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

func keyboardCaptureEvent(
	message uint32,
	input lowLevelKeyboardInput,
	repeat bool,
) (KeyboardCaptureEvent, bool) {
	keyDown := message == keyboardMessageKeyDown ||
		message == keyboardMessageSysKeyDown
	keyUp := message == keyboardMessageKeyUp ||
		message == keyboardMessageSysKeyUp
	if !keyDown && !keyUp {
		return KeyboardCaptureEvent{}, false
	}

	return KeyboardCaptureEvent{
		Message:                message,
		VirtualKey:             normalizeCapturedVirtualKey(input.VirtualKey, input.ScanCode, input.Flags),
		RawVirtualKey:          input.VirtualKey,
		ScanCode:               input.ScanCode,
		Flags:                  input.Flags,
		Time:                   input.Time,
		ExtraInfo:              input.ExtraInfo,
		KeyDown:                keyDown,
		KeyUp:                  keyUp,
		System:                 message == keyboardMessageSysKeyDown || message == keyboardMessageSysKeyUp,
		Extended:               input.Flags&keyboardFlagExtended != 0,
		Injected:               input.Flags&(keyboardFlagInjected|keyboardFlagLowerILInjected) != 0,
		LowerIntegrityInjected: input.Flags&keyboardFlagLowerILInjected != 0,
		AltDown:                input.Flags&keyboardFlagAltDown != 0,
		Repeat:                 repeat,
	}, true
}

// normalizeCapturedVirtualKey preserves ordinary virtual keys and splits the
// three generic modifier values using the low-level scan code/extended flag.
// Windows normally already emits left/right VKs, but keyboard drivers and
// synthetic input are allowed to emit the generic forms.
func normalizeCapturedVirtualKey(virtualKey, scanCode, flags uint32) uint32 {
	switch virtualKey {
	case 0x10: // VK_SHIFT
		switch scanCode & 0xFF {
		case 0x2A:
			return 0xA0 // VK_LSHIFT
		case 0x36:
			return 0xA1 // VK_RSHIFT
		}
	case 0x11: // VK_CONTROL
		if flags&keyboardFlagExtended != 0 {
			return 0xA3 // VK_RCONTROL
		}
		return 0xA2 // VK_LCONTROL
	case 0x12: // VK_MENU
		if flags&keyboardFlagExtended != 0 {
			return 0xA5 // VK_RMENU
		}
		return 0xA4 // VK_LMENU
	}
	return virtualKey
}
