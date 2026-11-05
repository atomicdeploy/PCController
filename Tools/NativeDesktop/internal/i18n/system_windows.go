//go:build windows

package i18n

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const localeNameMaxLength = 85

var getUserDefaultLocaleName = windows.NewLazySystemDLL(
	"kernel32.dll",
).NewProc("GetUserDefaultLocaleName")

// SystemLocaleName returns the current user's Windows locale name, such as
// en-US or fa-IR.
func SystemLocaleName() (string, error) {
	buffer := make([]uint16, localeNameMaxLength)
	length, _, callErr := getUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if length == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == 0 {
			return "", errors.New("GetUserDefaultLocaleName returned an empty locale")
		}
		return "", callErr
	}
	return windows.UTF16ToString(buffer[:length]), nil
}
