//go:build windows

package winfx

import "testing"

func TestDialogRoundedRasterHasFractionalSymmetricPremultipliedEdges(t *testing.T) {
	for _, dpi := range []int32{96, 120, 144, 192} {
		w, h := scaleDialogFieldMetric(160, dpi), scaleDialogFieldMetric(32, dpi)
		for _, fill := range []bool{false, true} {
			key := dialogRasterKey{Width: w, Height: h, Radius: scaleDialogFieldMetric(7, dpi), Stroke: max(1, scaleDialogFieldMetric(1, dpi)), Fill: fill,
				Top: dialogButtonRGB(60, 40, 90), Bottom: dialogButtonRGB(30, 20, 50), Border: dialogButtonRGB(170, 140, 240)}
			pixels := make([]byte, int(w*h*4))
			rasterDialogShape(key, pixels)
			partial := 0
			for y := int32(0); y < h; y++ {
				for x := int32(0); x < w; x++ {
					p := pixels[(y*w+x)*4:][:4]
					if p[3] > 0 && p[3] < 255 {
						partial++
					}
					if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
						t.Fatalf("non-premultiplied edge at %d DPI: %v", dpi, p)
					}
					if p[3] != pixels[(y*w+w-1-x)*4+3] || p[3] != pixels[((h-1-y)*w+x)*4+3] {
						t.Fatal("asymmetric corner coverage")
					}
				}
			}
			if partial < 8 {
				t.Fatalf("binary/aliased edges at %d DPI (fill=%v): %d partial pixels", dpi, fill, partial)
			}
			if pixels[3] != 0 {
				t.Fatal("rounded corner must reveal the parent material")
			}
			center := pixels[((h/2)*w+w/2)*4+3]
			if fill && center != 255 || !fill && center != 0 {
				t.Fatal("stroke raster modified native text interior")
			}
		}
	}
}

func TestDialogPolylineRasterHasSoftEdgesAndContinuousJoins(t *testing.T) {
	key := dialogRasterKey{Kind: 1, Width: 20, Height: 15, Count: 3, Points: [6]int32{3, 4, 9, 10, 16, 3}, Top: dialogButtonRGB(200, 140, 90), Stroke: 2}
	pixels := make([]byte, int(key.Width*key.Height*4))
	rasterDialogPolyline(key, pixels)
	partial := 0
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] > 0 && pixels[i] < 255 {
			partial++
		}
	}
	if partial < 5 {
		t.Fatal("checkbox/chevron stroke has no fractional coverage")
	}
	if pixels[(9*key.Width+9)*4+3] < 128 {
		t.Fatal("polyline join is disconnected")
	}
}

func TestDialogRasterCacheIsBoundedAndReusesGDIResources(t *testing.T) {
	dialogRasterCache.Lock()
	defer dialogRasterCache.Unlock()
	clearCache := func() {
		for key, s := range dialogRasterCache.entries {
			closeDialogRasterSurface(s)
			delete(dialogRasterCache.entries, key)
		}
		dialogRasterCache.bytes = 0
	}
	clearCache()
	defer clearCache()
	base := dialogRasterKey{Width: 64, Height: 32, Radius: 7, Fill: true, Top: 1, Bottom: 1}
	first := acquireDialogRaster(base)
	if first == nil || acquireDialogRaster(base) != first {
		t.Fatal("identical chrome allocates GDI resources again")
	}
	for i := 0; i < dialogRasterMaxEntries+20; i++ {
		key := base
		key.Top = uint32(i + 2)
		if acquireDialogRaster(key) == nil {
			t.Fatal("DIB allocation failed")
		}
	}
	if len(dialogRasterCache.entries) > dialogRasterMaxEntries || dialogRasterCache.bytes > dialogRasterMaxBytes {
		t.Fatal("unbounded chrome cache")
	}
	if acquireDialogRaster(dialogRasterKey{Width: 8192, Height: 8192}) != nil {
		t.Fatal("accepted an unbounded control bitmap")
	}
}

func TestDialogRasterBlendsFractionalEdgesIntoRealHDC(t *testing.T) {
	key := dialogRasterKey{Width: 80, Height: 40, Radius: 9}
	target := newDialogRasterSurface(key)
	if target == nil {
		t.Fatal("create target DIB")
	}
	defer closeDialogRasterSurface(target)
	for i := 0; i < len(target.pixels); i += 4 {
		target.pixels[i] = 20
		target.pixels[i+1] = 20
		target.pixels[i+2] = 20
		target.pixels[i+3] = 255
	}
	if !drawDialogAARoundRect(target.dc, dialogButtonRect{Right: 80, Bottom: 40}, dialogButtonRGB(200, 200, 200), dialogButtonRGB(200, 200, 200), 0, 9, 0, true) {
		t.Fatal("AlphaBlend failed")
	}
	dialogRasterFlush.Call()
	partial := 0
	for i := 0; i < len(target.pixels); i += 4 {
		if value := target.pixels[i]; value > 20 && value < 200 {
			partial++
		}
	}
	if partial < 8 || target.pixels[0] != 20 {
		t.Fatal("HDC lost soft coverage or overwrote rounded corner")
	}
}
