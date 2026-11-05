//go:build windows

package ui

import (
	"github.com/lxn/win"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/winfx"
)

func windowStateForStartupAction(action StartupWindowAction) string {
	switch action {
	case StartupShowMinimized:
		return config.WindowStateMinimized
	case StartupShowMaximized:
		return config.WindowStateMaximized
	default:
		return config.WindowStateNormal
	}
}

func (s *Shell) showRememberedWindow(state string) {
	if s == nil || s.mw == nil {
		return
	}
	state = normalizedWindowState(state)
	s.setRememberedWindowSession(true, state)
	activate := state != config.WindowStateMinimized
	s.showWindowWithState(state, activate, activate)
}

func (s *Shell) showWindowWithState(state string, activate, animate bool) {
	if s == nil || s.mw == nil {
		return
	}
	state = normalizedWindowState(state)
	firstAppearance := !s.windowShownOnce
	s.windowShownOnce = true
	animateEntrance := animate &&
		state != config.WindowStateMinimized &&
		firstAppearance &&
		s.beginEntranceFade()

	s.resetDragSnap()
	showCommand := int32(win.SW_RESTORE)
	switch state {
	case config.WindowStateMinimized:
		showCommand = win.SW_SHOWMINNOACTIVE
	case config.WindowStateMaximized:
		showCommand = win.SW_SHOWMAXIMIZED
	}
	win.ShowWindow(s.mw.Handle(), showCommand)
	// Keep Walk's internal visibility/layout bookkeeping synchronized. Because
	// the HWND is already visible, this does not create a separate normal frame
	// before a minimized or maximized first presentation.
	s.mw.SetVisible(true)

	// Some Windows builds accept a system backdrop while hidden but do not
	// attach its visual until the HWND is shown. Reapply in the same show
	// transaction without clearing the active material.
	if !animateEntrance ||
		s.windowFadeBackend != windowFadeBackendUniform {
		s.reapplyComposition()
	}
	if animateEntrance {
		// Force the first composited frame to use alpha zero before the timer
		// advances it. Hidden windows may defer the earlier paint.
		if s.surface != nil {
			s.surface.Invalidate()
			win.UpdateWindow(s.surface.Handle())
		}
		if !s.startWindowFadeTimer() {
			s.logf("unable to start entrance fade timer")
			s.completeEntranceFade()
		}
	}
	if !activate || state == config.WindowStateMinimized {
		return
	}
	_ = s.mw.BringToTop()
	_ = s.mw.Activate()
	win.SetForegroundWindow(s.mw.Handle())
}

func normalizedWindowState(state string) string {
	switch state {
	case config.WindowStateMinimized, config.WindowStateMaximized:
		return state
	default:
		return config.WindowStateNormal
	}
}

func (s *Shell) rememberedWindowState() string {
	if s == nil {
		return config.WindowStateNormal
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return normalizedWindowState(s.cfg.UI.WindowState)
}

func (s *Shell) setRememberedWindowVisibility(visible bool) {
	if s == nil {
		return
	}
	s.cfgMu.Lock()
	s.cfg.UI.WindowVisible = visible
	s.cfgMu.Unlock()
}

func (s *Shell) setRememberedWindowSession(visible bool, state string) {
	if s == nil {
		return
	}
	s.cfgMu.Lock()
	s.cfg.UI.WindowVisible = visible
	s.cfg.UI.WindowState = normalizedWindowState(state)
	s.cfgMu.Unlock()
}

// observeNativeWindowPlacement classifies the native show state and captures
// screen-coordinate bounds only while the HWND is normal. GetWindowRect while
// iconic can return a sentinel rectangle, and while zoomed returns the monitor
// rectangle; neither is the normal restore rectangle users expect us to keep.
func (s *Shell) observeNativeWindowPlacement() bool {
	if s == nil || s.mw == nil || !s.windowSessionTracking.Load() {
		return false
	}
	hwnd := s.mw.Handle()
	state := config.WindowStateNormal
	switch {
	case win.IsIconic(hwnd):
		state = config.WindowStateMinimized
	case win.IsZoomed(hwnd):
		state = config.WindowStateMaximized
	}
	return s.observeWindowPlacementState(state, state == config.WindowStateNormal)
}

// observeWindowPlacementState updates the in-memory session and reports only a
// show-state transition. Normal WM_MOVE/WM_SIZE traffic continuously keeps the
// cached restore rectangle current without writing the config on every pixel.
func (s *Shell) observeWindowPlacementState(
	state string,
	captureNormalBounds bool,
) bool {
	if s == nil || s.mw == nil || !s.windowSessionTracking.Load() {
		return false
	}
	state = normalizedWindowState(state)
	var bounds config.WindowBounds
	var haveBounds bool
	if captureNormalBounds {
		bounds, haveBounds = screenWindowBounds(s.mw.Handle())
	}

	s.cfgMu.Lock()
	changed := updateRememberedWindowPlacement(
		&s.cfg.UI,
		state,
		bounds,
		haveBounds,
	)
	s.cfgMu.Unlock()
	return changed
}

func updateRememberedWindowPlacement(
	ui *config.UIConfig,
	state string,
	normalBounds config.WindowBounds,
	haveNormalBounds bool,
) bool {
	if ui == nil {
		return false
	}
	state = normalizedWindowState(state)
	changed := ui.WindowState != state
	ui.WindowState = state
	if haveNormalBounds &&
		normalBounds.Width > 0 &&
		normalBounds.Height > 0 {
		ui.WindowBounds = normalBounds
	}
	return changed
}

func screenWindowBounds(hwnd win.HWND) (config.WindowBounds, bool) {
	if hwnd == 0 {
		return config.WindowBounds{}, false
	}
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) {
		return config.WindowBounds{}, false
	}
	bounds := config.WindowBounds{
		X:      int(rect.Left),
		Y:      int(rect.Top),
		Width:  int(rect.Right - rect.Left),
		Height: int(rect.Bottom - rect.Top),
	}
	return bounds, bounds.Width > 0 && bounds.Height > 0
}

// reconcileRememberedWindowBounds keeps valid multi-monitor coordinates, but
// rescues a window whose former monitor disappeared. MonitorFromRect selects
// the nearest current work area for a completely off-screen saved rectangle.
func reconcileRememberedWindowBounds(bounds config.WindowBounds) config.WindowBounds {
	if bounds.X == -1 || bounds.Y == -1 ||
		bounds.Width < 1 || bounds.Height < 1 {
		return bounds
	}
	target := winfx.Rect{
		Left:   int32(bounds.X),
		Top:    int32(bounds.Y),
		Right:  int32(bounds.X + bounds.Width),
		Bottom: int32(bounds.Y + bounds.Height),
	}
	workArea, err := winfx.MonitorWorkAreaForRect(target)
	if err != nil {
		return bounds
	}
	return clampRememberedWindowBounds(bounds, workArea)
}

func (s *Shell) rememberedWindowSession() (
	config.WindowBounds,
	bool,
	string,
) {
	if s == nil {
		return config.WindowBounds{}, true, config.WindowStateNormal
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.UI.WindowBounds,
		s.cfg.UI.WindowVisible,
		normalizedWindowState(s.cfg.UI.WindowState)
}

// applyRememberedWindowSession stamps runtime-owned fields onto a potentially
// stale modeless Settings payload without replaying them as appearance edits.
func (s *Shell) applyRememberedWindowSession(cfg config.Config) config.Config {
	bounds, visible, state := s.rememberedWindowSession()
	cfg.UI.WindowBounds = bounds
	cfg.UI.WindowVisible = visible
	cfg.UI.WindowState = state
	return cfg
}

func (s *Shell) persistWindowPlacement() {
	if s == nil {
		return
	}
	bounds, visible, state := s.rememberedWindowSession()
	if s.configPath == "" {
		return
	}
	_, err := config.Update(s.configPath, func(cfg *config.Config) error {
		if bounds.Width > 0 && bounds.Height > 0 {
			cfg.UI.WindowBounds = bounds
		}
		cfg.UI.WindowVisible = visible
		cfg.UI.WindowState = state
		return nil
	})
	if err != nil {
		s.logf("save window placement: %v", err)
	}
}
