//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	gwlStyle = -16

	WindowBorderAuto = "auto"
	WindowBorderNone = "none"

	wsBorder       = 0x00800000
	wsDlgFrame     = 0x00400000
	wsCaption      = 0x00C00000
	wsThickFrame   = 0x00040000
	wsMinimizeBox  = 0x00020000
	wsMaximizeBox  = 0x00010000
	wsPopup        = 0x80000000
	wsSysMenu      = 0x00080000
	wsClipChildren = 0x02000000
	wsClipSiblings = 0x04000000

	wsExWindowEdge = 0x00000100

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	wmNCLButtonDown = 0x00A1
	htCaption       = 2
)

var (
	chromeUser32            = windows.NewLazySystemDLL("user32.dll")
	chromeGetWindowLongPtrW = chromeUser32.NewProc("GetWindowLongPtrW")
	chromeGetWindowLongW    = chromeUser32.NewProc("GetWindowLongW")
	chromeSetWindowLongPtrW = chromeUser32.NewProc("SetWindowLongPtrW")
	chromeSetWindowLongW    = chromeUser32.NewProc("SetWindowLongW")
	chromeSetWindowPos      = chromeUser32.NewProc("SetWindowPos")
	chromeReleaseCapture    = chromeUser32.NewProc("ReleaseCapture")
	chromeSendMessageW      = chromeUser32.NewProc("SendMessageW")
)

// MakeBorderless removes the ordinary caption while retaining Windows'
// invisible resize frame and an ordinary top-level window. GlassDWM's final
// HWND also has no system menu or WS_EX_WINDOWEDGE and clips its descendants.
// Retaining WS_THICKFRAME is the one intentional difference: it provides the
// user-requested native resize loop.
func MakeBorderless(hwnd uintptr) error {
	return ConfigureWindowChrome(hwnd, false, WindowBorderAuto)
}

// SetNativeTitleBar switches between the ordinary Windows caption and the
// captionless frame used by custom/none chrome modes. Both variants retain
// WS_THICKFRAME so resizing, system move loops, accessibility, and monitor snap
// behavior remain native.
func SetNativeTitleBar(hwnd uintptr, enabled bool) error {
	return ConfigureWindowChrome(hwnd, enabled, WindowBorderAuto)
}

// ConfigureWindowChrome applies the title-bar and outer-frame policy as one
// atomic style transition. Auto preserves the ordinary native resize frame.
// None removes every sizing/classic edge from a captionless window, allowing a
// per-pixel transparent host to remain genuinely frameless. A native caption
// necessarily owns a non-client frame and therefore cannot use none.
func ConfigureWindowChrome(
	hwnd uintptr,
	nativeTitleBar bool,
	borderMode string,
) error {
	mode, err := normalizeWindowBorderMode(borderMode)
	if err != nil {
		return err
	}
	if nativeTitleBar && mode == WindowBorderNone {
		return fmt.Errorf(
			"native title bar requires %q window border mode",
			WindowBorderAuto,
		)
	}
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	style, err := getWindowStyle(hwnd)
	if err != nil {
		return err
	}
	style, err = windowChromeStyle(style, nativeTitleBar, mode)
	if err != nil {
		return err
	}
	if err := setWindowStyle(hwnd, style); err != nil {
		return err
	}
	// Establish the target border color before asking USER32/DWM to rebuild
	// the frame. ReassertWindowChrome repeats it after the rebuild because
	// DefWindowProc is allowed to recreate the non-client visual.
	_ = ReassertWindowChrome(hwnd, nativeTitleBar)
	ok, _, callErr := chromeSetWindowPos.Call(
		hwnd,
		0,
		0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged,
	)
	if ok == 0 {
		return boolCallError("SetWindowPos(window chrome)", callErr)
	}
	return ReassertWindowChrome(hwnd, nativeTitleBar)
}

func titleBarWindowStyle(style uint32, native bool) uint32 {
	result, _ := windowChromeStyle(style, native, WindowBorderAuto)
	return result
}

func windowChromeStyle(
	style uint32,
	nativeTitleBar bool,
	borderMode string,
) (uint32, error) {
	mode, err := normalizeWindowBorderMode(borderMode)
	if err != nil {
		return style, err
	}
	if nativeTitleBar && mode == WindowBorderNone {
		return style, fmt.Errorf(
			"native title bar requires %q window border mode",
			WindowBorderAuto,
		)
	}
	if nativeTitleBar {
		style &^= wsPopup
		style |= wsCaption |
			wsSysMenu |
			wsMinimizeBox |
			wsMaximizeBox |
			wsThickFrame |
			wsClipChildren |
			wsClipSiblings
		return style, nil
	}
	style &^= wsCaption |
		wsBorder |
		wsDlgFrame |
		wsMinimizeBox |
		wsMaximizeBox |
		wsPopup |
		wsSysMenu |
		wsThickFrame
	if mode == WindowBorderAuto {
		style |= wsThickFrame
	}
	style |= wsClipChildren | wsClipSiblings
	return style, nil
}

func normalizeWindowBorderMode(value string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", WindowBorderAuto:
		return WindowBorderAuto, nil
	case WindowBorderNone:
		return WindowBorderNone, nil
	default:
		return "", fmt.Errorf(
			"unknown window border mode %q (want %q or %q)",
			value,
			WindowBorderAuto,
			WindowBorderNone,
		)
	}
}

// ClearWindowEdge removes the classic raised outer edge without forcing
// another non-client recalculation that could immediately add it back.
func ClearWindowEdge(hwnd uintptr) error {
	return changeOverlayExStyle(hwnd, 0, wsExWindowEdge)
}

// ReassertWindowChrome restores the compositor-owned outer-border policy
// without changing GWL_STYLE or forcing another frame recalculation.
//
// Captionless resizable windows intentionally retain WS_THICKFRAME. USER32
// can synthesize WS_EX_WINDOWEDGE from that style, so callers must ALSO
// suppress captionless WM_NCPAINT and use WM_NCACTIVATE with lParam=-1.
// Clearing the bit alone does not suppress the classic GDI frame. This helper
// separately suppresses Windows 11's DWM-drawn one-pixel outline. DWM may
// recreate that non-client visual when activation, showing, or a frame change
// passes through DefWindowProc. Callers therefore invoke this after those
// transitions, rather than assuming the value written during construction is
// permanent.
func ReassertWindowChrome(hwnd uintptr, nativeTitleBar bool) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	return reassertWindowChrome(
		nativeTitleBar,
		func(color int32) error {
			err := setDWMInt32(hwnd, dwmwaBorderColor, color)
			// DWMWA_BORDER_COLOR is a Windows 11 attribute. On older supported
			// versions the absent attribute must not prevent captionless chrome
			// from using the classic edge fallback.
			if err != nil && !CurrentVersion().IsWindows11() {
				return nil
			}
			return err
		},
		func() error { return ClearWindowEdge(hwnd) },
	)
}

func reassertWindowChrome(
	nativeTitleBar bool,
	setBorderColor func(int32) error,
	clearWindowEdge func() error,
) error {
	borderColor := int32(dwmColorNone)
	if nativeTitleBar {
		// DWMWA_COLOR_DEFAULT restores Windows' active/inactive border policy.
		borderColor = -1
	}
	var failures []error
	if setBorderColor != nil {
		if err := setBorderColor(borderColor); err != nil {
			failures = append(failures, err)
		}
	}
	if !nativeTitleBar && clearWindowEdge != nil {
		if err := clearWindowEdge(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// BeginWindowDrag starts the ordinary Windows caption drag loop for a
// borderless window. This deliberately uses the system move loop so WM_MOVING,
// monitor transitions, keyboard cancellation, and snap behavior keep working.
func BeginWindowDrag(hwnd uintptr) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	_, _, _ = chromeReleaseCapture.Call()
	result, _, callErr := chromeSendMessageW.Call(hwnd, wmNCLButtonDown, htCaption, 0)
	if result == 0 && callErr != windows.ERROR_SUCCESS {
		return boolCallError("SendMessageW(WM_NCLBUTTONDOWN)", callErr)
	}
	return nil
}

func getWindowStyle(hwnd uintptr) (uint32, error) {
	proc := chromeGetWindowLongPtrW
	if uintptrBits() == 32 {
		proc = chromeGetWindowLongW
	}
	index := int32(gwlStyle)
	value, _, callErr := proc.Call(hwnd, uintptr(uint32(index)))
	if value == 0 && callErr != windows.ERROR_SUCCESS {
		return 0, fmt.Errorf("GetWindowLong(GWL_STYLE): %w", callErr)
	}
	return uint32(value), nil
}

func setWindowStyle(hwnd uintptr, style uint32) error {
	proc := chromeSetWindowLongPtrW
	if uintptrBits() == 32 {
		proc = chromeSetWindowLongW
	}
	index := int32(gwlStyle)
	previous, _, callErr := proc.Call(hwnd, uintptr(uint32(index)), uintptr(style))
	if previous == 0 && callErr != windows.ERROR_SUCCESS {
		return fmt.Errorf("SetWindowLong(GWL_STYLE): %w", callErr)
	}
	return nil
}

func uintptrBits() int {
	return 32 << (^uintptr(0) >> 63)
}
