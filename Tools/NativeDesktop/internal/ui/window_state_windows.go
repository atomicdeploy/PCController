//go:build windows

package ui

import (
	"image"
	"strings"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

const minimumInteractiveAlpha byte = 8

func effectiveTitleBarMode(ui config.UIConfig) string {
	switch strings.ToLower(strings.TrimSpace(ui.TitleBarMode)) {
	case config.TitleBarNative:
		return config.TitleBarNative
	case config.TitleBarCustom:
		return config.TitleBarCustom
	case config.TitleBarNone:
		return config.TitleBarNone
	default:
		if ui.ShowTitleBar {
			return config.TitleBarCustom
		}
		return config.TitleBarNone
	}
}

func transparentBackdrop(ui config.UIConfig) bool {
	return strings.EqualFold(
		strings.TrimSpace(ui.Backdrop),
		config.BackdropTransparent,
	)
}

func noWindowBorder(ui config.UIConfig) bool {
	return strings.EqualFold(
		strings.TrimSpace(ui.BorderMode),
		config.BorderNone,
	)
}

func windowResizeBorderEnabled(ui config.UIConfig) bool {
	return !noWindowBorder(ui)
}

func transparentFramelessWindow(ui config.UIConfig) bool {
	return transparentBackdrop(ui) &&
		noWindowBorder(ui) &&
		effectiveTitleBarMode(ui) != config.TitleBarNative
}

func dwmWindowCornerRounding(ui config.UIConfig) bool {
	// In a transparent frameless window the presented alpha/HRGN owns the
	// silhouette. DWMWCP_ROUND would make DWM retain a shadow for the complete
	// rectangular HWND, including pixels that are deliberately alpha zero.
	return ui.RoundedCorners && !transparentFramelessWindow(ui)
}

func perPixelForegroundForUI(ui config.UIConfig) bool {
	// An ordinary non-client caption cannot be supplied through
	// UpdateLayeredWindow. Native mode therefore uses the standard HWND paint
	// path; transparent mode is available with custom or absent chrome.
	if effectiveTitleBarMode(ui) == config.TitleBarNative {
		return false
	}
	return ui.LayeredForeground || transparentBackdrop(ui)
}

func surfacePaintMode(ui config.UIConfig, actualBackdrop string) walk.PaintMode {
	if perPixelForegroundForUI(ui) {
		// UpdateLayeredWindow presents the complete foreground atomically. A
		// second Walk buffer only adds work and can erase the per-pixel host.
		return walk.PaintNoErase
	}
	if strings.EqualFold(
		strings.TrimSpace(actualBackdrop),
		winfx.BackdropSolid,
	) {
		// The ordinary solid HWND path owns every client pixel. Restore the
		// pre-compact double buffer so gradient, SVG, and text never become
		// individually visible during hover, resize, or native-frame updates.
		return walk.PaintBuffered
	}
	// Walk's built-in buffer starts as an empty compatible bitmap instead of a
	// copy of the DWM target. Using it for native acrylic/blur would BitBlt a
	// black rectangle over untouched material pixels.
	return walk.PaintNoErase
}

func alphaPixelInteractive(
	surface *image.RGBA,
	x int,
	y int,
	threshold byte,
) bool {
	if surface == nil || !image.Pt(x, y).In(surface.Bounds()) {
		return false
	}
	return surface.Pix[surface.PixOffset(x, y)+3] >= threshold
}

func (s *Shell) transparentPixelPassesThrough(lParam uintptr) bool {
	if s == nil || !transparentBackdrop(s.Config().UI) ||
		!s.usesPerPixelForeground() {
		return false
	}
	var bounds win.RECT
	if s.mw == nil || !win.GetWindowRect(s.mw.Handle(), &bounds) {
		return true
	}
	x := int32(int16(uint16(lParam & 0xffff)))
	y := int32(int16(uint16((lParam >> 16) & 0xffff)))
	// GetCursorPos avoids the 16-bit coordinate truncation in legacy
	// WM_NCHITTEST lParam packing on wide/negative virtual desktops.
	var cursor win.POINT
	if win.GetCursorPos(&cursor) {
		x, y = cursor.X, cursor.Y
	}
	return !alphaPixelInteractive(
		s.hitSurface,
		int(x-bounds.Left),
		int(y-bounds.Top),
		minimumInteractiveAlpha,
	)
}
