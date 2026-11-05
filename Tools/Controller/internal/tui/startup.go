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
func StartupFrame(title, phase string, width int, details ...string) string {
	return welcomeFrame(title, phase, width, 0, details...)
}

// WelcomeFrame animates the same first-paint card; it is not a second splash.
// The spectrum is decoration, never a guessed loading percentage.
func WelcomeFrame(title, phase string, width, frame int, details ...string) string {
	return welcomeFrame(title, phase, width, frame, details...)
}

func welcomeFrame(title, phase string, width, frame int, details ...string) string {
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
			accent.Render("TERMINAL")
		content = lipgloss.JoinHorizontal(lipgloss.Top, mark, "  ", caption) + "\n" + muted.Width(contentWidth).Render(phase)
	}
	for _, detail := range details {
		if detail = clean(detail); detail != "" {
			content += "\n" + muted.Width(contentWidth).Render(detail)
		}
	}
	palette := []lipgloss.Color{"#F472B6", "#FB7185", "#FBBF24", "#4ADE80", "#22D3EE", "#818CF8", "#C084FC"}
	var spectrum strings.Builder
	for column := 0; column < contentWidth; column++ {
		color := palette[(column*len(palette)/contentWidth+frame/2)%len(palette)]
		spectrum.WriteString(renderer.NewStyle().Foreground(color).Render("━"))
	}
	content += "\n\n" + spectrum.String()
	card := renderer.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPanel).
		Padding(1).Width(contentWidth + 2).Render(content)
	return "\n" + renderer.PlaceHorizontal(width, lipgloss.Center, card) + "\n"
}
