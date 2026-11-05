//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"rayanlamp.local/app/presentation"
	"time"
)

func (s *Shell) updateControllerSegments(state presentation.Snapshot) {
	changed := false
	for digit, bits := range state.Segments {
		for segment := 0; segment < 8; segment++ {
			target := 0.0
			if state.HavePanel && bits&(1<<uint(segment)) != 0 {
				target = 1
			}
			if s.controllerSegmentTargets[digit][segment] != target {
				changed = true
			}
			s.controllerSegmentTargets[digit][segment] = target
			if !s.controllerSegmentsReady || !s.motionEnabled {
				s.controllerSegmentLevels[digit][segment] = target
			}
		}
	}
	s.controllerSegmentsReady = true
	if changed && s.motionEnabled {
		s.startVisualAnimation()
	}
}

func (s *Shell) advanceControllerSegments(elapsed time.Duration) bool {
	active := false
	step := elapsed.Seconds() / 0.075
	for digit := range s.controllerSegmentLevels {
		for segment := range s.controllerSegmentLevels[digit] {
			value, target := s.controllerSegmentLevels[digit][segment], s.controllerSegmentTargets[digit][segment]
			if value < target {
				value = min(target, value+step)
			} else if value > target {
				value = max(target, value-step)
			}
			s.controllerSegmentLevels[digit][segment] = value
			active = active || value != target
		}
	}
	return active
}

func (s *Shell) paintControllerChrome(canvas *walk.Canvas, bounds walk.Rectangle, height int, colors palette) {
	if s.Config().UI.ControllerChrome != "windows" {
		s.layout.titleDrag = walk.Rectangle{X: bounds.X + 96, Y: bounds.Y, Width: bounds.Width - 192, Height: height}
		s.controllerText(canvas, s.controller.Title, s.captionFont, colors.secondary, s.layout.titleDrag, true)
		for i, item := range []struct {
			action actionID
			color  walk.Color
		}{{actionTitleClose, walk.RGB(255, 95, 86)}, {actionTitleMinimize, walk.RGB(255, 189, 46)}, {actionTitleZoom, walk.RGB(39, 201, 63)}} {
			r := walk.Rectangle{X: bounds.X + 12 + i*26, Y: bounds.Y + (height-26)/2, Width: 26, Height: 26}
			switch item.action {
			case actionTitleClose:
				s.layout.titleClose = r
			case actionTitleMinimize:
				s.layout.titleMinimize = r
			case actionTitleZoom:
				s.layout.titleZoom = r
			}
			if !s.usesGlassCompositor() {
				controllerFill(canvas, walk.Rectangle{X: r.X + 7, Y: r.Y + 7, Width: 12, Height: 12}, item.color, 6)
			}
		}
		return
	}
	s.layout.titleDrag = walk.Rectangle{X: bounds.X, Y: bounds.Y, Width: bounds.Width - 124, Height: height}
	s.controllerText(canvas, s.controller.Title, s.captionFont, colors.secondary, walk.Rectangle{X: bounds.X + 18, Y: bounds.Y, Width: bounds.Width - 152, Height: height}, false)
	for i, item := range []struct {
		action actionID
		glyph  string
	}{{actionTitleMinimize, "\uE921"}, {actionTitleZoom, "\uE922"}, {actionTitleClose, "\uE8BB"}} {
		rect := walk.Rectangle{X: bounds.X + bounds.Width - 120 + i*38, Y: bounds.Y + 3, Width: 32, Height: 32}
		s.controllerControl(canvas, rect, item.action, item.glyph, "", true, false, colors)
		switch item.action {
		case actionTitleClose:
			s.layout.titleClose = rect
		case actionTitleZoom:
			s.layout.titleZoom = rect
		case actionTitleMinimize:
			s.layout.titleMinimize = rect
		}
	}
}

func (s *Shell) drawControllerPanelIcon(canvas *walk.Canvas, r walk.Rectangle, color walk.Color) {
	pen, err := walk.NewCosmeticPen(walk.PenSolid, color)
	if err != nil {
		return
	}
	defer pen.Dispose()
	_ = canvas.DrawRoundedRectanglePixels(pen, r, walk.Size{Width: 3, Height: 3})
	_ = canvas.DrawRectanglePixels(pen, walk.Rectangle{X: r.X + 4, Y: r.Y + 4, Width: r.Width - 8, Height: 7})
	for i := 0; i < 4; i++ {
		controllerFill(canvas, walk.Rectangle{X: r.X + 4 + i*5, Y: r.Y + 14, Width: 2, Height: 2}, color, 1)
	}
}
