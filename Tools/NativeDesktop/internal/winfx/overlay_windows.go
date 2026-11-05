//go:build windows

package winfx

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	overlayGWLExStyle    = -20
	overlayExLayered     = 0x00080000
	overlayExTransparent = 0x00000020

	overlayBI_RGB       = 0
	overlayDIBRGBColors = 0
	overlayULWAlpha     = 0x00000002
	overlayACSrcOver    = 0x00
	overlayACSrcAlpha   = 0x01
)

var (
	overlayUser32              = windows.NewLazySystemDLL("user32.dll")
	overlayGDI32               = windows.NewLazySystemDLL("gdi32.dll")
	overlayGetWindowLongPtrW   = overlayUser32.NewProc("GetWindowLongPtrW")
	overlayGetWindowLongW      = overlayUser32.NewProc("GetWindowLongW")
	overlaySetWindowLongPtrW   = overlayUser32.NewProc("SetWindowLongPtrW")
	overlaySetWindowLongW      = overlayUser32.NewProc("SetWindowLongW")
	overlaySetWindowPos        = overlayUser32.NewProc("SetWindowPos")
	overlayRedrawWindow        = overlayUser32.NewProc("RedrawWindow")
	overlayUpdateLayeredWindow = overlayUser32.NewProc("UpdateLayeredWindow")
	overlayGetWindowRect       = overlayUser32.NewProc("GetWindowRect")
	overlayGetDC               = overlayUser32.NewProc("GetDC")
	overlayReleaseDC           = overlayUser32.NewProc("ReleaseDC")
	overlayCreateCompatibleDC  = overlayGDI32.NewProc("CreateCompatibleDC")
	overlayCreateDIBSection    = overlayGDI32.NewProc("CreateDIBSection")
	overlaySelectObject        = overlayGDI32.NewProc("SelectObject")
	overlayDeleteObject        = overlayGDI32.NewProc("DeleteObject")
	overlayDeleteDC            = overlayGDI32.NewProc("DeleteDC")
	overlayGetDIBits           = overlayGDI32.NewProc("GetDIBits")
)

type overlayBitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type overlayBitmapInfo struct {
	Header overlayBitmapInfoHeader
	Colors [1]uint32
}

type overlayPoint struct {
	X, Y int32
}

type overlaySize struct {
	CX, CY int32
}

type overlayBlendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}

// MakeTransparentChild prevents an otherwise empty Walk container from
// erasing the top-level DWM material before its foreground child is painted.
func MakeTransparentChild(hwnd uintptr) error {
	return addOverlayExStyle(hwnd, overlayExTransparent)
}

// MakePerPixelOverlay enables GlassDWM's top-level per-pixel-alpha path.
func MakePerPixelOverlay(hwnd uintptr) error {
	return SetPerPixelOverlay(hwnd, true)
}

// SetPerPixelOverlay toggles GlassDWM's Layered experiment on the top-level
// DWM host. When enabled its pixels must be supplied by UpdateLayeredWindow.
func SetPerPixelOverlay(hwnd uintptr, enabled bool) error {
	addFlags, removeFlags := uint32(0), uint32(wsExWindowEdge)
	if enabled {
		addFlags = overlayExLayered
	} else {
		removeFlags |= overlayExLayered
	}
	if err := changeOverlayExStyle(hwnd, addFlags, removeFlags); err != nil {
		return err
	}
	ok, _, callErr := overlaySetWindowPos.Call(
		hwnd,
		0,
		0, 0, 0, 0,
		0x0001| // SWP_NOSIZE
			0x0002| // SWP_NOMOVE
			0x0004| // SWP_NOZORDER
			0x0010| // SWP_NOACTIVATE
			0x0020, // SWP_FRAMECHANGED
	)
	if ok == 0 {
		return boolCallError("SetWindowPos(SWP_FRAMECHANGED)", callErr)
	}
	if err := ClearWindowEdge(hwnd); err != nil {
		return err
	}
	// RDW_INVALIDATE | RDW_ERASE | RDW_FRAME | RDW_ALLCHILDREN
	_, _, _ = overlayRedrawWindow.Call(hwnd, 0, 0, 0x0001|0x0004|0x0400|0x0080)
	return nil
}

// UpdatePerPixelOverlay replaces a layered top-level window with image using
// UpdateLayeredWindow and AC_SRC_ALPHA. The source is copied into a top-down
// 32-bit DIB as premultiplied BGRA, preserving antialiased SVG and APNG edges.
func UpdatePerPixelOverlay(hwnd uintptr, source *image.RGBA) error {
	return UpdatePerPixelOverlayWithOpacity(hwnd, source, 255)
}

// UpdatePerPixelOverlayWithOpacity presents the per-pixel surface with an
// additional whole-window opacity. Keeping this value in UpdateLayeredWindow's
// blend function is important: SetLayeredWindowAttributes is incompatible with
// subsequent UpdateLayeredWindow calls until WS_EX_LAYERED is reset.
func UpdatePerPixelOverlayWithOpacity(
	hwnd uintptr,
	source *image.RGBA,
	opacity byte,
) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	if source == nil || source.Bounds().Dx() < 1 || source.Bounds().Dy() < 1 {
		return fmt.Errorf("layered overlay image is empty")
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()

	screenDC, _, callErr := overlayGetDC.Call(0)
	if screenDC == 0 {
		return boolCallError("GetDC", callErr)
	}
	defer overlayReleaseDC.Call(0, screenDC)

	memoryDC, _, callErr := overlayCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return boolCallError("CreateCompatibleDC", callErr)
	}
	defer overlayDeleteDC.Call(memoryDC)

	info := overlayBitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(width)
	info.Header.Height = -int32(height)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = overlayBI_RGB
	info.Header.SizeImage = uint32(width * height * 4)
	var bits unsafe.Pointer
	bitmap, _, callErr := overlayCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&info)),
		overlayDIBRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return boolCallError("CreateDIBSection", callErr)
	}
	defer overlayDeleteObject.Call(bitmap)

	destination := unsafe.Slice((*byte)(bits), width*height*4)
	sourceBounds := source.Bounds()
	for y := 0; y < height; y++ {
		sourceOffset := source.PixOffset(sourceBounds.Min.X, sourceBounds.Min.Y+y)
		destinationOffset := y * width * 4
		for x := 0; x < width; x++ {
			src := sourceOffset + x*4
			dst := destinationOffset + x*4
			destination[dst+0] = source.Pix[src+2]
			destination[dst+1] = source.Pix[src+1]
			destination[dst+2] = source.Pix[src+0]
			destination[dst+3] = source.Pix[src+3]
		}
	}

	oldBitmap, _, callErr := overlaySelectObject.Call(memoryDC, bitmap)
	if oldBitmap == 0 {
		return boolCallError("SelectObject", callErr)
	}
	defer overlaySelectObject.Call(memoryDC, oldBitmap)

	size := overlaySize{CX: int32(width), CY: int32(height)}
	sourcePoint := overlayPoint{}
	destinationPoint := overlayPoint{}
	var windowBounds Rect
	if ok, _, callErr := overlayGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&windowBounds)),
	); ok == 0 {
		return boolCallError("GetWindowRect", callErr)
	}
	destinationPoint.X = windowBounds.Left
	destinationPoint.Y = windowBounds.Top
	blend := perPixelOverlayBlend(opacity)
	ok, _, callErr := overlayUpdateLayeredWindow.Call(
		hwnd,
		screenDC,
		uintptr(unsafe.Pointer(&destinationPoint)),
		uintptr(unsafe.Pointer(&size)),
		memoryDC,
		uintptr(unsafe.Pointer(&sourcePoint)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		overlayULWAlpha,
	)
	runtime.KeepAlive(source)
	runtime.KeepAlive(&info)
	runtime.KeepAlive(&size)
	runtime.KeepAlive(&sourcePoint)
	runtime.KeepAlive(&destinationPoint)
	runtime.KeepAlive(&blend)
	if ok == 0 {
		return boolCallError("UpdateLayeredWindow", callErr)
	}
	return nil
}

func perPixelOverlayBlend(opacity byte) overlayBlendFunction {
	return overlayBlendFunction{
		BlendOp:             overlayACSrcOver,
		SourceConstantAlpha: opacity,
		AlphaFormat:         overlayACSrcAlpha,
	}
}

// UpdatePerPixelOverlayBitmap presents an existing 32-bit premultiplied
// HBITMAP through a layered child without copying it back through
// walk.Bitmap.ToImage. The pinned Walk release leaks a screen DC in ToImage,
// so using the native bitmap directly is both faster and safe for a process
// that is expected to remain in the tray for weeks.
func UpdatePerPixelOverlayBitmap(
	hwnd uintptr,
	hbitmap uintptr,
	width int,
	height int,
) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	if hbitmap == 0 || width < 1 || height < 1 {
		return fmt.Errorf("layered overlay bitmap is empty")
	}

	screenDC, _, callErr := overlayGetDC.Call(0)
	if screenDC == 0 {
		return boolCallError("GetDC", callErr)
	}
	defer overlayReleaseDC.Call(0, screenDC)

	memoryDC, _, callErr := overlayCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return boolCallError("CreateCompatibleDC", callErr)
	}
	defer overlayDeleteDC.Call(memoryDC)

	info := overlayBitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	query, _, callErr := overlayGetDIBits.Call(
		screenDC,
		hbitmap,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&info)),
		overlayDIBRGBColors,
	)
	if query == 0 {
		return boolCallError("GetDIBits(bitmap info)", callErr)
	}
	if info.Header.Width != int32(width) ||
		absOverlayInt32(info.Header.Height) != int32(height) ||
		info.Header.BitCount != 32 {
		return fmt.Errorf(
			"layered bitmap dimensions are %dx%dx%d, expected %dx%dx32",
			info.Header.Width,
			absOverlayInt32(info.Header.Height),
			info.Header.BitCount,
			width,
			height,
		)
	}
	info.Header.Compression = overlayBI_RGB
	byteCount := int(info.Header.SizeImage)
	if byteCount < width*height*4 {
		byteCount = width * height * 4
		info.Header.SizeImage = uint32(byteCount)
	}
	pixels := make([]byte, byteCount)
	lines, _, callErr := overlayGetDIBits.Call(
		screenDC,
		hbitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&info)),
		overlayDIBRGBColors,
	)
	if lines == 0 {
		return boolCallError("GetDIBits", callErr)
	}
	// Alpha 1 is visually indistinguishable from fully transparent, while
	// making native layered-window background pixels participate in hit tests.
	for index := 3; index < len(pixels); index += 4 {
		if pixels[index] == 0 {
			pixels[index-3] = 0
			pixels[index-2] = 0
			pixels[index-1] = 0
			pixels[index] = 1
		}
	}

	var bits unsafe.Pointer
	bitmap, _, callErr := overlayCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&info)),
		overlayDIBRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return boolCallError("CreateDIBSection", callErr)
	}
	defer overlayDeleteObject.Call(bitmap)
	copy(unsafe.Slice((*byte)(bits), len(pixels)), pixels)

	oldBitmap, _, callErr := overlaySelectObject.Call(memoryDC, bitmap)
	if oldBitmap == 0 {
		return boolCallError("SelectObject", callErr)
	}
	defer overlaySelectObject.Call(memoryDC, oldBitmap)

	size := overlaySize{CX: int32(width), CY: int32(height)}
	sourcePoint := overlayPoint{}
	destinationPoint := overlayPoint{}
	var windowBounds Rect
	if ok, _, callErr := overlayGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&windowBounds)),
	); ok == 0 {
		return boolCallError("GetWindowRect", callErr)
	}
	destinationPoint.X = windowBounds.Left
	destinationPoint.Y = windowBounds.Top
	blend := overlayBlendFunction{
		BlendOp:             overlayACSrcOver,
		SourceConstantAlpha: 255,
		AlphaFormat:         overlayACSrcAlpha,
	}
	ok, _, callErr := overlayUpdateLayeredWindow.Call(
		hwnd,
		screenDC,
		uintptr(unsafe.Pointer(&destinationPoint)),
		uintptr(unsafe.Pointer(&size)),
		memoryDC,
		uintptr(unsafe.Pointer(&sourcePoint)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		overlayULWAlpha,
	)
	runtime.KeepAlive(&size)
	runtime.KeepAlive(&sourcePoint)
	runtime.KeepAlive(&destinationPoint)
	runtime.KeepAlive(&blend)
	runtime.KeepAlive(&info)
	runtime.KeepAlive(pixels)
	if ok == 0 {
		return boolCallError("UpdateLayeredWindow", callErr)
	}
	return nil
}

func absOverlayInt32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func addOverlayExStyle(hwnd uintptr, flags uint32) error {
	return changeOverlayExStyle(hwnd, flags, 0)
}

func setOverlayExStyle(hwnd uintptr, flags uint32, enabled bool) error {
	if enabled {
		return changeOverlayExStyle(hwnd, flags, 0)
	}
	return changeOverlayExStyle(hwnd, 0, flags)
}

func changeOverlayExStyle(hwnd uintptr, addFlags, removeFlags uint32) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	getProc, setProc := overlayGetWindowLongPtrW, overlaySetWindowLongPtrW
	if unsafe.Sizeof(uintptr(0)) == 4 {
		getProc, setProc = overlayGetWindowLongW, overlaySetWindowLongW
	}
	if err := getProc.Find(); err != nil {
		return err
	}
	if err := setProc.Find(); err != nil {
		return err
	}
	// Both APIs can successfully return zero. Last-error is thread-local and
	// is not cleared on success, so stale DWM errors must not trigger fallback.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	index := int32(overlayGWLExStyle)
	win.SetLastError(0)
	style, _, callErr := getProc.Call(hwnd, uintptr(uint32(index)))
	if style == 0 && callErr != windows.ERROR_SUCCESS {
		return fmt.Errorf("GetWindowLong(GWL_EXSTYLE): %w", callErr)
	}
	win.SetLastError(0)
	previous, _, callErr := setProc.Call(
		hwnd,
		uintptr(uint32(index)),
		uintptr((uint32(style)|addFlags)&^removeFlags),
	)
	if previous == 0 && callErr != windows.ERROR_SUCCESS {
		return fmt.Errorf("SetWindowLong(GWL_EXSTYLE): %w", callErr)
	}
	return nil
}
