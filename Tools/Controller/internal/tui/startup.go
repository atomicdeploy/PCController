package tui

import (
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// StartupFrame is a pure, immediate presentation: no configuration, transport,
// timers or device work. It shares the full TUI palette without changing the
// global renderer's colour profile or emitting a fictitious progress value.
func StartupFrame(title, phase string, width int) string {
	if width < 8 {
		return ""
	}
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	clean := func(value string) string {
		return strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, ansi.Strip(value)))
	}
	title, phase = clean(title), clean(phase)
	cardWidth := min(62, width-2)
	contentWidth := max(2, cardWidth-4)
	bright := renderer.NewStyle().Foreground(colorAccent).Bold(true)
	muted := renderer.NewStyle().Foreground(colorMuted)
	accent := renderer.NewStyle().Foreground(colorAccent2)
	content := bright.Render(ansi.Truncate(title, contentWidth, "…")) + "\n\n" + muted.Render(ansi.Truncate(phase, contentWidth, "…"))
	if contentWidth >= 32 {
		mark := strings.Join([]string{accent.Render("   │ │ │   "), accent.Render(" ╭─┴─┴─┴─╮ "),
			accent.Render("─┤  ") + bright.Render("PC") + accent.Render("   ├─"), accent.Render(" ╰─┬─┬─┬─╯ "), accent.Render("   │ │ │   ")}, "\n")
		captionWidth := contentWidth - 14
		caption := "\n" + bright.Render(ansi.Truncate(title, captionWidth, "…")) + "\n" +
			accent.Render("TERMINAL") + "\n\n" + muted.Render(ansi.Truncate(phase, captionWidth, "…"))
		content = lipgloss.JoinHorizontal(lipgloss.Top, mark, "  ", caption)
	}
	card := renderer.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPanel).
		Padding(1).Width(contentWidth).Render(content)
	return "\n" + renderer.PlaceHorizontal(width, lipgloss.Center, card) + "\n"
}
