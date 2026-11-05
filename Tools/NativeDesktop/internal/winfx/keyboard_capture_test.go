package winfx

import "testing"

func TestKeyboardCaptureEventDecodesEveryKeyboardMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		message uint32
		down    bool
		system  bool
	}{
		{keyboardMessageKeyDown, true, false},
		{keyboardMessageKeyUp, false, false},
		{keyboardMessageSysKeyDown, true, true},
		{keyboardMessageSysKeyUp, false, true},
	}
	input := lowLevelKeyboardInput{
		VirtualKey: 0x41,
		ScanCode:   0x1E,
		Flags: keyboardFlagExtended |
			keyboardFlagInjected |
			keyboardFlagLowerILInjected |
			keyboardFlagAltDown |
			keyboardFlagUp,
		Time:      42,
		ExtraInfo: 99,
	}
	for _, test := range tests {
		event, ok := keyboardCaptureEvent(test.message, input, true)
		if !ok {
			t.Fatalf("message %#x was rejected", test.message)
		}
		if event.Message != test.message ||
			event.KeyDown != test.down ||
			event.KeyUp == test.down ||
			event.System != test.system ||
			event.VirtualKey != 0x41 ||
			event.RawVirtualKey != 0x41 ||
			event.ScanCode != input.ScanCode ||
			event.Flags != input.Flags ||
			event.Time != input.Time ||
			event.ExtraInfo != input.ExtraInfo ||
			!event.Extended ||
			!event.Injected ||
			!event.LowerIntegrityInjected ||
			!event.AltDown ||
			!event.Repeat {
			t.Fatalf("message %#x decoded as %+v", test.message, event)
		}
	}
}

func TestKeyboardCaptureEventRejectsNonKeyboardMessage(t *testing.T) {
	t.Parallel()

	if event, ok := keyboardCaptureEvent(0x1234, lowLevelKeyboardInput{}, false); ok {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestNormalizeCapturedVirtualKeyDistinguishesModifierSides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		virtualKey uint32
		scanCode   uint32
		flags      uint32
		want       uint32
	}{
		{"left shift", 0x10, 0x2A, 0, 0xA0},
		{"right shift", 0x10, 0x36, 0, 0xA1},
		{"unknown shift", 0x10, 0, 0, 0x10},
		{"left control", 0x11, 0x1D, 0, 0xA2},
		{"right control", 0x11, 0x1D, keyboardFlagExtended, 0xA3},
		{"left alt", 0x12, 0x38, 0, 0xA4},
		{"right alt", 0x12, 0x38, keyboardFlagExtended, 0xA5},
		{"existing left shift", 0xA0, 0x2A, 0, 0xA0},
		{"letter", 0x4B, 0x25, 0, 0x4B},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeCapturedVirtualKey(
				test.virtualKey,
				test.scanCode,
				test.flags,
			); got != test.want {
				t.Fatalf("normalizeCapturedVirtualKey(%#x, %#x, %#x) = %#x, want %#x",
					test.virtualKey, test.scanCode, test.flags, got, test.want)
			}
		})
	}
}

func TestKeyboardModifierStateProvidesCanonicalKeysAndMask(t *testing.T) {
	t.Parallel()

	state := KeyboardModifierState{
		LeftControl: true,
		RightAlt:    true,
		LeftShift:   true,
		RightShift:  true,
		RightWin:    true,
	}
	keys := state.VirtualKeys()
	want := []uint32{0xA2, 0xA5, 0xA0, 0xA1, 0x5C}
	if len(keys) != len(want) {
		t.Fatalf("VirtualKeys = %#v, want %#v", keys, want)
	}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("VirtualKeys = %#v, want %#v", keys, want)
		}
	}
	if got, wantMask := state.Modifiers(), ModControl|ModAlt|ModShift|ModWin; got != wantMask {
		t.Fatalf("Modifiers = %#x, want %#x", got, wantMask)
	}
}
