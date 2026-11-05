//go:build windows

package ui

import (
	"image"
	"testing"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func TestSurfaceTextMaskProducesGrayscaleAntialiasCoverage(t *testing.T) {
	t.Parallel()

	fontKey := surfaceFontKey{
		family: "Segoe UI", pointSize: 22,
		style: walk.FontBold, dpi: 96,
	}
	logFont := surfaceLOGFONT(fontKey)
	font := win.CreateFontIndirect(&logFont)
	if font == 0 {
		t.Fatal("CreateFontIndirect failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(font))

	key := surfaceTextMaskKey{
		text: "LIGHT IS OFF", font: fontKey,
		width: 280, height: 64,
		format: walk.TextCenter | walk.TextVCenter |
			walk.TextSingleLine | walk.TextNoPrefix,
	}
	mask, err := renderSurfaceTextMask(key, font)
	if err != nil {
		t.Fatal(err)
	}

	var opaque, partial int
	for _, alpha := range mask.Pix {
		switch {
		case alpha == 255:
			opaque++
		case alpha > 0:
			partial++
		}
	}
	if opaque == 0 {
		t.Fatal("real GDI glyph mask has no opaque pixels")
	}
	if partial == 0 {
		t.Fatal("real GDI glyph mask has no antialiased coverage")
	}

	target := image.NewRGBA(mask.Bounds())
	color := walk.RGB(145, 166, 178)
	compositeSurfaceTextMask(target, mask, image.Point{}, color)
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			alpha := mask.AlphaAt(x, y).A
			if alpha == 0 {
				continue
			}
			got := target.RGBAAt(x, y)
			if got.R != byte(uint16(145)*uint16(alpha)/255) ||
				got.G != byte(uint16(166)*uint16(alpha)/255) ||
				got.B != byte(uint16(178)*uint16(alpha)/255) ||
				got.A != alpha {
				t.Fatalf(
					"real glyph edge at (%d,%d) is not exact premultiplied text color: %#v alpha=%d",
					x,
					y,
					got,
					alpha,
				)
			}
		}
	}
}

func TestSurfaceTextMaskCompositesExactPremultipliedColor(t *testing.T) {
	t.Parallel()

	mask := image.NewAlpha(image.Rect(0, 0, 4, 1))
	copy(mask.Pix, []byte{0, 31, 128, 255})
	target := image.NewRGBA(image.Rect(0, 0, 4, 1))
	color := walk.RGB(145, 166, 178)
	compositeSurfaceTextMask(target, mask, image.Point{}, color)

	for x, alpha := range mask.Pix {
		got := target.RGBAAt(x, 0)
		wantRed := byte(uint16(145) * uint16(alpha) / 255)
		wantGreen := byte(uint16(166) * uint16(alpha) / 255)
		wantBlue := byte(uint16(178) * uint16(alpha) / 255)
		if got.R != wantRed || got.G != wantGreen ||
			got.B != wantBlue || got.A != alpha {
			t.Fatalf(
				"x=%d rgba=(%d,%d,%d,%d), want (%d,%d,%d,%d)",
				x,
				got.R,
				got.G,
				got.B,
				got.A,
				wantRed,
				wantGreen,
				wantBlue,
				alpha,
			)
		}
	}
}
