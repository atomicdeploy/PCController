// Package vector renders the application's authored SVG artwork directly at
// the device-pixel size requested by the Windows UI.
package vector

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const maxDimension = 4096

// RenderSVG rasterizes SVG data into a transparent RGBA image at exactly the
// requested device-pixel dimensions. No intermediate fixed-size bitmap is
// scaled, which keeps the device sharp at every supported DPI.
func RenderSVG(data []byte, width, height int) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, errors.New("vector: empty SVG")
	}
	if width < 1 || height < 1 || width > maxDimension || height > maxDimension {
		return nil, fmt.Errorf("vector: invalid target size %dx%d", width, height)
	}

	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("vector: parse SVG: %w", err)
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, errors.New("vector: SVG has an empty viewBox")
	}

	target := image.NewRGBA(image.Rect(0, 0, width, height))
	icon.SetTarget(0, 0, float64(width), float64(height))
	scanner := rasterx.NewScannerGV(width, height, target, target.Bounds())
	icon.Draw(rasterx.NewDasher(width, height, scanner), 1)
	return target, nil
}
