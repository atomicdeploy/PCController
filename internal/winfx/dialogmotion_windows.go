//go:build windows

package winfx

import (
	"time"

	"golang.org/x/sys/windows"
)

const dialogMotionTimerInterval = 16 * time.Millisecond

var (
	dialogMotionUser32       = windows.NewLazySystemDLL("user32.dll")
	dialogMotionSetTimer     = dialogMotionUser32.NewProc("SetTimer")
	dialogMotionKillTimer    = dialogMotionUser32.NewProc("KillTimer")
	dialogMotionUpdateWindow = dialogMotionUser32.NewProc("UpdateWindow")
)

// dialogEase matches the smoothstep curve used by the main native window.
// Keeping the curve shared makes settings controls feel like part of the same
// surface even though they are child HWNDs rather than Walk canvas elements.
func dialogEase(value float64) float64 {
	value = dialogClamp(value)
	return value * value * (3 - 2*value)
}

func dialogClamp(value float64) float64 {
	switch {
	case value <= 0:
		return 0
	case value >= 1:
		return 1
	default:
		return value
	}
}

// dialogMixColor interpolates Win32 COLORREF values channel by channel.
func dialogMixColor(first, second uint32, amount float64) uint32 {
	amount = dialogClamp(amount)
	mix := func(a, b byte) byte {
		return byte(float64(a)*(1-amount) + float64(b)*amount + 0.5)
	}
	return uint32(mix(byte(first), byte(second))) |
		uint32(mix(byte(first>>8), byte(second>>8)))<<8 |
		uint32(mix(byte(first>>16), byte(second>>16)))<<16
}

// advanceDialogMotion moves one normalized value toward its target using real
// elapsed time. It returns whether another frame is required.
func advanceDialogMotion(
	value, target float64,
	elapsed time.Duration,
	durationMS int,
) (float64, bool) {
	target = dialogClamp(target)
	value = dialogClamp(value)
	if value == target {
		return target, false
	}
	if durationMS <= 0 {
		return target, false
	}
	if elapsed <= 0 {
		elapsed = dialogMotionTimerInterval
	}
	step := float64(elapsed) / float64(time.Duration(durationMS)*time.Millisecond)
	if step >= 1 {
		return target, false
	}
	if value < target {
		value += step
		if value >= target {
			return target, false
		}
	} else {
		value -= step
		if value <= target {
			return target, false
		}
	}
	return dialogClamp(value), true
}

// presentDialogMotionFrame turns an invalidation into a frame in the same
// timer dispatch. WM_PAINT is ordinarily a low-priority message; leaving it
// queued behind mouse and layout traffic made several logical animation ticks
// collapse into one visible jump. UpdateWindow paints only the already-dirty
// child synchronously and does not erase its double-buffered background.
func presentDialogMotionFrame(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = dialogMotionUpdateWindow.Call(hwnd)
}

func startDialogMotionTimer(hwnd, timerID uintptr) bool {
	if hwnd == 0 || timerID == 0 {
		return false
	}
	result, _, _ := dialogMotionSetTimer.Call(
		hwnd,
		timerID,
		uintptr(dialogMotionTimerInterval/time.Millisecond),
		0,
	)
	return result != 0
}

func stopDialogMotionTimer(hwnd, timerID uintptr) {
	if hwnd == 0 || timerID == 0 {
		return
	}
	_, _, _ = dialogMotionKillTimer.Call(hwnd, timerID)
}
