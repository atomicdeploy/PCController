package tui

import "testing"

// Tests asserting ANSI/emoji must establish that terminal capability. The
// production policy intentionally respects inherited TERM=dumb and NO_COLOR.
func setColorCapableTestTerminal(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"TERM", "NO_COLOR", "NO_EMOJI", "CLICOLOR", "CLICOLOR_FORCE",
		"FORCE_COLOR", "RAYANLAMP_FORCE_COLOR", "RAYANLAMP_EMOJI",
	} {
		t.Setenv(name, "")
	}
}
