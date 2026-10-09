package appconfig

import (
	"strings"
	"testing"
)

func TestDefaultKeyboardControlIsSafeAndPreconfigured(t *testing.T) {
	config := Defaults()
	keyboard := config.Integrations.Keyboard
	if keyboard.Enabled {
		t.Fatal("ordinary-key keyboard control must be disabled by default")
	}
	if len(keyboard.Bindings) != 13 {
		t.Fatalf("bindings=%d, want four motion plus nine output bindings", len(keyboard.Bindings))
	}
	wantMotion := map[string][2]string{
		"A": {"B", "up"},
		"S": {"B", "down"},
		"K": {"A", "up"},
		"L": {"A", "down"},
	}
	for _, binding := range keyboard.Bindings {
		if want, ok := wantMotion[binding.Key]; ok {
			if binding.Primary.Type != "motion" ||
				binding.Primary.Behavior != "momentary" ||
				binding.Primary.Side != want[0] ||
				binding.Primary.Direction != want[1] {
				t.Fatalf("motion %s=%+v, want side=%s direction=%s", binding.Key, binding.Primary, want[0], want[1])
			}
			if binding.Control == nil || binding.Control.Behavior != "latch" ||
				binding.Control.Side != want[0] || binding.Control.Direction != want[1] {
				t.Fatalf("Ctrl+%s=%+v, want fail-safe motion latch", binding.Key, binding.Control)
			}
		}
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
}

func TestKeyboardControlValidationRejectsUnsafeOrAmbiguousBindings(t *testing.T) {
	tests := []struct {
		name   string
		change func(*KeyboardControl)
		want   string
	}{
		{
			name: "enabled without enabled binding",
			change: func(value *KeyboardControl) {
				value.Enabled = true
				for index := range value.Bindings {
					value.Bindings[index].Enabled = false
				}
			},
			want: "at least one enabled binding",
		},
		{
			name: "modifier key",
			change: func(value *KeyboardControl) {
				value.Bindings[0].Key = "CTRL"
			},
			want: "cannot be a keyboard-control key",
		},
		{
			name: "duplicate key",
			change: func(value *KeyboardControl) {
				value.Bindings[1].Key = value.Bindings[0].Key
			},
			want: "duplicates",
		},
		{
			name: "toggled motion",
			change: func(value *KeyboardControl) {
				value.Bindings[0].Primary.Behavior = "toggle"
			},
			want: "motion behavior must be momentary or latch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Defaults()
			test.change(&config.Integrations.Keyboard)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestKeyboardControlCloneDoesNotAliasAlternateAction(t *testing.T) {
	source := Defaults()
	copyValue := clone(source)
	if copyValue.Integrations.Keyboard.Bindings[4].Control == nil {
		t.Fatal("default relay Ctrl action is missing")
	}
	copyValue.Integrations.Keyboard.Bindings[4].Control.Behavior = "latch"
	if source.Integrations.Keyboard.Bindings[4].Control.Behavior != "momentary" {
		t.Fatal("configuration clone aliased a Ctrl alternate action")
	}
}

func TestAutomationCanSubscribeToSourceTaggedKeyboardEvents(t *testing.T) {
	config := Defaults()
	config.Automations = []Automation{{
		Name: "keyboard-audit",
		Match: AutomationMatch{
			Kind: "keyboard.input", Source: "pc-keyboard",
		},
		Actions: []AutomationAction{{Type: "emit", Event: "keyboard.seen"}},
	}}
	if err := config.Validate(); err != nil {
		t.Fatalf("pc-keyboard event automation rejected: %v", err)
	}
}
