// Package graphics contains image operations used by RayanLamp's native UI.
package graphics

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

const lanczosRadius = 3.0

type linearPixel struct {
	r float64
	g float64
	b float64
	a float64
}

type weightedSample struct {
	index  int
	weight float64
}

// Resize scales src to exactly width by height pixels.
//
// It uses a separable Lanczos-3 filter in linear light. Colors are filtered in
// premultiplied-alpha form, which prevents transparent pixels from introducing
// dark or incorrectly colored fringes around antialiased artwork.
func Resize(src image.Image, width, height int) (*image.NRGBA, error) {
	if src == nil {
		return nil, errors.New("graphics: cannot resize a nil image")
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("graphics: invalid destination size %dx%d", width, height)
	}

	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return nil, errors.New("graphics: cannot resize an empty image")
	}

	if err := checkBufferSize(sourceWidth, 1, 32); err != nil {
		return nil, fmt.Errorf("graphics: source row: %w", err)
	}
	if err := checkBufferSize(width, height, 4); err != nil {
		return nil, fmt.Errorf("graphics: destination image: %w", err)
	}

	horizontalWeights := makeWeights(sourceWidth, width)
	verticalWeights := makeWeights(sourceHeight, height)
	maxVerticalSamples := 0
	for _, samples := range verticalWeights {
		maxVerticalSamples = max(maxVerticalSamples, len(samples))
	}
	if maxVerticalSamples == 0 {
		return nil, errors.New("graphics: vertical filter has no samples")
	}
	if err := checkBufferSize(width, maxVerticalSamples, 32); err != nil {
		return nil, fmt.Errorf("graphics: resize work buffer: %w", err)
	}

	// A separable resize does not require horizontally filtered copies of every
	// source row at once. Vertical Lanczos samples move monotonically down the
	// image, so retain only the small window of rows needed for the current
	// destination scanline. This replaces an 8 MiB 512x512 float buffer with
	// roughly 100 KiB while preserving the exact filter and accumulation order.
	sourceRowPixels := make([]linearPixel, sourceWidth)
	horizontalRows := make([]linearPixel, width*maxVerticalSamples)
	slotSourceRows := make([]int, maxVerticalSamples)
	for slot := range slotSourceRows {
		slotSourceRows[slot] = -1
	}
	sourceRowSlots := make(map[int]int, maxVerticalSamples)

	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y, samples := range verticalWeights {
		for slot, sourceRow := range slotSourceRows {
			if sourceRow >= 0 && !weightedSamplesContain(samples, sourceRow) {
				delete(sourceRowSlots, sourceRow)
				slotSourceRows[slot] = -1
			}
		}
		for _, sample := range samples {
			if _, found := sourceRowSlots[sample.index]; found {
				continue
			}
			slot := firstFreeRowSlot(slotSourceRows)
			if slot < 0 {
				return nil, errors.New("graphics: vertical row cache exhausted")
			}
			loadLinearRow(
				sourceRowPixels,
				src,
				bounds.Min.X,
				bounds.Min.Y+sample.index,
			)
			destinationRow := slot * width
			for x, horizontalSamples := range horizontalWeights {
				var pixel linearPixel
				for _, horizontalSample := range horizontalSamples {
					pixel.addWeighted(
						sourceRowPixels[horizontalSample.index],
						horizontalSample.weight,
					)
				}
				horizontalRows[destinationRow+x] = pixel
			}
			slotSourceRows[slot] = sample.index
			sourceRowSlots[sample.index] = slot
		}

		for x := 0; x < width; x++ {
			var pixel linearPixel
			for _, sample := range samples {
				slot := sourceRowSlots[sample.index]
				pixel.addWeighted(
					horizontalRows[slot*width+x],
					sample.weight,
				)
			}
			writeNRGBA(result, x, y, pixel)
		}
	}

	return result, nil
}

func weightedSamplesContain(samples []weightedSample, sourceIndex int) bool {
	low, high := 0, len(samples)
	for low < high {
		middle := int(uint(low+high) >> 1)
		index := samples[middle].index
		if index < sourceIndex {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low < len(samples) && samples[low].index == sourceIndex
}

func firstFreeRowSlot(sourceRows []int) int {
	for slot, sourceRow := range sourceRows {
		if sourceRow < 0 {
			return slot
		}
	}
	return -1
}

// Fit scales src to the largest size that fits within maxWidth by maxHeight
// while preserving its aspect ratio. Fit never enlarges the source image.
func Fit(src image.Image, maxWidth, maxHeight int) (*image.NRGBA, error) {
	if src == nil {
		return nil, errors.New("graphics: cannot fit a nil image")
	}
	if maxWidth <= 0 || maxHeight <= 0 {
		return nil, fmt.Errorf("graphics: invalid fit bounds %dx%d", maxWidth, maxHeight)
	}

	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return nil, errors.New("graphics: cannot fit an empty image")
	}

	if sourceWidth <= maxWidth && sourceHeight <= maxHeight {
		return Resize(src, sourceWidth, sourceHeight)
	}

	scale := math.Min(
		float64(maxWidth)/float64(sourceWidth),
		float64(maxHeight)/float64(sourceHeight),
	)
	width := max(1, int(math.Round(float64(sourceWidth)*scale)))
	height := max(1, int(math.Round(float64(sourceHeight)*scale)))

	// Floating-point rounding must never allow the fitted image to exceed its
	// requested box.
	width = min(width, maxWidth)
	height = min(height, maxHeight)

	return Resize(src, width, height)
}

func (p *linearPixel) addWeighted(other linearPixel, weight float64) {
	p.r += other.r * weight
	p.g += other.g * weight
	p.b += other.b * weight
	p.a += other.a * weight
}

func colorToLinearPremultiplied(c interface {
	RGBA() (r, g, b, a uint32)
}) linearPixel {
	r, g, b, a := c.RGBA()
	return rgbaToLinearPremultiplied(r, g, b, a)
}

func rgbaToLinearPremultiplied(r, g, b, a uint32) linearPixel {
	if a == 0 {
		return linearPixel{}
	}

	alpha := float64(a) / 65535.0
	red := clamp01(float64(r) / float64(a))
	green := clamp01(float64(g) / float64(a))
	blue := clamp01(float64(b) / float64(a))

	return linearPixel{
		r: srgbToLinear(red) * alpha,
		g: srgbToLinear(green) * alpha,
		b: srgbToLinear(blue) * alpha,
		a: alpha,
	}
}

func loadLinearRow(
	destination []linearPixel,
	source image.Image,
	sourceX, sourceY int,
) {
	// image.Image.At returns a color.Color interface. Calling it for every
	// pixel makes concrete color values escape on common Go image types,
	// producing one tiny heap allocation per source pixel. Artwork and APNG
	// frames use NRGBA/RGBA, so retain a generic path but keep these hot paths
	// concrete and allocation-free.
	switch typed := source.(type) {
	case *image.NRGBA:
		for x := range destination {
			r, g, b, a := typed.NRGBAAt(sourceX+x, sourceY).RGBA()
			destination[x] = rgbaToLinearPremultiplied(r, g, b, a)
		}
	case *image.RGBA:
		for x := range destination {
			r, g, b, a := typed.RGBAAt(sourceX+x, sourceY).RGBA()
			destination[x] = rgbaToLinearPremultiplied(r, g, b, a)
		}
	default:
		for x := range destination {
			destination[x] = colorToLinearPremultiplied(
				source.At(sourceX+x, sourceY),
			)
		}
	}
}

func writeNRGBA(destination *image.NRGBA, x, y int, pixel linearPixel) {
	alpha := clamp01(pixel.a)
	if alpha <= 0 {
		destination.SetNRGBA(x, y, zeroNRGBA)
		return
	}

	// A reconstruction filter can have small negative lobes. Constraining each
	// premultiplied channel to [0, alpha] preserves valid alpha semantics while
	// retaining the filter's sharp reconstruction in the useful range.
	red := linearToSRGB(clamp(pixel.r, 0, alpha) / alpha)
	green := linearToSRGB(clamp(pixel.g, 0, alpha) / alpha)
	blue := linearToSRGB(clamp(pixel.b, 0, alpha) / alpha)
	alphaByte := roundedByte(alpha)
	if alphaByte == 0 {
		destination.SetNRGBA(x, y, zeroNRGBA)
		return
	}

	offset := destination.PixOffset(x, y)
	destination.Pix[offset+0] = roundedByte(red)
	destination.Pix[offset+1] = roundedByte(green)
	destination.Pix[offset+2] = roundedByte(blue)
	destination.Pix[offset+3] = alphaByte
}

var zeroNRGBA color.NRGBA

func makeWeights(sourceLength, destinationLength int) [][]weightedSample {
	scale := float64(destinationLength) / float64(sourceLength)
	filterScale := math.Min(scale, 1)
	support := lanczosRadius / filterScale
	result := make([][]weightedSample, destinationLength)

	for destinationIndex := 0; destinationIndex < destinationLength; destinationIndex++ {
		center := (float64(destinationIndex)+0.5)/scale - 0.5
		first := int(math.Ceil(center - support))
		last := int(math.Floor(center + support))
		samples := make([]weightedSample, 0, last-first+1)
		totalWeight := 0.0

		for sourceIndex := first; sourceIndex <= last; sourceIndex++ {
			weight := lanczos3((float64(sourceIndex)-center)*filterScale) * filterScale
			if math.Abs(weight) < 1e-15 {
				continue
			}

			clampedIndex := min(max(sourceIndex, 0), sourceLength-1)
			if len(samples) != 0 && samples[len(samples)-1].index == clampedIndex {
				samples[len(samples)-1].weight += weight
			} else {
				samples = append(samples, weightedSample{index: clampedIndex, weight: weight})
			}
			totalWeight += weight
		}

		if math.Abs(totalWeight) < 1e-15 {
			nearest := min(max(int(math.Round(center)), 0), sourceLength-1)
			result[destinationIndex] = []weightedSample{{index: nearest, weight: 1}}
			continue
		}

		for index := range samples {
			samples[index].weight /= totalWeight
		}
		result[destinationIndex] = samples
	}

	return result
}

func lanczos3(x float64) float64 {
	x = math.Abs(x)
	if x >= lanczosRadius {
		return 0
	}
	return sinc(x) * sinc(x/lanczosRadius)
}

func sinc(x float64) float64 {
	if math.Abs(x) < 1e-12 {
		return 1
	}
	angle := math.Pi * x
	return math.Sin(angle) / angle
}

func srgbToLinear(value float64) float64 {
	value = clamp01(value)
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func linearToSRGB(value float64) float64 {
	value = clamp01(value)
	if value <= 0.0031308 {
		return value * 12.92
	}
	return 1.055*math.Pow(value, 1.0/2.4) - 0.055
}

func roundedByte(value float64) byte {
	return byte(math.Round(clamp01(value) * 255))
}

func clamp01(value float64) float64 {
	return clamp(value, 0, 1)
}

func clamp(value, low, high float64) float64 {
	return math.Min(math.Max(value, low), high)
}

func checkBufferSize(width, height, bytesPerPixel int) error {
	maxInt := int(^uint(0) >> 1)
	if width <= 0 || height <= 0 || bytesPerPixel <= 0 {
		return errors.New("invalid buffer dimensions")
	}
	if width > maxInt/height {
		return errors.New("pixel count overflows int")
	}
	pixels := width * height
	if pixels > maxInt/bytesPerPixel {
		return errors.New("buffer size overflows int")
	}
	return nil
}
