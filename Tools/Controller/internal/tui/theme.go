package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("#7DCFFF")
	colorAccent2 = lipgloss.Color("#BB9AF7")
	colorGood    = lipgloss.Color("#9ECE6A")
	colorWarn    = lipgloss.Color("#E0AF68")
	colorBad     = lipgloss.Color("#F7768E")
	colorMuted   = lipgloss.Color("#737DA0")
	colorPanel   = lipgloss.Color("#3B4261")
	colorBright  = lipgloss.Color("#C0CAF5")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
	valueStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBright)
	cardStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPanel).
			Padding(0, 1)
	errorStyle    = lipgloss.NewStyle().Foreground(colorBad)
	rxStyle       = lipgloss.NewStyle().Foreground(colorGood)
	txStyle       = lipgloss.NewStyle().Foreground(colorAccent)
	warnStyle     = lipgloss.NewStyle().Foreground(colorWarn)
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1A1B26")).
			Background(colorAccent).
			Bold(true)
	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1A1B26")).
			Background(colorAccent2).
			Bold(true).
			Padding(0, 1)
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 1)
	buttonStyle = lipgloss.NewStyle().
			Foreground(colorBright).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPanel).
			Padding(0, 1)
	buttonGoodStyle = buttonStyle.Copy().BorderForeground(colorGood).Foreground(colorGood)
	buttonBadStyle  = buttonStyle.Copy().BorderForeground(colorBad).Foreground(colorBad)
)
