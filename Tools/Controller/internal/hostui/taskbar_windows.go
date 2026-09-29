//go:build windows

package hostui

import (
	"context"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var taskbarConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
var taskbarWindowVisible = windows.NewLazySystemDLL("user32.dll").NewProc("IsWindowVisible")

// SetTaskbarProgress updates the classic console's native taskbar button.
// Windows Terminal receives OSC 9;4 separately; services have no console HWND.
func SetTaskbarProgress(progress TerminalProgress) error {
	if _, err := progress.OSCPayload(); err != nil {
		return err
	}
	window, _, _ := taskbarConsoleWindow.Call()
	if window == 0 {
		return nil
	}
	visible, _, _ := taskbarWindowVisible.Call(window)
	if visible == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return runCOMApartment(ctx, func() error {
		classID := windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
		interfaceID := windows.GUID{Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86, Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
		bar, err := createCOMInstance(classID, interfaceID)
		if err != nil {
			return err
		}
		defer releaseCOM(bar)
		if err := callCOM(bar, 3).error("initialize taskbar progress"); err != nil {
			return err
		}
		if progress.State == 1 {
			var result hresult
			if unsafe.Sizeof(uintptr(0)) == 4 {
				result = callCOM(bar, 9, window, uintptr(progress.Percent), 0, 100, 0)
			} else {
				result = callCOM(bar, 9, window, uintptr(progress.Percent), 100)
			}
			if err := result.error("set taskbar progress value"); err != nil {
				return err
			}
		}
		// Error/paused can be signaled without inventing a measured percentage.
		flags := [...]uintptr{0, 2, 4, 1, 8}
		return callCOM(bar, 10, window, flags[progress.State]).error("set taskbar progress state")
	})
}
