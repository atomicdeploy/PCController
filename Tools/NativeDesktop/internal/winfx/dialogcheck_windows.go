//go:build windows

package winfx

import (
	"github.com/lxn/win"
	"unsafe"
)

const dialogCheckSubclassID = 0x50434348 // PCCH

func isDialogCheckStyle(style uint32) bool {
	switch style & dialogButtonBSTypeMask {
	case 2, 3, 5, 6:
		return true
	}
	return false
}

// Keep the native checkbox HWND, input, focus rectangle, label and accessibility
// state. Only replace its glyph, which otherwise inherits the OS accent rather
// than the application's design tokens. High contrast retains the native path.
func setDialogCheckTheme(hwnd uintptr, dark, enabled bool) {
	_ = RemoveWindowSubclass(hwnd, dialogCheckSubclassID)
	if !enabled {
		return
	}
	_ = SetWindowSubclass(hwnd, dialogCheckSubclassID, func(hwnd, message, wParam, lParam uintptr) (uintptr, bool) {
		switch uint32(message) {
		case win.WM_PAINT, win.WM_PRINTCLIENT:
			result := callDefSubclassProc(hwnd, message, wParam, lParam)
			dc := win.HDC(wParam)
			if uint32(message) == win.WM_PAINT {
				dc = win.GetDC(win.HWND(hwnd))
				defer win.ReleaseDC(win.HWND(hwnd), dc)
			}
			paintDialogCheckGlyph(hwnd, dc, dark)
			return result, true
		case win.BM_SETCHECK, win.BM_SETSTATE:
			result := callDefSubclassProc(hwnd, message, wParam, lParam)
			win.InvalidateRect(win.HWND(hwnd), nil, false)
			return result, true
		}
		return 0, false
	})
	win.InvalidateRect(win.HWND(hwnd), nil, false)
}

func paintDialogCheckGlyph(hwnd uintptr, dc win.HDC, dark bool) {
	if dc == 0 {
		return
	}
	var client win.RECT
	if !win.GetClientRect(win.HWND(hwnd), &client) {
		return
	}
	dpi := dialogButtonDPI(hwnd)
	size := scaleDialogButtonMetric(13, dpi)
	class := []uint16{'B', 'u', 't', 't', 'o', 'n', 0}
	theme := win.OpenThemeData(win.HWND(hwnd), &class[0])
	if theme != 0 {
		var nativeSize win.SIZE
		if win.GetThemePartSize(theme, dc, 3 /* BP_CHECKBOX */, 1, nil, win.TS_TRUE, &nativeSize) == 0 && nativeSize.CX > 0 {
			size = nativeSize.CX
		}
		win.CloseThemeData(theme)
	}
	x := int32(0)
	if dialogWindowStyle(hwnd)&0x20 != 0 {
		x = client.Right - size
	} // BS_LEFTTEXT
	y := (client.Bottom - size) / 2
	rect := dialogButtonRect{Left: x, Top: y, Right: x + size, Bottom: y + size}
	palette := dialogButtonColors(dark)
	fillDialogButtonParent(uintptr(dc), hwnd, rect, palette.Window, nil)
	checked := win.SendMessage(win.HWND(hwnd), win.BM_GETCHECK, 0, 0)
	state := win.SendMessage(win.HWND(hwnd), win.BM_GETSTATE, 0, 0)
	enabled := win.IsWindowEnabled(win.HWND(hwnd))
	fill, border, mark := palette.NormalFill, palette.NormalBorder, palette.PrimaryText
	if checked != 0 {
		fill, border = palette.PrimaryFill, palette.PrimaryFill
	}
	if state&dialogButtonBSTPushed != 0 {
		fill = dialogMixColor(fill, palette.Window, 0.2)
	}
	if !enabled {
		fill, border, mark = palette.DisabledFill, palette.DisabledBorder, palette.DisabledText
	}
	drawDialogButtonRoundRect(uintptr(dc), rect, fill, border, scaleDialogButtonMetric(3, dpi), max(1, scaleDialogButtonMetric(1, dpi)), nil)
	if checked == 0 {
		return
	}
	pen, releasePen := acquireDialogButtonPen(uintptr(dc), nil, mark, max(1, scaleDialogButtonMetric(2, dpi)))
	if pen == 0 {
		return
	}
	old := win.SelectObject(dc, win.HGDIOBJ(pen))
	defer func() { win.SelectObject(dc, old); releasePen() }()
	points := []win.POINT{{X: x + size*3/13, Y: y + size*6/13}, {X: x + size*5/13, Y: y + size*9/13}, {X: x + size*10/13, Y: y + size*3/13}}
	if checked == 2 {
		points = []win.POINT{{X: x + size*3/13, Y: y + size/2}, {X: x + size*10/13, Y: y + size/2}}
	}
	win.Polyline(dc, unsafe.Pointer(&points[0]), int32(len(points)))
}
