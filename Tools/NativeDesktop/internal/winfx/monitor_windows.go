//go:build windows

package winfx

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	monitorUser32       = windows.NewLazySystemDLL("user32.dll")
	monitorFromRectProc = monitorUser32.NewProc("MonitorFromRect")
	monitorSHCore       = windows.NewLazySystemDLL("shcore.dll")
	monitorGetDPIProc   = monitorSHCore.NewProc("GetDpiForMonitor")
)

// MonitorMetrics describes the work area and effective DPI of one display.
type MonitorMetrics struct {
	WorkArea Rect
	DPI      uint32
}

// MonitorWorkAreaForRect returns the work area of the monitor containing the
// largest part of rect, or the nearest monitor when rect is off-screen. Rect
// and the returned work area use virtual-screen coordinates.
func MonitorWorkAreaForRect(rect Rect) (Rect, error) {
	metrics, err := MonitorMetricsForRect(rect, 96)
	return metrics.WorkArea, err
}

// MonitorMetricsForRect resolves both geometry and effective DPI from the
// proposed rectangle rather than from a window that may still belong to the
// previous monitor during WM_MOVING. fallbackDPI is used on systems predating
// GetDpiForMonitor and whenever the monitor DPI query fails.
func MonitorMetricsForRect(rect Rect, fallbackDPI uint32) (MonitorMetrics, error) {
	if !rect.Valid() {
		return MonitorMetrics{}, errors.New("winfx: monitor target rectangle is invalid")
	}
	if err := monitorFromRectProc.Find(); err != nil {
		return MonitorMetrics{}, err
	}
	native := win.RECT{
		Left:   rect.Left,
		Top:    rect.Top,
		Right:  rect.Right,
		Bottom: rect.Bottom,
	}
	monitor, _, callErr := monitorFromRectProc.Call(
		uintptr(unsafe.Pointer(&native)),
		win.MONITOR_DEFAULTTONEAREST,
	)
	runtime.KeepAlive(&native)
	if monitor == 0 {
		return MonitorMetrics{}, boolCallError("MonitorFromRect", callErr)
	}
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if !win.GetMonitorInfo(win.HMONITOR(monitor), &info) {
		return MonitorMetrics{}, errors.New("winfx: GetMonitorInfo failed")
	}
	return MonitorMetrics{
		WorkArea: Rect{
			Left:   info.RcWork.Left,
			Top:    info.RcWork.Top,
			Right:  info.RcWork.Right,
			Bottom: info.RcWork.Bottom,
		},
		DPI: effectiveMonitorDPI(win.HMONITOR(monitor), fallbackDPI),
	}, nil
}

func effectiveMonitorDPI(monitor win.HMONITOR, fallback uint32) uint32 {
	if fallback < 48 || fallback > 768 {
		fallback = 96
	}
	if monitor == 0 || monitorGetDPIProc.Find() != nil {
		return fallback
	}
	var x, y uint32
	result, _, _ := monitorGetDPIProc.Call(
		uintptr(monitor),
		0, // MDT_EFFECTIVE_DPI
		uintptr(unsafe.Pointer(&x)),
		uintptr(unsafe.Pointer(&y)),
	)
	runtime.KeepAlive(&x)
	runtime.KeepAlive(&y)
	if int32(result) < 0 || x < 48 || x > 768 {
		return fallback
	}
	return x
}
