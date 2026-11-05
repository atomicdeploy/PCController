package winfx

import (
	"errors"
	"testing"
)

func TestChordRecorderStartCanonicalizesAndCancelRestores(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	update, err := recorder.Start(" control + windows + pgdn ")
	if err != nil {
		t.Fatal(err)
	}
	if update.Action != RecorderStarted || update.Display != recorderPrompt ||
		update.Chord != "Ctrl+Win+PageDown" || !recorder.Active() {
		t.Fatalf("Start update = %+v, active = %v", update, recorder.Active())
	}

	recorder.KeyDown(0xA4) // VK_LMENU
	cancelled := recorder.KeyDown(0x1B)
	if cancelled.Action != RecorderCancelled ||
		cancelled.Chord != "Ctrl+Win+PageDown" ||
		cancelled.Display != cancelled.Chord ||
		cancelled.Modifiers != 0 || recorder.Active() {
		t.Fatalf("Escape cancel = %+v, active = %v", cancelled, recorder.Active())
	}
}

func TestChordRecorderInvalidStartIsTransactional(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	if _, err := recorder.Start("Ctrl+K"); err != nil {
		t.Fatal(err)
	}
	recorder.KeyDown(0xA4)
	before := recorder.snapshot(RecorderRecording)

	update, err := recorder.Start("Ctrl+NoSuchKey")
	if !errors.Is(err, ErrInvalidHotkey) {
		t.Fatalf("invalid Start error = %v", err)
	}
	after := recorder.snapshot(RecorderRecording)
	if update.Action != RecorderRejected ||
		update.Display != before.Display ||
		update.Chord != before.Chord ||
		update.Modifiers != before.Modifiers ||
		after != before {
		t.Fatalf("invalid Start mutated recorder: before %+v, after %+v", before, update)
	}
}

func TestChordRecorderNormalizesEveryModifierVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     uint32
		mask    uint32
		display string
		chord   string
	}{
		{"generic control", 0x11, ModControl, "Ctrl+...", "Ctrl+K"},
		{"left control", 0xA2, ModControl, "Ctrl+...", "Ctrl+K"},
		{"right control", 0xA3, ModControl, "Ctrl+...", "Ctrl+K"},
		{"generic alt", 0x12, ModAlt, "Alt+...", "Alt+K"},
		{"left alt", 0xA4, ModAlt, "Alt+...", "Alt+K"},
		{"right alt", 0xA5, ModAlt, "Alt+...", "Alt+K"},
		{"generic shift", 0x10, ModShift, "Shift+...", "Shift+K"},
		{"left shift", 0xA0, ModShift, "Shift+...", "Shift+K"},
		{"right shift", 0xA1, ModShift, "Shift+...", "Shift+K"},
		{"left win", 0x5B, ModWin, "Win+...", "Win+K"},
		{"right win", 0x5C, ModWin, "Win+...", "Win+K"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var recorder ChordRecorder
			if _, err := recorder.Start(""); err != nil {
				t.Fatal(err)
			}
			down := recorder.KeyDown(test.key)
			if down.Action != RecorderRecording ||
				down.Modifiers != test.mask ||
				down.Display != test.display {
				t.Fatalf("modifier down = %+v", down)
			}
			completed := recorder.KeyDown('K')
			if completed.Action != RecorderCompleted ||
				completed.Chord != test.chord ||
				completed.Display != test.chord ||
				completed.Modifiers != 0 {
				t.Fatalf("completion = %+v", completed)
			}
			assertRecorderChordParses(t, completed.Chord)
		})
	}
}

func TestChordRecorderTracksLeftAndRightModifiersIndependently(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	_, _ = recorder.Start("")
	recorder.KeyDown(0xA2) // VK_LCONTROL
	recorder.KeyDown(0xA3) // VK_RCONTROL
	if got := recorder.KeyUp(0xA2); got.Modifiers != ModControl ||
		got.Display != "Ctrl+..." {
		t.Fatalf("left release with right held = %+v", got)
	}
	if got := recorder.KeyUp(0xA3); got.Modifiers != 0 ||
		got.Display != recorderPrompt || !recorder.Active() {
		t.Fatalf("final control release = %+v", got)
	}
}

func TestChordRecorderCanonicalModifierOrderIsPressOrderIndependent(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	_, _ = recorder.Start("")
	for _, key := range []uint32{0x5C, 0xA1, 0xA5, 0xA3} {
		recorder.KeyDown(key)
	}
	got := recorder.KeyDown('7')
	want := "Ctrl+Alt+Shift+Win+7"
	if got.Action != RecorderCompleted || got.Chord != want {
		t.Fatalf("completion = %+v, want %q", got, want)
	}
	assertRecorderChordParses(t, got.Chord)
}

func TestChordRecorderCompletesEveryParserCompatibleVirtualKey(t *testing.T) {
	t.Parallel()

	keys := map[uint32]string{
		'0': "0", '9': "9", 'A': "A", 'Z': "Z",
		0x08: "Backspace", 0x09: "Tab", 0x0D: "Enter",
		0x13: "Pause", 0x14: "CapsLock", 0x20: "Space",
		0x21: "PageUp", 0x22: "PageDown", 0x23: "End",
		0x24: "Home", 0x25: "Left", 0x26: "Up", 0x27: "Right",
		0x28: "Down", 0x2C: "PrintScreen", 0x2D: "Insert",
		0x2E: "Delete", 0x5D: "Apps", 0x5F: "Sleep",
		0x6A: "Multiply", 0x6B: "Add", 0x6C: "Separator", 0x6D: "Subtract",
		0x6E: "Decimal", 0x6F: "Divide", 0x90: "NumLock",
		0x91: "ScrollLock", 0xA6: "BrowserBack", 0xA7: "BrowserForward",
		0xA8: "BrowserRefresh", 0xA9: "BrowserStop",
		0xAA: "BrowserSearch", 0xAB: "BrowserFavorites",
		0xAC: "BrowserHome", 0xAD: "VolumeMute", 0xAE: "VolumeDown",
		0xAF: "VolumeUp", 0xB0: "MediaNext", 0xB1: "MediaPrevious",
		0xB2: "MediaStop", 0xB3: "MediaPlayPause", 0xB4: "LaunchMail",
		0xB5: "LaunchMediaSelect", 0xB6: "LaunchApp1", 0xB7: "LaunchApp2",
		0xBA: "Semicolon", 0xBB: "Equal", 0xBC: "Comma", 0xBD: "Minus",
		0xBE: "Period", 0xBF: "Slash", 0xC0: "Backtick",
		0xDB: "LeftBracket", 0xDC: "Backslash", 0xDD: "RightBracket",
		0xDE: "Quote",
	}
	for key := uint32('1'); key <= '8'; key++ {
		keys[key] = string(rune(key))
	}
	for key := uint32('B'); key <= 'Y'; key++ {
		keys[key] = string(rune(key))
	}
	for number := uint32(1); number <= 24; number++ {
		keys[0x70+number-1] = "F" + decimalString(number)
	}
	for number := uint32(0); number <= 9; number++ {
		keys[0x60+number] = "Num" + decimalString(number)
	}

	for virtualKey, name := range keys {
		virtualKey, name := virtualKey, name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var recorder ChordRecorder
			_, _ = recorder.Start("")
			// Ctrl makes Backspace/Delete real chords instead of clear gestures.
			recorder.KeyDown(0xA2)
			got := recorder.KeyDown(virtualKey)
			want := "Ctrl+" + name
			if got.Action != RecorderCompleted || got.Chord != want {
				t.Fatalf("KeyDown(%#x) = %+v, want %q", virtualKey, got, want)
			}
			assertRecorderChordParses(t, got.Chord)
		})
	}
}

func TestChordRecorderEscapeAlwaysCancels(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	_, _ = recorder.Start("Shift+F3")
	for _, key := range []uint32{0xA2, 0xA4, 0xA0, 0x5B} {
		recorder.KeyDown(key)
	}
	got := recorder.KeyDown(0x1B)
	if got.Action != RecorderCancelled || got.Chord != "Shift+F3" ||
		got.Modifiers != 0 || recorder.Active() {
		t.Fatalf("modified Escape = %+v", got)
	}
}

func TestChordRecorderBackspaceAndDeleteClearOnlyWithoutModifiers(t *testing.T) {
	t.Parallel()

	for _, key := range []uint32{0x08, 0x2E} {
		key := key
		t.Run(decimalString(key), func(t *testing.T) {
			t.Parallel()

			var recorder ChordRecorder
			_, _ = recorder.Start("Ctrl+K")
			cleared := recorder.KeyDown(key)
			if cleared.Action != RecorderCleared || cleared.Chord != "" ||
				cleared.Display != "" || recorder.Active() {
				t.Fatalf("unmodified clear = %+v", cleared)
			}

			_, _ = recorder.Start("")
			recorder.KeyDown(0xA2)
			completed := recorder.KeyDown(key)
			want := "Ctrl+Backspace"
			if key == 0x2E {
				want = "Ctrl+Delete"
			}
			if completed.Action != RecorderCompleted || completed.Chord != want {
				t.Fatalf("modified completion = %+v, want %q", completed, want)
			}
			assertRecorderChordParses(t, completed.Chord)
		})
	}
}

func TestChordRecorderModifierOnlyNeverCompletes(t *testing.T) {
	t.Parallel()

	for _, key := range []uint32{
		0x10, 0x11, 0x12, 0x5B, 0x5C,
		0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5,
	} {
		key := key
		t.Run(decimalString(key), func(t *testing.T) {
			t.Parallel()

			var recorder ChordRecorder
			_, _ = recorder.Start("Alt+M")
			down := recorder.KeyDown(key)
			up := recorder.KeyUp(key)
			if down.Action != RecorderRecording ||
				up.Action != RecorderRecording ||
				!recorder.Active() || recorder.Chord() != "Alt+M" ||
				up.Display != recorderPrompt {
				t.Fatalf("modifier-only sequence = down %+v, up %+v", down, up)
			}
		})
	}
}

func TestChordRecorderRejectsUnsupportedKeyAndContinues(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	_, _ = recorder.Start("Ctrl+L")
	recorder.KeyDown(0xA4)
	rejected := recorder.KeyDown(0xFF)
	if rejected.Action != RecorderRejected || !recorder.Active() ||
		rejected.Chord != "Ctrl+L" || rejected.Display != "Alt+..." {
		t.Fatalf("unsupported key = %+v", rejected)
	}
	completed := recorder.KeyDown('M')
	if completed.Action != RecorderCompleted || completed.Chord != "Alt+M" {
		t.Fatalf("completion after rejection = %+v", completed)
	}
}

func TestChordRecorderExplicitClearAndIdleEvents(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	if got := recorder.KeyDown('K'); got.Action != RecorderIgnored {
		t.Fatalf("idle KeyDown = %+v", got)
	}
	if got := recorder.KeyUp(0xA2); got.Action != RecorderIgnored {
		t.Fatalf("idle KeyUp = %+v", got)
	}
	if got := recorder.Cancel(); got.Action != RecorderIgnored {
		t.Fatalf("idle Cancel = %+v", got)
	}

	_, _ = recorder.Start("Ctrl+K")
	recorder.KeyDown(0xA4)
	cleared := recorder.Clear()
	if cleared.Action != RecorderCleared || cleared.Chord != "" ||
		cleared.Display != "" || cleared.Modifiers != 0 || recorder.Active() {
		t.Fatalf("explicit Clear = %+v", cleared)
	}
	if got := recorder.Clear(); got.Action != RecorderCleared {
		t.Fatalf("idle Clear = %+v", got)
	}
}

func TestChordRecorderRestartClearsLiveModifierState(t *testing.T) {
	t.Parallel()

	var recorder ChordRecorder
	_, _ = recorder.Start("Ctrl+K")
	recorder.KeyDown(0xA2)
	recorder.KeyDown(0xA4)
	restarted, err := recorder.Start("Shift+F2")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Modifiers != 0 || restarted.Display != recorderPrompt ||
		restarted.Chord != "Shift+F2" {
		t.Fatalf("restart = %+v", restarted)
	}
}

func assertRecorderChordParses(t *testing.T, chord string) {
	t.Helper()
	parsed, err := ParseHotkey(chord)
	if err != nil {
		t.Fatalf("ParseHotkey(%q): %v", chord, err)
	}
	if parsed.String() != chord {
		t.Fatalf("ParseHotkey(%q).String() = %q", chord, parsed.String())
	}
}

func decimalString(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	index := len(digits)
	for value != 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
