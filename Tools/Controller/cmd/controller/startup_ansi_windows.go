//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// Bubble Tea normally enables VT after bootstrap. The startup frame precedes
// it, so classic conhost needs the same capability enabled before that frame.
func enableStartupANSI(output io.Writer) {
	file, ok := output.(*os.File)
	if !ok {
		return
	}
	var mode uint32
	handle := windows.Handle(file.Fd())
	if windows.GetConsoleMode(handle, &mode) == nil {
		_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}
