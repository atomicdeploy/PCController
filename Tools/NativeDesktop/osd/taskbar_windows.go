//go:build windows

package osd

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var taskbarOwnerProperty, _ = windows.UTF16PtrFromString(TaskbarOwnerProperty)
var taskbarSetOwner = osdUser.NewProc("SetPropW")
var taskbarRemoveOwner = osdUser.NewProc("RemovePropW")
var taskbarOwnerProcess = osdUser.NewProc("GetWindowThreadProcessId")

// RegisterTaskbarWindow marks a main GUI HWND, without granting IPC ownership.
// The caller must remove it during disposal; Windows also clears properties
// when the HWND is destroyed. No other process's window can be registered.
func RegisterTaskbarWindow(hwnd uintptr) error {
	var pid uint32
	if hwnd != 0 {
		taskbarOwnerProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	}
	if pid == 0 || pid != windows.GetCurrentProcessId() {
		return fmt.Errorf("OSD taskbar owner must be a window of the current process")
	}
	result, _, err := taskbarSetOwner.Call(hwnd, uintptr(unsafe.Pointer(taskbarOwnerProperty)), 1)
	if result == 0 {
		return fmt.Errorf("register OSD taskbar owner: %v", err)
	}
	return nil
}

func UnregisterTaskbarWindow(hwnd uintptr) {
	if hwnd != 0 {
		taskbarRemoveOwner.Call(hwnd, uintptr(unsafe.Pointer(taskbarOwnerProperty)))
	}
}
