//go:build windows

package winfx

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	cornersUser32        = windows.NewLazySystemDLL("user32.dll")
	cornersGDI32         = windows.NewLazySystemDLL("gdi32.dll")
	cornersGetWindowRect = cornersUser32.NewProc("GetWindowRect")
	cornersSetWindowRgn  = cornersUser32.NewProc("SetWindowRgn")
	createRoundRectRgn   = cornersGDI32.NewProc("CreateRoundRectRgn")
	cornersDeleteObject  = cornersGDI32.NewProc("DeleteObject")
)

// ApplyRoundedRegion gives the top-level window deterministic rounded
// geometry. DWM's corner preference remains enabled for its native shadow and
// antialiasing, while this region is the compatibility backstop for builds or
// window-style combinations where that preference is only advisory.
func ApplyRoundedRegion(hwnd uintptr, radiusPixels int) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	if radiusPixels < 1 {
		applied, _, callErr := cornersSetWindowRgn.Call(hwnd, 0, 1)
		if applied == 0 {
			return boolCallError("SetWindowRgn(clear)", callErr)
		}
		return nil
	}

	var bounds Rect
	ok, _, callErr := cornersGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&bounds)),
	)
	if ok == 0 {
		return boolCallError("GetWindowRect", callErr)
	}
	width, height := bounds.Width(), bounds.Height()
	return applyRoundedRegionSize(hwnd, radiusPixels, width, height)
}

func applyRoundedRegionSize(
	hwnd uintptr,
	radiusPixels int,
	width int32,
	height int32,
) error {
	if width < 1 || height < 1 {
		return fmt.Errorf("rounded window has invalid size %dx%d", width, height)
	}

	diameter := int32(radiusPixels * 2)
	region, _, callErr := createRoundRectRgn.Call(
		0,
		0,
		uintptr(width+1),
		uintptr(height+1),
		uintptr(diameter),
		uintptr(diameter),
	)
	if region == 0 {
		return boolCallError("CreateRoundRectRgn", callErr)
	}

	// On success Windows owns the region handle. On failure it remains ours.
	applied, _, callErr := cornersSetWindowRgn.Call(hwnd, region, 1)
	if applied == 0 {
		_, _, _ = cornersDeleteObject.Call(region)
		return boolCallError("SetWindowRgn", callErr)
	}
	return nil
}

// SetDWMWindowCornerPreference changes only DWM's native corner policy.
// A per-pixel, alpha-shaped HWND must use DONOTROUND: leaving ROUND active
// makes Windows 11 retain a full-window compositor shadow even when the
// ordinary frame and non-client rendering are disabled.
//
// Reading first is important for layered windows. DwmSetWindowAttribute can
// invalidate the current UpdateLayeredWindow visual even when the requested
// value is unchanged, which otherwise turns routine composition refreshes into
// a one-frame disappearance.
func SetDWMWindowCornerPreference(hwnd uintptr, rounded bool) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	if !CurrentVersion().IsWindows11() {
		return nil
	}
	preference := int32(1) // DWMWCP_DONOTROUND
	if rounded {
		preference = 2 // DWMWCP_ROUND
	}
	current, err := getDWMInt32(hwnd, dwmwaWindowCornerPref)
	if err == nil && current == preference {
		return nil
	}
	return setDWMInt32(hwnd, dwmwaWindowCornerPref, preference)
}

// ApplyWindowCorners mirrors GlassDWM's Borders experiment. Windows 11 uses
// the same DWM preference as the C# project. A physical HRGN is reserved for
// older Windows versions where the DWM attribute does not exist; combining
// both mechanisms on Windows 11 creates competing contours.
func ApplyWindowCorners(hwnd uintptr, enabled bool, radiusPixels int) error {
	_ = SetDWMWindowCornerPreference(hwnd, enabled)
	if CurrentVersion().IsWindows11() {
		// Clear a region left by an earlier build or a runtime mode switch.
		return ApplyRoundedRegion(hwnd, 0)
	}
	if !enabled {
		return ApplyRoundedRegion(hwnd, 0)
	}
	return ApplyRoundedRegion(hwnd, radiusPixels)
}

// ApplyWindowCornersForSize is the WM_SIZE-safe variant. During a live native
// resize GetWindowRect may still expose the previous committed rectangle while
// WM_SIZE already carries the new client dimensions. Because PCControllerDesktop removes
// the non-client inset, those dimensions are the exact region size.
func ApplyWindowCornersForSize(
	hwnd uintptr,
	enabled bool,
	radiusPixels int,
	width int,
	height int,
) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	_ = SetDWMWindowCornerPreference(hwnd, enabled)
	if CurrentVersion().IsWindows11() {
		return ApplyRoundedRegion(hwnd, 0)
	}
	if !enabled {
		return ApplyRoundedRegion(hwnd, 0)
	}
	return applyRoundedRegionSize(
		hwnd,
		radiusPixels,
		int32(width),
		int32(height),
	)
}
