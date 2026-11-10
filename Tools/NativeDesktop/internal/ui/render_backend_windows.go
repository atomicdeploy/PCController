//go:build windows

package ui

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"sync/atomic"
	"time"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/windx"
	"pccontroller.local/desktop/internal/winfx"
)

const (
	directXRecoveryInitialDelay = 400 * time.Millisecond
	directXRecoveryMaximumDelay = 8 * time.Second
)

// renderingState owns only the adaptive selection around windx. The
// Presenter owns and serializes the native D3D/DXGI/DComp objects.
// All fields except recoveryToken are accessed on the native UI thread.
type renderingState struct {
	requested      windx.Backend
	policyEligible bool
	probeAttempted bool
	capability     windx.Capability
	probeErr       error

	enabled   bool
	presenter *windx.Presenter
	status    windx.Status

	fallbackReason  string
	lastFailure     string
	failures        uint64
	deviceLosses    uint64
	retryCount      int
	recoveryPending bool
	recoveryToken   atomic.Uint64
}

func normalizedRenderingBackend(value string) windx.Backend {
	switch windx.Backend(strings.ToLower(strings.TrimSpace(value))) {
	case windx.BackendDirectX:
		return windx.BackendDirectX
	case windx.BackendLegacy:
		return windx.BackendLegacy
	default:
		return windx.BackendAuto
	}
}

func renderingPolicyEligible(
	backend windx.Backend,
	ui config.UIConfig,
) bool {
	if backend == windx.BackendLegacy {
		return false
	}
	// DirectComposition visuals are alpha-composited, but their alpha does not
	// participate in native HWND hit testing. Preserve UpdateLayeredWindow for
	// the exact-alpha transparent mode so alpha-zero pixels remain true native
	// click-through areas.
	return !transparentBackdrop(ui)
}

// configureRenderingBackend resolves the requested backend before the top
// level WS_EX_LAYERED policy is committed. It probes the complete hardware
// path, not merely the presence of d3d11.dll.
func (s *Shell) configureRenderingBackend(ui config.UIConfig) bool {
	if s == nil {
		return false
	}
	state := &s.rendering
	requested := normalizedRenderingBackend(ui.RenderBackend)
	eligible := renderingPolicyEligible(requested, ui)
	policyChanged := requested != state.requested ||
		eligible != state.policyEligible
	if policyChanged {
		state.recoveryToken.Add(1)
		state.recoveryPending = false
		state.retryCount = 0
		state.requested = requested
		state.policyEligible = eligible
		s.closeDirectXPresenter()
		if requested != windx.BackendLegacy {
			// A deliberate policy change is also an explicit opportunity to
			// retry a previously unavailable adapter/runtime.
			state.probeAttempted = false
			state.probeErr = nil
		}
	}

	if !eligible {
		state.enabled = false
		s.closeDirectXPresenter()
		switch {
		case requested == windx.BackendLegacy:
			state.fallbackReason = "Compatibility renderer selected"
		case transparentBackdrop(ui):
			state.fallbackReason =
				"Exact transparent click-through uses the per-pixel Windows renderer"
		default:
			state.fallbackReason = "DirectX policy is not eligible"
		}
		return false
	}
	if state.recoveryPending {
		state.enabled = false
		return false
	}
	if !state.probeAttempted {
		state.probeAttempted = true
		capability, err := windx.Probe()
		state.capability = capability
		state.probeErr = err
		if err != nil || !capability.Available {
			state.enabled = false
			if err == nil {
				err = windx.ErrUnavailable
				state.probeErr = err
			}
			state.fallbackReason =
				"DirectX initialization unavailable; using the compatibility renderer"
			s.logf("DirectX capability probe: %v", err)
			return false
		}
		s.logf(
			"render backend ready: DirectX 11 hardware via %s (feature level %s, DirectComposition)",
			capability.Adapter,
			capability.FeatureLevel.String(),
		)
	}
	if state.probeErr != nil || !state.capability.Available {
		state.enabled = false
		return false
	}
	state.enabled = true
	state.fallbackReason = ""
	return true
}

func (s *Shell) directXForegroundEnabled() bool {
	return s != nil &&
		s.surface != nil &&
		s.rendering.enabled &&
		!s.rendering.recoveryPending &&
		!s.perPixelOverlay
}

func (s *Shell) presentDirectXForeground(frame *image.RGBA) error {
	if s == nil || frame == nil || !s.directXForegroundEnabled() {
		return windx.ErrUnavailable
	}
	bounds := frame.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return windx.ErrInvalidSize
	}

	state := &s.rendering
	if state.presenter == nil {
		presenter, err := windx.NewPresenter(
			uintptr(s.surface.Handle()),
			width,
			height,
		)
		if err != nil {
			return err
		}
		state.presenter = presenter
		state.capability = presenter.Capability()
		state.status = presenter.Status()
		s.logf(
			"render backend: DirectX 11 hardware via %s (feature level %s, DirectComposition)",
			state.capability.Adapter,
			state.capability.FeatureLevel.String(),
		)
	}
	opacity := 1.0
	if s.windowFadeBackend == windowFadeBackendDirectX {
		opacity = float64(s.windowFade.Opacity()) / 255
	}
	if err := state.presenter.SetOpacity(opacity); err != nil {
		state.status = state.presenter.Status()
		return err
	}
	if err := state.presenter.PresentResized(frame); err != nil {
		state.status = state.presenter.Status()
		return err
	}
	state.status = state.presenter.Status()
	state.lastFailure = ""
	state.retryCount = 0
	return nil
}

func (s *Shell) setDirectXOpacity(opacity byte) error {
	if s == nil || !s.directXForegroundEnabled() {
		return windx.ErrUnavailable
	}
	if s.rendering.presenter == nil {
		if s.surface != nil {
			_ = s.surface.Invalidate()
		}
		return nil
	}
	err := s.rendering.presenter.SetOpacity(float64(opacity) / 255)
	s.rendering.status = s.rendering.presenter.Status()
	if err != nil {
		s.handleDirectXPresentationFailure(err)
	}
	return err
}

func (s *Shell) handleDirectXPresentationFailure(failure error) {
	if s == nil || failure == nil {
		return
	}
	state := &s.rendering
	if state.presenter != nil {
		state.status = state.presenter.Status()
	}
	state.failures++
	if windx.IsDeviceLost(failure) || state.status.DeviceLost {
		state.deviceLosses++
	}
	state.lastFailure = failure.Error()
	state.fallbackReason =
		"DirectX presentation was interrupted; compatibility rendering is active while recovery is attempted"
	state.enabled = false
	s.closeDirectXPresenter()

	// Restore the exact legacy policy synchronously so the frame that detected
	// device loss can still be presented instead of blanking the window.
	ui := s.Config().UI
	layered := perPixelForegroundForUI(ui)
	if layered != s.perPixelOverlay {
		if err := winfx.SetPerPixelOverlay(s.HWND(), layered); err != nil {
			s.logf("restore renderer after DirectX failure: %v", err)
		} else {
			s.perPixelOverlay = layered
		}
	}
	actual, err := winfx.Refresh(
		s.HWND(),
		ui.Backdrop,
		s.dark,
		effectiveTitleBarMode(ui) == config.TitleBarNative,
	)
	s.backdrop = actual
	if err != nil {
		s.logf("restore backdrop after DirectX failure: %v", err)
	}
	if s.surface != nil {
		s.surface.SetPaintMode(surfacePaintMode(ui, actual))
	}
	s.scheduleDirectXRecovery()
}

func (s *Shell) scheduleDirectXRecovery() {
	if s == nil || s.rendering.recoveryPending ||
		!s.rendering.policyEligible ||
		s.rendering.requested == windx.BackendLegacy {
		return
	}
	state := &s.rendering
	state.recoveryPending = true
	state.retryCount++
	delay := directXRecoveryInitialDelay
	for attempt := 1; attempt < state.retryCount; attempt++ {
		if delay >= directXRecoveryMaximumDelay/2 {
			delay = directXRecoveryMaximumDelay
			break
		}
		delay *= 2
	}
	token := state.recoveryToken.Add(1)
	time.AfterFunc(delay, func() {
		if s.closed.Load() || s.quitting.Load() || s.mw == nil {
			return
		}
		s.mw.Synchronize(func() {
			if s.closed.Load() || s.quitting.Load() ||
				state.recoveryToken.Load() != token ||
				!state.recoveryPending {
				return
			}
			state.recoveryPending = false
			state.probeAttempted = false
			state.probeErr = nil
			ui := s.Config().UI
			if !s.configureRenderingBackend(ui) {
				if renderingPolicyEligible(
					normalizedRenderingBackend(ui.RenderBackend),
					ui,
				) {
					s.scheduleDirectXRecovery()
				}
				return
			}
			if s.perPixelOverlay {
				if err := winfx.SetPerPixelOverlay(s.HWND(), false); err != nil {
					state.enabled = false
					state.fallbackReason =
						"DirectX recovered, but the window could not leave compatibility mode"
					s.logf("activate recovered DirectX renderer: %v", err)
					s.scheduleDirectXRecovery()
					return
				}
				s.perPixelOverlay = false
			}
			actual, err := winfx.Refresh(
				s.HWND(),
				ui.Backdrop,
				s.dark,
				effectiveTitleBarMode(ui) == config.TitleBarNative,
			)
			s.backdrop = actual
			if err != nil {
				s.logf("refresh backdrop for recovered DirectX renderer: %v", err)
			}
			if s.surface != nil {
				s.surface.SetPaintMode(surfacePaintMode(ui, actual))
				_ = s.surface.Invalidate()
			}
		})
	})
}

func (s *Shell) noteDirectXStyleFailure(failure error) {
	if s == nil || failure == nil || !s.rendering.enabled {
		return
	}
	state := &s.rendering
	state.enabled = false
	state.lastFailure = failure.Error()
	state.fallbackReason =
		"DirectX is available, but Windows rejected the required window-style transition"
	s.closeDirectXPresenter()
	s.scheduleDirectXRecovery()
}

func (s *Shell) closeDirectXPresenter() {
	if s == nil || s.rendering.presenter == nil {
		return
	}
	presenter := s.rendering.presenter
	s.rendering.status = presenter.Status()
	s.rendering.presenter = nil
	if err := presenter.Close(); err != nil {
		s.logf("close DirectX presenter: %v", err)
	}
}

func (s *Shell) closeRenderingBackend() {
	if s == nil {
		return
	}
	s.rendering.recoveryToken.Add(1)
	s.rendering.recoveryPending = false
	s.rendering.enabled = false
	s.closeDirectXPresenter()
}

func (s *Shell) renderingSummary() string {
	if s == nil {
		return "Requested: Not reported\r\nEffective: Compatibility renderer"
	}
	state := &s.rendering
	requested := state.requested
	if requested == "" {
		requested = normalizedRenderingBackend(s.Config().UI.RenderBackend)
	}
	effective := "Compatibility GDI / UpdateLayeredWindow"
	if state.enabled {
		effective = "DirectX 11 + DXGI + DirectComposition"
		if state.presenter == nil {
			effective += " (ready)"
		}
	}
	lines := []string{
		"Requested: " + renderingBackendDisplayName(requested),
		"Effective: " + effective,
	}
	if capability := state.capability; capability.Adapter != "" {
		lines = append(lines, "Adapter: "+capability.Adapter)
	}
	if level := state.capability.FeatureLevel.String(); level != "unknown" {
		lines = append(lines, "D3D feature level: "+level)
	}
	if state.capability.DedicatedVideoMemory > 0 {
		lines = append(lines, fmt.Sprintf(
			"Dedicated video memory: %.0f MiB",
			float64(state.capability.DedicatedVideoMemory)/(1024*1024),
		))
	}
	if state.probeAttempted {
		lines = append(lines, fmt.Sprintf(
			"Hardware / DirectComposition: %s / %s",
			yesNo(state.capability.Hardware),
			yesNo(state.capability.DirectComposition),
		))
	}
	status := state.status
	if state.presenter != nil {
		status = state.presenter.Status()
	}
	if status.Presents > 0 {
		lines = append(lines, fmt.Sprintf(
			"Presented frames: %d (%dx%d)",
			status.Presents,
			status.Width,
			status.Height,
		))
	}
	if state.deviceLosses > 0 {
		lines = append(lines, fmt.Sprintf(
			"Recovered device losses: %d",
			state.deviceLosses,
		))
	}
	if state.recoveryPending {
		lines = append(lines, "Recovery: scheduled automatically")
	}
	if state.fallbackReason != "" {
		lines = append(lines, "Fallback: "+state.fallbackReason)
	}
	if state.lastFailure != "" {
		lines = append(lines, "Last graphics error: "+state.lastFailure)
	} else if state.probeErr != nil &&
		!errors.Is(state.probeErr, windx.ErrUnavailable) {
		lines = append(lines, "Probe: "+state.probeErr.Error())
	}
	return strings.Join(lines, "\r\n")
}

func renderingBackendDisplayName(backend windx.Backend) string {
	switch backend {
	case windx.BackendDirectX:
		return "DirectX 11 hardware"
	case windx.BackendLegacy:
		return "Compatibility GDI"
	default:
		return "Automatic"
	}
}

func yesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}
