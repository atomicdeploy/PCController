//go:build windows && amd64

package osd

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"strings"
	"syscall"
	"unicode"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Windows SDK interfaces: IDWriteFactory, IDWriteTextFormat/TextLayout and
// ID2D1Factory/ID2D1DCRenderTarget. This small ABI adapter is deliberately amd64:
// the Windows x64 float/8-byte aggregate calling convention is not ARM64's.
// Other architectures retain the tested GDI path, with no extra DLL shipped.
type textCOM struct{ vtable *[67]uintptr }

//go:uintptrescapes
func (p *textCOM) call(slot int, args ...uintptr) uintptr {
	values := append([]uintptr{uintptr(unsafe.Pointer(p))}, args...)
	result, _, _ := syscall.SyscallN(p.vtable[slot], values...)
	runtime.KeepAlive(p)
	return result
}
func (p *textCOM) release() {
	if p != nil {
		p.call(2)
	}
}
func textHR(operation string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("OSD %s: HRESULT 0x%08x", operation, uint32(result))
	}
	return nil
}
func textFloat(value float32) uintptr { return uintptr(math.Float32bits(value)) }

type textTargetProperties struct {
	Type, Format, AlphaMode uint32
	DPIX, DPIY              float32
	Usage, MinLevel         uint32
}
type textMetrics struct {
	Left, Top, Width, WidthTrailing, Height, LayoutWidth, LayoutHeight float32
	BidiDepth, Lines                                                   uint32
}
type textOverhang struct{ Left, Top, Right, Bottom float32 }
type textFormatKey struct {
	size   float32
	weight uint32
}
type directWriteRasterizer struct {
	write, factory, target, brush *textCOM
	formats                       map[textFormatKey]*textCOM
	weights                       map[uint32]uint32
}

var (
	textWriteFactory = windows.NewLazySystemDLL("dwrite.dll").NewProc("DWriteCreateFactory")
	textD2DFactory   = windows.NewLazySystemDLL("d2d1.dll").NewProc("D2D1CreateFactory")
	iidTextWrite     = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
	iidTextD2D       = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
)

func newDirectWriteRasterizer() (textRasterizer, error) {
	if err := textWriteFactory.Find(); err != nil {
		return nil, err
	}
	if err := textD2DFactory.Find(); err != nil {
		return nil, err
	}
	r := &directWriteRasterizer{formats: make(map[textFormatKey]*textCOM), weights: make(map[uint32]uint32)}
	ok := false
	defer func() {
		if !ok {
			r.close()
		}
	}()
	hr, _, _ := textWriteFactory.Call(0, uintptr(unsafe.Pointer(&iidTextWrite)), uintptr(unsafe.Pointer(&r.write)))
	if err := textHR("DWriteCreateFactory", hr); err != nil {
		return nil, err
	}
	hr, _, _ = textD2DFactory.Call(0, uintptr(unsafe.Pointer(&iidTextD2D)), 0, uintptr(unsafe.Pointer(&r.factory)))
	if err := textHR("D2D1CreateFactory", hr); err != nil {
		return nil, err
	}
	// SOFTWARE + BGRA8 premultiplied, physical-pixel units. It remains usable on
	// RDP/older GPUs; only preparation uses Direct2D, not the animation frame pump.
	properties := textTargetProperties{Type: 1, Format: 87, AlphaMode: 1, DPIX: 96, DPIY: 96}
	if err := textHR("CreateDCRenderTarget", r.factory.call(16, uintptr(unsafe.Pointer(&properties)), uintptr(unsafe.Pointer(&r.target)))); err != nil {
		return nil, err
	}
	// RGB ClearType assumes an opaque, stationary background. Explicit grayscale
	// is essential for correct per-pixel alpha during fades, blur and movement.
	r.target.call(34, 2)
	white := [4]float32{1, 1, 1, 1}
	if err := textHR("CreateSolidColorBrush", r.target.call(8, uintptr(unsafe.Pointer(&white)), 0, uintptr(unsafe.Pointer(&r.brush)))); err != nil {
		return nil, err
	}
	ok = true
	return r, nil
}
func (r *directWriteRasterizer) name() string { return "directwrite" }
func (r *directWriteRasterizer) close() {
	for _, format := range r.formats {
		format.release()
	}
	r.formats = nil
	r.brush.release()
	r.target.release()
	r.factory.release()
	r.write.release()
	r.brush, r.target, r.factory, r.write = nil, nil, nil, nil
}

// Verify the actual primary-family style, not merely the requested format
// weight. This prevents repeating GDI's 600-request/700-substitution regression.
func (r *directWriteRasterizer) fontWeight(wanted uint32) (uint32, error) {
	if weight, ok := r.weights[wanted]; ok {
		return weight, nil
	}
	var collection, family, font *textCOM
	if err := textHR("GetSystemFontCollection", r.write.call(3, uintptr(unsafe.Pointer(&collection)), 0)); err != nil {
		return 0, err
	}
	defer collection.release()
	name := windows.StringToUTF16("Segoe UI")
	var index, exists uint32
	if err := textHR("FindFamilyName", collection.call(5, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&exists)))); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, errors.New("OSD Segoe UI family is unavailable")
	}
	if err := textHR("GetFontFamily", collection.call(4, uintptr(index), uintptr(unsafe.Pointer(&family)))); err != nil {
		return 0, err
	}
	defer family.release()
	if err := textHR("GetFirstMatchingFont", family.call(7, uintptr(wanted), 5, 0, uintptr(unsafe.Pointer(&font)))); err != nil {
		return 0, err
	}
	defer font.release()
	weight := uint32(font.call(4))
	if weight != wanted {
		if wanted == 600 {
			fallback, err := r.fontWeight(400)
			if err == nil {
				r.weights[wanted] = fallback
			}
			return fallback, err
		}
		return 0, fmt.Errorf("OSD regular font substituted with weight %d", weight)
	}
	r.weights[wanted] = weight
	return weight, nil
}

func (r *directWriteRasterizer) format(size float32, semibold bool) (*textCOM, error) {
	weight := uint32(400)
	if semibold {
		weight = 600
	}
	weight, err := r.fontWeight(weight)
	if err != nil {
		return nil, err
	}
	key := textFormatKey{size, weight}
	if format := r.formats[key]; format != nil {
		return format, nil
	}
	// Monitor/DPI changes must not grow a presenter's format cache indefinitely.
	// All layouts are released before another image is prepared on this thread.
	if len(r.formats) >= 32 {
		for _, format := range r.formats {
			format.release()
		}
		clear(r.formats)
	}
	family, locale := windows.StringToUTF16("Segoe UI"), windows.StringToUTF16("en-us")
	var format *textCOM
	if err := textHR("CreateTextFormat", r.write.call(15, uintptr(unsafe.Pointer(&family[0])), 0, uintptr(weight), 0, 5, textFloat(size), uintptr(unsafe.Pointer(&locale[0])), uintptr(unsafe.Pointer(&format)))); err != nil {
		return nil, err
	}
	if err := textHR("SetWordWrapping", format.call(5, 1)); err != nil {
		format.release()
		return nil, err
	}
	r.formats[key] = format
	return format, nil
}

func (r *directWriteRasterizer) image(text string, size float32, semibold bool, c color.RGBA) (*image.RGBA, error) {
	text = strings.Join(strings.Fields(text), " ")
	if size < 1 || size > 128 || math.IsNaN(float64(size)) {
		return nil, errors.New("OSD invalid text size")
	}
	if text == "" {
		return image.NewRGBA(image.Rect(0, 0, 1, int(math.Ceil(float64(size))))), nil
	}
	format, err := r.format(size, semibold)
	if err != nil {
		return nil, err
	}
	u := windows.StringToUTF16(text)
	var layout *textCOM
	if err := textHR("CreateTextLayout", r.write.call(18, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(format)), textFloat(262144), textFloat(512), uintptr(unsafe.Pointer(&layout)))); err != nil {
		return nil, err
	}
	defer layout.release()
	for _, ch := range text {
		if unicode.Is(unicode.Arabic, ch) || unicode.Is(unicode.Hebrew, ch) {
			if err := textHR("SetReadingDirection", layout.call(6, 1)); err != nil {
				return nil, err
			}
			break
		}
		if unicode.IsLetter(ch) {
			break
		}
	}
	var metrics textMetrics
	if err := textHR("GetMetrics", layout.call(60, uintptr(unsafe.Pointer(&metrics)))); err != nil {
		return nil, err
	}
	if err := textHR("SetMaxWidth", layout.call(28, textFloat(max(1, metrics.WidthTrailing)))); err != nil {
		return nil, err
	}
	if err := textHR("GetMetrics", layout.call(60, uintptr(unsafe.Pointer(&metrics)))); err != nil {
		return nil, err
	}
	var overhang textOverhang
	if err := textHR("GetOverhangMetrics", layout.call(61, uintptr(unsafe.Pointer(&overhang)))); err != nil {
		return nil, err
	}
	left, top := max(0, overhang.Left), max(0, overhang.Top)
	w := int(math.Ceil(float64(metrics.WidthTrailing+left+max(0, overhang.Right)))) + 4
	h := max(int(math.Round(float64(size)))+8, int(math.Ceil(float64(metrics.Height+top+max(0, overhang.Bottom))))+4)
	maximum := int(r.target.call(55))
	if w < 1 || w > maximum || h > maximum {
		return nil, errors.New("OSD text exceeds Direct2D surface bounds")
	}
	bitmap, err := newDIB(w, h)
	if err != nil {
		return nil, err
	}
	defer bitmap.close()
	bounds := win.RECT{Right: int32(w), Bottom: int32(h)}
	if err := textHR("BindDC", r.target.call(57, uintptr(bitmap.dc), uintptr(unsafe.Pointer(&bounds)))); err != nil {
		return nil, err
	}
	r.target.call(48) // BeginDraw
	clearColor := [4]float32{}
	r.target.call(47, uintptr(unsafe.Pointer(&clearColor)))
	// POINT_2F is passed as one 8-byte aggregate on Windows amd64.
	origin := uint64(math.Float32bits(2+left-metrics.Left)) | uint64(math.Float32bits(2+top-metrics.Top))<<32
	r.target.call(28, uintptr(origin), uintptr(unsafe.Pointer(layout)), uintptr(unsafe.Pointer(r.brush)), 0)
	if err := textHR("EndDraw", r.target.call(49, 0, 0)); err != nil {
		return nil, err
	}
	gdiFlush.Call()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(bitmap.pixels); i += 4 {
		// Direct2D supplies actual coverage alpha, not max(R,G,B) extracted
		// from a GDI black matte. Tint once, with correct premultiplication.
		a := uint16(bitmap.pixels[i+3]) * uint16(c.A) / 255
		img.Pix[i] = byte(uint16(c.R) * a / 255)
		img.Pix[i+1] = byte(uint16(c.G) * a / 255)
		img.Pix[i+2] = byte(uint16(c.B) * a / 255)
		img.Pix[i+3] = byte(a)
	}
	return img, nil
}
