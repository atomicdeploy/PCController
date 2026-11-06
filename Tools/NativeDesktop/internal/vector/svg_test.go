package vector

import (
	"image"
	"testing"

	"pccontroller.local/desktop/assets"
)

func TestRenderEmbeddedDeviceSVGAtDeviceSize(t *testing.T) {
	for _, name := range []string{"device-off.svg", "device-on.svg"} {
		t.Run(name, func(t *testing.T) {
			data, err := assets.Files.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			image, err := RenderSVG(data, 333, 271)
			if err != nil {
				t.Fatal(err)
			}
			if got := image.Bounds().Size(); got.X != 333 || got.Y != 271 {
				t.Fatalf("rendered size = %v", got)
			}
			var visible int
			for index := 3; index < len(image.Pix); index += 4 {
				if image.Pix[index] != 0 {
					visible++
				}
			}
			if visible < 333*271/10 {
				t.Fatalf("visible pixels = %d, SVG did not render", visible)
			}
		})
	}
}

func TestRenderOnStateKeepsEveryVectorRay(t *testing.T) {
	data, err := assets.Files.ReadFile("device-on.svg")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderSVG(data, 512, 512)
	if err != nil {
		t.Fatal(err)
	}

	// Sample the middle of every authored ray. This guards against silently
	// falling back to an SVG renderer that only handles a subset of the paths.
	for _, point := range []image.Point{
		{X: 256, Y: 32},
		{X: 136, Y: 72},
		{X: 70, Y: 177},
		{X: 376, Y: 72},
		{X: 442, Y: 177},
		{X: 88, Y: 297},
		{X: 424, Y: 297},
	} {
		_, _, _, alpha := rendered.At(point.X, point.Y).RGBA()
		if alpha < 0x9000 {
			t.Errorf("ray at %v has alpha %#04x", point, alpha)
		}
	}
}

func TestRenderSVGRejectsUnsafeInputs(t *testing.T) {
	if _, err := RenderSVG(nil, 32, 32); err == nil {
		t.Fatal("empty SVG accepted")
	}
	if _, err := RenderSVG([]byte("<svg/>"), 0, 32); err == nil {
		t.Fatal("zero width accepted")
	}
	if _, err := RenderSVG([]byte("<svg/>"), maxDimension+1, 32); err == nil {
		t.Fatal("oversized render accepted")
	}
}
