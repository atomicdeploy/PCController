package tui

import (
	tea "charm.land/bubbletea/v2"
	"pccontroller.local/desktop/internal/terminal"
)

func initialThemeIsDark(mode ThemeMode) bool {
	switch mode {
	case ThemeDark:
		return true
	case ThemeLight:
		return false
	default:
		// OSC 11 is asynchronous and is not supported by every Windows
		// console host. Start with a readable platform preference immediately;
		// a later BackgroundColorMsg remains authoritative in auto mode.
		return systemThemeFallbackDark()
	}
}

func backgroundColorRequest(mode ThemeMode) tea.Cmd {
	if mode != ThemeAuto {
		return nil
	}
	return tea.RequestBackgroundColor
}

// noColorRequested shares the executable's complete terminal policy, including
// NO_COLOR, TERM=dumb, CLICOLOR=0, and FORCE_COLOR=0.
func noColorRequested() bool {
	return !terminal.ColorRequested()
}

func colorRefIsDark(value uint32) bool {
	// COLORREF is 0x00BBGGRR.
	red := int(value & 0xff)
	green := int((value >> 8) & 0xff)
	blue := int((value >> 16) & 0xff)
	return (299*red+587*green+114*blue)/1000 < 128
}
