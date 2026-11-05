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

const (
	dialogGeometrySubclassID = 0x524C4447 // "RLDG"

	dialogGeometryWMEnterSizeMove = 0x0231
	dialogGeometryWMExitSizeMove  = 0x0232
	dialogGeometryWMGetMinMaxInfo = 0x0024
)

var (
	dialogGeometryUser32        = windows.NewLazySystemDLL("user32.dll")
	dialogGeometryGetWindowRect = dialogGeometryUser32.NewProc("GetWindowRect")
	dialogGeometryGetDPI        = dialogGeometryUser32.NewProc("GetDpiForWindow")
	dialogGeometrySetWindowPos  = dialogGeometryUser32.NewProc("SetWindowPos")
	dialogGeometryRtlMoveMemory = windows.NewLazySystemDLL("ntdll.dll").
					NewProc("RtlMoveMemory")
)

// DialogGeometrySession keeps a complex native dialog on Walk's coalescing
// layout path during a live move or resize. Walk's default FormBase switches
// every WM_WINDOWPOSCHANGED to a synchronous full-tree layout after
// WM_ENTERSIZEMOVE. That is useful for small forms, but a Settings hierarchy
// with many child HWNDs blocks the pointer loop for every resize step.
type DialogGeometrySession struct {
	hwnd uintptr
}

// AttachResponsiveDialogGeometry leaves the native window manager in charge
// of the move/resize loop while preventing Walk from entering its synchronous
// per-pixel layout mode. Ordinary WM_WINDOWPOSCHANGED and WM_DPICHANGED
// messages still reach Walk, so its existing asynchronous layout performer
// coalesces stale sizes and commits the final geometry.
func AttachResponsiveDialogGeometry(
	hwnd uintptr,
) (*DialogGeometrySession, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return nil, ErrInvalidWindow
	}
	session := &DialogGeometrySession{hwnd: hwnd}
	if err := SetWindowSubclass(
		hwnd,
		dialogGeometrySubclassID,
		session.windowMessage,
	); err != nil {
		return nil, fmt.Errorf("attach responsive dialog geometry: %w", err)
	}
	return session, nil
}

func (session *DialogGeometrySession) windowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	if session == nil {
		return 0, false
	}
	if uint32(message) == dialogGeometryWMGetMinMaxInfo && lParam != 0 {
		// Walk computes its DPI-scaled content minimum in the original chain.
		// Let it do so first, then cap that minimum to the current monitor's
		// physical work area. Settings places the tab hierarchy in a ScrollView,
		// so a constrained high-DPI display remains usable instead of creating
		// an outer window taller than its desktop.
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		var info win.MINMAXINFO
		_, _, _ = dialogGeometryRtlMoveMemory.Call(
			uintptr(unsafe.Pointer(&info)),
			lParam,
			unsafe.Sizeof(info),
		)
		limitDialogMinimumToWorkArea(hwnd, &info)
		_, _, _ = dialogGeometryRtlMoveMemory.Call(
			lParam,
			uintptr(unsafe.Pointer(&info)),
			unsafe.Sizeof(info),
		)
		runtime.KeepAlive(&info)
		return result, true
	}
	if deferDialogSizingMessage(uint32(message)) {
		// WM_ENTERSIZEMOVE/WM_EXITSIZEMOVE are advisory notifications. The
		// system move loop is already active and does not require default
		// processing. Consuming only these two messages keeps Walk out of its
		// synchronous sizing branch without replacing native dragging.
		return 0, true
	}
	if uint32(message) == wmNCDestroy {
		session.hwnd = 0
	}
	return 0, false
}

func limitDialogMinimumToWorkArea(hwnd uintptr, info *win.MINMAXINFO) {
	if hwnd == 0 || info == nil {
		return
	}
	monitor := win.MonitorFromWindow(
		win.HWND(hwnd),
		win.MONITOR_DEFAULTTONEAREST,
	)
	if monitor == 0 {
		return
	}
	monitorInfo := win.MONITORINFO{
		CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{})),
	}
	if !win.GetMonitorInfo(monitor, &monitorInfo) {
		return
	}
	workWidth := monitorInfo.RcWork.Right - monitorInfo.RcWork.Left
	workHeight := monitorInfo.RcWork.Bottom - monitorInfo.RcWork.Top
	info.PtMinTrackSize.X, info.PtMinTrackSize.Y =
		clampDialogMinimumTrackSize(
			info.PtMinTrackSize.X,
			info.PtMinTrackSize.Y,
			workWidth,
			workHeight,
		)
}

func clampDialogMinimumTrackSize(
	width, height, workWidth, workHeight int32,
) (int32, int32) {
	if workWidth > 0 && width > workWidth {
		width = workWidth
	}
	if workHeight > 0 && height > workHeight {
		height = workHeight
	}
	return maxDialogGeometryDimension(width),
		maxDialogGeometryDimension(height)
}

func maxDialogGeometryDimension(value int32) int32 {
	if value < 1 {
		return 1
	}
	return value
}

func deferDialogSizingMessage(message uint32) bool {
	return message == dialogGeometryWMEnterSizeMove ||
		message == dialogGeometryWMExitSizeMove
}

// Dispose removes the dialog geometry hook. It is safe to call after the HWND
// has already received WM_NCDESTROY.
func (session *DialogGeometrySession) Dispose() {
	if session == nil || session.hwnd == 0 {
		return
	}
	_ = RemoveWindowSubclass(session.hwnd, dialogGeometrySubclassID)
	session.hwnd = 0
}

// PrepareOwnedDialogOnOwnerMonitor moves a still-hidden owned dialog onto its
// owner's monitor using dimensions already scaled for that target DPI.
//
// Walk creates Dialog HWNDs at CW_USEDEFAULT and builds their layout before it
// centers them over the owner. When CW_USEDEFAULT chooses a monitor with a
// different DPI, the outer MoveWindow retains its old-DPI size after the
// re-entrant WM_DPICHANGED handler returns. The window is then smaller or
// larger than its new minimum track size, and the first resize jumps badly.
// Seeding the hidden HWND with target-DPI bounds makes the later Walk Show
// pass, control layout, and resize constraints agree from the first frame.
func PrepareOwnedDialogOnOwnerMonitor(
	hwnd, owner uintptr,
	widthDIP, heightDIP int,
) error {
	return PrepareOwnedDialogPlacement(
		hwnd,
		owner,
		DialogPlacement{
			X:      -1,
			Y:      -1,
			Width:  widthDIP,
			Height: heightDIP,
		},
	)
}

// DialogPlacement stores a top-level dialog position in virtual-screen
// pixels and its dimensions in 96-DPI logical pixels. Keeping the size
// logical prevents a window saved on a 200% display from reopening twice as
// large after that monitor changes to 100%.
type DialogPlacement struct {
	X      int
	Y      int
	Width  int
	Height int
}

// PrepareOwnedDialogPlacement commits a hidden owned dialog's first-show
// rectangle. A placement with both X and Y equal to -1 is centered on the owner;
// otherwise it is restored to its previous monitor and rescued wholly into
// that monitor's current work area.
func PrepareOwnedDialogPlacement(
	hwnd, owner uintptr,
	placement DialogPlacement,
) error {
	if hwnd == 0 || !isWindow(hwnd) || (owner != 0 && !isWindow(owner)) {
		return ErrInvalidWindow
	}
	if owner == 0 {
		// SDK callers can open Settings without a parent. Use the hidden
		// dialog's initial monitor as the placement reference in that case.
		owner = hwnd
	}
	if placement.Width <= 0 || placement.Height <= 0 {
		return errors.New("winfx: dialog dimensions must be positive")
	}
	ownerRect, err := dialogGeometryWindowRect(owner)
	if err != nil {
		return fmt.Errorf("read dialog owner bounds: %w", err)
	}
	metrics, err := MonitorMetricsForRect(ownerRect, 96)
	if err != nil {
		return fmt.Errorf("resolve dialog owner monitor: %w", err)
	}
	ownerDPI := dialogGeometryWindowDPI(owner, metrics.DPI)
	var bounds Rect
	if placement.X == -1 && placement.Y == -1 {
		bounds = centeredDialogRect(
			ownerRect,
			metrics.WorkArea,
			placement.Width,
			placement.Height,
			ownerDPI,
		)
	} else {
		hint := Rect{
			Left:   int32(placement.X),
			Top:    int32(placement.Y),
			Right:  int32(placement.X + 1),
			Bottom: int32(placement.Y + 1),
		}
		target, targetErr := MonitorMetricsForRect(hint, ownerDPI)
		if targetErr != nil {
			return fmt.Errorf(
				"resolve saved dialog monitor: %w",
				targetErr,
			)
		}
		bounds = restoredDialogRect(
			placement,
			target.WorkArea,
			target.DPI,
		)
	}
	ok, _, callErr := dialogGeometrySetWindowPos.Call(
		hwnd,
		0,
		uintptr(int(bounds.Left)),
		uintptr(int(bounds.Top)),
		uintptr(int(bounds.Width())),
		uintptr(int(bounds.Height())),
		win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER|win.SWP_NOZORDER,
	)
	if ok == 0 {
		return boolCallError(
			"SetWindowPos(prepare owned dialog monitor)",
			callErr,
		)
	}
	return nil
}

// CaptureDialogPlacement snapshots the restorable dialog bounds before its
// HWND is destroyed. Position remains in virtual-screen pixels; dimensions
// are converted back to 96-DPI logical pixels for scale-independent restore.
func CaptureDialogPlacement(hwnd uintptr) (DialogPlacement, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return DialogPlacement{}, ErrInvalidWindow
	}
	rect, err := restorableDialogWindowRect(hwnd)
	if err != nil {
		return DialogPlacement{}, err
	}
	dpi := dialogGeometryWindowDPI(hwnd, 96)
	return DialogPlacement{
		X:      int(rect.Left),
		Y:      int(rect.Top),
		Width:  unscaleDialogDimension(rect.Width(), dpi),
		Height: unscaleDialogDimension(rect.Height(), dpi),
	}, nil
}

func restorableDialogWindowRect(hwnd uintptr) (Rect, error) {
	// WINDOWPLACEMENT uses workspace coordinates for ordinary top-level
	// windows, whereas SetWindowPos uses screen coordinates. In the normal
	// state GetWindowRect avoids a taskbar-sized drift on every reopen.
	if !win.IsIconic(win.HWND(hwnd)) && !win.IsZoomed(win.HWND(hwnd)) {
		return dialogGeometryWindowRect(hwnd)
	}
	placement := win.WINDOWPLACEMENT{
		Length: uint32(unsafe.Sizeof(win.WINDOWPLACEMENT{})),
	}
	if win.GetWindowPlacement(win.HWND(hwnd), &placement) {
		rect := Rect{
			Left:   placement.RcNormalPosition.Left,
			Top:    placement.RcNormalPosition.Top,
			Right:  placement.RcNormalPosition.Right,
			Bottom: placement.RcNormalPosition.Bottom,
		}
		if rect.Valid() {
			// Minimized/maximized windows need the restored rectangle. Convert
			// workspace coordinates back to screen coordinates (tool windows
			// already use screen coordinates in WINDOWPLACEMENT).
			exStyle, styleErr := overlayWindowExStyle(hwnd)
			if styleErr != nil {
				return Rect{}, styleErr
			}
			monitor := win.MonitorFromWindow(win.HWND(hwnd), win.MONITOR_DEFAULTTONEAREST)
			info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
			if exStyle&win.WS_EX_TOOLWINDOW == 0 && win.GetMonitorInfo(monitor, &info) {
				rect = dialogWorkspaceToScreen(rect, info.RcMonitor, info.RcWork)
			}
			return rect, nil
		}
	}
	return dialogGeometryWindowRect(hwnd)
}

func dialogWorkspaceToScreen(rect Rect, monitor, work win.RECT) Rect {
	dx, dy := work.Left-monitor.Left, work.Top-monitor.Top
	return Rect{Left: rect.Left + dx, Top: rect.Top + dy,
		Right: rect.Right + dx, Bottom: rect.Bottom + dy}
}

func dialogGeometryWindowRect(hwnd uintptr) (Rect, error) {
	var native win.RECT
	ok, _, callErr := dialogGeometryGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&native)),
	)
	runtime.KeepAlive(&native)
	if ok == 0 {
		return Rect{}, boolCallError("GetWindowRect(dialog geometry)", callErr)
	}
	return Rect{
		Left:   native.Left,
		Top:    native.Top,
		Right:  native.Right,
		Bottom: native.Bottom,
	}, nil
}

func dialogGeometryWindowDPI(hwnd uintptr, fallback uint32) uint32 {
	if fallback < 48 || fallback > 768 {
		fallback = 96
	}
	if hwnd == 0 || dialogGeometryGetDPI.Find() != nil {
		return fallback
	}
	dpi, _, _ := dialogGeometryGetDPI.Call(hwnd)
	if dpi < 48 || dpi > 768 {
		return fallback
	}
	return uint32(dpi)
}

func centeredDialogRect(
	owner, workArea Rect,
	widthDIP, heightDIP int,
	dpi uint32,
) Rect {
	if dpi < 48 || dpi > 768 {
		dpi = 96
	}
	width := scaleDialogDimension(widthDIP, dpi)
	height := scaleDialogDimension(heightDIP, dpi)
	if !workArea.Valid() {
		workArea = owner
	}
	if available := workArea.Width(); available > 0 && width > available {
		width = available
	}
	if available := workArea.Height(); available > 0 && height > available {
		height = available
	}

	left := int64(owner.Left) +
		(int64(owner.Width())-int64(width))/2
	top := int64(owner.Top) +
		(int64(owner.Height())-int64(height))/2
	left = fitDialogAxis(
		left,
		int64(width),
		int64(workArea.Left),
		int64(workArea.Right),
	)
	top = fitDialogAxis(
		top,
		int64(height),
		int64(workArea.Top),
		int64(workArea.Bottom),
	)
	return Rect{
		Left:   clampDialogCoordinate(left),
		Top:    clampDialogCoordinate(top),
		Right:  clampDialogCoordinate(left + int64(width)),
		Bottom: clampDialogCoordinate(top + int64(height)),
	}
}

func restoredDialogRect(
	placement DialogPlacement,
	workArea Rect,
	dpi uint32,
) Rect {
	if dpi < 48 || dpi > 768 {
		dpi = 96
	}
	width := scaleDialogDimension(placement.Width, dpi)
	height := scaleDialogDimension(placement.Height, dpi)
	if workArea.Valid() {
		if width > workArea.Width() {
			width = workArea.Width()
		}
		if height > workArea.Height() {
			height = workArea.Height()
		}
	}
	left := fitDialogAxis(
		int64(placement.X),
		int64(width),
		int64(workArea.Left),
		int64(workArea.Right),
	)
	top := fitDialogAxis(
		int64(placement.Y),
		int64(height),
		int64(workArea.Top),
		int64(workArea.Bottom),
	)
	return Rect{
		Left:   clampDialogCoordinate(left),
		Top:    clampDialogCoordinate(top),
		Right:  clampDialogCoordinate(left + int64(width)),
		Bottom: clampDialogCoordinate(top + int64(height)),
	}
}

func scaleDialogDimension(value int, dpi uint32) int32 {
	if dpi < 48 || dpi > 768 {
		dpi = 96
	}
	if value < 1 {
		value = 1
	}
	scaled := (int64(value)*int64(dpi) + 48) / 96
	if scaled < 1 {
		return 1
	}
	const maxInt32 = int64(^uint32(0) >> 1)
	if scaled > maxInt32 {
		return int32(maxInt32)
	}
	return int32(scaled)
}

func unscaleDialogDimension(value int32, dpi uint32) int {
	if value < 1 {
		return 1
	}
	if dpi < 48 || dpi > 768 {
		dpi = 96
	}
	logical := (int64(value)*96 + int64(dpi)/2) / int64(dpi)
	if logical < 1 {
		return 1
	}
	const maxInt = int64(^uint32(0) >> 1)
	if logical > maxInt {
		return int(maxInt)
	}
	return int(logical)
}

func fitDialogAxis(position, size, near, far int64) int64 {
	available := far - near
	if available <= 0 || size >= available {
		// Keep the caption/leading resize edge reachable when a very high DPI
		// makes the requested surface larger than the target work area.
		return near
	}
	if position < near {
		return near
	}
	if position+size > far {
		return far - size
	}
	return position
}

func clampDialogCoordinate(value int64) int32 {
	const (
		minInt32 = -int64(^uint32(0)>>1) - 1
		maxInt32 = int64(^uint32(0) >> 1)
	)
	if value < minInt32 {
		return int32(minInt32)
	}
	if value > maxInt32 {
		return int32(maxInt32)
	}
	return int32(value)
}
