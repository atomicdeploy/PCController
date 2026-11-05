//go:build windows

package shellintegration

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	appIDShell32 = windows.NewLazySystemDLL("shell32.dll")
	appIDProc    = appIDShell32.NewProc("SetCurrentProcessExplicitAppUserModelID")

	setProcessAppUserModelID = setProcessAppUserModelIDWindows
)

// SetAppUserModelID gives the current process a stable taskbar identity. It
// changes process state only; it does not register the portable executable.
func SetAppUserModelID() error {
	return setProcessAppUserModelID(AppUserModelID)
}

func setProcessAppUserModelIDWindows(value string) error {
	if err := appIDProc.Find(); err != nil {
		return fmt.Errorf("%w: SetCurrentProcessExplicitAppUserModelID: %v", ErrUnsupported, err)
	}
	pointer, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return fmt.Errorf("shellintegration: encode AppUserModelID: %w", err)
	}
	result, _, _ := appIDProc.Call(uintptr(unsafe.Pointer(pointer)))
	if int32(uint32(result)) < 0 {
		return fmt.Errorf(
			"shellintegration: SetCurrentProcessExplicitAppUserModelID failed with HRESULT 0x%08X",
			uint32(result),
		)
	}
	return nil
}
