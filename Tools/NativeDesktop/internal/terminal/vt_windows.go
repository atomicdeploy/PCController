//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

const windowsUTF8CodePage = 65001

// EnableVT100 enables processed ANSI/VT output on every supplied Windows
// console stream. Redirected writers are deliberately left untouched. The
// bool reports whether at least one stream supports VT output.
func EnableVT100(writers ...io.Writer) (bool, error) {
	var enabled bool
	var failures []error
	for index, writer := range writers {
		file := outputFile(writer)
		if file == nil {
			continue
		}
		handle := windows.Handle(file.Fd())
		var mode uint32
		if err := windows.GetConsoleMode(handle, &mode); err != nil {
			// Pipes and files report ERROR_INVALID_HANDLE here. They should
			// receive plain output, not terminal setup errors.
			continue
		}
		wanted := mode |
			windows.ENABLE_PROCESSED_OUTPUT |
			windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		if wanted != mode {
			if err := windows.SetConsoleMode(handle, wanted); err != nil {
				failures = append(
					failures,
					fmt.Errorf("enable VT output on stream %d: %w", index+1, err),
				)
				continue
			}
		}
		enabled = true
	}
	return enabled, errors.Join(failures...)
}

func platformEmojiSupported(_ *os.File) bool {
	if os.Getenv("WT_SESSION") != "" ||
		os.Getenv("ConEmuANSI") != "" ||
		os.Getenv("ANSICON") != "" {
		return true
	}
	codePage, err := windows.GetConsoleOutputCP()
	return err == nil && codePage == windowsUTF8CodePage
}
