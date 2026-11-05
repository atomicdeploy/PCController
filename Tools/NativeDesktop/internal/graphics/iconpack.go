package graphics

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

const (
	iconDirectoryHeaderSize = 6
	iconDirectoryEntrySize  = 16
	maxIconFrames           = 64
	maxIconPackBytes        = 32 << 20
)

// IconFrame is one encoded RT_ICON-compatible image from an ICO container.
// Data is retained as encoded PNG or DIB bytes so Windows can consume the
// native icon payload without another raster conversion.
type IconFrame struct {
	Width    int
	Height   int
	BitDepth int
	Data     []byte
}

// IconPack is a validated, multi-resolution Windows icon container.
type IconPack struct {
	frames []IconFrame
}

// DecodeIconPack validates an ICO directory and exposes its individual frames.
func DecodeIconPack(data []byte) (*IconPack, error) {
	if len(data) < iconDirectoryHeaderSize {
		return nil, errors.New("graphics: ICO header is truncated")
	}
	if len(data) > maxIconPackBytes {
		return nil, errors.New("graphics: ICO exceeds the safe size limit")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 ||
		binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, errors.New("graphics: invalid ICO header")
	}

	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count < 1 || count > maxIconFrames {
		return nil, fmt.Errorf("graphics: unsafe ICO frame count %d", count)
	}
	directoryEnd := iconDirectoryHeaderSize + count*iconDirectoryEntrySize
	if directoryEnd > len(data) {
		return nil, errors.New("graphics: ICO directory is truncated")
	}

	frames := make([]IconFrame, 0, count)
	seen := make(map[[2]int]struct{}, count)
	for index := 0; index < count; index++ {
		entry := iconDirectoryHeaderSize + index*iconDirectoryEntrySize
		width := iconDimension(data[entry])
		height := iconDimension(data[entry+1])
		if width < 1 || height < 1 {
			return nil, fmt.Errorf("graphics: ICO frame %d has invalid dimensions", index)
		}
		dimensions := [2]int{width, height}
		if _, duplicate := seen[dimensions]; duplicate {
			return nil, fmt.Errorf(
				"graphics: ICO contains duplicate %dx%d frames",
				width,
				height,
			)
		}
		seen[dimensions] = struct{}{}

		length := uint64(binary.LittleEndian.Uint32(data[entry+8 : entry+12]))
		offset := uint64(binary.LittleEndian.Uint32(data[entry+12 : entry+16]))
		end := offset + length
		if length == 0 || offset < uint64(directoryEnd) ||
			end < offset || end > uint64(len(data)) {
			return nil, fmt.Errorf("graphics: ICO frame %d has invalid data bounds", index)
		}
		frames = append(frames, IconFrame{
			Width:    width,
			Height:   height,
			BitDepth: int(binary.LittleEndian.Uint16(data[entry+6 : entry+8])),
			Data:     data[int(offset):int(end)],
		})
	}

	slices.SortFunc(frames, func(left, right IconFrame) int {
		leftArea := left.Width * left.Height
		rightArea := right.Width * right.Height
		switch {
		case leftArea < rightArea:
			return -1
		case leftArea > rightArea:
			return 1
		default:
			return left.Width - right.Width
		}
	})
	return &IconPack{frames: frames}, nil
}

// Frames returns a shallow copy of the validated frames, ordered from smallest
// to largest. The encoded Data slices remain owned by the input ICO byte slice.
func (pack *IconPack) Frames() []IconFrame {
	if pack == nil {
		return nil
	}
	return slices.Clone(pack.frames)
}

// Select returns the best source frame for the requested native pixel size.
// Exact frames win. Otherwise it prefers the nearest frame that can be scaled
// down, avoiding the visibly softer result of enlarging a smaller icon.
func (pack *IconPack) Select(width, height int) (IconFrame, bool) {
	if pack == nil || width < 1 || height < 1 || len(pack.frames) == 0 {
		return IconFrame{}, false
	}

	bestIndex := -1
	bestScore := math.Inf(1)
	for index, frame := range pack.frames {
		if frame.Width == width && frame.Height == height {
			return frame, true
		}
		scaleX := float64(frame.Width) / float64(width)
		scaleY := float64(frame.Height) / float64(height)
		canScaleDown := scaleX >= 1 && scaleY >= 1
		score := math.Abs(math.Log(scaleX)) + math.Abs(math.Log(scaleY))
		if !canScaleDown {
			// A modest penalty makes a slightly larger source preferable to a
			// similarly close smaller one without selecting a huge frame.
			score += 0.35
		}
		if score < bestScore {
			bestIndex = index
			bestScore = score
		}
	}
	if bestIndex < 0 {
		return IconFrame{}, false
	}
	return pack.frames[bestIndex], true
}

func iconDimension(value byte) int {
	if value == 0 {
		return 256
	}
	return int(value)
}
