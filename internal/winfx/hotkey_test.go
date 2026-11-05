package winfx

import (
	"errors"
	"testing"
)

func TestParseHotkey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		modifiers uint32
		key       uint32
		canonical string
	}{
		{"basic", "Ctrl+Alt+L", ModControl | ModAlt, 'L', "Ctrl+Alt+L"},
		{"aliases", " control + windows + pgdn ", ModControl | ModWin, 0x22, "Ctrl+Win+PageDown"},
		{"function", "SHIFT+F24", ModShift, 0x87, "Shift+F24"},
		{"media", "Alt+Media_Play_Pause", ModAlt, 0xB3, "Alt+MediaPlayPause"},
		{"digit", "Ctrl+7", ModControl, '7', "Ctrl+7"},
		{"print screen", "Win+PrtSc", ModWin, 0x2C, "Win+PrintScreen"},
		{"apps menu", "Ctrl+Menu", ModControl, 0x5D, "Ctrl+Apps"},
		{"browser", "Alt+browser_back", ModAlt, 0xA6, "Alt+BrowserBack"},
		{"launch", "Shift+media_select", ModShift, 0xB5, "Shift+LaunchMediaSelect"},
		{"punctuation", "Ctrl+;", ModControl, 0xBA, "Ctrl+Semicolon"},
		{"separator", "Ctrl+NumSeparator", ModControl, 0x6C, "Ctrl+Separator"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseHotkey(test.input)
			if err != nil {
				t.Fatalf("ParseHotkey(%q): %v", test.input, err)
			}
			if got.Modifiers != test.modifiers || got.Key != test.key {
				t.Fatalf("ParseHotkey(%q) = %#v, want modifiers %#x key %#x", test.input, got, test.modifiers, test.key)
			}
			if got.String() != test.canonical {
				t.Fatalf("canonical = %q, want %q", got.String(), test.canonical)
			}
		})
	}
}

func TestParseHotkeyProfessionalKeyAliasesRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		alias     string
		key       uint32
		canonical string
	}{
		{"snapshot", 0x2C, "PrintScreen"},
		{"context_menu", 0x5D, "Apps"},
		{"sleep", 0x5F, "Sleep"},
		{"browser_forward", 0xA7, "BrowserForward"},
		{"browser_refresh", 0xA8, "BrowserRefresh"},
		{"browser_stop", 0xA9, "BrowserStop"},
		{"browser_search", 0xAA, "BrowserSearch"},
		{"browser_favorites", 0xAB, "BrowserFavorites"},
		{"browser_home", 0xAC, "BrowserHome"},
		{"mail", 0xB4, "LaunchMail"},
		{"launch_media", 0xB5, "LaunchMediaSelect"},
		{"app1", 0xB6, "LaunchApp1"},
		{"app2", 0xB7, "LaunchApp2"},
		{";", 0xBA, "Semicolon"},
		{"=", 0xBB, "Equal"},
		{",", 0xBC, "Comma"},
		{"-", 0xBD, "Minus"},
		{".", 0xBE, "Period"},
		{"/", 0xBF, "Slash"},
		{"`", 0xC0, "Backtick"},
		{"[", 0xDB, "LeftBracket"},
		{"\\", 0xDC, "Backslash"},
		{"]", 0xDD, "RightBracket"},
		{"'", 0xDE, "Quote"},
		{"numpadseparator", 0x6C, "Separator"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.canonical, func(t *testing.T) {
			t.Parallel()

			parsed, err := ParseHotkey("Ctrl+" + test.alias)
			if err != nil {
				t.Fatalf("ParseHotkey alias %q: %v", test.alias, err)
			}
			want := "Ctrl+" + test.canonical
			if parsed.Key != test.key || parsed.String() != want {
				t.Fatalf("alias %q = %#v / %q, want key %#x / %q",
					test.alias, parsed, parsed.String(), test.key, want)
			}
			roundTrip, err := ParseHotkey(parsed.String())
			if err != nil || roundTrip != parsed {
				t.Fatalf("round trip %q = %#v, %v; want %#v",
					parsed.String(), roundTrip, err, parsed)
			}
		})
	}
}

func TestParseHotkeyKeepsF12Parseable(t *testing.T) {
	t.Parallel()

	parsed, err := ParseHotkey("Ctrl+F12")
	if err != nil {
		t.Fatalf("ParseHotkey F12: %v", err)
	}
	if parsed.Key != 0x7B || parsed.String() != "Ctrl+F12" {
		t.Fatalf("F12 = %#v / %q", parsed, parsed.String())
	}
}

func TestParseHotkeyErrors(t *testing.T) {
	tests := []struct {
		input string
		want  error
	}{
		{"", ErrEmptyHotkey},
		{"Ctrl+Alt", ErrInvalidHotkey},
		{"Ctrl++L", ErrInvalidHotkey},
		{"Ctrl+Control+L", ErrInvalidHotkey},
		{"Ctrl+L+M", ErrInvalidHotkey},
		{"Ctrl+NoSuchKey", ErrInvalidHotkey},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			_, err := ParseHotkey(test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseHotkey(%q) error = %v, want %v", test.input, err, test.want)
			}
		})
	}
}
