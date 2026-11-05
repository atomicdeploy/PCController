//go:build windows

package ui

import (
	"time"

	"github.com/lxn/win"

	"rayanlamp.local/app/internal/winfx"
)

func (s *Shell) beginEntranceFade() bool {
	if s == nil || s.mw == nil || !s.motionEnabled {
		return false
	}
	s.stopWindowFadeTimer()
	s.restoreUniformWindowOpacity()
	s.windowFade.Begin(
		time.Now(),
		windowFadeDuration,
		0,
		255,
		false,
	)
	if s.directXForegroundEnabled() {
		s.windowFadeBackend = windowFadeBackendDirectX
		if err := s.setDirectXOpacity(0); err == nil {
			return true
		} else {
			s.logf("prepare DirectX entrance fade: %v", err)
			s.windowFadeBackend = windowFadeBackendNone
		}
	}
	if s.perPixelOverlay {
		s.windowFadeBackend = windowFadeBackendPerPixel
		if err := s.presentWindowFadeOpacity(0); err != nil {
			s.logf("prepare per-pixel entrance fade: %v", err)
			s.windowFade.Set(255)
			s.windowFadeBackend = windowFadeBackendNone
			return false
		}
		return true
	}

	session, err := winfx.BeginUniformWindowOpacity(s.HWND(), 0)
	if err != nil {
		s.logf("prepare native entrance fade: %v", err)
		if session != nil {
			s.windowOpacitySession = session
			s.restoreUniformWindowOpacity()
		}
		s.windowFade.Set(255)
		return false
	}
	s.windowOpacitySession = session
	s.windowFadeBackend = windowFadeBackendUniform
	return true
}

func (s *Shell) startWindowFadeTimer() bool {
	if s == nil || s.mw == nil {
		return false
	}
	if s.windowFadeTimerRunning {
		return true
	}
	s.windowFadeTimerRunning =
		win.SetTimer(s.mw.Handle(), windowFadeTimerID, 16, 0) != 0
	return s.windowFadeTimerRunning
}

func (s *Shell) stopWindowFadeTimer() {
	if s == nil || s.mw == nil {
		return
	}
	if s.windowFadeTimerRunning {
		win.KillTimer(s.mw.Handle(), windowFadeTimerID)
		s.windowFadeTimerRunning = false
	}
}

func (s *Shell) advanceWindowFade() {
	if s == nil || !s.windowFade.Active() {
		s.stopWindowFadeTimer()
		return
	}
	closing := s.windowFade.closing
	opacity, done := s.windowFade.Sample(time.Now())
	if err := s.presentWindowFadeOpacity(opacity); err != nil {
		s.logf("animate window opacity: %v", err)
		if closing {
			s.completeWindowExit()
		} else {
			s.completeEntranceFade()
		}
		return
	}
	if !done {
		return
	}
	if closing {
		s.completeWindowExit()
	} else {
		s.completeEntranceFade()
	}
}

func (s *Shell) presentWindowFadeOpacity(opacity byte) error {
	switch s.windowFadeBackend {
	case windowFadeBackendPerPixel:
		if s.presentedSurface != nil {
			return winfx.UpdatePerPixelOverlayWithOpacity(
				s.HWND(),
				s.presentedSurface,
				opacity,
			)
		}
		// Before the first Show there may be no cached pixels yet. Ask Walk to
		// render while the layered HWND is still hidden; if Windows defers that
		// paint, the first visible WM_PAINT will use windowFade.Opacity().
		if s.surface != nil {
			s.surface.Invalidate()
			win.UpdateWindow(s.surface.Handle())
		}
	case windowFadeBackendDirectX:
		return s.setDirectXOpacity(opacity)
	case windowFadeBackendUniform:
		if s.windowOpacitySession != nil {
			return s.windowOpacitySession.Set(opacity)
		}
	}
	return nil
}

func (s *Shell) completeEntranceFade() {
	if s == nil {
		return
	}
	s.stopWindowFadeTimer()
	s.windowFade.Set(255)
	backend := s.windowFadeBackend
	switch backend {
	case windowFadeBackendPerPixel:
		if err := s.presentWindowFadeOpacity(255); err != nil {
			s.logf("finish per-pixel entrance fade: %v", err)
		}
	case windowFadeBackendDirectX:
		if err := s.setDirectXOpacity(255); err != nil {
			s.logf("finish DirectX entrance fade: %v", err)
		}
	case windowFadeBackendUniform:
		s.restoreUniformWindowOpacity()
	}
	s.windowFadeBackend = windowFadeBackendNone
	if backend == windowFadeBackendUniform {
		// Removing the temporary layered style lets DWM own the ordinary
		// native/acrylic surface again.
		s.reapplyComposition()
	}
}

func (s *Shell) settleEntranceFade() {
	if s != nil && s.windowFade.Active() && !s.windowFade.closing {
		s.completeEntranceFade()
	}
}

func (s *Shell) cancelEntranceFadeWhileHidden() {
	if s == nil || !s.windowFade.Active() || s.windowFade.closing {
		return
	}
	s.stopWindowFadeTimer()
	s.windowFade.Set(255)
	if s.windowFadeBackend == windowFadeBackendPerPixel &&
		s.presentedSurface != nil {
		if err := winfx.UpdatePerPixelOverlayWithOpacity(
			s.HWND(),
			s.presentedSurface,
			255,
		); err != nil {
			s.logf("restore hidden per-pixel opacity: %v", err)
		}
	}
	if s.windowFadeBackend == windowFadeBackendDirectX {
		if err := s.setDirectXOpacity(255); err != nil {
			s.logf("restore hidden DirectX opacity: %v", err)
		}
	}
	s.restoreUniformWindowOpacity()
	s.windowFadeBackend = windowFadeBackendNone
}

func (s *Shell) beginExitTransition() {
	if s == nil || s.mw == nil || s.windowCloseAllowed ||
		s.exitTransitionStarted {
		return
	}
	s.exitTransitionStarted = true
	s.closeAuxiliaryWindowsForExit()
	s.saveWindowBounds()
	if !s.canAnimateWindowExit() {
		s.completeWindowExit()
		return
	}
	s.mw.SetEnabled(false)

	if s.windowFade.Active() && !s.windowFade.closing {
		// A very fast quit can arrive during startup. Reverse from the opacity
		// already on screen instead of flashing to fully opaque first.
		current := s.windowFade.Opacity()
		duration := time.Duration(
			int64(windowFadeDuration) * int64(current) / 255,
		)
		s.windowFade.Begin(time.Now(), duration, current, 0, true)
		if !s.windowFade.Active() {
			s.completeWindowExit()
			return
		}
		if !s.startWindowFadeTimer() {
			s.logf("unable to start reversed exit fade timer")
			s.completeWindowExit()
		}
		return
	}

	s.stopWindowFadeTimer()
	s.windowFade.Begin(
		time.Now(),
		windowFadeDuration,
		255,
		0,
		true,
	)
	if s.directXForegroundEnabled() {
		s.windowFadeBackend = windowFadeBackendDirectX
	} else if s.perPixelOverlay {
		s.windowFadeBackend = windowFadeBackendPerPixel
	} else {
		session, err := winfx.BeginUniformWindowOpacity(s.HWND(), 255)
		if err != nil {
			s.logf("prepare native exit fade: %v", err)
			if session != nil {
				s.windowOpacitySession = session
			}
			s.completeWindowExit()
			return
		}
		s.windowOpacitySession = session
		s.windowFadeBackend = windowFadeBackendUniform
	}
	if !s.startWindowFadeTimer() {
		s.logf("unable to start exit fade timer")
		s.completeWindowExit()
	}
}

func (s *Shell) canAnimateWindowExit() bool {
	return s != nil &&
		s.mw != nil &&
		s.motionEnabled &&
		win.IsWindowVisible(s.mw.Handle()) &&
		!win.IsIconic(s.mw.Handle())
}

func (s *Shell) completeWindowExit() {
	if s == nil || s.mw == nil {
		return
	}
	s.stopWindowFadeTimer()
	// Restore temporary styles only after the zero-alpha window is hidden;
	// otherwise restoring opacity to 255 could produce a one-frame exit flash.
	win.ShowWindow(s.mw.Handle(), win.SW_HIDE)
	s.restoreUniformWindowOpacity()
	s.windowFade.Set(0)
	s.windowFadeBackend = windowFadeBackendNone
	s.windowCloseAllowed = true
	if win.PostMessage(s.mw.Handle(), win.WM_CLOSE, 0, 0) == 0 {
		s.logf("unable to post final WM_CLOSE after exit fade")
		s.mw.Close()
	}
}

func (s *Shell) restoreUniformWindowOpacity() {
	if s == nil || s.windowOpacitySession == nil {
		return
	}
	session := s.windowOpacitySession
	if err := session.Restore(); err != nil {
		s.logf("restore native window opacity: %v", err)
		if session.Active() {
			// Style writes can fail transiently during teardown. The broader
			// per-pixel reset is intentionally reserved for this recovery path.
			if recoveryErr := winfx.SetPerPixelOverlay(
				s.HWND(),
				false,
			); recoveryErr != nil {
				s.logf("recover native window style: %v", recoveryErr)
				return
			}
		}
	}
	s.windowOpacitySession = nil
}

func (s *Shell) cleanupWindowFade() {
	if s == nil {
		return
	}
	s.stopWindowFadeTimer()
	if s.exitTransitionStarted && s.mw != nil &&
		win.IsWindowVisible(s.mw.Handle()) {
		win.ShowWindow(s.mw.Handle(), win.SW_HIDE)
	}
	s.restoreUniformWindowOpacity()
	s.windowFadeBackend = windowFadeBackendNone
	s.presentedSurface = nil
}

func (s *Shell) closeAuxiliaryWindowsForExit() {
	if s == nil {
		return
	}
	if settings := s.settings; settings != nil && !settings.IsDisposed() {
		settings.Dispose()
		s.settings = nil
	}
	if dialog := s.diagnostics; dialog != nil && !dialog.IsDisposed() {
		win.ShowWindow(dialog.Handle(), win.SW_HIDE)
		dialog.Cancel()
		s.diagnostics = nil
	}
}
