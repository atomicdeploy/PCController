//go:build windows

package ui

import "image"

// Reuse a tightly packed logical image, with at most 25% spare backing memory
// when it grows; a shrink beyond 2:1 releases peak capacity. Dimensions/stride
// remain exact: no stretched or stale pixels.
// Call only after any fallible native-resource creation has succeeded.
func reuseRGBA(previous *image.RGBA, bounds image.Rectangle) *image.RGBA {
	width, height := bounds.Dx(), bounds.Dy()
	maximum := int(^uint(0) >> 1)
	if width <= 0 || height <= 0 || width > maximum/4 || height > maximum/(width*4) {
		return image.NewRGBA(bounds)
	}
	count := width * height * 4
	var pixels []byte
	if previous != nil && cap(previous.Pix) >= count && cap(previous.Pix)-count <= count {
		pixels = previous.Pix[:count]
		clear(pixels)
	} else {
		capacity := count
		if count <= maximum-count/4 {
			capacity += count / 4
		}
		pixels = make([]byte, count, capacity)
	}
	return &image.RGBA{Pix: pixels, Stride: width * 4, Rect: bounds}
}
