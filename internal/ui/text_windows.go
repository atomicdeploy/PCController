//go:build windows

package ui

import (
	"fmt"
	"image"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

const maximumSurfaceTextMasks = 128

type surfaceFontKey struct {
	family    string
	pointSize int
	style     walk.FontStyle
	dpi       int
}

type surfaceTextMode uint8

const (
	surfaceTextDirect surfaceTextMode = iota
	surfaceTextCapture
	surfaceTextSuppress
)

type surfaceTextMaskKey struct {
	text   string
	font   surfaceFontKey
	width  int
	height int
	format walk.DrawTextFormat
}

type surfaceTextRun struct {
	maskKey surfaceTextMaskKey
	font    win.HFONT
	color   walk.Color
	bounds  walk.Rectangle
}

// drawSurfaceText captures layered-window text instead of drawing it into the
// black and white recovery passes. GDI's grayscale font smoothing is
// gamma-dependent: its edge pixels over black and white are not a linear
// source-over pair, so inferring alpha from those two colors creates a bright
// or dark fringe. Captured runs are later rasterized once as white glyphs over
// black, treating that grayscale result only as a coverage mask.
func (s *Shell) drawSurfaceText(
	canvas *walk.Canvas,
	text string,
	font *walk.Font,
	color walk.Color,
	bounds walk.Rectangle,
	format walk.DrawTextFormat,
) error {
	if canvas == nil || font == nil {
		return fmt.Errorf("ui: text canvas and font are required")
	}
	if !s.usesPerPixelForeground() {
		return canvas.DrawTextPixels(text, font, color, bounds, format)
	}
	if s.surfaceTextMode == surfaceTextSuppress {
		return nil
	}
	hfont, err := s.surfaceFont(font, canvas.DPI())
	if err != nil {
		return err
	}
	if s.surfaceTextMode == surfaceTextCapture {
		if text == "" || bounds.Width <= 0 || bounds.Height <= 0 {
			return nil
		}
		s.surfaceTextRuns = append(s.surfaceTextRuns, surfaceTextRun{
			maskKey: surfaceTextMaskKey{
				text:  text,
				font:  surfaceFontKeyFor(font, canvas.DPI()),
				width: bounds.Width, height: bounds.Height,
				format: format,
			},
			font: hfont, color: color, bounds: bounds,
		})
		return nil
	}
	hdc := canvas.HDC()
	previous := win.SelectObject(hdc, win.HGDIOBJ(hfont))
	if previous == 0 {
		return fmt.Errorf("SelectObject(grayscale font) failed")
	}
	defer win.SelectObject(hdc, previous)

	previousColor := win.SetTextColor(hdc, win.COLORREF(color))
	if previousColor == win.CLR_INVALID {
		return fmt.Errorf("SetTextColor failed")
	}
	defer win.SetTextColor(hdc, previousColor)

	previousMode := win.SetBkMode(hdc, win.TRANSPARENT)
	if previousMode == 0 {
		return fmt.Errorf("SetBkMode(TRANSPARENT) failed")
	}
	defer win.SetBkMode(hdc, previousMode)

	rect := win.RECT{
		Left:   int32(bounds.X),
		Top:    int32(bounds.Y),
		Right:  int32(bounds.X + bounds.Width),
		Bottom: int32(bounds.Y + bounds.Height),
	}
	if win.DrawTextEx(
		hdc,
		syscall.StringToUTF16Ptr(text),
		-1,
		&rect,
		uint32(format)|win.DT_EDITCONTROL,
		nil,
	) == 0 {
		return fmt.Errorf("DrawTextEx(grayscale) failed")
	}
	return nil
}

func (s *Shell) surfaceFont(font *walk.Font, dpi int) (win.HFONT, error) {
	key := surfaceFontKeyFor(font, dpi)
	if cached := s.antialiasFonts[key]; cached != 0 {
		return cached, nil
	}
	logFont := surfaceLOGFONT(key)
	hfont := win.CreateFontIndirect(&logFont)
	if hfont == 0 {
		return 0, fmt.Errorf("CreateFontIndirect(grayscale) failed")
	}
	if s.antialiasFonts == nil {
		s.antialiasFonts = make(map[surfaceFontKey]win.HFONT)
	}
	s.antialiasFonts[key] = hfont
	return hfont, nil
}

func surfaceFontKeyFor(font *walk.Font, dpi int) surfaceFontKey {
	if dpi < 1 {
		dpi = 96
	}
	return surfaceFontKey{
		family:    font.Family(),
		pointSize: font.PointSize(),
		style:     font.Style(),
		dpi:       dpi,
	}
}

func surfaceLOGFONT(key surfaceFontKey) win.LOGFONT {
	var font win.LOGFONT
	font.LfHeight = -win.MulDiv(
		int32(key.pointSize),
		int32(key.dpi),
		72,
	)
	if key.style&walk.FontBold != 0 {
		font.LfWeight = win.FW_BOLD
	} else {
		font.LfWeight = win.FW_NORMAL
	}
	if key.style&walk.FontItalic != 0 {
		font.LfItalic = 1
	}
	if key.style&walk.FontUnderline != 0 {
		font.LfUnderline = 1
	}
	if key.style&walk.FontStrikeOut != 0 {
		font.LfStrikeOut = 1
	}
	font.LfCharSet = win.DEFAULT_CHARSET
	font.LfOutPrecision = win.OUT_TT_PRECIS
	font.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
	font.LfQuality = win.ANTIALIASED_QUALITY
	font.LfPitchAndFamily = win.VARIABLE_PITCH | win.FF_SWISS
	copy(font.LfFaceName[:], syscall.StringToUTF16(key.family))
	return font
}

func (s *Shell) surfaceTextMask(run surfaceTextRun) (*image.Alpha, error) {
	if cached := s.surfaceTextMasks[run.maskKey]; cached != nil {
		return cached, nil
	}
	mask, err := renderSurfaceTextMask(run.maskKey, run.font)
	if err != nil {
		return nil, err
	}
	if s.surfaceTextMasks == nil {
		s.surfaceTextMasks = make(map[surfaceTextMaskKey]*image.Alpha)
	} else if len(s.surfaceTextMasks) >= maximumSurfaceTextMasks {
		// Width participates in DrawText alignment, so a resize can otherwise
		// grow this cache without bound. Text masks are cheap to recreate.
		clear(s.surfaceTextMasks)
	}
	s.surfaceTextMasks[run.maskKey] = mask
	return mask, nil
}

func renderSurfaceTextMask(
	key surfaceTextMaskKey,
	font win.HFONT,
) (*image.Alpha, error) {
	if font == 0 || key.width <= 0 || key.height <= 0 {
		return nil, fmt.Errorf("ui: invalid surface text mask")
	}
	hdc := win.CreateCompatibleDC(0)
	if hdc == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC(text mask) failed")
	}
	defer win.DeleteDC(hdc)

	header := win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       int32(key.width),
		BiHeight:      -int32(key.height),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
		BiSizeImage:   uint32(key.width * key.height * 4),
	}
	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(
		hdc,
		&header,
		win.DIB_RGB_COLORS,
		&bits,
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection(text mask) failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	pixels := unsafe.Slice((*byte)(bits), key.width*key.height*4)
	clear(pixels)

	previousFont := win.SelectObject(hdc, win.HGDIOBJ(font))
	if previousFont == 0 {
		return nil, fmt.Errorf("SelectObject(text mask font) failed")
	}
	defer win.SelectObject(hdc, previousFont)
	previousBitmap := win.SelectObject(hdc, win.HGDIOBJ(bitmap))
	if previousBitmap == 0 {
		return nil, fmt.Errorf("SelectObject(text mask bitmap) failed")
	}
	defer win.SelectObject(hdc, previousBitmap)

	previousColor := win.SetTextColor(hdc, win.COLORREF(0x00ffffff))
	if previousColor == win.CLR_INVALID {
		return nil, fmt.Errorf("SetTextColor(text mask) failed")
	}
	defer win.SetTextColor(hdc, previousColor)
	previousMode := win.SetBkMode(hdc, win.TRANSPARENT)
	if previousMode == 0 {
		return nil, fmt.Errorf("SetBkMode(text mask) failed")
	}
	defer win.SetBkMode(hdc, previousMode)

	rect := win.RECT{
		Right:  int32(key.width),
		Bottom: int32(key.height),
	}
	if win.DrawTextEx(
		hdc,
		syscall.StringToUTF16Ptr(key.text),
		-1,
		&rect,
		uint32(key.format)|win.DT_EDITCONTROL,
		nil,
	) == 0 {
		return nil, fmt.Errorf("DrawTextEx(text mask) failed")
	}
	win.GdiFlush()

	mask := image.NewAlpha(image.Rect(0, 0, key.width, key.height))
	for y := 0; y < key.height; y++ {
		sourceRow := y * key.width * 4
		maskRow := y * mask.Stride
		for x := 0; x < key.width; x++ {
			offset := sourceRow + x*4
			coverage := pixels[offset]
			if pixels[offset+1] > coverage {
				coverage = pixels[offset+1]
			}
			if pixels[offset+2] > coverage {
				coverage = pixels[offset+2]
			}
			mask.Pix[maskRow+x] = coverage
		}
	}
	runtime.KeepAlive(&header)
	runtime.KeepAlive(bits)
	return mask, nil
}

func compositeSurfaceTextMask(
	target *image.RGBA,
	mask *image.Alpha,
	location image.Point,
	color walk.Color,
) {
	if target == nil || mask == nil {
		return
	}
	red := float64(byte(uint32(color)))
	green := float64(byte(uint32(color) >> 8))
	blue := float64(byte(uint32(color) >> 16))
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			alpha := mask.Pix[mask.PixOffset(x, y)]
			if alpha == 0 {
				continue
			}
			compositeStraightPixel(
				target,
				location.X+x-mask.Bounds().Min.X,
				location.Y+y-mask.Bounds().Min.Y,
				red,
				green,
				blue,
				float64(alpha),
			)
		}
	}
}

func (s *Shell) compositeSurfaceTextRuns(
	target *image.RGBA,
	runs []surfaceTextRun,
) error {
	for _, run := range runs {
		mask, err := s.surfaceTextMask(run)
		if err != nil {
			return fmt.Errorf("render %q: %w", run.maskKey.text, err)
		}
		compositeSurfaceTextMask(
			target,
			mask,
			image.Pt(run.bounds.X, run.bounds.Y),
			run.color,
		)
	}
	return nil
}

func (s *Shell) disposeSurfaceFonts() {
	for key, font := range s.antialiasFonts {
		if font != 0 {
			win.DeleteObject(win.HGDIOBJ(font))
		}
		delete(s.antialiasFonts, key)
	}
	s.antialiasFonts = nil
	s.surfaceTextMasks = nil
	s.surfaceTextRuns = nil
}
