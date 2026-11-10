//go:build windows

package ui

import (
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

func captionButtonHit(hit int) bool {
	return hit == win.HTMINBUTTON || hit == win.HTMAXBUTTON || hit == win.HTCLOSE
}

func (layout surfaceLayout) captionHit(point walk.Point) int {
	for _, item := range []struct {
		rect walk.Rectangle
		hit  int
	}{{layout.titleMinimize, win.HTMINBUTTON}, {layout.titleZoom, win.HTMAXBUTTON},
		{layout.titleClose, win.HTCLOSE}, {layout.titleDrag, win.HTCAPTION}} {
		if pointInRect(point, item.rect) {
			return item.hit
		}
	}
	return win.HTCLIENT
}

func (s *Shell) captionClientPoint(lParam uintptr) (walk.Point, bool) {
	if s.surface == nil {
		return walk.Point{}, false
	}
	point := win.POINT{X: int32(int16(uint16(lParam))), Y: int32(int16(uint16(lParam >> 16)))}
	if !win.ScreenToClient(s.surface.Handle(), &point) {
		return walk.Point{}, false
	}
	return walk.Point{X: int(point.X), Y: int(point.Y)}, true
}

func (s *Shell) customCaptionHit(lParam uintptr) int {
	ui := s.Config().UI
	if effectiveTitleBarMode(ui) == config.TitleBarNative || !windowResizeBorderEnabled(ui) {
		return win.HTCLIENT
	}
	if point, ok := s.captionClientPoint(lParam); ok {
		return s.layout.captionHit(point)
	}
	return win.HTCLIENT
}

// A maximized overlapped HWND includes an invisible resize frame outside the
// monitor work area. Reclaim its client without putting custom controls there.
// NCCALCSIZE_PARAMS and the wParam=0 RECT form both start with the proposed RECT.
func (s *Shell) constrainMaximizedClient(hwnd win.HWND, lParam uintptr) {
	if lParam == 0 || !win.IsZoomed(hwnd) {
		return
	}
	var rect win.RECT
	uiCopyMemory.Call(uintptr(unsafe.Pointer(&rect)), lParam, unsafe.Sizeof(rect))
	// During cross-monitor maximize the HWND may still identify its old monitor.
	work, err := winfx.MonitorWorkAreaForRect(winfx.Rect{Left: rect.Left, Top: rect.Top, Right: rect.Right, Bottom: rect.Bottom})
	if err != nil {
		return
	}
	rect.Left, rect.Top = max(rect.Left, work.Left), max(rect.Top, work.Top)
	rect.Right, rect.Bottom = min(rect.Right, work.Right), min(rect.Bottom, work.Bottom)
	if rect.Right > rect.Left && rect.Bottom > rect.Top {
		uiCopyMemory.Call(lParam, uintptr(unsafe.Pointer(&rect)), unsafe.Sizeof(rect))
	}
}
