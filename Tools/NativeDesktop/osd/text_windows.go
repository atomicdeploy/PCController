//go:build windows

package osd

import (
	"image"
	"image/color"
	"math"
)

// Rasterizers belong to one locked OS thread. A live presenter retains its
// target and bounded format cache; previews create a short-lived session.
// Animation composites cached images, never calling a font API per frame.
type textRasterizer interface {
	image(string, float32, bool, color.RGBA) (*image.RGBA, error)
	close()
	name() string
}

type gdiRasterizer struct{}

func (gdiRasterizer) image(text string, size float32, semibold bool, c color.RGBA) (*image.RGBA, error) {
	return gdiTextImage(text, int(math.Round(float64(size))), semibold, c)
}
func (gdiRasterizer) close()       {}
func (gdiRasterizer) name() string { return "gdi" }

func prepare(req Request, dpi, width int, icon image.Image, dark bool) (*rendering, error) {
	if text, err := newDirectWriteRasterizer(); err == nil {
		defer text.close()
		return preparePreferredText(req, dpi, width, icon, dark, text)
	}
	return prepareWithText(req, dpi, width, icon, dark, gdiRasterizer{})
}

func preparePreferredText(req Request, dpi, width int, icon image.Image, dark bool, text textRasterizer) (*rendering, error) {
	if result, err := prepareWithText(req, dpi, width, icon, dark, text); err == nil {
		return result, nil
	}
	// Retry the entire card, not individual glyphs: mixing metrics/rasterizers
	// after a device/API failure would shift labels within the same presentation.
	return prepareWithText(req, dpi, width, icon, dark, gdiRasterizer{})
}
