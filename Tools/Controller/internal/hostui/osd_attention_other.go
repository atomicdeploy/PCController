//go:build !windows

package hostui

import "pccontroller.local/desktop/osd"

func flashOSDTaskbar() (string, error) { return "unavailable", osd.ErrUnsupported }
