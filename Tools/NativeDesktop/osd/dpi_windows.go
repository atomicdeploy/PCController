//go:build windows

package osd

import "fmt"

var setThreadDPIContext = osdUser.NewProc("SetThreadDpiAwarenessContext")

// enterNativeDPI must be called on a locked OS thread before creating windows
// or GDI surfaces. A development executable need not have the host manifest;
// inheriting its DPI-unaware context would let Windows enlarge a 96-DPI bitmap.
// Only this thread changes: the existing GUI, TUI and callers retain their mode.
func enterNativeDPI() (restore func(), err error) {
	if setThreadDPIContext.Find() != nil {
		// Windows 7/8 use the packaged host's manifest and existing GDI fallback.
		return func() {}, nil
	}
	// Negative pseudo-handles: PMv2 (-4), then PMv1 (-3) for Windows 10 1607.
	for _, context := range []uintptr{^uintptr(3), ^uintptr(2)} {
		previous, _, callErr := setThreadDPIContext.Call(context)
		if previous != 0 {
			return func() { setThreadDPIContext.Call(previous) }, nil
		}
		err = fmt.Errorf("OSD cannot select native per-monitor DPI: %w", callErr)
	}
	return nil, err
}
