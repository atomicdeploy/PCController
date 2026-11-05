//go:build windows

package traymenu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaWindowCornerPref     = 33
	dwmwaSystemBackdropType   = 38

	dwmWindowCornerRound       = 2
	dwmSystemBackdropNone      = 1
	dwmSystemBackdropTransient = 3

	wcaAccentPolicy         = 19
	accentDisabled          = 0
	accentEnableBlurBehind  = 3
	accentEnableAcrylic     = 4
	dwmBlurBehindEnableFlag = 1

	updateLayeredWindowAlpha = 0x00000002
	acSourceOver             = 0x00
)

var (
	menuUser32                    = windows.NewLazySystemDLL("user32.dll")
	menuAppendMenuW               = menuUser32.NewProc("AppendMenuW")
	menuMonitorFromRect           = menuUser32.NewProc("MonitorFromRect")
	menuSetWindowRgn              = menuUser32.NewProc("SetWindowRgn")
	menuSetWindowCompositionAttr  = menuUser32.NewProc("SetWindowCompositionAttribute")
	menuUpdateLayeredWindow       = menuUser32.NewProc("UpdateLayeredWindow")
	menuFillRect                  = menuUser32.NewProc("FillRect")
	menuGetCapture                = menuUser32.NewProc("GetCapture")
	menuIsWindow                  = menuUser32.NewProc("IsWindow")
	menuValidateRect              = menuUser32.NewProc("ValidateRect")
	menuGDI32                     = windows.NewLazySystemDLL("gdi32.dll")
	menuExtCreateRegion           = menuGDI32.NewProc("ExtCreateRegion")
	menuCreateSolidBrush          = menuGDI32.NewProc("CreateSolidBrush")
	menuDWMAPI                    = windows.NewLazySystemDLL("dwmapi.dll")
	menuDwmSetWindowAttribute     = menuDWMAPI.NewProc("DwmSetWindowAttribute")
	menuDwmEnableBlurBehindWindow = menuDWMAPI.NewProc("DwmEnableBlurBehindWindow")
	menuDwmExtendFrameIntoClient  = menuDWMAPI.NewProc("DwmExtendFrameIntoClientArea")
	menuDwmIsCompositionEnabled   = menuDWMAPI.NewProc("DwmIsCompositionEnabled")
	menuDwmFlush                  = menuDWMAPI.NewProc("DwmFlush")
	menuSHCore                    = windows.NewLazySystemDLL("shcore.dll")
	menuGetDpiForMonitor          = menuSHCore.NewProc("GetDpiForMonitor")
)

type accentPolicy struct {
	State         int32
	Flags         uint32
	GradientColor uint32
	AnimationID   int32
}

type compositionAttributeData struct {
	Attribute uint32
	Data      uintptr
	Size      uintptr
}

type dwmBlurBehind struct {
	Flags                 uint32
	Enable                int32
	BlurRegion            uintptr
	TransitionOnMaximized int32
}

type dwmMargins struct {
	Left, Right, Top, Bottom int32
}

func appendNativeMenu(menu win.HMENU, flags uint32, command uint32, text *uint16) error {
	if err := menuAppendMenuW.Find(); err != nil {
		return err
	}
	result, _, callErr := menuAppendMenuW.Call(
		uintptr(menu),
		uintptr(flags),
		uintptr(command),
		uintptr(unsafe.Pointer(text)),
	)
	runtime.KeepAlive(text)
	if result == 0 {
		return callError("AppendMenuW", callErr)
	}
	return nil
}

func monitorFor(anchor Rect, owner uintptr) win.HMONITOR {
	if menuMonitorFromRect.Find() == nil {
		rect := win.RECT{
			Left:   anchor.Left,
			Top:    anchor.Top,
			Right:  anchor.Right,
			Bottom: anchor.Bottom,
		}
		monitor, _, _ := menuMonitorFromRect.Call(
			uintptr(unsafe.Pointer(&rect)),
			win.MONITOR_DEFAULTTONEAREST,
		)
		runtime.KeepAlive(&rect)
		if monitor != 0 {
			return win.HMONITOR(monitor)
		}
	}
	return win.MonitorFromWindow(win.HWND(owner), win.MONITOR_DEFAULTTONEAREST)
}

func monitorWorkArea(monitor win.HMONITOR) (Rect, error) {
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if monitor == 0 || !win.GetMonitorInfo(monitor, &info) {
		return Rect{}, errors.New("traymenu: GetMonitorInfo failed")
	}
	return Rect{
		Left:   info.RcWork.Left,
		Top:    info.RcWork.Top,
		Right:  info.RcWork.Right,
		Bottom: info.RcWork.Bottom,
	}, nil
}

func monitorDPI(monitor win.HMONITOR, owner uintptr) int32 {
	if monitor != 0 && menuGetDpiForMonitor.Find() == nil {
		var x, y uint32
		result, _, _ := menuGetDpiForMonitor.Call(
			uintptr(monitor),
			0, // MDT_EFFECTIVE_DPI
			uintptr(unsafe.Pointer(&x)),
			uintptr(unsafe.Pointer(&y)),
		)
		runtime.KeepAlive(&x)
		runtime.KeepAlive(&y)
		if int32(result) >= 0 && x >= 48 && x <= 768 {
			return int32(x)
		}
	}
	dpi := win.GetDpiForWindow(win.HWND(owner))
	if dpi < 48 || dpi > 768 {
		return baseDPI
	}
	return int32(dpi)
}

func applyRoundedRegion(hwnd win.HWND, width, height, radius int32) error {
	if menuExtCreateRegion.Find() != nil || menuSetWindowRgn.Find() != nil {
		return errors.New("traymenu: rounded-region APIs unavailable")
	}
	region, err := createRoundedRegion(width, height, radius)
	if err != nil {
		return err
	}
	applied, _, setErr := menuSetWindowRgn.Call(uintptr(hwnd), region, 1)
	if applied == 0 {
		win.DeleteObject(win.HGDIOBJ(region))
		return callError("SetWindowRgn", setErr)
	}
	// Windows owns the region after a successful SetWindowRgn call.
	return nil
}

// createRoundedRegion uses the exact same pixel predicate as the layered BGRA
// foreground. CreateRoundRectRgn has subtly different inclusive bounds and
// ellipse rasterization; combining the two left visible one-pixel spikes and
// notches at the popup corners on several DPI scales.
func createRoundedRegion(width, height, radius int32) (uintptr, error) {
	rectangles := roundedRegionRectangles(width, height, radius)
	if len(rectangles) == 0 {
		return 0, errors.New("traymenu: rounded-region dimensions are invalid")
	}
	const (
		regionHeaderSize    = 32
		regionRectangleSize = 16
		regionDataRects     = 1
	)
	data := make(
		[]byte,
		regionHeaderSize+len(rectangles)*regionRectangleSize,
	)
	binary.LittleEndian.PutUint32(data[0:4], regionHeaderSize)
	binary.LittleEndian.PutUint32(data[4:8], regionDataRects)
	binary.LittleEndian.PutUint32(data[8:12], uint32(len(rectangles)))
	binary.LittleEndian.PutUint32(
		data[12:16],
		uint32(len(rectangles)*regionRectangleSize),
	)
	binary.LittleEndian.PutUint32(data[16:20], 0)
	binary.LittleEndian.PutUint32(data[20:24], 0)
	binary.LittleEndian.PutUint32(data[24:28], uint32(width))
	binary.LittleEndian.PutUint32(data[28:32], uint32(height))
	for index, rectangle := range rectangles {
		offset := regionHeaderSize + index*regionRectangleSize
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			uint32(rectangle.Left),
		)
		binary.LittleEndian.PutUint32(
			data[offset+4:offset+8],
			uint32(rectangle.Top),
		)
		binary.LittleEndian.PutUint32(
			data[offset+8:offset+12],
			uint32(rectangle.Right),
		)
		binary.LittleEndian.PutUint32(
			data[offset+12:offset+16],
			uint32(rectangle.Bottom),
		)
	}
	region, _, callErr := menuExtCreateRegion.Call(
		0,
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&data[0])),
	)
	runtime.KeepAlive(data)
	if region == 0 {
		return 0, callError("ExtCreateRegion", callErr)
	}
	return region, nil
}

func roundedRegionRectangles(width, height, radius int32) []win.RECT {
	if width < 1 || height < 1 {
		return nil
	}
	if radius < 0 {
		radius = 0
	}
	if maximum := min32(width, height) / 2; radius > maximum {
		radius = maximum
	}
	rectangles := make([]win.RECT, 0, height)
	for y := int32(0); y < height; y++ {
		left := int32(0)
		for left < width &&
			!insideRoundedRect(left, y, 0, 0, width, height, radius) {
			left++
		}
		right := width
		for right > left &&
			!insideRoundedRect(right-1, y, 0, 0, width, height, radius) {
			right--
		}
		if left < right {
			rectangles = append(rectangles, win.RECT{
				Left: left, Top: y, Right: right, Bottom: y + 1,
			})
		}
	}
	return rectangles
}

func applyWindowBackdrop(
	hwnd win.HWND,
	requested Backdrop,
	dark, transparencyEnabled bool,
) Backdrop {
	if requested == "" {
		requested = BackdropAuto
	}
	// AccentPolicy and the system-backdrop APIs can rebuild the compositor
	// visual. Make the menu contour the final DWM mutation on every setup and
	// refresh, including cached-window reuse and WM_DWMCOMPOSITIONCHANGED.
	defer func() {
		_ = setDWMInt(
			hwnd,
			dwmwaWindowCornerPref,
			dwmWindowCornerRound,
		)
	}()
	darkValue := int32(0)
	if dark {
		darkValue = 1
	}
	_ = setDWMInt(hwnd, dwmwaUseImmersiveDarkMode, darkValue)
	// The foreground and compatibility HRGNs use the same 8-DIP radius as
	// Windows 11's standard rounded top-level contour. AccentPolicy can
	// composite outside an HWND region, so DWMWCP_DONOTROUND would leave its
	// live material as a visible square behind otherwise rounded pixels.

	if requested == BackdropSolid {
		clearWindowBackdrop(hwnd)
		return BackdropSolid
	}
	if !transparencyEnabled || !dwmCompositionEnabled() {
		clearWindowBackdrop(hwnd)
		return BackdropSolid
	}

	// A full glass client is the transparent native host. All menu graphics are
	// placed on a separate per-pixel-alpha child, so none of these live effects
	// are hidden by an opaque GDI client paint.
	clearWindowBackdrop(hwnd)
	if err := extendDWMFrame(hwnd, true); err != nil {
		return BackdropSolid
	}

	if requested == BackdropAuto || requested == BackdropAcrylic {
		// AccentPolicy produces genuine behind-window Acrylic on both Windows
		// 10 and 11. Prefer it for a short-lived popup, then use the official
		// transient system material where available.
		if err := setAccent(hwnd, accentEnableAcrylic, dark); err == nil {
			return BackdropAcrylic
		}
		if err := setDWMInt(hwnd, dwmwaSystemBackdropType, dwmSystemBackdropTransient); err == nil {
			return BackdropAcrylic
		}
	}
	if requested == BackdropAuto || requested == BackdropAcrylic || requested == BackdropBlur {
		if err := setAccent(hwnd, accentEnableBlurBehind, dark); err == nil {
			return BackdropBlur
		}
		if err := setDWMBlur(hwnd, true); err == nil {
			return BackdropBlur
		}
	}
	clearWindowBackdrop(hwnd)
	return BackdropSolid
}

func dwmCompositionEnabled() bool {
	if menuDwmIsCompositionEnabled.Find() != nil {
		return false
	}
	var enabled int32
	result, _, _ := menuDwmIsCompositionEnabled.Call(
		uintptr(unsafe.Pointer(&enabled)),
	)
	runtime.KeepAlive(&enabled)
	if int32(result) < 0 || enabled == 0 {
		return false
	}
	return true
}

func clearWindowBackdrop(hwnd win.HWND) {
	_ = setDWMInt(hwnd, dwmwaSystemBackdropType, dwmSystemBackdropNone)
	_ = setAccent(hwnd, accentDisabled, false)
	_ = setDWMBlur(hwnd, false)
	_ = extendDWMFrame(hwnd, false)
}

func setDWMInt(hwnd win.HWND, attribute uint32, value int32) error {
	if err := menuDwmSetWindowAttribute.Find(); err != nil {
		return err
	}
	result, _, _ := menuDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	runtime.KeepAlive(&value)
	if int32(result) < 0 {
		return fmt.Errorf("DwmSetWindowAttribute HRESULT %#x", uint32(result))
	}
	return nil
}

func setAccent(hwnd win.HWND, state int32, dark bool) error {
	if err := menuSetWindowCompositionAttr.Find(); err != nil {
		return err
	}
	gradient := uint32(0xC8FAFAFA)
	if dark {
		gradient = 0xB8222222
	}
	if state == accentEnableBlurBehind {
		gradient = 0x01000000
	}
	if state == accentDisabled {
		gradient = 0
	}
	policy := accentPolicy{State: state, GradientColor: gradient}
	data := compositionAttributeData{
		Attribute: wcaAccentPolicy,
		Data:      uintptr(unsafe.Pointer(&policy)),
		Size:      unsafe.Sizeof(policy),
	}
	result, _, callErr := menuSetWindowCompositionAttr.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&data)),
	)
	runtime.KeepAlive(&policy)
	runtime.KeepAlive(&data)
	if result == 0 {
		return callError("SetWindowCompositionAttribute", callErr)
	}
	return nil
}

func setDWMBlur(hwnd win.HWND, enabled bool) error {
	if err := menuDwmEnableBlurBehindWindow.Find(); err != nil {
		return err
	}
	value := dwmBlurBehind{Flags: dwmBlurBehindEnableFlag}
	if enabled {
		value.Enable = 1
	}
	result, _, _ := menuDwmEnableBlurBehindWindow.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&value)),
	)
	runtime.KeepAlive(&value)
	if int32(result) < 0 {
		return fmt.Errorf("DwmEnableBlurBehindWindow HRESULT %#x", uint32(result))
	}
	return nil
}

func extendDWMFrame(hwnd win.HWND, full bool) error {
	if err := menuDwmExtendFrameIntoClient.Find(); err != nil {
		return err
	}
	margins := dwmMargins{}
	if full {
		margins = dwmMargins{Left: -1, Right: -1, Top: -1, Bottom: -1}
	}
	result, _, _ := menuDwmExtendFrameIntoClient.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&margins)),
	)
	runtime.KeepAlive(&margins)
	if int32(result) < 0 {
		return fmt.Errorf("DwmExtendFrameIntoClientArea HRESULT %#x", uint32(result))
	}
	return nil
}

func updateLayeredChild(
	hwnd win.HWND,
	source win.HDC,
	width, height int32,
	opacity byte,
) error {
	if hwnd == 0 || source == 0 || width < 1 || height < 1 {
		return errors.New("traymenu: layered foreground parameters are invalid")
	}
	if err := menuUpdateLayeredWindow.Find(); err != nil {
		return err
	}
	screen := win.GetDC(0)
	if screen == 0 {
		return errors.New("traymenu: GetDC for layered foreground failed")
	}
	defer win.ReleaseDC(0, screen)

	size := win.SIZE{CX: width, CY: height}
	sourcePoint := win.POINT{}
	blend := win.BLENDFUNCTION{
		BlendOp:             acSourceOver,
		SourceConstantAlpha: opacity,
		AlphaFormat:         win.AC_SRC_ALPHA,
	}
	result, _, callErr := menuUpdateLayeredWindow.Call(
		uintptr(hwnd),
		uintptr(screen),
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(source),
		uintptr(unsafe.Pointer(&sourcePoint)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		updateLayeredWindowAlpha,
	)
	runtime.KeepAlive(&size)
	runtime.KeepAlive(&sourcePoint)
	runtime.KeepAlive(&blend)
	if result == 0 {
		return callError("UpdateLayeredWindow", callErr)
	}
	return nil
}

func fillBlackClient(dc win.HDC, width, height int32) error {
	if dc == 0 || width < 1 || height < 1 {
		return errors.New("traymenu: glass-host paint parameters are invalid")
	}
	if err := menuFillRect.Find(); err != nil {
		return err
	}
	rect := win.RECT{Right: width, Bottom: height}
	brush := win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH))
	if brush == 0 {
		return errors.New("traymenu: black stock brush is unavailable")
	}
	result, _, callErr := menuFillRect.Call(
		uintptr(dc),
		uintptr(unsafe.Pointer(&rect)),
		uintptr(brush),
	)
	runtime.KeepAlive(&rect)
	if result == 0 {
		return callError("FillRect", callErr)
	}
	return nil
}

func fillColorClient(dc win.HDC, width, height int32, color rgb) error {
	if dc == 0 || width < 1 || height < 1 {
		return errors.New("traymenu: solid-host paint parameters are invalid")
	}
	if err := menuCreateSolidBrush.Find(); err != nil {
		return err
	}
	brushValue, _, callErr := menuCreateSolidBrush.Call(
		uintptr(win.RGB(color.r, color.g, color.b)),
	)
	if brushValue == 0 {
		return callError("CreateSolidBrush", callErr)
	}
	brush := win.HBRUSH(brushValue)
	defer win.DeleteObject(win.HGDIOBJ(brush))

	rect := win.RECT{Right: width, Bottom: height}
	result, _, fillErr := menuFillRect.Call(
		uintptr(dc),
		uintptr(unsafe.Pointer(&rect)),
		uintptr(brush),
	)
	runtime.KeepAlive(&rect)
	if result == 0 {
		return callError("FillRect", fillErr)
	}
	return nil
}

func validateWindow(hwnd win.HWND) {
	if hwnd == 0 || menuValidateRect.Find() != nil {
		return
	}
	menuValidateRect.Call(uintptr(hwnd), 0)
}

func nativeWindowExists(hwnd win.HWND) bool {
	if hwnd == 0 || menuIsWindow.Find() != nil {
		return false
	}
	exists, _, _ := menuIsWindow.Call(uintptr(hwnd))
	return exists != 0
}

func releasePopupCapture(hwnd win.HWND) {
	if hwnd == 0 || menuGetCapture.Find() != nil {
		return
	}
	captured, _, _ := menuGetCapture.Call()
	if win.HWND(captured) == hwnd {
		win.ReleaseCapture()
	}
}

func flushDWM() error {
	if err := menuDwmFlush.Find(); err != nil {
		return err
	}
	result, _, _ := menuDwmFlush.Call()
	if int32(result) < 0 {
		return fmt.Errorf("DwmFlush HRESULT %#x", uint32(result))
	}
	return nil
}

func callError(name string, err error) error {
	if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
		err = windows.ERROR_GEN_FAILURE
	}
	return fmt.Errorf("traymenu: %s: %w", name, err)
}
