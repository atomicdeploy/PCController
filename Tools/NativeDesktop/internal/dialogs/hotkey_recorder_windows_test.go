//go:build windows

package dialogs

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/winfx"
)

func TestHotkeyRecorderBindingSetChordCanonicalizes(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add("Toggle device", "Toggle the current state", "")

	binding.SetChord(" control + windows + pgdn ")

	if got, want := binding.Chord(), "Ctrl+Win+PageDown"; got != want {
		t.Fatalf("SetChord canonical value = %q, want %q", got, want)
	}
}

func TestHotkeyRecorderButtonsUseUniquePageAccelerators(t *testing.T) {
	actions := []string{"Toggle device", "Turn on", "Turn off", "Show window"}
	seen := make(map[byte]string)
	captions := []string{
		settingsSaveCaption,
		settingsCancelCaption,
		"Cancel r&ecording",
	}
	for _, action := range actions {
		record, clear := hotkeyRecorderButtonCaptions(action)
		captions = append(captions, record, clear)
	}
	for _, caption := range captions {
		index := strings.IndexByte(caption, '&')
		if index < 0 || index+1 >= len(caption) {
			t.Fatalf("%q has no accelerator", caption)
		}
		key := caption[index+1]
		if key >= 'A' && key <= 'Z' {
			key += 'a' - 'A'
		}
		if previous, exists := seen[key]; exists {
			t.Fatalf(
				"%q and %q both use Alt+%c",
				previous,
				caption,
				key,
			)
		}
		seen[key] = caption
	}
}

func TestHotkeyRecorderGroupDetectsSemanticDuplicate(t *testing.T) {
	group := newHotkeyRecorderGroup()
	toggle := group.Add(
		"Toggle device",
		"Toggle the current state",
		"Ctrl+Alt+L",
	)
	show := group.Add(
		"Show window",
		"Bring the main window forward",
		" alt + control + l ",
	)

	group.RefreshValidation()

	if toggle.messageKind != hotkeyRecorderMessageError ||
		show.messageKind != hotkeyRecorderMessageError {
		t.Fatalf(
			"duplicate kinds = toggle %v, show %v; want both error",
			toggle.messageKind,
			show.messageKind,
		)
	}
	if !strings.Contains(toggle.message, "Show window") ||
		!strings.Contains(show.message, "Toggle device") {
		t.Fatalf(
			"duplicate messages = toggle %q, show %q",
			toggle.message,
			show.message,
		)
	}
	if err := group.Validate(); err == nil ||
		!strings.Contains(err.Error(), "Conflicts") {
		t.Fatalf("Validate duplicate error = %v, want conflict", err)
	}
}

func TestHotkeyRecorderGroupRejectsReservedF12(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"Ctrl+F12",
	)

	group.RefreshValidation()

	if binding.messageKind != hotkeyRecorderMessageError {
		t.Fatalf(
			"F12 message kind = %v, want error",
			binding.messageKind,
		)
	}
	if !strings.Contains(binding.message, "reserved") {
		t.Fatalf("F12 message = %q, want reserved explanation", binding.message)
	}
	if err := group.Validate(); err == nil ||
		!strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Validate F12 error = %v, want reserved error", err)
	}
}

func TestHotkeyRecorderGroupAcceptsSingleKeyWithWarning(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"L",
	)

	group.RefreshValidation()

	if binding.messageKind != hotkeyRecorderMessageWarning {
		t.Fatalf(
			"single-key message kind = %v, want warning",
			binding.messageKind,
		)
	}
	if !strings.Contains(binding.message, "use carefully") {
		t.Fatalf("single-key message = %q, want caution", binding.message)
	}
	if err := group.Validate(); err != nil {
		t.Fatalf("Validate rejected warning-only binding: %v", err)
	}
}

func TestHotkeyRecorderGroupRejectsValidationWhileActive(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"Ctrl+Alt+L",
	)

	group.Start(binding)
	t.Cleanup(func() { group.CancelActive(false) })

	if !group.Active() {
		t.Fatal("group is not active after Start")
	}
	if err := group.Validate(); err == nil ||
		!strings.Contains(err.Error(), "finish or cancel") {
		t.Fatalf("Validate active error = %v, want finish-or-cancel error", err)
	}
}

func TestHotkeyRecorderGroupCancelRestoresOriginalChord(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		" control + alt + l ",
	)
	changes := 0
	var captureStates []bool
	group.SetOnChanged(func() { changes++ })
	group.SetOnCaptureChanged(func(active bool) {
		captureStates = append(captureStates, active)
	})

	group.Start(binding)
	group.handleUpdate(binding, binding.recorder.KeyDown(0xA0)) // VK_LSHIFT
	group.CancelActive(true)

	if group.Active() {
		t.Fatal("group remained active after CancelActive")
	}
	if got, want := binding.Chord(), "Ctrl+Alt+L"; got != want {
		t.Fatalf("cancelled chord = %q, want restored %q", got, want)
	}
	if got, want := binding.recorder.Modifiers(), uint32(0); got != want {
		t.Fatalf("modifiers after cancel = %#x, want %#x", got, want)
	}
	if changes != 0 {
		t.Fatalf("cancel published %d configuration changes, want 0", changes)
	}
	if got, want := captureStates, []bool{true, false}; !slices.Equal(got, want) {
		t.Fatalf("capture states = %v, want %v", got, want)
	}
}

func TestHotkeyRecorderGroupClearDisablesBinding(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"Ctrl+Alt+L",
	)
	changes := 0
	var captureStates []bool
	group.SetOnChanged(func() { changes++ })
	group.SetOnCaptureChanged(func(active bool) {
		captureStates = append(captureStates, active)
	})

	group.Start(binding)
	group.handleUpdate(
		binding,
		binding.recorder.KeyDown(0xA2), // VK_LCONTROL
	)
	group.Clear(binding)

	if group.Active() {
		t.Fatal("group remained active after Clear")
	}
	if got := binding.Chord(); got != "" {
		t.Fatalf("cleared chord = %q, want empty", got)
	}
	if binding.messageKind == hotkeyRecorderMessageError {
		t.Fatalf("cleared binding is invalid: %q", binding.message)
	}
	if err := group.Validate(); err != nil {
		t.Fatalf("Validate rejected a disabled binding: %v", err)
	}
	if changes != 1 {
		t.Fatalf("clear published %d configuration changes, want 1", changes)
	}
	if got, want := captureStates, []bool{true, false}; !slices.Equal(got, want) {
		t.Fatalf("capture states = %v, want %v", got, want)
	}
}

func TestHotkeyRecorderGroupCompletesRecordedChord(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"",
	)
	group.Start(binding)

	group.handleUpdate(binding, binding.recorder.KeyDown(0xA2)) // VK_LCONTROL
	group.handleUpdate(binding, binding.recorder.KeyDown(0xA4)) // VK_LMENU
	update := binding.recorder.KeyDown('K')
	if update.Action != winfx.RecorderCompleted {
		t.Fatalf("final key action = %v, want completed", update.Action)
	}
	group.handleUpdate(binding, update)

	if group.Active() {
		t.Fatal("group remained active after a completed chord")
	}
	if got, want := binding.Chord(), "Ctrl+Alt+K"; got != want {
		t.Fatalf("recorded chord = %q, want %q", got, want)
	}
	if err := group.Validate(); err != nil {
		t.Fatalf("Validate rejected recorded chord: %v", err)
	}
}

func TestHotkeyRecorderAvailabilityFailurePersistsAndCanBeRetried(t *testing.T) {
	group := newHotkeyRecorderGroup()
	binding := group.Add(
		"Toggle device",
		"Toggle the current state",
		"Ctrl+Alt+L",
	)
	available := false
	group.SetAvailabilityCheck(func(_ string, _ string) error {
		if !available {
			return errors.New("already registered")
		}
		return nil
	})

	recordCtrlK := func() {
		group.Start(binding)
		group.handleUpdate(binding, binding.recorder.KeyDown(0xA2))
		group.handleUpdate(binding, binding.recorder.KeyDown('K'))
	}
	recordCtrlK()

	if err := group.Validate(); err == nil ||
		!strings.Contains(err.Error(), "Unavailable") {
		t.Fatalf("availability error did not survive Validate: %v", err)
	}
	group.RefreshValidation()
	if binding.messageKind != hotkeyRecorderMessageError ||
		!strings.Contains(binding.message, "another app") {
		t.Fatalf("availability state was cleared: %q", binding.message)
	}
	if got := group.PreviewChords(); len(got) != 1 ||
		got[0] != "Ctrl+Alt+L" {
		t.Fatalf("preview after failed probe = %v, want stable chord", got)
	}

	available = true
	recordCtrlK()
	if err := group.Validate(); err != nil {
		t.Fatalf("same-chord retry remained unavailable: %v", err)
	}
	if got := group.PreviewChords(); len(got) != 1 ||
		got[0] != "Ctrl+K" {
		t.Fatalf("preview after successful retry = %v, want Ctrl+K", got)
	}
}
