//go:build windows

package winfx

import (
	"os"

	"rayanlamp.local/app/internal/terminal"
)

// EnableVT100 is kept as a compatibility wrapper. New console code should use
// internal/terminal so Windows, Linux-hosted cross builds, and TUI policy all
// share one implementation.
func EnableVT100() (bool, error) {
	return terminal.EnableVT100(os.Stdout, os.Stderr)
}

// IsTerminal delegates to the shared terminal policy.
func IsTerminal(file *os.File) bool {
	return terminal.IsTerminal(file)
}

// NoColor delegates to the shared terminal policy.
func NoColor() bool {
	return terminal.NoColor()
}

// ColorEnabled delegates to the shared terminal policy.
func ColorEnabled(file *os.File) bool {
	return terminal.ColorEnabled(file)
}
