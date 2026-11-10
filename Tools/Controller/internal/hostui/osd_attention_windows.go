//go:build windows

package hostui

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/osd"
)

var osdAttentionUser32 = windows.NewLazySystemDLL("user32.dll")
var osdEnumWindows = osdAttentionUser32.NewProc("EnumWindows")
var osdWindowProcess = osdAttentionUser32.NewProc("GetWindowThreadProcessId")
var osdGetProp = osdAttentionUser32.NewProc("GetPropW")
var osdGetForeground = osdAttentionUser32.NewProc("GetForegroundWindow")
var osdFlashWindow = osdAttentionUser32.NewProc("FlashWindowEx")

var osdPrimaryProperty, _ = windows.UTF16PtrFromString(osd.TaskbarOwnerProperty)
var osdAttentionEnumeration struct {
	sync.Mutex
	target uintptr
}

// NewCallback slots are permanent in Go. Allocate once, not on every overlay.
var osdFindAttentionWindow = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
	var pid uint32
	osdWindowProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != windows.GetCurrentProcessId() {
		return 1
	}
	visible, _, _ := taskbarWindowVisible.Call(hwnd)
	marker, _, _ := osdGetProp.Call(hwnd, uintptr(unsafe.Pointer(osdPrimaryProperty)))
	if visible != 0 && marker != 0 {
		osdAttentionEnumeration.target = hwnd
		return 0
	}
	return 1
})

type osdFlashInfo struct {
	Size    uint32
	Window  uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// Only the current process's registered primary GUI or its own classic console
// is eligible. Never borrow a foreground HWND or another running controller.
func flashOSDTaskbar() (string, error) {
	osdAttentionEnumeration.Lock()
	osdAttentionEnumeration.target = 0
	osdEnumWindows.Call(osdFindAttentionWindow, 0)
	target := osdAttentionEnumeration.target
	osdAttentionEnumeration.Unlock()
	if target == 0 {
		// GetConsoleWindow is an authoritative handle for this process; do not
		// enumerate Windows Terminal windows (which can belong to other tabs).
		target, _, _ = taskbarConsoleWindow.Call()
		visible, _, _ := taskbarWindowVisible.Call(target)
		if target == 0 || visible == 0 {
			return taskbarAttention(0, 0, nil), nil
		}
	}
	foreground, _, _ := osdGetForeground.Call()
	return taskbarAttention(target, foreground, func(hwnd uintptr) {
		info := osdFlashInfo{Window: hwnd, Flags: 2, Count: 3} // FLASHW_TRAY, bounded three flashes
		info.Size = uint32(unsafe.Sizeof(info))
		// The return value is the previous active state, not a success indicator.
		osdFlashWindow.Call(uintptr(unsafe.Pointer(&info)))
	}), nil
}
