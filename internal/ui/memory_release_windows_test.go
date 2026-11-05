//go:build windows

package ui

import (
	"image"
	"testing"

	"rayanlamp.local/app/internal/animation"
)

func TestResidentAnimationBytesCountsOnlyResidentPixels(t *testing.T) {
	decoded := &animation.Animation{
		Frames: []image.Image{
			image.NewNRGBA(image.Rect(0, 0, 512, 512)),
			nil,
			image.NewRGBA(image.Rect(7, 9, 17, 29)),
		},
	}
	want := int64(512*512*4 + 10*20*4)
	if got := residentAnimationBytes(decoded); got != want {
		t.Fatalf("resident bytes = %d, want %d", got, want)
	}
	decoded.ReleaseFrames()
	if got := residentAnimationBytes(decoded); got != 0 {
		t.Fatalf("released resident bytes = %d, want 0", got)
	}
}
