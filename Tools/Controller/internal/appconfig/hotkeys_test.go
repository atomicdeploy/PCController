package appconfig

import (
	"strings"
	"testing"
)

func TestHotkeyValidationCanonicalizesConflicts(t *testing.T) {
	config := Defaults()
	config.Integrations.Hotkeys = []Hotkey{
		{Name: "first", Enabled: true, Chord: "Ctrl+Alt+P", Command: "status"},
		{Name: "second", Enabled: false, Chord: "control+option+p", Command: "hello"},
	}
	err := config.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicates accelerator Ctrl+Alt+P") {
		t.Fatalf("canonical duplicate err=%v", err)
	}
}

func TestHotkeyValidationRejectsUnregistrableOrUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		value  Hotkey
		needle string
	}{
		{
			name:   "bare ordinary key",
			value:  Hotkey{Name: "plain", Enabled: true, Chord: "P", Command: "status"},
			needle: "bare global hotkeys",
		},
		{
			name:   "control in name",
			value:  Hotkey{Name: "bad\nname", Enabled: true, Chord: "F18", Command: "status"},
			needle: "without control characters",
		},
		{
			name:   "multiline command",
			value:  Hotkey{Name: "bad-command", Enabled: true, Chord: "F18", Command: "status\nquit"},
			needle: "single line",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Defaults()
			config.Integrations.Hotkeys = []Hotkey{test.value}
			if err := config.Validate(); err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("validation err=%v", err)
			}
		})
	}
}

func TestDefaultHotkeysPassServerValidation(t *testing.T) {
	if err := ValidateHotkeys(Defaults().Integrations.Hotkeys); err != nil {
		t.Fatal(err)
	}
}
