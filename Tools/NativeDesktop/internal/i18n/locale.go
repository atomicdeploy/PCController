// Package i18n provides the shared, presentation-agnostic localization
// foundation used by RayanLamp's native, terminal, and web-facing surfaces.
package i18n

import (
	"errors"
	"strings"
)

// Locale is a supported locale preference or a resolved application locale.
type Locale string

const (
	// Auto follows the operating-system locale and falls back to English when
	// the system locale is unavailable or unsupported.
	Auto Locale = "auto"
	// English is the English application locale.
	English Locale = "en"
	// Persian is the Persian (Farsi) application locale.
	Persian Locale = "fa"
)

// Direction describes the base writing direction of a resolved locale.
type Direction string

const (
	LTR Direction = "ltr"
	RTL Direction = "rtl"
)

// DigitSet describes the preferred decimal digit glyphs for a locale.
type DigitSet string

const (
	LatinDigits   DigitSet = "latin"
	PersianDigits DigitSet = "persian"
)

// Metadata contains layout and formatting information for a resolved locale.
type Metadata struct {
	Locale      Locale    `json:"locale"`
	LanguageTag string    `json:"language_tag"`
	Direction   Direction `json:"direction"`
	RTL         bool      `json:"rtl"`
	Digits      DigitSet  `json:"digits"`
}

// ParsePreference validates and canonicalizes a persisted locale preference.
// The only accepted values are auto, en, and fa.
func ParsePreference(value string) (Locale, error) {
	locale := Locale(strings.ToLower(strings.TrimSpace(value)))
	switch locale {
	case Auto, English, Persian:
		return locale, nil
	default:
		return "", errors.New("locale must be auto, en, or fa")
	}
}

// Resolve resolves a preference using the operating-system locale. Explicit
// English and Persian preferences do not query the operating system.
func Resolve(preference Locale) Locale {
	switch preference {
	case English, Persian:
		return preference
	case Auto:
		name, err := SystemLocaleName()
		if err != nil {
			return English
		}
		return ResolveName(Auto, name)
	default:
		return English
	}
}

// ResolveName resolves a locale preference against an explicit system locale
// name. It is useful for synchronized clients and deterministic tests.
func ResolveName(preference Locale, systemLocaleName string) Locale {
	switch preference {
	case English, Persian:
		return preference
	case Auto:
		if baseLanguage(systemLocaleName) == string(Persian) {
			return Persian
		}
		return English
	default:
		return English
	}
}

// Describe returns layout and number-formatting metadata for a locale. Auto is
// resolved using the operating system.
func Describe(locale Locale) Metadata {
	switch Resolve(locale) {
	case Persian:
		return Metadata{
			Locale:      Persian,
			LanguageTag: "fa-IR",
			Direction:   RTL,
			RTL:         true,
			Digits:      PersianDigits,
		}
	default:
		return Metadata{
			Locale:      English,
			LanguageTag: "en-US",
			Direction:   LTR,
			RTL:         false,
			Digits:      LatinDigits,
		}
	}
}

func baseLanguage(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if cut, _, ok := strings.Cut(name, "."); ok {
		name = cut
	}
	if cut, _, ok := strings.Cut(name, "@"); ok {
		name = cut
	}
	name = strings.ReplaceAll(name, "_", "-")
	if cut, _, ok := strings.Cut(name, "-"); ok {
		name = cut
	}
	return name
}
