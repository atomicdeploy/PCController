//go:build windows

package ui

import (
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/lxn/walk"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// Coordinates are normalized sample positions, not integer device pixels.
// Invalid points break the path; antialiasing never bridges missing telemetry.
type controllerChartPoint struct {
	x, y  float64
	valid bool
}

type controllerChartRaster struct {
	width, height, dpi int
	ink                walk.Color
	points             []controllerChartPoint
	bitmap             *walk.Bitmap
}

func controllerChartStroke(dpi int) float64 {
	return 1.8 * float64(max(96, dpi)) / 96
}

func rasterControllerChart(width, height, dpi int, ink walk.Color, points []controllerChartPoint) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, target, target.Bounds())
	scanner.SetColor(color.RGBA{R: byte(ink), G: byte(ink >> 8), B: byte(ink >> 16), A: 255})
	stroke := controllerChartStroke(dpi)
	path := rasterx.NewStroker(width, height, scanner)
	path.SetStroke(fixed.Int26_6(math.Round(stroke*64)), 4<<6, rasterx.RoundCap, rasterx.RoundCap, rasterx.RoundGap, rasterx.Round)
	inset := stroke/2 + 0.5
	spanX, spanY := math.Max(0, float64(width)-2*inset), math.Max(0, float64(height)-2*inset)
	toFixed := func(p controllerChartPoint) fixed.Point26_6 {
		return fixed.Point26_6{X: fixed.Int26_6(math.Round((inset + p.x*spanX) * 64)), Y: fixed.Int26_6(math.Round((inset + p.y*spanY) * 64))}
	}
	count := 0
	var first fixed.Point26_6
	stop := func() {
		if count == 0 {
			return
		}
		if count == 1 {
			// A tiny segment yields the same round coverage for an isolated
			// observation, without inventing a link to another sample.
			path.Line(fixed.Point26_6{X: first.X + 1, Y: first.Y})
		}
		path.Stop(false)
		count = 0
	}
	for _, point := range points {
		if !point.valid || math.IsNaN(point.x) || math.IsNaN(point.y) || math.IsInf(point.x, 0) || math.IsInf(point.y, 0) {
			stop()
			continue
		}
		point.x, point.y = math.Max(0, math.Min(1, point.x)), math.Max(0, math.Min(1, point.y))
		p := toFixed(point)
		if count == 0 {
			first = p
			path.Start(p)
		} else {
			path.Line(p)
		}
		count++
	}
	stop()
	path.Draw()
	return target
}

func (s *Shell) drawControllerChart(c *walk.Canvas, slot int, bounds walk.Rectangle, ink walk.Color, points []controllerChartPoint) error {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}
	if s.controllerChartRasters == nil {
		s.controllerChartRasters = make(map[int]*controllerChartRaster)
	}
	dpi := c.DPI()
	cached := s.controllerChartRasters[slot]
	if cached == nil || cached.width != bounds.Width || cached.height != bounds.Height || cached.dpi != dpi || cached.ink != ink || !slices.Equal(cached.points, points) {
		rgba := rasterControllerChart(bounds.Width, bounds.Height, dpi, ink, points)
		bitmap, err := walk.NewBitmapFromImageForDPI(rgba, dpi)
		if err != nil {
			return err
		}
		if cached != nil {
			cached.bitmap.Dispose()
		}
		cached = &controllerChartRaster{width: bounds.Width, height: bounds.Height, dpi: dpi, ink: ink, points: slices.Clone(points), bitmap: bitmap}
		s.controllerChartRasters[slot] = cached
	}
	return c.DrawImagePixels(cached.bitmap, walk.Point{X: bounds.X, Y: bounds.Y})
}

func (s *Shell) disposeControllerChartRasters() {
	for _, cached := range s.controllerChartRasters {
		cached.bitmap.Dispose()
	}
	s.controllerChartRasters = nil
}
