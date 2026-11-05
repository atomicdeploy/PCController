//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const windowOpacityLWAAlpha = 0x00000002

var windowOpacitySetLayeredWindowAttributes = overlayUser32.
	NewProc("SetLayeredWindowAttributes")

// UniformWindowOpacitySession temporarily equips an ordinary HWND with
// WS_EX_LAYERED so SetLayeredWindowAttributes can fade the complete native
// surface. It intentionally rejects windows that were already layered:
// applying this API to a per-pixel UpdateLayeredWindow host poisons later
// UpdateLayeredWindow calls until the style is reset.
type UniformWindowOpacitySession struct {
	hwnd         uintptr
	addedLayered bool
	active       bool
}

func (session *UniformWindowOpacitySession) Active() bool {
	return session != nil && session.active
}

func BeginUniformWindowOpacity(
	hwnd uintptr,
	opacity byte,
) (*UniformWindowOpacitySession, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return nil, ErrInvalidWindow
	}
	style, err := overlayWindowExStyle(hwnd)
	if err != nil {
		return nil, err
	}
	if style&overlayExLayered != 0 {
		return nil, fmt.Errorf(
			"uniform window opacity requires a non-layered window",
		)
	}
	if err := changeOverlayExStyle(hwnd, overlayExLayered, 0); err != nil {
		return nil, err
	}
	session := &UniformWindowOpacitySession{
		hwnd:         hwnd,
		addedLayered: true,
		active:       true,
	}
	if err := session.Set(opacity); err != nil {
		rollbackErr := changeOverlayExStyle(hwnd, 0, overlayExLayered)
		if rollbackErr == nil {
			session.active = false
			return nil, err
		}
		return session, errors.Join(
			err,
			fmt.Errorf(
				"rollback temporary WS_EX_LAYERED: %w",
				rollbackErr,
			),
		)
	}
	return session, nil
}

func (session *UniformWindowOpacitySession) Set(opacity byte) error {
	if session == nil || !session.active || session.hwnd == 0 {
		return fmt.Errorf("uniform window opacity session is inactive")
	}
	ok, _, callErr := windowOpacitySetLayeredWindowAttributes.Call(
		session.hwnd,
		0,
		uintptr(opacity),
		windowOpacityLWAAlpha,
	)
	if ok == 0 {
		return boolCallError("SetLayeredWindowAttributes", callErr)
	}
	return nil
}

// Restore returns the HWND to its original non-layered rendering path.
// Restore is idempotent so shutdown cleanup can safely call it after either a
// completed transition or an error.
func (session *UniformWindowOpacitySession) Restore() error {
	if session == nil || !session.active {
		return nil
	}
	opacityErr := session.Set(255)
	var styleErr error
	if session.addedLayered {
		styleErr = changeOverlayExStyle(
			session.hwnd,
			0,
			overlayExLayered,
		)
	}
	if styleErr != nil {
		return errors.Join(opacityErr, styleErr)
	}
	session.active = false
	session.hwnd = 0
	return opacityErr
}

func overlayWindowExStyle(hwnd uintptr) (uint32, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return 0, ErrInvalidWindow
	}
	proc := overlayGetWindowLongPtrW
	if unsafe.Sizeof(uintptr(0)) == 4 {
		proc = overlayGetWindowLongW
	}
	if err := proc.Find(); err != nil {
		return 0, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	index := int32(overlayGWLExStyle)
	win.SetLastError(0)
	style, _, callErr := proc.Call(hwnd, uintptr(uint32(index)))
	if style == 0 && callErr != windows.ERROR_SUCCESS {
		return 0, fmt.Errorf("GetWindowLong(GWL_EXSTYLE): %w", callErr)
	}
	return uint32(style), nil
}
