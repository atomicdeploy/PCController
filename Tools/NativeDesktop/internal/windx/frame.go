package windx

import (
	"fmt"
	"image"
	"math"
)

func frameByteCount(width, height int) (int, error) {
	if width < 1 || height < 1 {
		return 0, ErrInvalidSize
	}
	maximum := int(^uint(0) >> 1)
	if width > maximum/4 || height > maximum/(width*4) {
		return 0, ErrInvalidSize
	}
	return width * height * 4, nil
}

func resizeFrameBuffer(buffer []byte, width, height int) ([]byte, error) {
	count, err := frameByteCount(width, height)
	if err != nil {
		return nil, err
	}
	if cap(buffer) < count {
		return make([]byte, count), nil
	}
	buffer = buffer[:count]
	clear(buffer)
	return buffer, nil
}

// convertPremultipliedRGBA converts Go's premultiplied RGBA byte order into
// the premultiplied BGRA order required by DXGI_FORMAT_B8G8R8A8_UNORM. It
// honors non-zero image origins and row strides.
func convertPremultipliedRGBA(
	frame *image.RGBA,
	destination []byte,
	width, height int,
) error {
	if frame == nil {
		return fmt.Errorf("%w: frame is nil", ErrFrameSize)
	}
	bounds := frame.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		return fmt.Errorf(
			"%w: got %dx%d, want %dx%d",
			ErrFrameSize,
			bounds.Dx(),
			bounds.Dy(),
			width,
			height,
		)
	}
	count, err := frameByteCount(width, height)
	if err != nil {
		return err
	}
	if len(destination) < count {
		return fmt.Errorf(
			"windx: BGRA destination has %d bytes, need %d",
			len(destination),
			count,
		)
	}
	for y := 0; y < height; y++ {
		sourceOffset := frame.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		destinationOffset := y * width * 4
		for x := 0; x < width; x++ {
			source := sourceOffset + x*4
			target := destinationOffset + x*4
			destination[target+0] = frame.Pix[source+2]
			destination[target+1] = frame.Pix[source+1]
			destination[target+2] = frame.Pix[source+0]
			destination[target+3] = frame.Pix[source+3]
		}
	}
	return nil
}

func opacityByte(opacity float64) (byte, error) {
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) ||
		opacity < 0 || opacity > 1 {
		return 0, fmt.Errorf(
			"windx: opacity %v is outside 0..1",
			opacity,
		)
	}
	return byte(math.Round(opacity * 255)), nil
}

// applyPremultipliedOpacity keeps every color channel premultiplied when a
// whole-surface opacity is applied. The source and destination must not alias.
func applyPremultipliedOpacity(
	source, destination []byte,
	opacity byte,
) {
	limit := min(len(source), len(destination))
	limit -= limit % 4
	if opacity == 255 {
		copy(destination[:limit], source[:limit])
		return
	}
	factor := uint16(opacity)
	for offset := 0; offset < limit; offset += 4 {
		destination[offset+0] = byte(
			(uint16(source[offset+0])*factor + 127) / 255,
		)
		destination[offset+1] = byte(
			(uint16(source[offset+1])*factor + 127) / 255,
		)
		destination[offset+2] = byte(
			(uint16(source[offset+2])*factor + 127) / 255,
		)
		destination[offset+3] = byte(
			(uint16(source[offset+3])*factor + 127) / 255,
		)
	}
}
