//go:build windows

package ui

import (
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/winfx"
)

const minimumRememberedWindowVisibility = 32

// StartupWindowMode describes whether startup should honor the remembered
// window session or an explicit command-line intent.
type StartupWindowMode uint8

const (
	StartupRemembered StartupWindowMode = iota
	StartupForceShow
	StartupForceHidden
)

// StartupWindowAction is the visibility and native show state to apply once
// the main HWND has been configured.
type StartupWindowAction uint8

const (
	StartupHide StartupWindowAction = iota
	StartupShowNormal
	StartupShowMinimized
	StartupShowMaximized
)

// ResolveStartupWindowAction combines an explicit startup intent with the
// user's startup preference and the last persisted window session.
//
// An explicit hide wins without changing the remembered state. An explicit
// show restores a remembered maximized window, but promotes minimized to
// normal because a show command must reveal an interactable window.
func ResolveStartupWindowAction(
	mode StartupWindowMode,
	ui config.UIConfig,
) StartupWindowAction {
	switch mode {
	case StartupForceHidden:
		return StartupHide
	case StartupForceShow:
		if ui.WindowState == config.WindowStateMaximized {
			return StartupShowMaximized
		}
		return StartupShowNormal
	}

	if ui.StartMinimized || !ui.WindowVisible {
		return StartupHide
	}
	switch ui.WindowState {
	case config.WindowStateMinimized:
		return StartupShowMinimized
	case config.WindowStateMaximized:
		return StartupShowMaximized
	default:
		return StartupShowNormal
	}
}

// clampRememberedWindowBounds rescues a remembered window only when less
// than a 32-by-32 pixel portion intersects the selected monitor work area.
// Already reachable windows are returned byte-for-byte unchanged, including
// valid positions on monitors with negative virtual-screen coordinates.
func clampRememberedWindowBounds(
	bounds config.WindowBounds,
	workArea winfx.Rect,
) config.WindowBounds {
	if bounds.Width <= 0 || bounds.Height <= 0 || !workArea.Valid() {
		return bounds
	}

	left := int64(bounds.X)
	top := int64(bounds.Y)
	right := left + int64(bounds.Width)
	bottom := top + int64(bounds.Height)
	workLeft := int64(workArea.Left)
	workTop := int64(workArea.Top)
	workRight := int64(workArea.Right)
	workBottom := int64(workArea.Bottom)

	visibleWidth := intersectionLength(left, right, workLeft, workRight)
	visibleHeight := intersectionLength(top, bottom, workTop, workBottom)
	if visibleWidth >= minimumRememberedWindowVisibility &&
		visibleHeight >= minimumRememberedWindowVisibility {
		return bounds
	}

	result := bounds
	if visibleWidth < minimumRememberedWindowVisibility {
		result.X = int(clampWindowAxis(
			left,
			int64(bounds.Width),
			workLeft,
			workRight,
		))
	}
	if visibleHeight < minimumRememberedWindowVisibility {
		result.Y = int(clampWindowAxis(
			top,
			int64(bounds.Height),
			workTop,
			workBottom,
		))
	}
	return result
}

func intersectionLength(firstStart, firstEnd, secondStart, secondEnd int64) int64 {
	start := max(firstStart, secondStart)
	end := min(firstEnd, secondEnd)
	if end <= start {
		return 0
	}
	return end - start
}

func clampWindowAxis(start, size, workStart, workEnd int64) int64 {
	workSize := workEnd - workStart
	if size >= workSize {
		return workStart
	}
	return max(workStart, min(start, workEnd-size))
}
