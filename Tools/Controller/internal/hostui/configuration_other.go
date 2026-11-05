//go:build !windows && !darwin

package hostui

import (
	"os"
)

func ConfigurationDesktopAvailable() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
