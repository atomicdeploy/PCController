//go:build windows

package ui

import (
	"image"
	"testing"
)

func TestResizeRGBAReusesBoundedStorageWithoutStalePixels(t *testing.T) {
	frame := reuseRGBA(nil, image.Rect(0, 0, 100, 100))
	base := &frame.Pix[0]
	for i := range frame.Pix {
		frame.Pix[i] = 255
	}
	for _, bounds := range []image.Rectangle{image.Rect(0, 0, 110, 105), image.Rect(5, 7, 85, 97), image.Rect(0, 0, 100, 100)} {
		frame = reuseRGBA(frame, bounds)
		if &frame.Pix[0] != base || frame.Stride != bounds.Dx()*4 || len(frame.Pix) != bounds.Dx()*bounds.Dy()*4 || frame.Bounds() != bounds {
			t.Fatal("resize lost its storage or exact logical size/stride")
		}
		for _, value := range frame.Pix {
			if value != 0 {
				t.Fatal("resize exposed pixels from the previous frame")
			}
		}
	}
	frame = reuseRGBA(frame, image.Rect(0, 0, 200, 200))
	if &frame.Pix[0] == base || cap(frame.Pix) != len(frame.Pix)*5/4 {
		t.Fatal("growth did not allocate bounded spare capacity")
	}
	frame = reuseRGBA(frame, image.Rect(0, 0, 30, 30))
	if cap(frame.Pix) > 2*len(frame.Pix) {
		t.Fatal("large shrink retained peak-size memory")
	}
}

func TestForegroundResizeReusesCPUPixelsAndRecreatesDPIBitmaps(t *testing.T) {
	previous, err := newForegroundRecovery(100, 100, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer previous.Dispose()
	replacement, err := newForegroundRecoveryReusing(110, 105, 144, previous)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Dispose()
	if !replacement.Matches(110, 105, 144) || replacement.blackBitmap == previous.blackBitmap || replacement.whiteBitmap == previous.whiteBitmap {
		t.Fatal("DPI/size transition reused incompatible native bitmaps")
	}
	if &replacement.black.Pix[0] != &previous.black.Pix[0] || &replacement.white.Pix[0] != &previous.white.Pix[0] || &replacement.recovered.Pix[0] != &previous.recovered.Pix[0] {
		t.Fatal("resize did not retain independent CPU backing stores")
	}
	replacement.black.Pix[0] = 80
	if replacement.white.Pix[0] != 0 || replacement.recovered.Pix[0] != 0 {
		t.Fatal("black/white/recovered stores alias")
	}
	if failed, err := newForegroundRecoveryReusing(0, 105, 144, replacement); err == nil || failed != nil || replacement.black.Pix[0] != 80 {
		t.Fatal("invalid replacement destroyed the previous valid frame")
	}
}
