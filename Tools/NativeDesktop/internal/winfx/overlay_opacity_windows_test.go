//go:build windows

package winfx

import "testing"

func TestPerPixelOverlayBlendPreservesRequestedOpacity(t *testing.T) {
	for _, opacity := range []byte{0, 1, 64, 128, 254, 255} {
		blend := perPixelOverlayBlend(opacity)
		if blend.BlendOp != overlayACSrcOver {
			t.Fatalf("opacity %d blend op = %d", opacity, blend.BlendOp)
		}
		if blend.SourceConstantAlpha != opacity {
			t.Fatalf(
				"constant alpha = %d, want %d",
				blend.SourceConstantAlpha,
				opacity,
			)
		}
		if blend.AlphaFormat != overlayACSrcAlpha {
			t.Fatalf(
				"opacity %d alpha format = %d",
				opacity,
				blend.AlphaFormat,
			)
		}
	}
}
