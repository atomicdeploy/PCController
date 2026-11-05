//go:build windows

package ui

import (
	"fmt"
	"github.com/lxn/walk"
)

// Cards share the donor compositor and activation lifecycle with the front
// panel. Actions are catalog IDs, never inferred commands from display names.
func (s *Shell) paintControllerChannels(canvas *walk.Canvas, bounds walk.Rectangle, colors palette) error {
	s.controllerActionIDs = make(map[actionID]string)
	s.controllerChannelHits = nil
	titleX := bounds.X
	if s.controllerChannels {
		s.controllerControl(canvas, walk.Rectangle{X: bounds.X, Y: bounds.Y, Width: 36, Height: 36}, actionChannelBack, "\uE72B", "", true, false, colors)
		titleX += 48
	}
	s.controllerText(canvas, "Outputs", s.bodyFont, colors.text, walk.Rectangle{X: titleX, Y: bounds.Y, Width: bounds.Width - 160, Height: 36}, false)
	columns := 2
	if bounds.Width < 520 {
		columns = 1
	} else if bounds.Width >= 840 {
		columns = 3
	}
	rows := max(1, (bounds.Height-100)/174)
	perPage := min(6, columns*rows)
	pages := max(1, (len(s.controllerState.Channels)+perPage-1)/perPage)
	s.controllerPage = min(s.controllerPage, pages-1)
	cw := (bounds.Width - (columns-1)*12) / columns
	start := s.controllerPage * perPage
	for slot, ch := range s.controllerState.Channels[start:min(start+perPage, len(s.controllerState.Channels))] {
		x, y := bounds.X+(slot%columns)*(cw+12), bounds.Y+50+(slot/columns)*174
		rect := walk.Rectangle{X: x, Y: y, Width: cw, Height: 162}
		s.controllerCard(canvas, rect, colors)
		header := walk.Rectangle{X: x + 8, Y: y + 8, Width: cw - 52, Height: 32}
		slider := walk.Rectangle{X: x + 20, Y: y + 70, Width: cw - 40, Height: 30}
		s.controllerChannelHits = append(s.controllerChannelHits, controllerChannelHit{key: ch.Key, rect: rect, header: header, slider: slider, channel: ch})
		editID := actionChannelFirst + actionID(slot*8+7)
		s.controllerActionIDs[editID] = "edit:" + ch.Key
		s.controllerControl(canvas, walk.Rectangle{X: x + cw - 42, Y: y + 10, Width: 30, Height: 28}, editID, "\uE70F", "", s.canEditControllerChannel(), false, colors)
		color := colors.secondary
		if ch.Known {
			if ch.State == "on" {
				color = colors.success
			} else {
				color = colors.danger
			}
		}
		controllerFill(canvas, walk.Rectangle{X: x + 16, Y: y + 20, Width: 8, Height: 8}, color, 4)
		glyph := "\uE945"
		if ch.Kind == "mosfet" {
			glyph = "\uE706"
		}
		glyph = controllerChannelGlyph(ch.Icon, glyph)
		s.controllerGlyph(canvas, glyph, colors.accent, walk.Rectangle{X: x + 34, Y: y + 10, Width: 24, Height: 28})
		s.controllerText(canvas, ch.Name, s.bodyFont, colors.text, walk.Rectangle{X: x + 66, Y: y + 10, Width: cw - 112, Height: 28}, false)
		percent := ch.Percent
		pending := s.controllerBusy && s.controllerPendingPWM.key == ch.Key
		if pending {
			percent = s.controllerPendingPWM.percent
		}
		if s.controllerGesture.key == ch.Key && s.controllerGesture.slider {
			percent = s.controllerGesture.percent
		}
		value := "—"
		if ch.Known {
			value = "Off"
			if ch.State == "on" {
				value = "On"
			}
			if ch.Kind == "mosfet" {
				value = fmt.Sprintf("%.0f%%", percent)
			}
		}
		if ch.Locked {
			value += " · Locked"
		}
		if pending {
			value += " · Pending"
			color = colors.secondary
		}
		s.controllerText(canvas, value, s.captionFont, color, walk.Rectangle{X: x + 16, Y: y + 42, Width: cw - 32, Height: 22}, false)
		if ch.Kind == "mosfet" {
			track := walk.Rectangle{X: slider.X, Y: slider.Y + 13, Width: slider.Width, Height: 4}
			controllerFill(canvas, track, colors.stroke, 2)
			fill := track
			fill.Width = int(float64(track.Width) * percent / 100)
			if fill.Width > 0 {
				controllerFill(canvas, fill, colors.accent, 2)
			}
			controllerFill(canvas, walk.Rectangle{X: track.X + fill.Width - 6, Y: slider.Y + 9, Width: 12, Height: 12}, colors.text, 6)
		} else {
			s.controllerText(canvas, ch.Key, s.captionFont, colors.secondary, slider, false)
		}
		if s.controllerGesture.dragging && pointInRect(s.controllerGesture.point, rect) {
			controllerFill(canvas, walk.Rectangle{X: x + 12, Y: y + 2, Width: cw - 24, Height: 3}, colors.accent, 1)
		}
		actions := ch.Actions
		if len(actions) > 5 {
			actions = actions[:5]
		}
		if len(actions) == 0 {
			continue
		}
		bw := (cw - 32 - (len(actions)-1)*6) / len(actions)
		for index, item := range actions {
			id := actionChannelFirst + actionID(slot*8+index)
			s.controllerActionIDs[id] = item.ID
			active := ch.Known && (item.Verb == "on" && ch.State == "on" || item.Verb == "off" && ch.State == "off")
			buttonColors := colors
			if active {
				buttonColors.accent = color
				buttonColors.accentHover = colors.primaryText
			}
			enabled := s.controllerState.Connected && !s.controllerState.Busy && !s.controllerBusy && !ch.Locked && s.controller.InvokeAction != nil
			label, glyph := item.Label, ""
			switch item.Verb {
			case "on":
				label = "On"
			case "off":
				label = "Off"
			case "increase":
				label = ""
				glyph = "\uE710"
			case "decrease":
				label = ""
				glyph = "\uE738"
			case "toggle":
				label = ""
				glyph = "\uE7E8"
			}
			button := walk.Rectangle{X: x + 16 + index*(bw+6), Y: y + 114, Width: bw, Height: 32}
			s.controllerControl(canvas, button, id, glyph, "", enabled, active, buttonColors)
			if label != "" {
				s.controllerText(canvas, label, s.captionFont, func() walk.Color {
					if active {
						return colors.primaryText
					}
					if !enabled {
						return colors.secondary
					}
					return colors.text
				}(), button, true)
			}
			if active {
				s.layout.controllerButtons[len(s.layout.controllerButtons)-1].tint = color
			}
		}
	}
	if pages > 1 {
		y := bounds.Y + bounds.Height - 36
		s.controllerControl(canvas, walk.Rectangle{X: bounds.X, Y: y, Width: 36, Height: 36}, actionChannelPrevious, "\uE76B", "", s.controllerPage > 0, false, colors)
		s.controllerText(canvas, fmt.Sprintf("%d / %d", s.controllerPage+1, pages), s.captionFont, colors.secondary, walk.Rectangle{X: bounds.X + 44, Y: y, Width: 70, Height: 36}, true)
		s.controllerControl(canvas, walk.Rectangle{X: bounds.X + 122, Y: y, Width: 36, Height: 36}, actionChannelNext, "\uE76C", "", s.controllerPage+1 < pages, false, colors)
	}
	if s.controllerState.Error != "" {
		s.controllerText(canvas, s.controllerState.Error, s.captionFont, colors.danger, walk.Rectangle{X: bounds.X + 174, Y: bounds.Y + bounds.Height - 36, Width: bounds.Width - 174, Height: 36}, false)
	}
	return nil
}
