package tui

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"pccontroller.local/desktop/internal/design"
)

type palette struct {
	background color.Color
	brightText color.Color
	surface    color.Color
	surfaceAlt color.Color
	border     color.Color
	borderHot  color.Color
	text       color.Color
	muted      color.Color
	faint      color.Color
	accent     color.Color
	accentAlt  color.Color
	on         color.Color
	off        color.Color
	good       color.Color
	warning    color.Color
	bad        color.Color
}

type styles struct {
	palette         palette
	app             lipgloss.Style
	header          lipgloss.Style
	title           lipgloss.Style
	subtitle        lipgloss.Style
	panel           lipgloss.Style
	panelHot        lipgloss.Style
	diagnosticPanel lipgloss.Style
	terminalPanel   lipgloss.Style
	panelTitle      lipgloss.Style
	label           lipgloss.Style
	value           lipgloss.Style
	muted           lipgloss.Style
	pill            lipgloss.Style
	button          lipgloss.Style
	buttonHot       lipgloss.Style
	buttonOn        lipgloss.Style
	buttonOff       lipgloss.Style
	notice          lipgloss.Style
	errorNotice     lipgloss.Style
	logTime         lipgloss.Style
	logLevel        lipgloss.Style
	logEvent        lipgloss.Style
	logDetail       lipgloss.Style
}

func newStyles(isDark bool) styles {
	p := themePalette(isDark)
	if noColorRequested() {
		p = noColorPalette()
	}
	rounded := lipgloss.RoundedBorder()
	return styles{
		palette: p,
		app: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.background),
		header: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface).
			BorderStyle(rounded).
			BorderForeground(p.borderHot).
			Padding(0, 2),
		title: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		subtitle: lipgloss.NewStyle().
			Foreground(p.muted).
			Italic(true),
		panel: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface).
			BorderStyle(rounded).
			BorderForeground(p.border).
			Padding(1, 2),
		panelHot: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surfaceAlt).
			BorderStyle(rounded).
			BorderForeground(p.borderHot).
			Padding(1, 2),
		diagnosticPanel: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface).
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(p.borderHot).
			Padding(1, 2),
		terminalPanel: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(p.borderHot).
			Padding(0, 1),
		panelTitle: lipgloss.NewStyle().
			Foreground(p.accentAlt).
			Bold(true),
		label: lipgloss.NewStyle().
			Foreground(p.muted),
		value: lipgloss.NewStyle().
			Foreground(p.text).
			Bold(true),
		muted: lipgloss.NewStyle().
			Foreground(p.muted),
		pill: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surfaceAlt).
			Padding(0, 1).
			Bold(true),
		button: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surfaceAlt).
			Padding(0, 1).
			Bold(true),
		buttonHot: lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.accent).
			Padding(0, 2).
			Bold(true),
		buttonOn: lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.on).
			Padding(0, 1).
			Bold(true),
		buttonOff: lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.off).
			Padding(0, 1).
			Bold(true),
		notice: lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.good).
			Padding(0, 1).
			Bold(true),
		errorNotice: lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.bad).
			Padding(0, 1).
			Bold(true),
		logTime: lipgloss.NewStyle().
			Foreground(p.faint),
		logLevel: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		logEvent: lipgloss.NewStyle().
			Foreground(p.accent).
			Bold(true),
		logDetail: lipgloss.NewStyle().
			Foreground(p.muted),
	}
}

func themePalette(isDark bool) palette {
	tokens := design.Default.Colors.Light
	if isDark {
		tokens = design.Default.Colors.Dark
	}
	return palette{
		background: lipgloss.Color(tokens.Canvas),
		// Status colors are intentionally vivid in both canonical palettes.
		// Their foreground therefore comes from the canonical dark canvas,
		// which remains readable on warm, success, danger, and accent fills.
		brightText: lipgloss.Color(design.Default.Colors.Dark.Canvas),
		surface:    lipgloss.Color(tokens.Surface),
		surfaceAlt: lipgloss.Color(tokens.SurfaceHigh),
		border:     lipgloss.Color(tokens.Stroke),
		borderHot:  lipgloss.Color(tokens.Accent),
		text:       lipgloss.Color(tokens.Text),
		muted:      lipgloss.Color(tokens.TextMuted),
		faint:      lipgloss.Color(tokens.TextSubtle),
		accent:     lipgloss.Color(tokens.Accent),
		accentAlt:  lipgloss.Color(tokens.AccentHover),
		on:         lipgloss.Color(tokens.Warm),
		off:        lipgloss.Color(tokens.SurfaceHigh),
		good:       lipgloss.Color(tokens.Success),
		warning:    lipgloss.Color(tokens.Warm),
		bad:        lipgloss.Color(tokens.Danger),
	}
}

func noColorPalette() palette {
	none := lipgloss.NoColor{}
	return palette{
		background: none,
		brightText: none,
		surface:    none,
		surfaceAlt: none,
		border:     none,
		borderHot:  none,
		text:       none,
		muted:      none,
		faint:      none,
		accent:     none,
		accentAlt:  none,
		on:         none,
		off:        none,
		good:       none,
		warning:    none,
		bad:        none,
	}
}

func helpStyles(isDark bool, p palette) help.Styles {
	return helpStylesOn(isDark, p, p.background)
}

func helpStylesOn(
	isDark bool,
	p palette,
	background color.Color,
) help.Styles {
	result := help.DefaultStyles(isDark)
	result.ShortKey = result.ShortKey.
		Foreground(p.accent).
		Background(background).
		Bold(true)
	result.ShortDesc = result.ShortDesc.
		Foreground(p.muted).
		Background(background)
	result.ShortSeparator = result.ShortSeparator.
		Foreground(p.faint).
		Background(background)
	result.FullKey = result.FullKey.
		Foreground(p.accent).
		Background(background).
		Bold(true)
	result.FullDesc = result.FullDesc.
		Foreground(p.muted).
		Background(background)
	result.FullSeparator = result.FullSeparator.
		Foreground(p.faint).
		Background(background)
	result.Ellipsis = result.Ellipsis.
		Foreground(p.faint).
		Background(background)
	return result
}

func styleOn(
	style lipgloss.Style,
	background color.Color,
) lipgloss.Style {
	return style.Copy().Background(background)
}

func backgroundRun(background color.Color, value string) string {
	return lipgloss.NewStyle().
		Background(background).
		Render(value)
}

func placeHorizontalOn(
	width int,
	position lipgloss.Position,
	value string,
	background color.Color,
) string {
	return lipgloss.PlaceHorizontal(
		width,
		position,
		value,
		lipgloss.WithWhitespaceStyle(
			lipgloss.NewStyle().Background(background),
		),
	)
}
