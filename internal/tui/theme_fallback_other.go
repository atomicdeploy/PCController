//go:build !windows

package tui

func systemThemeFallbackDark() bool {
	// A dark default is the most broadly readable convention for terminal
	// hosts that neither answer OSC 11 nor expose the Windows app preference.
	return true
}
