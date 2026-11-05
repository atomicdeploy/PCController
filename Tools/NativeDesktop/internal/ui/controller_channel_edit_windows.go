//go:build windows

package ui

import (
	"context"
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"math"
	"rayanlamp.local/app/internal/dialogs"
	"rayanlamp.local/app/presentation"
	"sort"
	"strings"
)

type controllerChannelHit struct {
	key                  string
	rect, header, slider walk.Rectangle
	channel              presentation.Channel
}
type controllerChannelGesture struct {
	key, revision    string
	slider, dragging bool
	origin, point    walk.Point
	percent          float64
}

func (s *Shell) canEditControllerChannel() bool {
	return !s.controllerBusy && (s.controllerState.Preview || s.controller.UpdateChannel != nil && s.controllerState.CatalogRevision != "")
}

func controllerChannelGlyph(icon, fallback string) string {
	switch strings.TrimPrefix(icon, "lucide:") {
	case "lightbulb", "lamp", "lamp-ceiling", "lamp-floor", "lamp-desk", "lamp-wall-up", "lamp-wall-down":
		return "\uE781"
	case "sun", "sun-dim":
		return "\uE706"
	case "moon":
		return "\uE708"
	case "power":
		return "\uE7E8"
	case "fan":
		return "\uE9CA"
	case "monitor", "tv":
		return "\uE7F4"
	case "door-open", "door-closed":
		return "\uE8F4"
	case "trees", "flower", "leaf":
		return "\uE8BE"
	case "volume-2":
		return "\uE767"
	case "plug", "zap":
		return "\uE945"
	case "panel-top", "window":
		return "\uE737"
	}
	return fallback
}

func (s *Shell) beginControllerChannel(x, y int) bool {
	if s.controllerBusy || s.controllerState.Busy {
		return false
	}
	p := walk.Point{X: x, Y: y}
	for _, h := range s.controllerChannelHits {
		slider := h.channel.Kind == "mosfet" && pointInRect(p, h.slider)
		if slider && !h.channel.Locked && h.channel.Known && (s.controllerState.Preview || s.controllerState.Connected && s.controller.SetPWM != nil) {
			s.controllerGesture = controllerChannelGesture{key: h.key, slider: true, origin: p, point: p, percent: h.channel.Percent}
			s.moveControllerChannel(x, y)
			return true
		}
		if pointInRect(p, h.header) && s.canEditControllerChannel() {
			s.controllerGesture = controllerChannelGesture{key: h.key, revision: s.controllerState.CatalogRevision, origin: p, point: p}
			return true
		}
		if pointInRect(p, h.rect) && s.layout.hit(x, y) == actionNone {
			return true
		}
	}
	return false
}

func (s *Shell) moveControllerChannel(x, y int) bool {
	g := &s.controllerGesture
	if g.key == "" {
		return false
	}
	g.point = walk.Point{X: x, Y: y}
	if g.slider {
		for _, h := range s.controllerChannelHits {
			if h.key == g.key {
				g.percent = math.Round(math.Max(0, math.Min(100, float64(x-h.slider.X)*100/float64(h.slider.Width))))
			}
		}
	} else if math.Abs(float64(x-g.origin.X))+math.Abs(float64(y-g.origin.Y)) > 6 {
		g.dragging = true
	}
	s.surface.SetCursor(walk.CursorHand())
	s.surface.Invalidate()
	return true
}

func (s *Shell) endControllerChannel(x, y int) bool {
	g := s.controllerGesture
	if g.key == "" {
		return false
	}
	s.controllerGesture = controllerChannelGesture{}
	s.surface.Invalidate()
	if g.slider {
		if s.controllerState.Preview {
			for i := range s.controllerState.Channels {
				ch := &s.controllerState.Channels[i]
				if ch.Key == g.key {
					ch.Percent = g.percent
					ch.State = "off"
					if g.percent > 0 {
						ch.State = "on"
					}
				}
			}
			return true
		}
		if s.controllerState.Connected && !s.controllerState.Busy {
			s.controllerPendingPWM = g
			s.runControllerOperation(func(ctx context.Context) error { return s.controller.SetPWM(ctx, g.key, g.percent) })
		}
	} else if g.dragging {
		kind := ""
		for _, ch := range s.controllerState.Channels {
			if ch.Key == g.key {
				kind = ch.Kind
			}
		}
		for _, h := range s.controllerChannelHits {
			if h.key != g.key && h.channel.Kind == kind && pointInRect(walk.Point{X: x, Y: y}, h.rect) {
				s.saveControllerChannel(g.key, g.revision, map[string]any{"order": h.channel.Order})
				break
			}
		}
	}
	return true
}

func (s *Shell) saveControllerChannel(key, revision string, fields map[string]any) {
	if s.controllerState.Preview {
		for i := range s.controllerState.Channels {
			ch := &s.controllerState.Channels[i]
			if ch.Key == key {
				if v, ok := fields["name"].(string); ok {
					ch.Name = v
				}
				if v, ok := fields["icon"].(string); ok {
					ch.Icon = v
				}
				if v, ok := fields["order"].(int); ok {
					old := i
					target := max(0, min(v, len(s.controllerState.Channels)-1))
					item := *ch
					s.controllerState.Channels = append(s.controllerState.Channels[:old], s.controllerState.Channels[old+1:]...)
					s.controllerState.Channels = append(s.controllerState.Channels, presentation.Channel{})
					copy(s.controllerState.Channels[target+1:], s.controllerState.Channels[target:])
					s.controllerState.Channels[target] = item
				}
				break
			}
		}
		for i := range s.controllerState.Channels {
			s.controllerState.Channels[i].Order = i
		}
		s.surface.Invalidate()
		return
	}
	s.runControllerOperation(func(ctx context.Context) error { return s.controller.UpdateChannel(ctx, key, revision, fields) })
}

// A focused properties sheet is reserved for editing metadata, never used as
// the primary navigation or a substitute for a live board-control page.
func (s *Shell) editControllerChannel(key string) {
	if !s.canEditControllerChannel() {
		return
	}
	var ch presentation.Channel
	for _, item := range s.controllerState.Channels {
		if item.Key == key {
			ch = item
			break
		}
	}
	if ch.Key == "" {
		return
	}
	revision := s.controllerState.CatalogRevision
	var dlg *walk.Dialog
	var name *walk.LineEdit
	var icon *walk.ComboBox
	var save *walk.PushButton
	icons := []string{"", "lightbulb", "lamp-ceiling", "lamp-floor", "lamp-desk", "lamp-wall-up", "sun", "moon", "power", "fan", "monitor", "door-open", "trees", "volume-2", "plug", "zap", "panel-top"}
	found := false
	for _, v := range icons {
		found = found || v == ch.Icon
	}
	if !found {
		icons = append(icons, ch.Icon)
	}
	sort.Strings(icons)
	index := 0
	for i, v := range icons {
		if v == ch.Icon {
			index = i
		}
	}
	changed := func() {
		if name != nil && icon != nil && save != nil {
			save.SetEnabled(strings.TrimSpace(name.Text()) != "" && (name.Text() != ch.Name || icon.Text() != ch.Icon))
		}
	}
	spec := d.Dialog{AssignTo: &dlg, Title: "Channel properties", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 440}, Size: d.Size{Width: 460, Height: 240}, Layout: d.VBox{Margins: d.Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 12}, DefaultButton: &save, Children: []d.Widget{
		d.Label{Text: ch.Key, Font: d.Font{Bold: true, PointSize: 12}},
		d.Composite{Layout: d.Grid{Columns: 2, Spacing: 12}, Children: []d.Widget{d.Label{Text: "Name"}, d.LineEdit{AssignTo: &name, Text: ch.Name, OnTextChanged: changed}, d.Label{Text: "Icon"}, d.ComboBox{AssignTo: &icon, Model: icons, CurrentIndex: index, OnCurrentIndexChanged: changed}}},
		d.VSpacer{}, d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{d.HSpacer{}, d.PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }}, d.PushButton{AssignTo: &save, Text: "Save", Enabled: false, OnClicked: func() { dlg.Accept() }}}},
	}}
	if err := spec.Create(s.mw); err != nil {
		s.showError("Channel properties", err)
		return
	}
	defer dlg.Dispose()
	theme := dialogs.NewThemeSession(dlg)
	defer theme.Dispose()
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	dlg.SetIcon(s.appIcon)
	if dlg.Run() == walk.DlgCmdOK {
		s.saveControllerChannel(key, revision, map[string]any{"name": strings.TrimSpace(name.Text()), "icon": icon.Text()})
	}
}
