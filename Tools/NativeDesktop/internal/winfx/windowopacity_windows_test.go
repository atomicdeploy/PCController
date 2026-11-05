//go:build windows

package winfx

import (
	"runtime"
	"testing"

	"github.com/lxn/win"
)

func TestExtendedStyleIgnoresStaleLastError(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a native Windows fixture")
	}
	hwnd, stop := startHitTestWindow(t, 32, 32, 48, 48, false, nil)
	defer stop()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !win.SetWindowPos(hwnd, win.HWND_NOTOPMOST, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE) {
		t.Fatal("remove fixture topmost state")
	}
	if err := changeOverlayExStyle(uintptr(hwnd), 0, ^uint32(0)); err != nil {
		t.Fatal(err)
	}
	win.SetLastError(87)
	if style, err := overlayWindowExStyle(uintptr(hwnd)); err != nil || style != 0 {
		t.Fatalf("valid zero extended style: style=%#x err=%v", style, err)
	}
	win.SetLastError(87)
	if err := changeOverlayExStyle(uintptr(hwnd), overlayExTransparent, 0); err != nil {
		t.Fatalf("successful zero-previous-style update misreported: %v", err)
	}
	style, err := overlayWindowExStyle(uintptr(hwnd))
	if err != nil || style&overlayExTransparent == 0 {
		t.Fatalf("style update not applied: style=%#x err=%v", style, err)
	}
}

func TestUniformWindowOpacitySessionRestoresLayeredStyle(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a native Windows fixture")
	}
	hwnd, stop := startHitTestWindow(t, 32, 32, 48, 48, false, nil)
	defer stop()

	before, err := overlayWindowExStyle(uintptr(hwnd))
	if err != nil {
		t.Fatalf("read initial extended style: %v", err)
	}
	if before&overlayExLayered != 0 {
		t.Fatal("ordinary fixture unexpectedly starts layered")
	}

	session, err := BeginUniformWindowOpacity(uintptr(hwnd), 96)
	if err != nil {
		t.Fatalf("begin uniform opacity: %v", err)
	}
	during, err := overlayWindowExStyle(uintptr(hwnd))
	if err != nil {
		t.Fatalf("read temporary extended style: %v", err)
	}
	if during&overlayExLayered == 0 {
		t.Fatal("uniform opacity did not add WS_EX_LAYERED")
	}
	if err := session.Restore(); err != nil {
		t.Fatalf("restore uniform opacity: %v", err)
	}
	after, err := overlayWindowExStyle(uintptr(hwnd))
	if err != nil {
		t.Fatalf("read restored extended style: %v", err)
	}
	if after != before {
		t.Fatalf("restored style = %#x, want %#x", after, before)
	}
	if err := session.Restore(); err != nil {
		t.Fatalf("second restore should be idempotent: %v", err)
	}
}

func TestUniformWindowOpacityRejectsPerPixelLayeredWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("requires a native Windows fixture")
	}
	hwnd, stop := startHitTestWindow(t, 32, 32, 48, 48, false, nil)
	defer stop()

	if err := SetPerPixelOverlay(uintptr(hwnd), true); err != nil {
		t.Fatalf("make fixture per-pixel layered: %v", err)
	}
	defer SetPerPixelOverlay(uintptr(hwnd), false) //nolint:errcheck

	if session, err := BeginUniformWindowOpacity(uintptr(hwnd), 96); err == nil {
		if session != nil {
			_ = session.Restore()
		}
		t.Fatal("uniform opacity accepted an existing layered window")
	}
}
