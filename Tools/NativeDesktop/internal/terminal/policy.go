// Package terminal centralizes RayanLamp's terminal capability and output
// policy. Console output stays plain when redirected, honors the conventional
// opt-out environment variables, and emits complete SGR sequences only when
// the destination can consume them.
package terminal

import (
	"io"
	"os"
	"strings"

	xterm "github.com/charmbracelet/x/term"
)

const (
	// Reset is the complete SGR reset sequence. Styled always appends it so a
	// RayanLamp label cannot leak its attributes into the caller's prompt.
	Reset = "\x1b[0m"

	BoldRed   = "\x1b[1;31m"
	BoldGreen = "\x1b[1;32m"
	BoldCyan  = "\x1b[1;36m"
)

// Capabilities describes what is safe for one output destination.
type Capabilities struct {
	Terminal bool
	Color    bool
	Emoji    bool
}

// Detect evaluates a writer using terminal detection and the process-wide
// output policy. Unknown writers and redirected files are plain by default.
func Detect(writer io.Writer) Capabilities {
	file := outputFile(writer)
	interactive := file != nil && xterm.IsTerminal(file.Fd())
	return Capabilities{
		Terminal: interactive,
		Color:    colorEnabled(interactive),
		Emoji:    emojiEnabled(interactive, file),
	}
}

// IsTerminal reports whether writer is an actual console/TTY rather than a
// redirected file, pipe, or arbitrary io.Writer.
func IsTerminal(writer io.Writer) bool {
	return Detect(writer).Terminal
}

// NoColor follows https://no-color.org/: NO_COLOR disables color when it is
// present with any non-empty value.
func NoColor() bool {
	value, present := os.LookupEnv("NO_COLOR")
	return present && value != ""
}

// ColorRequested reports whether color has not been disabled process-wide.
// It intentionally does not inspect a particular writer.
func ColorRequested() bool {
	if NoColor() || termIsDumb() {
		return false
	}
	if value, present := os.LookupEnv("CLICOLOR"); present &&
		strings.TrimSpace(value) == "0" {
		return false
	}
	if value, present := os.LookupEnv("FORCE_COLOR"); present &&
		strings.TrimSpace(value) == "0" {
		return false
	}
	return true
}

// ColorEnabled reports whether SGR color is safe for writer. NO_COLOR,
// TERM=dumb, CLICOLOR=0, and FORCE_COLOR=0 always win. FORCE_COLOR or
// CLICOLOR_FORCE can deliberately enable color for redirected output.
func ColorEnabled(writer io.Writer) bool {
	return Detect(writer).Color
}

// Styled wraps text in one complete SGR style and reset when color is safe.
// start must be one of this package's complete SGR constants.
func Styled(writer io.Writer, start, text string) string {
	return StyledIf(writer, true, start, text)
}

// StyledIf is Styled with a caller-level opt-out, such as --no-color.
func StyledIf(
	writer io.Writer,
	allowed bool,
	start string,
	text string,
) string {
	if text == "" || !allowed || !ColorEnabled(writer) {
		return text
	}
	return start + text + Reset
}

// EmojiEnabled reports whether emoji are safe for writer. Redirected output
// uses the supplied ASCII fallback unless explicitly forced.
func EmojiEnabled(writer io.Writer) bool {
	return Detect(writer).Emoji
}

// DisplayEmojiEnabled applies the emoji policy for an already-interactive
// display such as Bubble Tea. It does not require stdout detection, which also
// keeps off-screen TUI rendering deterministic in tests.
func DisplayEmojiEnabled() bool {
	return emojiEnabled(true, os.Stdout)
}

// Symbol chooses a tasteful emoji only when writer can represent it.
func Symbol(writer io.Writer, emoji, ascii string) string {
	if EmojiEnabled(writer) {
		return emoji
	}
	return ascii
}

// DisplaySymbol is Symbol for an already-interactive terminal renderer.
func DisplaySymbol(emoji, ascii string) string {
	if DisplayEmojiEnabled() {
		return emoji
	}
	return ascii
}

func colorEnabled(interactive bool) bool {
	if !ColorRequested() {
		return false
	}
	if forceColorRequested() {
		return true
	}
	return interactive
}

func forceColorRequested() bool {
	return environmentEnabled("FORCE_COLOR") ||
		environmentEnabled("CLICOLOR_FORCE") ||
		environmentEnabled("RAYANLAMP_FORCE_COLOR")
}

func emojiEnabled(interactive bool, file *os.File) bool {
	if emojiDisabled() || termIsDumb() {
		return false
	}
	if forced, specified := emojiOverride(); specified {
		return forced
	}
	return interactive && platformEmojiSupported(file)
}

func emojiDisabled() bool {
	return environmentEnabled("NO_EMOJI")
}

func environmentEnabled(name string) bool {
	value, present := os.LookupEnv(name)
	if !present {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func emojiOverride() (bool, bool) {
	value, present := os.LookupEnv("RAYANLAMP_EMOJI")
	if !present {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "always", "force", "on", "true", "yes":
		return true, true
	case "0", "never", "off", "false", "no":
		return false, true
	default:
		return false, false
	}
}

func termIsDumb() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb")
}

func outputFile(writer io.Writer) *os.File {
	file, _ := writer.(*os.File)
	return file
}
