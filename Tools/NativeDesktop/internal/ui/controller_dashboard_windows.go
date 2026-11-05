//go:build windows

package ui

import (
	"fmt"
	"github.com/lxn/walk"
	"rayanlamp.local/app/internal/config"
	"strings"
)

func (s *Shell) controllerText(canvas *walk.Canvas, text string, font *walk.Font, color walk.Color, rect walk.Rectangle, centered bool) {
	format := walk.TextVCenter | walk.TextSingleLine | walk.TextNoPrefix | walk.TextEndEllipsis
	if centered {
		format |= walk.TextCenter
	} else if s.isRTL() {
		format |= walk.TextRight | walk.TextRTLReading
	} else {
		format |= walk.TextLeft
	}
	_ = s.drawSurfaceText(canvas, text, font, color, rect, format)
}

func controllerFill(canvas *walk.Canvas, rect walk.Rectangle, color walk.Color, radius int) {
	brush, err := walk.NewSolidColorBrush(color)
	if err != nil {
		return
	}
	defer brush.Dispose()
	_ = canvas.FillRoundedRectanglePixels(brush, rect, walk.Size{Width: radius, Height: radius})
}

func (s *Shell) controllerGlyph(canvas *walk.Canvas, glyph string, color walk.Color, rect walk.Rectangle) {
	if glyph == "" {
		return
	}
	// Layout rectangles are device pixels. A fixed 13pt glyph overflowed these
	// slots at 150/200% DPI even though its rectangle was correctly centered.
	points := max(5, min(24, min(rect.Width, rect.Height)-6)*72/max(96, s.surface.DPI()))
	if s.controllerIconFonts == nil {
		s.controllerIconFonts = make(map[int]*walk.Font)
	}
	font := s.controllerIconFonts[points]
	if font == nil {
		font, _ = walk.NewFont("Segoe MDL2 Assets", points, 0)
		if font != nil {
			s.controllerIconFonts[points] = font
		}
	}
	if font != nil {
		// Icons are not prose: DT_END_ELLIPSIS can replace a wide glyph with
		// dots (and shift its apparent baseline) in compact channel headers.
		_ = s.drawSurfaceText(canvas, glyph, font, color, rect, walk.TextVCenter|walk.TextSingleLine|walk.TextCenter|walk.TextNoPrefix)
	}
}

func (s *Shell) controllerControl(canvas *walk.Canvas, rect walk.Rectangle, action actionID, glyph, label string, enabled, active bool, colors palette) {
	s.layout.controllerButtons = append(s.layout.controllerButtons, controllerButton{rect: rect, action: action, enabled: enabled, active: active})
	if !s.usesGlassCompositor() {
		fill := blend(colors.panelHover, colors.accent, 0.12*s.hoverProgress(action))
		if active {
			fill = blend(colors.panelHover, colors.accent, 0.3)
		}
		controllerFill(canvas, rect, fill, 10)
		pen, err := walk.NewCosmeticPen(walk.PenSolid, blend(colors.stroke, colors.secondary, 0.2))
		if err == nil {
			_ = canvas.DrawRoundedRectanglePixels(pen, rect, walk.Size{Width: 10, Height: 10})
			pen.Dispose()
		}
	}
	color := colors.text
	if !enabled {
		color = blend(colors.secondary, colors.panel, 0.25)
	} else if active {
		color = colors.accentHover
	}
	if label == "" {
		s.controllerGlyph(canvas, glyph, color, rect)
		return
	}
	icon := walk.Rectangle{X: rect.X + 10, Y: rect.Y, Width: 24, Height: rect.Height}
	s.controllerGlyph(canvas, glyph, color, icon)
	labelRect := walk.Rectangle{X: rect.X + 40, Y: rect.Y, Width: rect.Width - 48, Height: rect.Height}
	s.controllerText(canvas, label, s.bodyFont, color, labelRect, false)
}

func (s *Shell) controllerCard(canvas *walk.Canvas, rect walk.Rectangle, colors palette) {
	s.layout.controllerCards = append(s.layout.controllerCards, rect)
	if !s.usesGlassCompositor() {
		controllerFill(canvas, rect, colors.panel, 18)
	}
}

func (s *Shell) paintController(canvas *walk.Canvas, bounds walk.Rectangle, colors palette) error {
	if s.controllerWidgetID != "" && s.controllerWidgetID != "front-panel" {
		return s.paintControllerWidget(canvas, bounds, colors)
	}
	s.controllerChannelHits = nil
	state := s.controllerState
	margin := 24
	compact := s.Config().UI.ViewMode == config.ViewCompact && !s.controllerPreferences && !s.controllerChannels
	if compact {
		margin = 12
	}
	width := bounds.Width - margin*2
	x, y := bounds.X+margin, bounds.Y+18
	if !compact {
		// Brand and lifecycle status occupy one aligned header; identity is not a
		// repeated all-caps section title. Windows owns the title-bar controls.
		if s.appIcon != nil {
			_ = canvas.DrawImageStretchedPixels(s.appIcon, walk.Rectangle{X: x, Y: y + 4, Width: 36, Height: 36})
		}
		title := strings.TrimSuffix(s.controller.Title, " — Preview")
		s.controllerText(canvas, title, s.titleFont, colors.text, walk.Rectangle{X: x + 48, Y: y, Width: width - 280, Height: 28}, false)
		secondary := state.Endpoint
		if strings.Contains(s.controller.Title, "Preview") {
			secondary = "Design preview"
		}
		s.controllerText(canvas, secondary, s.captionFont, colors.secondary, walk.Rectangle{X: x + 48, Y: y + 27, Width: width - 280, Height: 20}, false)
		status, color := "Offline", colors.secondary
		if state.Connected {
			status, color = "Connected", colors.success
		}
		pill := walk.Rectangle{X: x + width - 214, Y: y + 8, Width: 124, Height: 28}
		s.controllerControl(canvas, pill, actionConnection, "", "", s.controller.LoadPage != nil, false, colors)
		controllerFill(canvas, walk.Rectangle{X: pill.X + 10, Y: pill.Y + 11, Width: 6, Height: 6}, color, 3)
		s.controllerText(canvas, status, s.captionFont, color, walk.Rectangle{X: pill.X + 23, Y: pill.Y, Width: 94, Height: pill.Height}, false)
		s.controllerControl(canvas, walk.Rectangle{X: x + width - 36, Y: y + 4, Width: 36, Height: 36}, actionSettings, "\uE713", "", true, false, colors)
		consoleVisible := s.controller.ConsoleVisible != nil && s.controller.ConsoleVisible()
		s.controllerControl(canvas, walk.Rectangle{X: x + width - 78, Y: y + 4, Width: 36, Height: 36}, actionConsole, "\uE756", "", s.controller.ShowConsole != nil, consoleVisible, colors)
		if s.controllerPreferences {
			return s.paintControllerPreferences(canvas, walk.Rectangle{X: x, Y: y + 70, Width: width, Height: bounds.Height - 100}, colors)
		}
		y += 68
	}
	if s.controllerChannels {
		return s.paintControllerChannels(canvas, walk.Rectangle{X: x, Y: y, Width: width, Height: bounds.Height - (y - bounds.Y) - 24}, colors)
	}

	// A real front-panel instrument: neutral enclosure, independently red LED
	// segments, equal physical keys, and quieter labels beneath each key.
	fullWidth, panelY := width, y
	wide := width >= 700 && !compact
	if wide {
		width = (fullWidth - 16) * 3 / 5
	}
	panelHeight := 270
	panel := walk.Rectangle{X: x, Y: y, Width: width, Height: panelHeight}
	s.controllerCard(canvas, panel, colors)
	s.drawControllerPanelIcon(canvas, walk.Rectangle{X: x + 18, Y: y + 17, Width: 24, Height: 20}, colors.accent)
	s.controllerText(canvas, "Front panel", s.bodyFont, colors.text, walk.Rectangle{X: x + 48, Y: y + 13, Width: width - 80, Height: 26}, false)
	if s.controllerWidgetID == "" {
		glyph := "\uE73F"
		if compact {
			glyph = "\uE740"
		}
		s.controllerControl(canvas, walk.Rectangle{X: x + width - 42, Y: y + 12, Width: 28, Height: 28}, actionCompact, glyph, "", true, compact, colors)
	}
	screen := walk.Rectangle{X: x + 24, Y: y + 52, Width: width - 48, Height: 126}
	controllerFill(canvas, screen, walk.RGB(13, 13, 17), 12)
	pen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(52, 47, 52))
	if err == nil {
		_ = canvas.DrawRoundedRectanglePixels(pen, screen, walk.Size{Width: 12, Height: 12})
		pen.Dispose()
	}
	digitWidth := minInt(60, (screen.Width-56)/4)
	left := screen.X + (screen.Width-digitWidth*4)/2
	for i, raw := range state.Segments {
		if !state.HavePanel {
			raw = 0
		}
		s.drawControllerDigit(canvas, walk.Rectangle{X: left + i*digitWidth, Y: screen.Y + 20, Width: digitWidth - 12, Height: 84}, i, raw, state.Brightness, colors)
	}
	labels := []string{"Previous", "Next", "Decrease", "Increase"}
	glyphs := []string{"\uE76B", "\uE76C", "\uE738", "\uE710"}
	cell := (width - 48) / 4
	for i, label := range labels {
		center := x + 24 + i*cell + cell/2
		key := walk.Rectangle{X: center - 22, Y: y + 192, Width: 44, Height: 44}
		enabled := state.Connected && state.CanNavigate && !s.controllerBusy && !state.Busy
		s.controllerControl(canvas, key, actionKey1+actionID(i), glyphs[i], "", enabled, state.PressedKeys&(1<<uint(i)) != 0, colors)
		s.controllerText(canvas, label, s.captionFont, colors.secondary, walk.Rectangle{X: center - cell/2, Y: y + 240, Width: cell, Height: 20}, true)
	}
	if compact {
		return nil
	}
	y += panelHeight + 16
	if wide {
		s.paintBoardControl(canvas, walk.Rectangle{X: x + width + 16, Y: panelY, Width: fullWidth - width - 16, Height: 270}, 1, colors)
	}
	width = fullWidth
	if state.LCDLine1 != "" || state.LCDLine2 != "" {
		lcd := walk.Rectangle{X: x, Y: y, Width: width, Height: 62}
		s.controllerCard(canvas, lcd, colors)
		s.controllerText(canvas, state.LCDLine1, s.bodyFont, colors.text, walk.Rectangle{X: x + 18, Y: y + 7, Width: width - 36, Height: 24}, false)
		s.controllerText(canvas, state.LCDLine2, s.bodyFont, colors.secondary, walk.Rectangle{X: x + 18, Y: y + 31, Width: width - 36, Height: 24}, false)
		y += 78
	}
	if state.HavePower || state.HaveTemperature {
		labels, values := []string{}, []string{}
		if state.HavePower {
			labels = append(labels, "Supply", "Current")
			values = append(values, fmt.Sprintf("%.2f V", float64(state.SupplyMV)/1000), fmt.Sprintf("%d mA", state.CurrentMA))
		}
		if state.HaveTemperature {
			labels = append(labels, "Temperature")
			values = append(values, fmt.Sprintf("%.1f °C", state.TemperatureC))
		}
		cw := (width - (len(labels)-1)*10) / len(labels)
		for i, label := range labels {
			rect := walk.Rectangle{X: x + i*(cw+10), Y: y, Width: cw, Height: 68}
			s.controllerCard(canvas, rect, colors)
			s.controllerText(canvas, label, s.captionFont, colors.secondary, walk.Rectangle{X: rect.X + 14, Y: y + 8, Width: cw - 28, Height: 20}, false)
			s.controllerText(canvas, values[i], s.bodyFont, colors.text, walk.Rectangle{X: rect.X + 14, Y: y + 32, Width: cw - 28, Height: 25}, false)
		}
		y += 84
	}
	if !wide {
		s.paintBoardControl(canvas, walk.Rectangle{X: x, Y: y, Width: width, Height: 168}, 2, colors)
		y += 184
	}
	remaining := bounds.Height - (y - bounds.Y) - 24
	if remaining >= 230 && len(state.Channels) > 0 {
		return s.paintControllerChannels(canvas, walk.Rectangle{X: x, Y: y, Width: width, Height: remaining}, colors)
	}
	if state.Error != "" {
		s.controllerText(canvas, state.Error, s.captionFont, colors.danger, walk.Rectangle{X: x, Y: y, Width: width, Height: 24}, false)
	}
	return nil
}

func (s *Shell) paintBoardControl(canvas *walk.Canvas, bounds walk.Rectangle, columns int, colors palette) {
	x, y, width := bounds.X, bounds.Y, bounds.Width
	s.controllerCard(canvas, bounds, colors)
	s.controllerText(canvas, "Board control", s.bodyFont, colors.text, walk.Rectangle{X: x + 16, Y: y + 8, Width: width - 132, Height: 26}, false)
	for i, item := range []struct {
		id    actionID
		glyph string
	}{{actionConnection, "\uE839"}, {actionFirmware, "\uE896"}, {actionEvents, "\uE9D9"}} {
		s.controllerControl(canvas, walk.Rectangle{X: x + width - 112 + i*32, Y: y + 6, Width: 28, Height: 28}, item.id, item.glyph, "", s.controller.LoadPage != nil, false, colors)
	}
	actions := []struct {
		label, glyph string
		action       actionID
	}{{"Outputs", "\uE9E9", actionChannels}, {"RF assignments", "\uE704", actionRF}, {"Programs & effects", "\uE768", actionPrograms}, {"Buzzer & melodies", "\uE767", actionBuzzer}, {"LED strip", "\uE793", actionStrip}, {"Display text", "\uE7F4", actionDisplay}}
	bw := (width - 32 - (columns-1)*12) / columns
	for i, item := range actions {
		enabled := s.controller.LoadPage != nil
		if item.action == actionChannels {
			enabled = len(s.controllerState.Channels) > 0
		}
		s.controllerControl(canvas, walk.Rectangle{X: x + 16 + (i%columns)*(bw+12), Y: y + 42 + (i/columns)*36, Width: bw, Height: 32}, item.action, item.glyph, item.label, enabled, false, colors)
	}
}
