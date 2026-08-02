package appconfig

import (
	"math"
	"path/filepath"
	"testing"
)

func TestAppearanceDefaultsAndExplicitValuesRoundTrip(t *testing.T) {
	value := Defaults()
	if err := value.Validate(); err != nil {
		t.Fatalf("default appearance is invalid: %v", err)
	}
	if value.UI.Appearance.Theme != "system" || value.UI.Appearance.Locale != "en" ||
		value.UI.Appearance.Direction != "auto" || value.UI.Appearance.AudioVolume != 0.42 {
		t.Fatalf("unexpected defaults: %#v", value.UI.Appearance)
	}

	value.UI.Appearance = Appearance{
		Theme: " DARK ", Locale: "FA", Direction: "RTL",
		ReduceMotion: true, CompactNumbers: true, AudioMuted: true, AudioVolume: 0,
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatalf("write appearance: %v", err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("load appearance: %v", err)
	}
	want := Appearance{
		Theme: "dark", Locale: "fa", Direction: "rtl",
		ReduceMotion: true, CompactNumbers: true, AudioMuted: true, AudioVolume: 0,
	}
	if loaded.UI.Appearance != want {
		t.Fatalf("appearance=%#v want=%#v", loaded.UI.Appearance, want)
	}
}

func TestAppearanceValidationRejectsUnsupportedOrNonFiniteValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Appearance)
	}{
		{name: "theme", change: func(value *Appearance) { value.Theme = "neon" }},
		{name: "locale", change: func(value *Appearance) { value.Locale = "de" }},
		{name: "direction", change: func(value *Appearance) { value.Direction = "sideways" }},
		{name: "negative volume", change: func(value *Appearance) { value.AudioVolume = -0.1 }},
		{name: "large volume", change: func(value *Appearance) { value.AudioVolume = 1.1 }},
		{name: "NaN volume", change: func(value *Appearance) { value.AudioVolume = math.NaN() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := Defaults()
			test.change(&value.UI.Appearance)
			if err := value.Validate(); err == nil {
				t.Fatalf("invalid appearance was accepted: %#v", value.UI.Appearance)
			}
		})
	}
}
