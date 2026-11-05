//go:build windows

package ui

import "image"

// canonicalizeTransparentPixels makes the visual and hit-test thresholds
// identical. UpdateLayeredWindow only gives alpha-zero pixels native
// click-through semantics; leaving alpha 1..7 visible while treating them as
// transparent in WM_NCHITTEST is not sufficient across GUI threads.
func canonicalizeTransparentPixels(surface *image.RGBA, threshold byte) {
	if surface == nil {
		return
	}
	if threshold == 0 {
		threshold = 1
	}
	for offset := 0; offset+3 < len(surface.Pix); offset += 4 {
		if surface.Pix[offset+3] >= threshold {
			continue
		}
		surface.Pix[offset] = 0
		surface.Pix[offset+1] = 0
		surface.Pix[offset+2] = 0
		surface.Pix[offset+3] = 0
	}
}
