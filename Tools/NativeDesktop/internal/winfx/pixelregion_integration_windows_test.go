//go:build windows

package winfx

import (
	"errors"
	"image"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	hitTestClassOnce sync.Once
	hitTestClassName []uint16
	hitTestClassErr  error
	hitTestCallback  = syscall.NewCallback(func(
		hwnd uintptr,
		message uint32,
		wParam uintptr,
		lParam uintptr,
	) uintptr {
		return uintptr(win.DefWindowProc(
			win.HWND(hwnd),
			message,
			wParam,
			lParam,
		))
	})
)

// TestAlphaRegionPassesHitTestingToAnotherGUIThread exercises the actual
// Windows z-order and hit-test path. HTTRANSPARENT cannot provide this
// guarantee because it only walks windows owned by the same GUI thread.
func TestAlphaRegionPassesHitTestingToAnotherGUIThread(t *testing.T) {
	if testing.Short() {
		t.Skip("requires an interactive Windows desktop")
	}

	const (
		width  = 96
		height = 72
	)
	x := int32(win.GetSystemMetrics(win.SM_XVIRTUALSCREEN) + 24)
	y := int32(win.GetSystemMetrics(win.SM_YVIRTUALSCREEN) + 24)

	surface := image.NewRGBA(image.Rect(0, 0, width, height))
	for localY := 0; localY < height; localY++ {
		for localX := width / 2; localX < width; localX++ {
			offset := surface.PixOffset(localX, localY)
			surface.Pix[offset] = 32
			surface.Pix[offset+1] = 128
			surface.Pix[offset+2] = 224
			surface.Pix[offset+3] = 255
		}
	}

	bottom, stopBottom := startHitTestWindow(
		t, x, y, width, height, false, nil,
	)
	defer stopBottom()
	initialHit := win.WindowFromPoint(win.POINT{
		X: x + width/4,
		Y: y + height/2,
	})
	if root := win.GetAncestor(initialHit, win.GA_ROOT); root != bottom {
		var bounds win.RECT
		win.GetWindowRect(bottom, &bounds)
		hitTestResult := win.SendMessage(
			bottom,
			win.WM_NCHITTEST,
			0,
			uintptr(uint16(x+width/4))|
				uintptr(uint16(y+height/2))<<16,
		)
		t.Skipf(
			"underlying fixture is not hittable: hit %#x root %#x, window %#x at %+v dpi=%d visible=%t enabled=%t WM_NCHITTEST=%d",
			uintptr(initialHit),
			uintptr(root),
			uintptr(bottom),
			bounds,
			win.GetDpiForWindow(bottom),
			win.IsWindowVisible(bottom),
			win.IsWindowEnabled(bottom),
			hitTestResult,
		)
	}
	top, stopTop := startHitTestWindow(
		t, x, y, width, height, true, surface,
	)
	defer stopTop()

	transparentHit := win.WindowFromPoint(win.POINT{
		X: x + width/4,
		Y: y + height/2,
	})
	if root := win.GetAncestor(transparentHit, win.GA_ROOT); root != bottom {
		t.Fatalf(
			"alpha-zero point hit %#x (root %#x), want underlying %#x; top %#x",
			uintptr(transparentHit),
			uintptr(root),
			uintptr(bottom),
			uintptr(top),
		)
	}

	opaqueHit := win.WindowFromPoint(win.POINT{
		X: x + 3*width/4,
		Y: y + height/2,
	})
	if root := win.GetAncestor(opaqueHit, win.GA_ROOT); root != top {
		t.Fatalf(
			"opaque point hit %#x (root %#x), want layered %#x",
			uintptr(opaqueHit),
			uintptr(root),
			uintptr(top),
		)
	}
}

func startHitTestWindow(
	t *testing.T,
	x int32,
	y int32,
	width int32,
	height int32,
	withChild bool,
	surface *image.RGBA,
) (win.HWND, func()) {
	t.Helper()

	type fixtureResult struct {
		hwnd win.HWND
		err  error
	}
	ready := make(chan fixtureResult, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		if err := ensureHitTestWindowClass(); err != nil {
			ready <- fixtureResult{err: err}
			return
		}
		title, _ := windows.UTF16FromString("RayanLamp alpha hit test")
		hwnd := win.CreateWindowEx(
			win.WS_EX_TOOLWINDOW|win.WS_EX_NOACTIVATE|win.WS_EX_TOPMOST,
			&hitTestClassName[0],
			&title[0],
			win.WS_POPUP|win.WS_VISIBLE,
			x,
			y,
			width,
			height,
			0,
			0,
			win.GetModuleHandle(nil),
			nil,
		)
		if hwnd != 0 && withChild {
			childTitle, _ := windows.UTF16FromString("")
			_ = win.CreateWindowEx(
				0,
				&hitTestClassName[0],
				&childTitle[0],
				win.WS_CHILD|win.WS_VISIBLE,
				0,
				0,
				width,
				height,
				hwnd,
				0,
				win.GetModuleHandle(nil),
				nil,
			)
			runtime.KeepAlive(childTitle)
		}
		var fixtureErr error
		if hwnd != 0 && surface != nil {
			if fixtureErr = SetPerPixelOverlay(uintptr(hwnd), true); fixtureErr == nil {
				fixtureErr = UpdatePerPixelOverlay(uintptr(hwnd), surface)
			}
			if fixtureErr == nil {
				_, fixtureErr = ApplyAlphaRegion(uintptr(hwnd), surface, 1)
			}
		}
		if hwnd != 0 && fixtureErr == nil {
			if !win.SetWindowPos(
				hwnd,
				win.HWND_TOPMOST,
				x,
				y,
				width,
				height,
				win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW,
			) {
				fixtureErr = errors.New("SetWindowPos hit-test fixture failed")
			}
			win.ShowWindow(hwnd, win.SW_SHOW)
			win.UpdateWindow(hwnd)
			win.BringWindowToTop(hwnd)
		}
		ready <- fixtureResult{hwnd: hwnd, err: fixtureErr}
		const stopMessage = win.WM_APP + 0x31
		var message win.MSG
		for hwnd != 0 && fixtureErr == nil {
			result := win.GetMessage(&message, 0, 0, 0)
			if result <= 0 || message.Message == stopMessage {
				break
			}
			win.TranslateMessage(&message)
			win.DispatchMessage(&message)
		}
		if hwnd != 0 {
			win.DestroyWindow(hwnd)
		}
		runtime.KeepAlive(title)
	}()

	result := <-ready
	if result.hwnd == 0 || result.err != nil {
		<-done
		if result.err != nil {
			t.Fatalf("prepare layered hit-test fixture: %v", result.err)
		}
		t.Fatal("CreateWindowEx hit-test fixture failed")
	}
	return result.hwnd, func() {
		const stopMessage = win.WM_APP + 0x31
		win.PostMessage(result.hwnd, stopMessage, 0, 0)
		<-done
	}
}

func ensureHitTestWindowClass() error {
	hitTestClassOnce.Do(func() {
		hitTestClassName, _ = windows.UTF16FromString(
			"RayanLamp.PixelRegion.IntegrationTest",
		)
		class := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			LpfnWndProc:   hitTestCallback,
			HInstance:     win.GetModuleHandle(nil),
			HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
			LpszClassName: &hitTestClassName[0],
		}
		if win.RegisterClassEx(&class) == 0 {
			hitTestClassErr = errors.New("RegisterClassEx hit-test fixture failed")
		}
	})
	return hitTestClassErr
}
