//go:build windows

package hostui

import (
	"fmt"

	"golang.org/x/sys/windows"
)

var messageBeep = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBeep")

// WarningBeep plays the configured Windows exclamation sound without taking
// ownership of the board buzzer or blocking the controller event loop.
func WarningBeep() error {
	result, _, callErr := messageBeep.Call(0x00000030) // MB_ICONEXCLAMATION
	if result == 0 {
		return fmt.Errorf("Windows warning beep failed: %w", callErr)
	}
	return nil
}
