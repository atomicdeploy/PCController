//go:build windows

package walk

import (
	"bytes"
	"image"
	"testing"

	"github.com/lxn/win"
)

func TestBGRAToRGBAInPlacePreservesOrientationAndChannels(t *testing.T) {
	topDown := []byte{
		3, 2, 1, 4, 7, 6, 5, 8,
		11, 10, 9, 12, 15, 14, 13, 16,
	}
	want := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	bgraToRGBAInPlace(topDown, 2, 2, false)
	if !bytes.Equal(topDown, want) {
		t.Fatalf("top-down conversion = %v, want %v", topDown, want)
	}

	bottomUp := []byte{
		11, 10, 9, 12, 15, 14, 13, 16,
		3, 2, 1, 4, 7, 6, 5, 8,
	}
	bgraToRGBAInPlace(bottomUp, 2, 2, true)
	if !bytes.Equal(bottomUp, want) {
		t.Fatalf("bottom-up conversion = %v, want %v", bottomUp, want)
	}
}

func TestBitmapToImageIntoReusesExactPixelBuffer(t *testing.T) {
	bitmap, err := NewBitmapForDPI(Size{Width: 3, Height: 2}, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer bitmap.Dispose()

	want, err := bitmap.ToImage()
	if err != nil {
		t.Fatal(err)
	}
	destination := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for offset := range destination.Pix {
		destination.Pix[offset] = 0x7f
	}
	firstPixel := &destination.Pix[0]
	if err := bitmap.ToImageInto(destination); err != nil {
		t.Fatal(err)
	}
	if firstPixel != &destination.Pix[0] {
		t.Fatal("ToImageInto replaced the destination pixel buffer")
	}
	if !bytes.Equal(destination.Pix, want.Pix) {
		t.Fatalf(
			"ToImageInto pixels = %v, ToImage pixels = %v",
			destination.Pix,
			want.Pix,
		)
	}
	if err := bitmap.ToImageInto(
		image.NewRGBA(image.Rect(0, 0, 2, 2)),
	); err == nil {
		t.Fatal("ToImageInto accepted mismatched destination bounds")
	}
}

func TestBitmapToImageReleasesScreenDC(t *testing.T) {
	bitmap, err := NewBitmapForDPI(Size{Width: 2, Height: 2}, 96)
	if err != nil {
		t.Fatal(err)
	}
	defer bitmap.Dispose()

	originalRelease := bitmapReleaseDC
	releases := 0
	bitmapReleaseDC = func(hwnd win.HWND, dc win.HDC) bool {
		releases++
		return originalRelease(hwnd, dc)
	}
	defer func() { bitmapReleaseDC = originalRelease }()

	if _, err := bitmap.ToImage(); err != nil {
		t.Fatal(err)
	}
	if releases != 1 {
		t.Fatalf("screen DC releases = %d, want 1", releases)
	}
}
