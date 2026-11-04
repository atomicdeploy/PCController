//go:build !windows

package hostui

import (
	"os"
	"runtime"
)

func ConfigurationDesktopAvailable() bool {
	return runtime.GOOS == "darwin" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
