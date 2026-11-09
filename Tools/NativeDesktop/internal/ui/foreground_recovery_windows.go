//go:build windows

package ui

import (
	"fmt"
	"image"

	"github.com/lxn/walk"
)

// foregroundRecovery owns the two GDI render targets and three Go pixel
// surfaces used to recover per-pixel alpha from black/white passes. They are
// keyed to physical size and DPI and reused across paints; animated Fluent
// effects therefore do not allocate multiple full-window images at 60 Hz.
type foregroundRecovery struct {
	width, height int
	dpi           int
	blackBitmap   *walk.Bitmap
	whiteBitmap   *walk.Bitmap
	black         *image.RGBA
	white         *image.RGBA
	recovered     *image.RGBA
}

func newForegroundRecovery(
	width, height, dpi int,
) (_ *foregroundRecovery, err error) {
	return newForegroundRecoveryReusing(width, height, dpi, nil)
}

func newForegroundRecoveryReusing(width, height, dpi int, previous *foregroundRecovery) (_ *foregroundRecovery, err error) {
	if width < 1 || height < 1 || dpi < 1 {
		return nil, fmt.Errorf(
			"ui: invalid foreground recovery surface %dx%d at %d DPI",
			width,
			height,
			dpi,
		)
	}
	recovery := &foregroundRecovery{
		width: width, height: height, dpi: dpi,
	}
	defer func() {
		if err != nil {
			recovery.Dispose()
		}
	}()
	size := walk.Size{Width: width, Height: height}
	if recovery.blackBitmap, err = walk.NewBitmapForDPI(size, dpi); err != nil {
		return nil, fmt.Errorf("ui: create black recovery bitmap: %w", err)
	}
	if recovery.whiteBitmap, err = walk.NewBitmapForDPI(size, dpi); err != nil {
		return nil, fmt.Errorf("ui: create white recovery bitmap: %w", err)
	}
	// Both native allocations succeeded; only now may the previous frame's
	// CPU storage be cleared/reused. A creation failure retains the old frame.
	if previous == nil {
		previous = &foregroundRecovery{}
	}
	bounds := image.Rect(0, 0, width, height)
	recovery.black = reuseRGBA(previous.black, bounds)
	recovery.white = reuseRGBA(previous.white, bounds)
	recovery.recovered = reuseRGBA(previous.recovered, bounds)
	return recovery, nil
}

func (recovery *foregroundRecovery) Matches(
	width, height, dpi int,
) bool {
	return recovery != nil &&
		recovery.width == width &&
		recovery.height == height &&
		recovery.dpi == dpi &&
		recovery.blackBitmap != nil &&
		recovery.whiteBitmap != nil &&
		recovery.black != nil &&
		recovery.white != nil &&
		recovery.recovered != nil
}

func (recovery *foregroundRecovery) Dispose() {
	if recovery == nil {
		return
	}
	if recovery.blackBitmap != nil {
		recovery.blackBitmap.Dispose()
		recovery.blackBitmap = nil
	}
	if recovery.whiteBitmap != nil {
		recovery.whiteBitmap.Dispose()
		recovery.whiteBitmap = nil
	}
	recovery.black = nil
	recovery.white = nil
	recovery.recovered = nil
}

func (s *Shell) ensureForegroundRecovery(
	bounds walk.Rectangle,
) (*foregroundRecovery, error) {
	if s == nil || s.surface == nil {
		return nil, fmt.Errorf("ui: foreground surface is unavailable")
	}
	dpi := s.surface.DPI()
	if s.foregroundRecovery.Matches(bounds.Width, bounds.Height, dpi) {
		return s.foregroundRecovery, nil
	}
	replacement, err := newForegroundRecoveryReusing(
		bounds.Width,
		bounds.Height,
		dpi,
		s.foregroundRecovery,
	)
	if err != nil {
		return nil, err
	}
	s.disposeForegroundRecovery()
	s.foregroundRecovery = replacement
	return replacement, nil
}

func (s *Shell) disposeForegroundRecovery() {
	if s == nil || s.foregroundRecovery == nil {
		return
	}
	recovery := s.foregroundRecovery
	if s.hitSurface == recovery.recovered {
		s.hitSurface = nil
	}
	if s.presentedSurface == recovery.recovered {
		s.presentedSurface = nil
	}
	recovery.Dispose()
	s.foregroundRecovery = nil
}

func (s *Shell) releaseHiddenRenderResources() {
	if s == nil {
		return
	}
	s.disposeControllerChartRasters()
	// Keep the resolved backend policy/capability, but release its swap chain,
	// D3D device, DirectComposition tree, and CPU recovery surfaces while no
	// main-window pixels can be seen. The next visible paint recreates them
	// transparently through presentDirectXForeground.
	s.closeDirectXPresenter()
	s.disposeForegroundRecovery()
	s.glassFrame = nil
	s.glassCache = glassCompositionCache{}
	s.hitSurface = nil
	s.presentedSurface = nil
}
