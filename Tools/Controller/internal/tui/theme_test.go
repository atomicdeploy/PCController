package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestConfigureInteractiveColorProfileIgnoresNonInteractiveParentEnvironment(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	priorProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(priorProfile) })

	ConfigureInteractiveColorProfile()

	if got := lipgloss.ColorProfile(); got != termenv.TrueColor {
		t.Fatalf("color profile = %v, want %v", got, termenv.TrueColor)
	}
	if rendered := titleStyle.Render("PCController"); !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("interactive title did not contain ANSI styling: %q", rendered)
	}
}
