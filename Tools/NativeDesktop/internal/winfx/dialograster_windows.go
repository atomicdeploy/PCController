//go:build windows

package winfx

import (
	"math"
	"sync"
	"unsafe"
)

// Native HWNDs keep text/input/accessibility. Only their geometric chrome is
// composited with fractional pixel coverage; text is never scaled or filtered.
// The bounded cache retains DIBs across hover frames instead of creating GDI
// objects and rerasterizing unchanged geometry on every animation tick.
const dialogRasterMaxBytes = 16 << 20
const dialogRasterMaxEntries = 64

type dialogRasterKey struct {
	Kind                          uint8 // 0 rounded shape, 1 polyline, 2 parent-material corner mask
	Width, Height, Radius, Stroke int32
	Top, Bottom, Border           uint32
	Fill                          bool
	Clip                          dialogButtonRect
	Points                        [6]int32
	Count                         int
}

type dialogRasterSurface struct {
	dc, bitmap, oldBitmap uintptr
	pixels                []byte
	mask                  []byte
	used                  uint64
}

var dialogRasterCache = struct {
	sync.Mutex
	entries map[dialogRasterKey]*dialogRasterSurface
	bytes   int
	clock   uint64
}{entries: make(map[dialogRasterKey]*dialogRasterSurface)}

var dialogRasterFlush = overlayGDI32.NewProc("GdiFlush")
var dialogRasterGetBrushOrg = overlayGDI32.NewProc("GetBrushOrgEx")
var dialogRasterSetBrushOrg = overlayGDI32.NewProc("SetBrushOrgEx")

func dialogAncestorMaterialBrush(dc, hwnd uintptr) uintptr {
	parent, _, _ := dialogButtonGetParent.Call(hwnd)
	if parent == 0 {
		return 0
	}
	// Asking for the input HWND returns its solid input brush. Ask for the
	// container's effective background, then align that pattern to the child.
	brush, _, _ := dialogThemeSendMessage.Call(parent, dialogButtonWMControlColorStatic, dc, parent)
	if brush != 0 {
		var origin dialogComboPoint
		var childRect, parentRect dialogFieldRect
		dialogRasterGetBrushOrg.Call(dc, uintptr(unsafe.Pointer(&origin)))
		dialogFieldGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&childRect)))
		dialogFieldGetWindowRect.Call(parent, uintptr(unsafe.Pointer(&parentRect)))
		dialogRasterSetBrushOrg.Call(dc, uintptr(origin.X+parentRect.Left-childRect.Left), uintptr(origin.Y+parentRect.Top-childRect.Top), 0)
	}
	return brush
}

func roundedDialogCoverage(x, y float64, width, height, radius float64) float64 {
	if width <= 0 || height <= 0 {
		return 0
	}
	radius = max(0, min(radius, min(width, height)/2))
	qx := math.Abs(x-width/2) - width/2 + radius
	qy := math.Abs(y-height/2) - height/2 + radius
	distance := math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - radius
	return max(0, min(1, 0.5-distance))
}

func dialogRasterPixel(destination []byte, fill, border uint32, fillCoverage, borderCoverage float64) {
	alpha := min(1, fillCoverage+borderCoverage)
	// COLORREF is RGB in low-to-high bytes; DIBs require premultiplied BGRA.
	for i, shift := range []uint{16, 8, 0} {
		destination[i] = byte(math.Round(float64(byte(fill>>shift))*fillCoverage + float64(byte(border>>shift))*borderCoverage))
	}
	destination[3] = byte(math.Round(255 * alpha))
}

func rasterDialogShape(key dialogRasterKey, pixels []byte) {
	w, h := float64(key.Width), float64(key.Height)
	for y := int32(0); y < key.Height; y++ {
		fill := dialogMixColor(key.Top, key.Bottom, float64(y)/max(1, h-1))
		for x := int32(0); x < key.Width; x++ {
			pixel := pixels[(y*key.Width+x)*4:][:4]
			if key.Clip.Right > key.Clip.Left && (x < key.Clip.Left || x >= key.Clip.Right || y < key.Clip.Top || y >= key.Clip.Bottom) {
				continue
			}
			outer := roundedDialogCoverage(float64(x)+0.5, float64(y)+0.5, w, h, float64(key.Radius))
			inner := outer
			if key.Stroke > 0 {
				s := float64(key.Stroke)
				inner = min(outer, roundedDialogCoverage(float64(x)+0.5-s, float64(y)+0.5-s, w-2*s, h-2*s, max(0, float64(key.Radius)-s)))
			}
			fillCoverage := 0.0
			if key.Fill {
				fillCoverage = inner
			}
			dialogRasterPixel(pixel, fill, key.Border, fillCoverage, outer-inner)
		}
	}
}

func rasterDialogPolyline(key dialogRasterKey, pixels []byte) {
	for y := int32(0); y < key.Height; y++ {
		for x := int32(0); x < key.Width; x++ {
			coverage := 0.0
			for i := 1; i < key.Count; i++ {
				ax, ay := float64(key.Points[(i-1)*2]), float64(key.Points[(i-1)*2+1])
				bx, by := float64(key.Points[i*2]), float64(key.Points[i*2+1])
				dx, dy := bx-ax, by-ay
				t := 0.0
				if length := dx*dx + dy*dy; length > 0 {
					t = max(0, min(1, ((float64(x)+0.5-ax)*dx+(float64(y)+0.5-ay)*dy)/length))
				}
				distance := math.Hypot(float64(x)+0.5-ax-t*dx, float64(y)+0.5-ay-t*dy)
				coverage = max(coverage, max(0, min(1, float64(key.Stroke)/2+0.5-distance)))
			}
			dialogRasterPixel(pixels[(y*key.Width+x)*4:][:4], key.Top, 0, coverage, 0)
		}
	}
}

func newDialogRasterSurface(key dialogRasterKey) *dialogRasterSurface {
	s := &dialogRasterSurface{}
	s.dc, _, _ = overlayCreateCompatibleDC.Call(0)
	if s.dc == 0 {
		return nil
	}
	info := overlayBitmapInfo{Header: overlayBitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(overlayBitmapInfoHeader{})), Width: key.Width,
		Height: -key.Height, Planes: 1, BitCount: 32,
	}}
	var bits unsafe.Pointer
	s.bitmap, _, _ = overlayCreateDIBSection.Call(s.dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if s.bitmap == 0 || bits == nil {
		closeDialogRasterSurface(s)
		return nil
	}
	s.oldBitmap, _, _ = overlaySelectObject.Call(s.dc, s.bitmap)
	if s.oldBitmap == 0 || s.oldBitmap == ^uintptr(0) {
		closeDialogRasterSurface(s)
		return nil
	}
	s.pixels = unsafe.Slice((*byte)(bits), int(key.Width)*int(key.Height)*4)
	clear(s.pixels)
	if key.Kind == 2 {
		s.mask = make([]byte, int(key.Width)*int(key.Height))
		for y := int32(0); y < key.Height; y++ {
			for x := int32(0); x < key.Width; x++ {
				s.mask[y*key.Width+x] = byte(math.Round(255 * (1 - roundedDialogCoverage(float64(x)+0.5, float64(y)+0.5, float64(key.Width), float64(key.Height), float64(key.Radius)))))
			}
		}
	}
	if key.Kind == 0 {
		rasterDialogShape(key, s.pixels)
	}
	if key.Kind == 1 {
		rasterDialogPolyline(key, s.pixels)
	}
	return s
}

func closeDialogRasterSurface(s *dialogRasterSurface) {
	if s == nil {
		return
	}
	if s.dc != 0 && s.oldBitmap != 0 && s.oldBitmap != ^uintptr(0) {
		overlaySelectObject.Call(s.dc, s.oldBitmap)
	}
	if s.bitmap != 0 {
		overlayDeleteObject.Call(s.bitmap)
	}
	if s.dc != 0 {
		overlayDeleteDC.Call(s.dc)
	}
}

func releaseDialogRasterCache() {
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	for key, s := range dialogRasterCache.entries {
		closeDialogRasterSurface(s)
		delete(dialogRasterCache.entries, key)
	}
	dialogRasterCache.bytes = 0
}

// Caller holds dialogRasterCache for the entire HDC use, including eviction.
func acquireDialogRaster(key dialogRasterKey) *dialogRasterSurface {
	bytes := int64(key.Width) * int64(key.Height) * 4
	if key.Kind == 2 {
		bytes += int64(key.Width) * int64(key.Height)
	}
	if key.Width < 1 || key.Height < 1 || key.Width > 8192 || key.Height > 8192 || bytes > 4<<20 {
		return nil
	}
	dialogRasterCache.clock++
	if s := dialogRasterCache.entries[key]; s != nil {
		s.used = dialogRasterCache.clock
		return s
	}
	for len(dialogRasterCache.entries) >= dialogRasterMaxEntries || dialogRasterCache.bytes+int(bytes) > dialogRasterMaxBytes {
		var oldestKey dialogRasterKey
		var oldest *dialogRasterSurface
		for k, s := range dialogRasterCache.entries {
			if oldest == nil || s.used < oldest.used {
				oldestKey, oldest = k, s
			}
		}
		if oldest == nil {
			break
		}
		delete(dialogRasterCache.entries, oldestKey)
		dialogRasterCache.bytes -= len(oldest.pixels) + len(oldest.mask)
		closeDialogRasterSurface(oldest)
	}
	s := newDialogRasterSurface(key)
	if s != nil {
		s.used = dialogRasterCache.clock
		dialogRasterCache.entries[key] = s
		dialogRasterCache.bytes += len(s.pixels) + len(s.mask)
	}
	return s
}

func blendDialogRaster(dc uintptr, x, y int32, key dialogRasterKey, s *dialogRasterSurface) bool {
	if s == nil {
		return false
	}
	ok, _, _ := dialogPageAlphaBlend.Call(dc, uintptr(x), uintptr(y), uintptr(key.Width), uintptr(key.Height),
		s.dc, 0, 0, uintptr(key.Width), uintptr(key.Height), 0x01ff0000 /* AC_SRC_ALPHA, opacity 255 */)
	if key.Kind == 2 {
		dialogRasterFlush.Call()
	} // Mutable scratch DIB can next be used by another UI thread.
	return ok != 0
}

func drawDialogAARoundRect(dc uintptr, rect dialogButtonRect, top, bottom, border uint32, radius, stroke int32, fill bool) bool {
	if dc == 0 {
		return false
	}
	key := dialogRasterKey{Width: rect.Right - rect.Left, Height: rect.Bottom - rect.Top, Radius: radius, Stroke: stroke,
		Top: top, Bottom: bottom, Border: border, Fill: fill}
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	return blendDialogRaster(dc, rect.Left, rect.Top, key, acquireDialogRaster(key))
}

func drawDialogAAPolyline(dc uintptr, points []dialogComboPoint, color uint32, stroke int32) bool {
	if dc == 0 || len(points) < 2 || len(points) > 3 {
		return false
	}
	left, top, right, bottom := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		left = min(left, p.X)
		right = max(right, p.X)
		top = min(top, p.Y)
		bottom = max(bottom, p.Y)
	}
	inset := max(1, stroke/2+1)
	left -= inset
	top -= inset
	right += inset + 1
	bottom += inset + 1
	key := dialogRasterKey{Kind: 1, Width: right - left, Height: bottom - top, Stroke: max(1, stroke), Top: color, Count: len(points)}
	for i, p := range points {
		key.Points[i*2] = p.X - left
		key.Points[i*2+1] = p.Y - top
	}
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	return blendDialogRaster(dc, left, top, key, acquireDialogRaster(key))
}

func fillDialogAARoundedPart(dc uintptr, rect dialogFieldRect, width, height, radius int32, color uint32) bool {
	key := dialogRasterKey{Width: width, Height: height, Radius: radius, Top: color, Bottom: color, Fill: true,
		Clip: dialogButtonRect{Left: rect.Left, Top: rect.Top, Right: rect.Right, Bottom: rect.Bottom}}
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	return blendDialogRaster(dc, 0, 0, key, acquireDialogRaster(key))
}

// HRGNs are binary masks and cannot carry edge coverage. Leave child HWNDs
// rectangular; restore their outside corners from the actual aligned ancestor
// material, then draw the rounded border. No screenshot/desktop pixels are read.
func restoreDialogAACorners(hwnd, dc uintptr, width, height, radius int32, dark bool) bool {
	key := dialogRasterKey{Kind: 2, Width: width, Height: height, Radius: radius}
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	s := acquireDialogRaster(key)
	if s == nil {
		return false
	}
	rect := dialogButtonRect{Right: width, Bottom: height}
	if brush := dialogAncestorMaterialBrush(s.dc, hwnd); brush != 0 {
		dialogButtonFillRect.Call(s.dc, uintptr(unsafe.Pointer(&rect)), brush)
	} else {
		fillDialogButtonRect(s.dc, rect, dialogButtonColors(dark).Window, nil)
	}
	dialogRasterFlush.Call()
	for index, coverage := range s.mask {
		pixel := s.pixels[index*4:][:4]
		if coverage == 0 {
			clear(pixel)
			continue
		}
		for i := 0; i < 3; i++ {
			pixel[i] = byte((uint32(pixel[i])*uint32(coverage) + 127) / 255)
		}
		pixel[3] = coverage
	}
	return blendDialogRaster(dc, 0, 0, key, s)
}

// PaintRoundedControl shares the native dialog chrome rasterizer with custom
// recorder surfaces. Bounds/radius/stroke are physical pixels; text stays native.
func PaintRoundedControl(dc uintptr, bounds Rect, fill, border uint32, radius, stroke int32) bool {
	return drawDialogAARoundRect(dc, dialogButtonRect{Left: bounds.Left, Top: bounds.Top, Right: bounds.Right, Bottom: bounds.Bottom}, fill, fill, border, radius, stroke, true)
}
