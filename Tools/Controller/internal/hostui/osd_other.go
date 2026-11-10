//go:build !windows

package hostui

import (
	"image"
	"pccontroller.local/desktop/osd"
)

func activateOSDAction(string) error          { return osd.ErrUnsupported }
func loadOSDIcon(string) (image.Image, error) { return nil, osd.ErrUnsupported }
