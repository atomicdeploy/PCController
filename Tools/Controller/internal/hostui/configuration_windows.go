//go:build windows

package hostui

import (
	"os"
	"syscall"
	"unsafe"
)

func ConfigurationDesktopAvailable() bool {
	var session uint32
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("ProcessIdToSessionId")
	ok, _, _ := proc.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&session)))
	return ok != 0 && session != 0 // Services cannot display editors in session zero.
}
