//go:build windows

package winfx

import (
	"image"
	"testing"
)

func TestAlphaRegionRectanglesExcludeTransparentHolesAndCoalesceRows(
	t *testing.T,
) {
	t.Parallel()

	surface := image.NewRGBA(image.Rect(0, 0, 6, 4))
	setAlpha := func(x, y int, alpha byte) {
		offset := surface.PixOffset(x, y)
		surface.Pix[offset+3] = alpha
	}
	for y := 0; y < 3; y++ {
		for x := 1; x < 5; x++ {
			setAlpha(x, y, 255)
		}
	}
	// Split the last row around a transparent hole. The below-threshold pixel
	// beside it must be excluded as well.
	setAlpha(1, 3, 255)
	setAlpha(2, 3, 7)
	setAlpha(4, 3, 255)

	rectangles, width, height := alphaRegionRectangles(surface, 8)
	if width != 6 || height != 4 {
		t.Fatalf("region size = %dx%d, want 6x4", width, height)
	}
	want := []Rect{
		{Left: 1, Top: 0, Right: 5, Bottom: 3},
		{Left: 1, Top: 3, Right: 2, Bottom: 4},
		{Left: 4, Top: 3, Right: 5, Bottom: 4},
	}
	if len(rectangles) != len(want) {
		t.Fatalf("rectangles = %#v, want %#v", rectangles, want)
	}
	for index := range want {
		if rectangles[index] != want[index] {
			t.Fatalf(
				"rectangle %d = %#v, want %#v",
				index,
				rectangles[index],
				want[index],
			)
		}
	}
}

func TestAlphaRegionSignatureTracksShapeNotColor(t *testing.T) {
	t.Parallel()

	first := image.NewRGBA(image.Rect(0, 0, 2, 1))
	second := image.NewRGBA(image.Rect(0, 0, 2, 1))
	first.Pix = []byte{10, 20, 30, 255, 0, 0, 0, 0}
	second.Pix = []byte{200, 40, 90, 255, 0, 0, 0, 0}
	if got, want := AlphaRegionSignature(first, 8),
		AlphaRegionSignature(second, 8); got != want {
		t.Fatalf("same alpha silhouette signatures differ: %x != %x", got, want)
	}
	second.Pix[7] = 255
	if got, want := AlphaRegionSignature(first, 8),
		AlphaRegionSignature(second, 8); got == want {
		t.Fatalf("different alpha silhouettes share signature %x", got)
	}
}
