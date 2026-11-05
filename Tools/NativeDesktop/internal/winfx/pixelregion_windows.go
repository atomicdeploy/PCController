//go:build windows

package winfx

import (
	"encoding/binary"
	"fmt"
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	regionDataRectangles = 1
	regionHeaderSize     = 32
	regionRectangleSize  = 16
)

var (
	pixelRegionGDI32           = windows.NewLazySystemDLL("gdi32.dll")
	pixelRegionExtCreateRegion = pixelRegionGDI32.NewProc("ExtCreateRegion")
	pixelRegionCreateRectRgn   = pixelRegionGDI32.NewProc("CreateRectRgn")
)

// AlphaRegionSignature returns a stable signature for the binary window shape
// produced by thresholding source. It lets animation frames reuse an existing
// HRGN when only colors changed and the hit-test silhouette did not.
func AlphaRegionSignature(source *image.RGBA, threshold byte) uint64 {
	rectangles, width, height := alphaRegionRectangles(source, threshold)
	hash := uint64(1469598103934665603)
	mix := func(value uint32) {
		for shift := uint(0); shift < 32; shift += 8 {
			hash ^= uint64(byte(value >> shift))
			hash *= 1099511628211
		}
	}
	mix(uint32(width))
	mix(uint32(height))
	mix(uint32(threshold))
	mix(uint32(len(rectangles)))
	for _, rectangle := range rectangles {
		mix(uint32(rectangle.Left))
		mix(uint32(rectangle.Top))
		mix(uint32(rectangle.Right))
		mix(uint32(rectangle.Bottom))
	}
	return hash
}

// ApplyAlphaRegion gives a layered top-level HWND a real binary shape derived
// from its presented alpha. Unlike HTTRANSPARENT, SetWindowRgn is honored
// across process and GUI-thread boundaries, so pixels outside the region
// genuinely belong to whatever window is underneath.
func ApplyAlphaRegion(
	hwnd uintptr,
	source *image.RGBA,
	threshold byte,
) (uint64, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return 0, ErrInvalidWindow
	}
	rectangles, width, height := alphaRegionRectangles(source, threshold)
	if source == nil || width < 1 || height < 1 {
		return 0, fmt.Errorf("alpha window region image is empty")
	}
	signature := AlphaRegionSignature(source, threshold)

	var region uintptr
	var callErr error
	if len(rectangles) == 0 {
		region, _, callErr = pixelRegionCreateRectRgn.Call(0, 0, 0, 0)
	} else {
		data := make(
			[]byte,
			regionHeaderSize+len(rectangles)*regionRectangleSize,
		)
		binary.LittleEndian.PutUint32(data[0:4], regionHeaderSize)
		binary.LittleEndian.PutUint32(data[4:8], regionDataRectangles)
		binary.LittleEndian.PutUint32(data[8:12], uint32(len(rectangles)))
		binary.LittleEndian.PutUint32(
			data[12:16],
			uint32(len(rectangles)*regionRectangleSize),
		)
		bounds := rectangleBounds(rectangles)
		putRegionRectangle(data[16:32], bounds)
		for index, rectangle := range rectangles {
			offset := regionHeaderSize + index*regionRectangleSize
			putRegionRectangle(data[offset:offset+regionRectangleSize], rectangle)
		}
		region, _, callErr = pixelRegionExtCreateRegion.Call(
			0,
			uintptr(len(data)),
			uintptr(unsafe.Pointer(&data[0])),
		)
	}
	if region == 0 {
		return 0, boolCallError("Create alpha window region", callErr)
	}

	// Windows owns region after a successful SetWindowRgn. It remains ours on
	// failure, matching the ownership rules used by the rounded-region path.
	applied, _, callErr := cornersSetWindowRgn.Call(hwnd, region, 1)
	if applied == 0 {
		_, _, _ = cornersDeleteObject.Call(region)
		return 0, boolCallError("SetWindowRgn(alpha)", callErr)
	}
	return signature, nil
}

func alphaRegionRectangles(
	source *image.RGBA,
	threshold byte,
) ([]Rect, int, int) {
	if source == nil {
		return nil, 0, 0
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return nil, width, height
	}
	if threshold == 0 {
		threshold = 1
	}

	type span struct {
		left, right int32
	}
	rectangles := make([]Rect, 0, height)
	active := make(map[span]int)
	for localY := 0; localY < height; localY++ {
		y := bounds.Min.Y + localY
		spans := make([]span, 0, 4)
		for localX := 0; localX < width; {
			x := bounds.Min.X + localX
			if source.Pix[source.PixOffset(x, y)+3] < threshold {
				localX++
				continue
			}
			left := localX
			for localX < width {
				x = bounds.Min.X + localX
				if source.Pix[source.PixOffset(x, y)+3] < threshold {
					break
				}
				localX++
			}
			spans = append(spans, span{
				left: int32(left), right: int32(localX),
			})
		}

		nextActive := make(map[span]int, len(spans))
		for _, current := range spans {
			if index, ok := active[current]; ok {
				rectangles[index].Bottom = int32(localY + 1)
				nextActive[current] = index
				continue
			}
			rectangles = append(rectangles, Rect{
				Left: current.left, Top: int32(localY),
				Right: current.right, Bottom: int32(localY + 1),
			})
			nextActive[current] = len(rectangles) - 1
		}
		active = nextActive
	}
	return rectangles, width, height
}

func rectangleBounds(rectangles []Rect) Rect {
	if len(rectangles) == 0 {
		return Rect{}
	}
	bounds := rectangles[0]
	for _, rectangle := range rectangles[1:] {
		if rectangle.Left < bounds.Left {
			bounds.Left = rectangle.Left
		}
		if rectangle.Top < bounds.Top {
			bounds.Top = rectangle.Top
		}
		if rectangle.Right > bounds.Right {
			bounds.Right = rectangle.Right
		}
		if rectangle.Bottom > bounds.Bottom {
			bounds.Bottom = rectangle.Bottom
		}
	}
	return bounds
}

func putRegionRectangle(destination []byte, rectangle Rect) {
	binary.LittleEndian.PutUint32(destination[0:4], uint32(rectangle.Left))
	binary.LittleEndian.PutUint32(destination[4:8], uint32(rectangle.Top))
	binary.LittleEndian.PutUint32(destination[8:12], uint32(rectangle.Right))
	binary.LittleEndian.PutUint32(destination[12:16], uint32(rectangle.Bottom))
}
