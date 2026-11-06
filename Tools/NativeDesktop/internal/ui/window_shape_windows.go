//go:build windows

package ui

import (
	"image"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

func (s *Shell) applyPresentedAlphaRegion(surface *image.RGBA) error {
	if s == nil || s.mw == nil || surface == nil {
		return nil
	}
	signature := winfx.AlphaRegionSignature(surface, 1)
	if s.alphaRegionActive && signature == s.alphaRegionShape {
		return nil
	}
	applied, err := winfx.ApplyAlphaRegion(s.HWND(), surface, 1)
	if err != nil {
		return err
	}
	s.alphaRegionActive = true
	s.alphaRegionShape = applied
	return nil
}

func (s *Shell) clearPresentedAlphaRegion() error {
	if s == nil || s.mw == nil || !s.alphaRegionActive {
		return nil
	}
	if err := winfx.ApplyRoundedRegion(s.HWND(), 0); err != nil {
		return err
	}
	s.alphaRegionActive = false
	s.alphaRegionShape = 0
	return nil
}

func (s *Shell) applyConfiguredWindowCorners(ui config.UIConfig) error {
	if s == nil || s.mw == nil {
		return nil
	}
	if transparentFramelessWindow(ui) {
		// The next layered presentation supplies the exact alpha-derived HRGN.
		// DWM rounding must be explicitly disabled first; otherwise Windows 11
		// retains a full-HWND shadow behind the alpha-shaped device.
		return winfx.SetDWMWindowCornerPreference(s.HWND(), false)
	}
	if err := s.clearPresentedAlphaRegion(); err != nil {
		return err
	}
	return winfx.ApplyWindowCorners(
		s.HWND(),
		dwmWindowCornerRounding(ui),
		viewCornerRadius(ui.ViewMode),
	)
}

func (s *Shell) applyConfiguredWindowCornersForSize(
	ui config.UIConfig,
	width int,
	height int,
) error {
	if s == nil || s.mw == nil {
		return nil
	}
	if transparentFramelessWindow(ui) {
		// Keep the previous region during the resize transaction. paint()
		// replaces it atomically with the new-size alpha silhouette. The native
		// corner preference remains off so DWM cannot reintroduce its rectangle.
		return winfx.SetDWMWindowCornerPreference(s.HWND(), false)
	}
	if err := s.clearPresentedAlphaRegion(); err != nil {
		return err
	}
	if effectiveTitleBarMode(ui) == config.TitleBarNative {
		// WM_SIZE reports client dimensions. A top-level HRGN uses the full
		// outer window, including the ordinary Windows caption and frame.
		return winfx.ApplyWindowCorners(
			s.HWND(),
			dwmWindowCornerRounding(ui),
			viewCornerRadius(ui.ViewMode),
		)
	}
	return winfx.ApplyWindowCornersForSize(
		s.HWND(),
		dwmWindowCornerRounding(ui),
		viewCornerRadius(ui.ViewMode),
		width,
		height,
	)
}
