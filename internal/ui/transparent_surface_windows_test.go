//go:build windows

package ui

import (
	"image"
	"testing"
)

func TestCanonicalizeTransparentPixelsZerosPresentedFringe(t *testing.T) {
	t.Parallel()

	surface := image.NewRGBA(image.Rect(0, 0, 3, 1))
	surface.Pix = []byte{
		7, 6, 5, 0,
		7, 6, 5, minimumInteractiveAlpha - 1,
		8, 7, 6, minimumInteractiveAlpha,
	}
	canonicalizeTransparentPixels(surface, minimumInteractiveAlpha)

	for _, offset := range []int{0, 4} {
		if got := [4]byte{
			surface.Pix[offset],
			surface.Pix[offset+1],
			surface.Pix[offset+2],
			surface.Pix[offset+3],
		}; got != [4]byte{} {
			t.Fatalf("sub-threshold pixel = %#v, want exact zero", got)
		}
	}
	if got := surface.Pix[11]; got != minimumInteractiveAlpha {
		t.Fatalf("threshold alpha = %d, want %d", got, minimumInteractiveAlpha)
	}
}
