package i18n

import (
	"strings"
	"testing"
)

func TestParsePreference(t *testing.T) {
	tests := []struct {
		input string
		want  Locale
	}{
		{"auto", Auto},
		{" AUTO ", Auto},
		{"en", English},
		{" EN ", English},
		{"fa", Persian},
		{" Fa ", Persian},
	}
	for _, test := range tests {
		got, err := ParsePreference(test.input)
		if err != nil {
			t.Fatalf("ParsePreference(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("ParsePreference(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	for _, invalid := range []string{"", "system", "fa-IR", "ar", "de"} {
		if got, err := ParsePreference(invalid); err == nil {
			t.Errorf("ParsePreference(%q) = %q without error", invalid, got)
		}
	}
}

func TestResolveName(t *testing.T) {
	tests := []struct {
		preference Locale
		systemName string
		want       Locale
	}{
		{Auto, "fa-IR", Persian},
		{Auto, "FA_ir.UTF-8", Persian},
		{Auto, "fa@calendar=persian", Persian},
		{Auto, "en-US", English},
		{Auto, "ar-SA", English},
		{Auto, "", English},
		{English, "fa-IR", English},
		{Persian, "en-US", Persian},
		{Locale("invalid"), "fa-IR", English},
	}
	for _, test := range tests {
		if got := ResolveName(test.preference, test.systemName); got != test.want {
			t.Errorf(
				"ResolveName(%q, %q) = %q, want %q",
				test.preference,
				test.systemName,
				got,
				test.want,
			)
		}
	}
}

func TestMetadata(t *testing.T) {
	english := NewForSystem(Auto, "en-GB").Metadata()
	if english.Locale != English || english.Direction != LTR ||
		english.RTL || english.Digits != LatinDigits ||
		english.LanguageTag != "en-US" {
		t.Fatalf("English metadata = %+v", english)
	}

	persian := NewForSystem(Auto, "fa-IR").Metadata()
	if persian.Locale != Persian || persian.Direction != RTL ||
		!persian.RTL || persian.Digits != PersianDigits ||
		persian.LanguageTag != "fa-IR" {
		t.Fatalf("Persian metadata = %+v", persian)
	}
}

func TestSystemLocaleNameIsUsable(t *testing.T) {
	name, err := SystemLocaleName()
	if err != nil {
		t.Fatalf("SystemLocaleName(): %v", err)
	}
	if strings.TrimSpace(name) == "" {
		t.Fatal("SystemLocaleName() returned an empty name")
	}
	if got := Resolve(Auto); got != English && got != Persian {
		t.Fatalf("Resolve(Auto) = %q", got)
	}
}
