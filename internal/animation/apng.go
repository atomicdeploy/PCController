package animation

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"io"
	"time"

	"github.com/kettek/apng"
)

type Animation struct {
	Frames    []image.Image
	Delays    []time.Duration
	LoopCount uint
}

// FrameCount remains valid after ReleaseFrames. Delays are retained as the
// compact playback manifest, while Frames contains the optional decoded pixel
// sources used to materialize a size-specific native cache.
func (animation *Animation) FrameCount() int {
	if animation == nil {
		return 0
	}
	if len(animation.Delays) > len(animation.Frames) {
		return len(animation.Delays)
	}
	return len(animation.Frames)
}

// Frame returns a decoded source frame when one is resident. A nil result for
// a valid index means the pixels were deliberately evicted after the native
// frame cache was completed; playback timing and frame order remain available.
func (animation *Animation) Frame(index int) image.Image {
	if animation == nil || index < 0 || index >= len(animation.Frames) {
		return nil
	}
	return animation.Frames[index]
}

// HasFrames reports whether every playback frame still has a decoded source.
func (animation *Animation) HasFrames() bool {
	if animation == nil || animation.FrameCount() == 0 ||
		len(animation.Frames) != animation.FrameCount() {
		return false
	}
	for _, frame := range animation.Frames {
		if frame == nil {
			return false
		}
	}
	return true
}

// ReleaseFrames drops only decoded pixels. Delays, loop metadata, and the
// logical frame count remain intact so a player already in flight is not
// disturbed and the UI can transparently re-decode after a resize.
func (animation *Animation) ReleaseFrames() {
	if animation == nil {
		return
	}
	clear(animation.Frames)
	animation.Frames = nil
}

func Decode(data []byte) (*Animation, error) {
	return DecodeReader(bytes.NewReader(data))
}

// DecodeReader decodes an APNG without requiring callers to first copy an
// embedded or file-backed payload into one large []byte.
func DecodeReader(reader io.Reader) (*Animation, error) {
	if reader == nil {
		return nil, fmt.Errorf("decode APNG: reader is nil")
	}
	decoded, err := apng.DecodeAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decode APNG: %w", err)
	}
	if len(decoded.Frames) == 0 {
		return nil, fmt.Errorf("decode APNG: no frames")
	}

	canvasBounds := image.Rectangle{}
	for _, frame := range decoded.Frames {
		if frame.Image == nil {
			continue
		}
		b := frame.Image.Bounds()
		target := image.Rect(
			frame.XOffset,
			frame.YOffset,
			frame.XOffset+b.Dx(),
			frame.YOffset+b.Dy(),
		)
		canvasBounds = canvasBounds.Union(target)
	}
	if canvasBounds.Empty() {
		return nil, fmt.Errorf("decode APNG: empty canvas")
	}
	canvasBounds = image.Rect(0, 0, canvasBounds.Max.X, canvasBounds.Max.Y)

	canvas := image.NewNRGBA(canvasBounds)
	result := &Animation{LoopCount: decoded.LoopCount}

	for _, frame := range decoded.Frames {
		if frame.Image == nil || frame.IsDefault {
			continue
		}

		b := frame.Image.Bounds()
		target := image.Rect(
			frame.XOffset,
			frame.YOffset,
			frame.XOffset+b.Dx(),
			frame.YOffset+b.Dy(),
		)

		var previous *image.NRGBA
		if frame.DisposeOp == apng.DISPOSE_OP_PREVIOUS {
			previous = cloneNRGBA(canvas)
		}

		op := draw.Over
		if frame.BlendOp == apng.BLEND_OP_SOURCE {
			op = draw.Src
		}
		draw.Draw(canvas, target, frame.Image, b.Min, op)
		result.Frames = append(result.Frames, cloneNRGBA(canvas))

		delay := time.Duration(frame.GetDelay() * float64(time.Second))
		if delay < 16*time.Millisecond {
			delay = 16 * time.Millisecond
		}
		result.Delays = append(result.Delays, delay)

		switch frame.DisposeOp {
		case apng.DISPOSE_OP_BACKGROUND:
			draw.Draw(canvas, target, image.Transparent, image.Point{}, draw.Src)
		case apng.DISPOSE_OP_PREVIOUS:
			if previous != nil {
				canvas = previous
			}
		}
	}

	// Some encoders mark the only/first frame as the default image. It remains
	// a useful static animation rather than being rejected.
	if len(result.Frames) == 0 {
		first := decoded.Frames[0]
		result.Frames = append(result.Frames, first.Image)
		result.Delays = append(result.Delays, 100*time.Millisecond)
	}

	return result, nil
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}
