//go:build !windows

package traymenu

func showAtCursor(Options) (Result, error) {
	return Result{}, ErrUnsupportedPlatform
}

func showAtAnchor(Options, Rect) (Result, error) {
	return Result{}, ErrUnsupportedPlatform
}

// DiscardCached releases retained native popup windows on supported systems.
func DiscardCached(uintptr) {}
