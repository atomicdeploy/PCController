package graphics

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

type genericImage struct {
	image.Image
}

func TestResizeUsesExactDimensionsAndArbitrarySourceBounds(t *testing.T) {
	source := image.NewNRGBA(image.Rect(11, 17, 14, 19))
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 25, G: 90, B: 180, A: 255})
		}
	}

	scaled, err := Resize(source, 13, 7)
	if err != nil {
		t.Fatalf("Resize returned an error: %v", err)
	}
	wantBounds := image.Rect(0, 0, 13, 7)
	if scaled.Bounds() != wantBounds {
		t.Fatalf("Resize bounds = %v, want %v", scaled.Bounds(), wantBounds)
	}
}

func TestResizeInterpolatesSmoothlyInLinearLight(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	scaled, err := Resize(source, 9, 1)
	if err != nil {
		t.Fatalf("Resize returned an error: %v", err)
	}

	center := scaled.NRGBAAt(4, 0)
	if center.R < 180 || center.R > 195 {
		t.Fatalf("linear-light midpoint R = %d, want a smooth midpoint near 188", center.R)
	}
	if center.R != center.G || center.R != center.B || center.A != 255 {
		t.Fatalf("midpoint = %#v, want opaque neutral gray", center)
	}

	distinctIntermediateValues := map[uint8]bool{}
	for x := 0; x < scaled.Bounds().Dx(); x++ {
		value := scaled.NRGBAAt(x, 0).R
		if value > 0 && value < 255 {
			distinctIntermediateValues[value] = true
		}
	}
	if len(distinctIntermediateValues) < 3 {
		t.Fatalf("got only %d distinct intermediate values; interpolation is not smooth", len(distinctIntermediateValues))
	}
}

func TestFitPreservesAspectRatioAndDoesNotEnlarge(t *testing.T) {
	tests := []struct {
		name         string
		sourceWidth  int
		sourceHeight int
		maxWidth     int
		maxHeight    int
		wantWidth    int
		wantHeight   int
	}{
		{
			name:         "landscape",
			sourceWidth:  400,
			sourceHeight: 200,
			maxWidth:     100,
			maxHeight:    100,
			wantWidth:    100,
			wantHeight:   50,
		},
		{
			name:         "portrait",
			sourceWidth:  200,
			sourceHeight: 400,
			maxWidth:     100,
			maxHeight:    100,
			wantWidth:    50,
			wantHeight:   100,
		},
		{
			name:         "fractional result",
			sourceWidth:  160,
			sourceHeight: 90,
			maxWidth:     100,
			maxHeight:    100,
			wantWidth:    100,
			wantHeight:   56,
		},
		{
			name:         "does not enlarge",
			sourceWidth:  40,
			sourceHeight: 20,
			maxWidth:     100,
			maxHeight:    100,
			wantWidth:    40,
			wantHeight:   20,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, test.sourceWidth, test.sourceHeight))
			scaled, err := Fit(source, test.maxWidth, test.maxHeight)
			if err != nil {
				t.Fatalf("Fit returned an error: %v", err)
			}
			if got := scaled.Bounds().Size(); got != (image.Point{X: test.wantWidth, Y: test.wantHeight}) {
				t.Fatalf("Fit size = %v, want (%d,%d)", got, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestResizeDoesNotCreateTransparentEdgeColorFringes(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	// Hidden blue in a fully transparent pixel catches straight-alpha filters:
	// it must not bleed blue into the visible red edge.
	source.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 0})

	scaled, err := Resize(source, 33, 1)
	if err != nil {
		t.Fatalf("Resize returned an error: %v", err)
	}

	foundPartialAlpha := false
	for x := 0; x < scaled.Bounds().Dx(); x++ {
		pixel := scaled.NRGBAAt(x, 0)
		if pixel.A == 0 {
			continue
		}
		if pixel.A < 255 {
			foundPartialAlpha = true
		}
		if pixel.R < 252 || pixel.G > 2 || pixel.B > 2 {
			t.Fatalf("pixel %d = %#v; transparent edge changed the visible red hue", x, pixel)
		}

		// Compositing a correctly filtered red edge over white keeps the red
		// channel white-hot. A black fringe would make this noticeably darker.
		compositedRed := (int(pixel.R)*int(pixel.A) + 255*(255-int(pixel.A)) + 127) / 255
		if compositedRed < 252 {
			t.Fatalf("pixel %d composites with a dark red fringe: %#v -> R=%d", x, pixel, compositedRed)
		}
	}
	if !foundPartialAlpha {
		t.Fatal("scaled edge contained no partially transparent pixels")
	}
}

func TestResizeRejectsInvalidInput(t *testing.T) {
	if _, err := Resize(nil, 1, 1); err == nil {
		t.Fatal("Resize(nil, ...) unexpectedly succeeded")
	}
	if _, err := Resize(image.NewNRGBA(image.Rect(0, 0, 1, 1)), 0, 1); err == nil {
		t.Fatal("Resize with zero width unexpectedly succeeded")
	}
	if _, err := Fit(image.NewNRGBA(image.Rect(0, 0, 1, 1)), 1, -1); err == nil {
		t.Fatal("Fit with negative height unexpectedly succeeded")
	}
}

func TestResizeConcreteFastPathsMatchGenericImageExactly(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x524159414e))
	nrgba := image.NewNRGBA(image.Rect(7, 11, 36, 34))
	for offset := range nrgba.Pix {
		nrgba.Pix[offset] = byte(random.Intn(256))
	}
	rgba := image.NewRGBA(image.Rect(-5, 3, 24, 26))
	for offset := range rgba.Pix {
		rgba.Pix[offset] = byte(random.Intn(256))
	}

	for name, source := range map[string]image.Image{
		"nrgba": nrgba,
		"rgba":  rgba,
	} {
		source := source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fast, err := Resize(source, 41, 37)
			if err != nil {
				t.Fatalf("fast Resize returned an error: %v", err)
			}
			generic, err := Resize(genericImage{Image: source}, 41, 37)
			if err != nil {
				t.Fatalf("generic Resize returned an error: %v", err)
			}
			if fast.Bounds() != generic.Bounds() {
				t.Fatalf(
					"fast bounds = %v, generic bounds = %v",
					fast.Bounds(),
					generic.Bounds(),
				)
			}
			if string(fast.Pix) != string(generic.Pix) {
				for offset := range fast.Pix {
					if fast.Pix[offset] != generic.Pix[offset] {
						t.Fatalf(
							"fast pixel byte %d = %d, generic = %d",
							offset,
							fast.Pix[offset],
							generic.Pix[offset],
						)
					}
				}
				t.Fatal("fast and generic pixel buffers differ")
			}
		})
	}
}

func TestResizeRowWindowMatchesFullIntermediateReference(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x4c414d50))
	source := image.NewNRGBA(image.Rect(9, -4, 46, 25))
	for offset := range source.Pix {
		source.Pix[offset] = byte(random.Intn(256))
	}
	for _, size := range []image.Point{
		{X: 1, Y: 1},
		{X: 13, Y: 7},
		{X: 37, Y: 29},
		{X: 64, Y: 51},
		{X: 9, Y: 73},
	} {
		got, err := Resize(source, size.X, size.Y)
		if err != nil {
			t.Fatalf("Resize(%v) returned an error: %v", size, err)
		}
		want := referenceResize(source, size.X, size.Y)
		if string(got.Pix) != string(want.Pix) {
			for offset := range got.Pix {
				if got.Pix[offset] != want.Pix[offset] {
					t.Fatalf(
						"Resize(%v) byte %d = %d, full-buffer reference = %d",
						size,
						offset,
						got.Pix[offset],
						want.Pix[offset],
					)
				}
			}
			t.Fatalf("Resize(%v) differs from full-buffer reference", size)
		}
	}
}

func referenceResize(source image.Image, width, height int) *image.NRGBA {
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	sourcePixels := make([]linearPixel, sourceWidth*sourceHeight)
	for y := 0; y < sourceHeight; y++ {
		row := y * sourceWidth
		for x := 0; x < sourceWidth; x++ {
			sourcePixels[row+x] = colorToLinearPremultiplied(
				source.At(bounds.Min.X+x, bounds.Min.Y+y),
			)
		}
	}

	horizontalWeights := makeWeights(sourceWidth, width)
	horizontal := make([]linearPixel, width*sourceHeight)
	for y := 0; y < sourceHeight; y++ {
		sourceRow := y * sourceWidth
		destinationRow := y * width
		for x, samples := range horizontalWeights {
			var pixel linearPixel
			for _, sample := range samples {
				pixel.addWeighted(
					sourcePixels[sourceRow+sample.index],
					sample.weight,
				)
			}
			horizontal[destinationRow+x] = pixel
		}
	}

	verticalWeights := makeWeights(sourceHeight, height)
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y, samples := range verticalWeights {
		for x := 0; x < width; x++ {
			var pixel linearPixel
			for _, sample := range samples {
				pixel.addWeighted(
					horizontal[sample.index*width+x],
					sample.weight,
				)
			}
			writeNRGBA(result, x, y, pixel)
		}
	}
	return result
}
