package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// sanitizeTerminalLine turns all externally sourced text into one printable
// line before Lip Gloss adds RayanLamp-owned styling. ANSI CSI/OSC sequences,
// C0/C1 controls, and bidi overrides must never reach the user's terminal.
func sanitizeTerminalLine(value string) string {
	value = ansi.Strip(value)
	var result strings.Builder
	result.Grow(len(value))
	for _, character := range value {
		switch {
		case character == '\r' || character == '\n' || character == '\t':
			result.WriteByte(' ')
		case unicode.IsControl(character) || unsafeBidiControl(character):
			result.WriteRune('�')
		default:
			result.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(result.String()), " ")
}

func unsafeBidiControl(character rune) bool {
	switch character {
	case '\u061c', '\u200e', '\u200f', '\ufeff':
		return true
	}
	return character >= '\u202a' && character <= '\u202e' ||
		character >= '\u2066' && character <= '\u2069'
}

func truncateTerminalLine(value string, width int) string {
	value = sanitizeTerminalLine(value)
	if width <= 0 || value == "" {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}
