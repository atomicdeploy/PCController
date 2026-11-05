//go:build windows

package ui

import (
	"errors"
	"fmt"

	"github.com/lxn/win"

	"rayanlamp.local/app/internal/winfx"
)

type backdropCommitOps struct {
	recalculateFrame func() error
	presentSurface   func() error
	redrawFrame      func() error
	flushComposition func() error
}

// immediateBackdropCommitRequired identifies the one destructive transition
// that cannot safely wait for an ordinary queued WM_PAINT. Transparent mode
// leaves DWM with no client material and may leave an alpha-derived HRGN. When
// Blur/Acrylic/Mica is enabled, the frame and layered surface must both be
// committed before returning to the modeless settings event handler.
func immediateBackdropCommitRequired(previous, next string) bool {
	if previous != winfx.BackdropTransparent {
		return false
	}
	switch next {
	case winfx.BackdropAuto,
		winfx.BackdropBlur,
		winfx.BackdropAcrylic,
		winfx.BackdropMica,
		winfx.BackdropTabbed:
		return true
	default:
		return false
	}
}

func commitBackdropTransition(operations backdropCommitOps) error {
	var failures []error
	for _, operation := range []func() error{
		operations.recalculateFrame,
		operations.presentSurface,
		operations.redrawFrame,
		operations.flushComposition,
	} {
		if operation == nil {
			continue
		}
		if err := operation(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Shell) commitBackdropTransition(nativeTitleBar bool) error {
	if s == nil || s.mw == nil {
		return nil
	}
	hwnd := s.mw.Handle()
	return commitBackdropTransition(backdropCommitOps{
		recalculateFrame: func() error {
			if !win.SetWindowPos(
				hwnd,
				0,
				0, 0, 0, 0,
				win.SWP_NOMOVE|
					win.SWP_NOSIZE|
					win.SWP_NOZORDER|
					win.SWP_NOACTIVATE|
					win.SWP_FRAMECHANGED,
			) {
				return errors.New(
					"SetWindowPos(SWP_FRAMECHANGED) did not recompose backdrop",
				)
			}
			if !nativeTitleBar {
				// DefWindowProc is allowed to restore WS_EX_WINDOWEDGE while
				// recalculating WS_THICKFRAME and DWM can recreate its active
				// one-pixel outline at the same time. Captionless modes own
				// neither native border.
				if err := winfx.ReassertWindowChrome(s.HWND(), false); err != nil {
					return fmt.Errorf("restore captionless frame policy: %w", err)
				}
			}
			return nil
		},
		presentSurface: func() error {
			if s.surface == nil {
				return nil
			}
			if err := s.surface.Invalidate(); err != nil {
				return fmt.Errorf("invalidate backdrop foreground: %w", err)
			}
			// GlassDWM's top-level Invalidate reaches OnPaint, which calls
			// UpdateLayeredWindow. RayanLamp paints through a child CustomWidget,
			// so explicitly dispatch that WM_PAINT now; paint() performs the
			// equivalent UpdateLayeredWindow commit for per-pixel mode.
			win.UpdateWindow(s.surface.Handle())
			return nil
		},
		redrawFrame: func() error {
			if !win.RedrawWindow(
				hwnd,
				nil,
				0,
				win.RDW_INVALIDATE|
					win.RDW_FRAME|
					win.RDW_ALLCHILDREN|
					win.RDW_UPDATENOW,
			) {
				return errors.New("RedrawWindow did not commit backdrop damage")
			}
			return nil
		},
		flushComposition: winfx.FlushComposition,
	})
}
