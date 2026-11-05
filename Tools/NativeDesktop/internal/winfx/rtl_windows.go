//go:build windows

package winfx

import "github.com/lxn/win"

func windowRTLReading(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	return win.GetWindowLong(
		win.HWND(hwnd),
		win.GWL_EXSTYLE,
	)&win.WS_EX_RTLREADING != 0
}

func mirrorHorizontalEdges(
	containerLeft, containerRight, left, right int32,
) (int32, int32) {
	return containerLeft + containerRight - right,
		containerLeft + containerRight - left
}
