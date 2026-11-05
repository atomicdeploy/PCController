//go:build windows

package tui

import (
	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/winfx"
)

func systemThemeFallbackDark() bool {
	// High Contrast owns the effective terminal-adjacent system colors even
	// when AppsUseLightTheme still contains the user's ordinary preference.
	if highContrast, err := winfx.HighContrast(); err == nil && highContrast {
		return colorRefIsDark(uint32(win.GetSysColor(win.COLOR_WINDOW)))
	}
	if dark, err := winfx.SystemDarkMode(); err == nil {
		return dark
	}
	return colorRefIsDark(uint32(win.GetSysColor(win.COLOR_WINDOW)))
}
