//go:build windows

package ui

import (
	"math"
	"testing"

	"github.com/lxn/walk"
)

func TestControllerChartHasSubpixelCoverageAtEachDPI(t *testing.T) {
	for _, dpi := range []int{96, 144, 192} {
		raster := rasterControllerChart(160, 60, dpi, walk.RGB(167, 139, 250), []controllerChartPoint{{x: 0, y: 0.2, valid: true}, {x: 1, y: 0.8, valid: true}})
		partial, opaque := 0, 0
		for i := 0; i < len(raster.Pix); i += 4 {
			a := raster.Pix[i+3]
			if a > 0 && a < 255 {
				partial++
			}
			if a == 255 {
				opaque++
			}
			if raster.Pix[i] > a || raster.Pix[i+1] > a || raster.Pix[i+2] > a {
				t.Fatal("chart is not premultiplied; acrylic edges would fringe")
			}
		}
		if partial < 150 || opaque < 20 {
			t.Fatalf("dpi=%d: missing antialiasing/solid stroke: partial=%d opaque=%d", dpi, partial, opaque)
		}
	}
}

func TestControllerChartPreservesGapsAndIsolatedSamples(t *testing.T) {
	raster := rasterControllerChart(160, 60, 96, walk.RGB(115, 198, 154), []controllerChartPoint{
		{x: 0, y: 0.5, valid: true}, {x: 0.2, y: 0.5, valid: true}, {},
		{x: 0.5, y: 0.5, valid: true}, {},
		{x: math.NaN(), y: 0.5, valid: true},
		{x: 0.8, y: 0.5, valid: true}, {x: 1, y: 0.5, valid: true},
	})
	for _, x := range []int{55, 105} {
		for y := 0; y < 60; y++ {
			if raster.RGBAAt(x, y).A != 0 {
				t.Fatal("stroke connected across missing telemetry")
			}
		}
	}
	if raster.RGBAAt(80, 30).A == 0 {
		t.Fatal("isolated observed sample disappeared")
	}
}

func TestControllerChartRasterCacheLifecycle(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.mw.Show()
	pumpControllerFixture(s)
	c, err := s.surface.CreateCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Dispose()
	points := []controllerChartPoint{{x: 0, y: 0.2, valid: true}, {x: 1, y: 0.8, valid: true}}
	bounds := walk.Rectangle{Width: 160, Height: 60}
	draw := func() {
		t.Helper()
		if err := s.drawControllerChart(c, 0, bounds, walk.RGB(167, 139, 250), points); err != nil {
			t.Fatal(err)
		}
	}
	draw()
	first := s.controllerChartRasters[0]
	draw()
	if first != s.controllerChartRasters[0] {
		t.Fatal("unchanged paint rerasterized the chart")
	}
	points[1].y = 0.6
	draw()
	if first == s.controllerChartRasters[0] {
		t.Fatal("new sample did not invalidate chart")
	}
	s.releaseHiddenRenderResources()
	if len(s.controllerChartRasters) != 0 {
		t.Fatal("hidden widget retained native chart bitmaps")
	}
}
