//go:build windows

package winfx

import "testing"

func TestCallSubclassCallback(t *testing.T) {
	result, handled := callSubclassCallback(
		func(hwnd, msg, wParam, lParam uintptr) (uintptr, bool) {
			if hwnd != 1 || msg != 2 || wParam != 3 || lParam != 4 {
				t.Fatalf("unexpected callback arguments")
			}
			return 99, true
		},
		1, 2, 3, 4,
	)
	if !handled || result != 99 {
		t.Fatalf("callSubclassCallback = (%d, %v), want (99, true)", result, handled)
	}
}

func TestCallSubclassCallbackRecoversPanic(t *testing.T) {
	result, handled := callSubclassCallback(
		func(uintptr, uintptr, uintptr, uintptr) (uintptr, bool) {
			panic("boom")
		},
		0, 0, 0, 0,
	)
	if handled || result != 0 {
		t.Fatalf("panic result = (%d, %v), want (0, false)", result, handled)
	}
}
