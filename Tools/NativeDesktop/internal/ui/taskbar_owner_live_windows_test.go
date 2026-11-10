//go:build windows

package ui

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/internal/wmipc"
	"pccontroller.local/desktop/osd"
)

func TestControllerTaskbarOwnerDoesNotClaimLegacyIPC(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	getProp := windows.NewLazySystemDLL("user32.dll").NewProc("GetPropW")
	property := func(name string) uintptr {
		text, _ := windows.UTF16PtrFromString(name)
		value, _, _ := getProp.Call(s.HWND(), uintptr(unsafe.Pointer(text)))
		return value
	}
	if property(osd.TaskbarOwnerProperty) == 0 {
		t.Fatal("controller-backed GUI has no taskbar owner marker")
	}
	if property(wmipc.WindowProperty) != 0 {
		t.Fatal("taskbar registration incorrectly claimed the legacy WM_COPYDATA route")
	}
}
