//go:build windows

package osd

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/internal/design"
	"pccontroller.local/desktop/internal/graphics"
)

type dib struct {
	dc            win.HDC
	bitmap        win.HBITMAP
	old           win.HGDIOBJ
	pixels        []byte
	width, height int
}

func newDIB(w, h int) (*dib, error) {
	if w < 1 || h < 1 || int64(w)*int64(h) > 16<<20 {
		return nil, errors.New("OSD surface exceeds safe pixel bounds")
	}
	d := &dib{width: w, height: h, dc: win.CreateCompatibleDC(0)}
	if d.dc == 0 {
		return nil, errors.New("OSD CreateCompatibleDC failed")
	}
	info := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32}
	var bits unsafe.Pointer
	d.bitmap = win.CreateDIBSection(d.dc, &info, 0, &bits, 0, 0)
	if d.bitmap == 0 || bits == nil {
		d.close()
		return nil, errors.New("OSD CreateDIBSection failed")
	}
	d.old = win.SelectObject(d.dc, win.HGDIOBJ(d.bitmap))
	d.pixels = unsafe.Slice((*byte)(bits), w*h*4)
	return d, nil
}
func (d *dib) close() {
	if d == nil {
		return
	}
	if d.old != 0 {
		win.SelectObject(d.dc, d.old)
	}
	if d.bitmap != 0 {
		win.DeleteObject(win.HGDIOBJ(d.bitmap))
	}
	if d.dc != 0 {
		win.DeleteDC(d.dc)
	}
	d.pixels = nil
	d.dc = 0
	d.bitmap = 0
}

func newTextFont(size int, semibold bool) (win.HFONT, error) {
	var lf win.LOGFONT
	lf.LfHeight = -int32(size)
	lf.LfWeight = win.FW_NORMAL
	family := "Segoe UI"
	if semibold {
		lf.LfWeight = win.FW_SEMIBOLD
		// Generic Segoe UI + weight 600 maps to the Bold (700) style on GDI.
		// The distinct Semibold face must be selected explicitly.
		family = "Segoe UI Semibold"
	}
	lf.LfQuality = win.ANTIALIASED_QUALITY
	copy(lf.LfFaceName[:], windows.StringToUTF16(family))
	f := win.CreateFontIndirect(&lf)
	if f == 0 {
		return 0, errors.New("OSD font creation failed")
	}
	if semibold {
		dc := win.CreateCompatibleDC(0)
		if dc == 0 {
			win.DeleteObject(win.HGDIOBJ(f))
			return 0, errors.New("OSD font verification DC failed")
		}
		old := win.SelectObject(dc, win.HGDIOBJ(f))
		var metrics win.TEXTMETRIC
		ok := win.GetTextMetrics(dc, &metrics)
		win.SelectObject(dc, old)
		win.DeleteDC(dc)
		if !ok {
			win.DeleteObject(win.HGDIOBJ(f))
			return 0, errors.New("OSD font weight verification failed")
		}
		if metrics.TmWeight != win.FW_SEMIBOLD {
			// Older/missing-font installations get legible Regular, never an
			// unintended Bold substitution masquerading as Semibold.
			win.DeleteObject(win.HGDIOBJ(f))
			return newTextFont(size, false)
		}
	}
	return f, nil
}

func gdiTextImage(text string, size int, semibold bool, c color.RGBA) (*image.RGBA, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return image.NewRGBA(image.Rect(0, 0, 1, max(1, size))), nil
	}
	f, err := newTextFont(size, semibold)
	if err != nil {
		return nil, err
	}
	defer win.DeleteObject(win.HGDIOBJ(f))
	dc := win.CreateCompatibleDC(0)
	if dc == 0 {
		return nil, errors.New("OSD text DC failed")
	}
	defer win.DeleteDC(dc)
	old := win.SelectObject(dc, win.HGDIOBJ(f))
	defer win.SelectObject(dc, old)
	u, _ := windows.UTF16FromString(text)
	var extent win.SIZE
	if !win.GetTextExtentPoint32(dc, &u[0], int32(len(u)-1), &extent) {
		return nil, errors.New("OSD text measurement failed")
	}
	w, h := max(1, int(extent.CX)+4), max(size+8, int(extent.CY)+4)
	mask, err := newDIB(w, h)
	if err != nil {
		return nil, err
	}
	defer mask.close()
	fontBefore := win.SelectObject(mask.dc, win.HGDIOBJ(f))
	defer win.SelectObject(mask.dc, fontBefore)
	clear(mask.pixels)
	win.SetTextColor(mask.dc, 0xffffff)
	win.SetBkColor(mask.dc, 0)
	win.SetBkMode(mask.dc, win.TRANSPARENT)
	r := win.RECT{Right: int32(w), Bottom: int32(h)}
	flags := uint32(win.DT_SINGLELINE | win.DT_VCENTER | win.DT_NOPREFIX)
	for _, ch := range text {
		if unicode.Is(unicode.Arabic, ch) || unicode.Is(unicode.Hebrew, ch) {
			flags |= win.DT_RTLREADING
			break
		}
		if unicode.IsLetter(ch) {
			break
		}
	}
	win.DrawTextEx(mask.dc, &u[0], int32(len(u)-1), &r, flags, nil)
	gdiFlush.Call()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(mask.pixels); i += 4 {
		a := uint16(max(mask.pixels[i], max(mask.pixels[i+1], mask.pixels[i+2])))
		img.Pix[i] = byte(uint16(c.R) * a / 255)
		img.Pix[i+1] = byte(uint16(c.G) * a / 255)
		img.Pix[i+2] = byte(uint16(c.B) * a / 255)
		img.Pix[i+3] = byte(uint16(c.A) * a / 255)
	}
	return img, nil
}

// A standalone glyph is an icon, not a line of text: font ascent/descent and
// advance padding must not determine its position within a square hit target.
func glyphInkBounds(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	var ink image.Rectangle
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] != 0 {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return ink
}

func trimGlyph(img *image.RGBA) *image.RGBA {
	ink := glyphInkBounds(img)
	cropped := image.NewRGBA(image.Rect(0, 0, max(1, ink.Dx()), max(1, ink.Dy())))
	draw.Draw(cropped, cropped.Bounds(), img, ink.Min, draw.Src)
	return cropped
}

type rendering struct {
	textBackend          string
	base, frame          *image.RGBA
	surface              *dib
	title                *image.RGBA
	bodies               []*image.RGBA
	titleShadow          *image.RGBA
	bodyShadows          []*image.RGBA
	buttons              []*image.RGBA
	closeGlyph           *image.RGBA
	closeGlyphAt         image.Point
	hits                 []image.Rectangle
	scale                float64
	titleRect, closeRect image.Rectangle
	bodyRects            []image.Rectangle
	ink, muted, accent   color.RGBA
}

func (r *rendering) close() {
	if r != nil {
		r.surface.close()
	}
}
func rgba(s string) color.RGBA {
	var v uint32
	for _, b := range strings.TrimPrefix(s, "#") {
		v = v*16 + uint32(strings.IndexRune("0123456789abcdef", unicode.ToLower(b)))
	}
	return color.RGBA{byte(v >> 16), byte(v >> 8), byte(v), 255}
}

// rounded uses analytic distance coverage; no jagged region clipping or
// low-resolution scaling of corners. All coordinates are physical DPI pixels.
func rounded(img *image.RGBA, r image.Rectangle, radius float64, c color.RGBA) {
	paintRounded(img, r, radius, 0, c)
}
func paintRounded(img *image.RGBA, r image.Rectangle, radius, stroke float64, c color.RGBA) {
	clip := r.Inset(-1).Intersect(img.Bounds())
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	hx, hy := float64(r.Dx())/2-radius, float64(r.Dy())/2-radius
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			qx, qy := math.Abs(float64(x)+.5-cx)-hx, math.Abs(float64(y)+.5-cy)-hy
			distance := math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - radius
			coverage := min(1, max(0, .5-distance))
			if stroke > 0 {
				coverage -= min(1, max(0, .5-distance-stroke))
			}
			if coverage == 0 {
				continue
			}
			a := float64(c.A) / 255 * coverage
			i := img.PixOffset(x, y)
			img.Pix[i] = byte(float64(c.R)*a + float64(img.Pix[i])*(1-a))
			img.Pix[i+1] = byte(float64(c.G)*a + float64(img.Pix[i+1])*(1-a))
			img.Pix[i+2] = byte(float64(c.B)*a + float64(img.Pix[i+2])*(1-a))
			img.Pix[i+3] = byte(255*a + float64(img.Pix[i+3])*(1-a))
		}
	}
}

func prepareWithText(req Request, dpi, width int, icon image.Image, dark bool, text textRasterizer) (*rendering, error) {
	s := float64(dpi) / 96
	px := func(v int) int { return int(math.Round(float64(v) * s)) }
	width = min(px(540), width-px(32))
	if width < px(260) {
		return nil, errors.New("display work area is too narrow for OSD")
	}
	rows := (len(req.Actions) + 2) / 3
	lines := strings.Split(req.Text, "\n")
	extra := (len(lines) - 1) * 24
	height := px(106 + extra)
	if rows > 0 {
		height += px(rows*40 + 4)
	}
	r := &rendering{scale: s, textBackend: text.name(), base: image.NewRGBA(image.Rect(0, 0, width, height))}
	tokens := design.ControllerTokens().Colors.Dark
	if !dark {
		tokens = design.ControllerTokens().Colors.Light
	}
	r.ink = rgba(tokens.Text)
	r.muted = rgba(tokens.TextMuted)
	r.accent = rgba(tokens.Accent)
	transparent := req.Background == "transparent" || req.Background == "none"
	if transparent {
		r.muted = r.ink
	}
	switch req.Severity {
	case "success":
		r.accent = rgba(tokens.Success)
	case "warning":
		r.accent = rgba(tokens.Warm)
	case "error":
		r.accent = rgba(tokens.Danger)
	}
	card := image.Rect(px(8), px(5), width-px(8), height-px(9))
	glass := req.Background == "acrylic" || req.Background == "blur"
	if glass {
		bg := rgba(tokens.Surface)
		bg.A = 90
		rounded(r.base, r.base.Bounds(), float64(px(8)), bg)
	}
	if !transparent && !glass {
		for i := px(7); i > 0; i-- {
			paintRounded(r.base, card.Add(image.Pt(0, px(2))).Inset(-i), float64(px(18)+i), float64(i), color.RGBA{0, 0, 0, byte(2 + 8*(px(7)-i)/max(1, px(7)))})
		}
		border := rgba(tokens.StrokeHighlight)
		border.A = 180
		bg := rgba(tokens.Surface)
		bg.A = 246
		rounded(r.base, card.Inset(1), float64(px(18)-1), bg)
		paintRounded(r.base, card, float64(px(18)), 1, border)
		// One continuous surface, with a soft brand glow rather than a second slab.
		for y := card.Min.Y + 1; y < min(card.Max.Y, card.Min.Y+px(48)); y++ {
			mix := .06 * math.Pow(1-float64(y-card.Min.Y)/float64(px(48)), 2)
			for x := card.Min.X + 1; x < card.Max.X-1; x++ {
				i := r.base.PixOffset(x, y)
				if r.base.Pix[i+3] > 230 {
					r.base.Pix[i] = byte(float64(r.base.Pix[i])*(1-mix) + float64(r.accent.R)*mix)
					r.base.Pix[i+1] = byte(float64(r.base.Pix[i+1])*(1-mix) + float64(r.accent.G)*mix)
					r.base.Pix[i+2] = byte(float64(r.base.Pix[i+2])*(1-mix) + float64(r.accent.B)*mix)
				}
			}
		}
	}
	if icon != nil {
		bounds := icon.Bounds()
		factor := min(float64(px(42))/float64(bounds.Dx()), float64(px(42))/float64(bounds.Dy()))
		iw, ih := max(1, int(math.Round(float64(bounds.Dx())*factor))), max(1, int(math.Round(float64(bounds.Dy())*factor)))
		scaled, err := graphics.Resize(icon, iw, ih)
		if err != nil {
			return nil, err
		}
		at := image.Pt(px(29)+(px(42)-iw)/2, px(28)+(px(42)-ih)/2)
		draw.Draw(r.base, image.Rectangle{Min: at, Max: at.Add(image.Pt(iw, ih))}, scaled, image.Point{}, draw.Over)
	}
	closeSize := px(28)
	r.closeRect = image.Rect(width-px(17)-closeSize, px(15), width-px(17), px(15)+closeSize)
	// Close glyph remains subordinate to the semantic icon, not stretched to fill.
	closeText, err := text.image("×", float32(20*s), false, r.muted)
	if err != nil {
		return nil, err
	}
	r.closeGlyph = trimGlyph(closeText)
	r.closeGlyphAt = image.Pt(
		r.closeRect.Min.X+(r.closeRect.Dx()-r.closeGlyph.Bounds().Dx())/2,
		r.closeRect.Min.Y+(r.closeRect.Dy()-r.closeGlyph.Bounds().Dy())/2,
	)
	r.titleRect = image.Rect(px(88), px(23), width-px(58), px(48))
	r.title, err = text.image(req.Title, float32(16*s), true, r.ink)
	if err != nil {
		return nil, err
	}
	if transparent {
		r.titleShadow = textShadow(r.title, dark)
	}
	for i, line := range lines {
		body, bodyErr := text.image(line, float32(14*s), false, r.muted)
		if bodyErr != nil {
			return nil, bodyErr
		}
		r.bodies = append(r.bodies, body)
		if transparent {
			r.bodyShadows = append(r.bodyShadows, textShadow(body, dark))
		}
		r.bodyRects = append(r.bodyRects, image.Rect(px(88), px(49+i*24), width-px(28), px(77+i*24)))
	}
	for row := 0; row < rows; row++ {
		count := min(3, len(req.Actions)-row*3)
		cell := min(px(156), (width-px(48)-px(8)*(count-1))/count)
		left := width - px(24) - count*cell - (count-1)*px(8)
		for col := 0; col < count; col++ {
			index := row*3 + col
			box := image.Rect(left+col*(cell+px(8)), px(87+extra+row*40), left+col*(cell+px(8))+cell, px(119+extra+row*40))
			r.hits = append(r.hits, box)
			label := req.Actions[index].Label
			var im *image.RGBA
			for {
				im, err = text.image(label, float32(14*s), false, r.ink)
				if err != nil {
					return nil, err
				}
				if im.Bounds().Dx() <= cell-px(16) || len([]rune(label)) < 2 {
					break
				}
				chars := []rune(strings.TrimSuffix(label, "…"))
				label = string(chars[:len(chars)-1]) + "…"
			}
			r.buttons = append(r.buttons, im)
		}
	}
	r.frame = image.NewRGBA(r.base.Bounds())
	r.surface, err = newDIB(width, height)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// RenderPreview uses the actual renderer without creating a window or timer.
func RenderPreview(request Request, dpi int, dark bool, icon image.Image) (*image.RGBA, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI, err := enterNativeDPI()
	if err != nil {
		return nil, err
	}
	defer restoreDPI()
	request, err = Normalize(request)
	if err != nil {
		return nil, err
	}
	if dpi < 96 || dpi > 384 {
		return nil, errors.New("preview DPI must be 96..384")
	}
	r, err := prepare(request, dpi, 1920*dpi/96, icon, dark)
	if err != nil {
		return nil, err
	}
	defer r.close()
	return r.paint(0, request.ReduceMotion, nil), nil
}

func (r *rendering) paint(elapsed time.Duration, reduced bool, hover []float64) *image.RGBA {
	copy(r.frame.Pix, r.base.Pix)
	amount := func(i int) float64 {
		if i < len(hover) {
			return min(1, max(0, hover[i]))
		}
		return 0
	}
	if level := amount(len(r.hits)); level > 0 {
		c := r.muted
		c.A = byte(30 * level)
		rounded(r.frame, r.closeRect, 7*r.scale, c)
	}
	draw.Draw(r.frame, r.closeGlyph.Bounds().Add(r.closeGlyphAt).Intersect(r.closeRect), r.closeGlyph, image.Point{}, draw.Over)
	line := func(im, shadow *image.RGBA, box image.Rectangle) {
		offset := int(Marquee(elapsed, float64(im.Bounds().Dx()-box.Dx()), r.scale, reduced))
		if shadow != nil {
			draw.Draw(r.frame, box, shadow, image.Pt(offset-1, -1), draw.Over)
		}
		draw.Draw(r.frame, box, im, image.Pt(offset, 0), draw.Over)
		if im.Bounds().Dx() > box.Dx() {
			fade := max(1, int(14*r.scale))
			clip := box.Intersect(r.frame.Bounds())
			for y := clip.Min.Y; y < clip.Max.Y; y++ {
				for x := clip.Min.X; x < clip.Max.X; x++ {
					factor := 1.0
					if offset > 0 {
						factor = min(factor, float64(x-box.Min.X)/float64(fade))
					}
					if offset+box.Dx() < im.Bounds().Dx() {
						factor = min(factor, float64(box.Max.X-1-x)/float64(fade))
					}
					factor = max(0, factor)
					if factor < 1 {
						i := r.frame.PixOffset(x, y)
						for c := 0; c < 4; c++ {
							r.frame.Pix[i+c] = byte(float64(r.base.Pix[i+c])*(1-factor) + float64(r.frame.Pix[i+c])*factor)
						}
					}
				}
			}
		}
	}
	line(r.title, r.titleShadow, r.titleRect)
	for i, body := range r.bodies {
		var shadow *image.RGBA
		if len(r.bodyShadows) > i {
			shadow = r.bodyShadows[i]
		}
		line(body, shadow, r.bodyRects[i])
	}
	for i, box := range r.hits {
		stroke := r.muted
		level := amount(i)
		stroke.A = byte(65 + 25*level)
		rounded(r.frame, box, 7*r.scale, stroke)
		fill := color.RGBA{65, 60, 75, 220}
		if r.ink.R < 128 {
			fill = color.RGBA{224, 219, 233, 245}
		}
		// Keep surface opacity stable: the old hover replaced the opaque fill
		// with a near-transparent tint, producing a conspicuous flash.
		mix := .20 * level
		fill.R = byte(float64(fill.R)*(1-mix) + float64(r.accent.R)*mix)
		fill.G = byte(float64(fill.G)*(1-mix) + float64(r.accent.G)*mix)
		fill.B = byte(float64(fill.B)*(1-mix) + float64(r.accent.B)*mix)
		rounded(r.frame, box.Inset(1), max(1, 7*r.scale-1), fill)
		im := r.buttons[i]
		at := image.Pt(box.Min.X+(box.Dx()-im.Bounds().Dx())/2, box.Min.Y+(box.Dy()-im.Bounds().Dy())/2)
		draw.Draw(r.frame, im.Bounds().Add(at).Intersect(box), im, image.Point{}, draw.Over)
	}
	return r.frame
}

func textShadow(im *image.RGBA, dark bool) *image.RGBA {
	shadow := image.NewRGBA(im.Bounds())
	for i := 0; i < len(im.Pix); i += 4 {
		a := byte(uint16(im.Pix[i+3]) * 3 / 4)
		shadow.Pix[i+3] = a
		if !dark {
			shadow.Pix[i] = a
			shadow.Pix[i+1] = a
			shadow.Pix[i+2] = a
		}
	}
	return shadow
}

func (r *rendering) offsets(elapsed time.Duration, reduced bool) []int {
	result := []int{int(Marquee(elapsed, float64(r.title.Bounds().Dx()-r.titleRect.Dx()), r.scale, reduced))}
	for i, body := range r.bodies {
		result = append(result, int(Marquee(elapsed, float64(body.Bounds().Dx()-r.bodyRects[i].Dx()), r.scale, reduced)))
	}
	return result
}

func (r *rendering) scrolls() bool {
	if r.title.Bounds().Dx() > r.titleRect.Dx() {
		return true
	}
	for i, body := range r.bodies {
		if body.Bounds().Dx() > r.bodyRects[i].Dx() {
			return true
		}
	}
	return false
}
