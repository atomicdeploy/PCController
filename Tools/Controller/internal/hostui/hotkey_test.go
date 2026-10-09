package hostui

import (
	"reflect"
	"testing"
)

func TestParseAcceleratorCanonicalizesAndSuppressesRepeat(t *testing.T) {
	value, err := ParseAccelerator("shift + ctrl + p")
	if err != nil {
		t.Fatal(err)
	}
	if value.Canonical != "Ctrl+Shift+P" || value.VirtualKey != 'P' || value.Modifiers&ModifierNoRepeat == 0 {
		t.Fatalf("accelerator=%#v", value)
	}
}

func TestParseAcceleratorSupportsFunctionAndMediaKeys(t *testing.T) {
	for _, input := range []string{"F12", "Ctrl+Alt+MEDIA_PLAY_PAUSE", "Win+Shift+1"} {
		if _, err := ParseAccelerator(input); err != nil {
			t.Errorf("%s: %v", input, err)
		}
	}
}

func TestParseAcceleratorRejectsUnsafeOrAmbiguousBindings(t *testing.T) {
	for _, input := range []string{"P", "Ctrl+Ctrl+P", "Ctrl+P+Q", "Ctrl+NotAKey", "Ctrl"} {
		if _, err := ParseAccelerator(input); err == nil {
			t.Errorf("%q unexpectedly accepted", input)
		}
	}
	_, err := validateBindings([]HotkeyBinding{
		{Name: "open", Accelerator: "Ctrl+Alt+P", Command: "open"},
		{Name: "other", Accelerator: "Alt+Ctrl+P", Command: "status"},
	})
	if err == nil {
		t.Fatal("duplicate normalized accelerator accepted")
	}
}

func TestHotkeyStatusCopiesBindings(t *testing.T) {
	bindings := []HotkeyBinding{{Name: "status", Accelerator: "Ctrl+Alt+P", Command: "status"}}
	accelerators, err := validateBindings(bindings)
	if err != nil || !reflect.DeepEqual(accelerators[0].Canonical, "Ctrl+Alt+P") {
		t.Fatalf("validate=%#v err=%v", accelerators, err)
	}
}
