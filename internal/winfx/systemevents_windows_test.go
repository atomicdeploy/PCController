//go:build windows

package winfx

import (
	"testing"

	"github.com/lxn/win"
)

func TestNetworkCallbackPostsOnlyStableValues(t *testing.T) {
	previous := systemEventsPostMessage
	defer func() { systemEventsPostMessage = previous }()

	var gotHWND win.HWND
	var gotMessage uint32
	var gotWParam uintptr
	var gotLParam uintptr
	systemEventsPostMessage = func(
		hwnd win.HWND,
		message uint32,
		wParam uintptr,
		lParam uintptr,
	) uintptr {
		gotHWND = hwnd
		gotMessage = message
		gotWParam = wParam
		gotLParam = lParam
		return 1
	}

	const (
		hwnd             = uintptr(0x5241)
		rowPointer       = uintptr(0xDEADBEEF)
		notificationType = uintptr(3)
	)
	if result := systemEventsNetworkChange(
		hwnd,
		rowPointer,
		notificationType,
	); result != 0 {
		t.Fatalf("callback result = %d, want 0", result)
	}
	if gotHWND != win.HWND(hwnd) ||
		gotMessage != WMSystemNetworkChange ||
		gotWParam != notificationType ||
		gotLParam != 0 {
		t.Fatalf(
			"posted = (%#x, %#x, %#x, %#x)",
			gotHWND,
			gotMessage,
			gotWParam,
			gotLParam,
		)
	}
	if gotLParam == rowPointer {
		t.Fatal("ephemeral MIB row pointer crossed callback boundary")
	}
}

func TestSystemEventsCloseWithoutRegistrationsIsIdempotent(t *testing.T) {
	registration := &SystemEvents{}
	if err := registration.Close(); err != nil {
		t.Fatal(err)
	}
	if err := registration.Close(); err != nil {
		t.Fatal(err)
	}
}
