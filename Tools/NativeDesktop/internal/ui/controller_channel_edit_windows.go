//go:build windows

package ui

import (
	"context"
	"math"
	"strings"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/dialogs"
	"pccontroller.local/desktop/presentation"
)

type controllerChannelHit struct {
	key                                    string
	rect, header, grip, name, icon, slider walk.Rectangle
	channel                                presentation.Channel
}
type controllerChannelGesture struct {
	key, revision    string
	slider, dragging bool
	origin, point    walk.Point
	percent          float64
	token            uint64
	owner            presentation.ControlOwner
	localContext     string
}

func (s *Shell) canEditControllerChannel() bool {
	return s.controller != nil && !s.controllerBusy && !s.controllerState.Busy && (s.controllerState.Preview || s.controller.UpdateChannel != nil && s.controllerState.ProfileAttached && s.controllerState.ProfileConfigured && s.controllerState.CatalogRevision != "")
}

// Unattached/unconfigured cards have no writable board profile. Their desktop
// arrangement is local, scoped to this host and identity, never replayed as a
// board configuration when an authenticated profile subsequently appears.
func (s *Shell) controllerLocalOrderContext() string {
	if s.controllerState.Preview || s.controllerState.ProfileAttached && s.controllerState.ProfileConfigured {
		return ""
	}
	return s.controllerState.Endpoint + "|" + s.controllerState.ControlOwner.BoardIdentity
}

func (s *Shell) canArrangeControllerChannels() bool {
	return s.controller != nil && !s.controllerBusy && !s.controllerState.Busy && s.controllerInline == nil && (s.canEditControllerChannel() || s.controllerLocalOrderContext() != "")
}

func (s *Shell) controllerChannelCursor(p walk.Point) int {
	if g := s.controllerGesture; g.key != "" {
		if g.slider {
			return win.IDC_SIZEWE
		}
		if !g.dragging || controllerDropTarget(g, s.controllerChannelHits, p) != "" {
			return win.IDC_SIZEALL
		}
		for _, h := range s.controllerChannelHits {
			if h.key == g.key && pointInRect(p, h.rect) {
				return win.IDC_SIZEALL
			}
		}
		return win.IDC_NO
	}
	for _, h := range s.controllerChannelHits {
		if pointInRect(p, h.grip) && s.canArrangeControllerChannels() {
			return win.IDC_SIZEALL
		}
		if pointInRect(p, h.header) && s.canEditControllerChannel() {
			return win.IDC_HAND
		}
	}
	if s.layout.hit(p.X, p.Y) != actionNone {
		return win.IDC_HAND
	}
	return win.IDC_ARROW
}

func (s *Shell) updateControllerChannelCursor(p walk.Point) {
	if s.surface == nil {
		return
	}
	id := s.controllerChannelCursor(p)
	cursor := walk.CursorArrow()
	switch id {
	case win.IDC_SIZEALL:
		cursor = walk.CursorSizeAll()
	case win.IDC_SIZEWE:
		cursor = walk.CursorSizeWE()
	case win.IDC_NO:
		cursor = walk.CursorNo()
	case win.IDC_HAND:
		cursor = walk.CursorHand()
	}
	s.surface.SetCursor(cursor)
	// Walk.SetCursor only stores the value; apply it now, including during
	// capture when Windows doesn't send WM_SETCURSOR on every pointer move.
	win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(uintptr(id))))
}

func controllerChannelGlyph(icon, fallback string) string {
	if icon != "" {
		for _, choice := range controllerIconChoices {
			if choice.id == strings.TrimPrefix(icon, "lucide:") {
				return choice.glyph
			}
		}
	}
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
	if s.layout.hit(x, y) != actionNone {
		return false
	}
	for _, h := range s.controllerChannelHits {
		slider := h.channel.Kind == "mosfet" && pointInRect(p, h.slider)
		if slider && !h.channel.Locked && h.channel.Known && (s.controllerState.Preview || s.controllerState.Connected && s.controller.SetPWM != nil) {
			s.controllerGestureGeneration++
			s.controllerGesture = controllerChannelGesture{key: h.key, revision: s.controllerState.CatalogRevision, owner: s.controllerState.ControlOwner, slider: true, origin: p, point: p, percent: h.channel.Percent, token: s.controllerGestureGeneration}
			s.moveControllerChannel(x, y)
			return true
		}
		if pointInRect(p, h.header) && s.canArrangeControllerChannels() && (pointInRect(p, h.grip) || s.canEditControllerChannel()) {
			s.controllerGestureGeneration++
			s.controllerGesture = controllerChannelGesture{key: h.key, revision: s.controllerState.CatalogRevision, owner: s.controllerState.ControlOwner, localContext: s.controllerLocalOrderContext(), origin: p, point: p, token: s.controllerGestureGeneration}
			s.updateControllerChannelCursor(p)
			return true
		}
		if s.controllerInline != nil && s.controllerInline.key == h.key && pointInRect(p, h.icon) {
			s.pickControllerInlineIcon()
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
	if s.surface != nil {
		s.updateControllerChannelCursor(g.point)
		s.surface.Invalidate()
	}
	return true
}

func (s *Shell) endControllerChannel(x, y int) bool {
	if s.controllerGesture.key != "" {
		s.moveControllerChannel(x, y)
	}
	g := s.controllerGesture
	if g.key == "" {
		return false
	}
	s.controllerGesture = controllerChannelGesture{}
	s.updateControllerChannelCursor(walk.Point{X: x, Y: y})
	if s.surface != nil {
		s.surface.Invalidate()
	}
	owner, currentOwner := g.owner, s.controllerState.ControlOwner
	if !g.slider {
		// A PWM calibration update doesn't change presentation ownership.
		owner.PWMRevision, currentOwner.PWMRevision = "", ""
	}
	if s.controllerState.Busy || s.controllerBusy || (!s.controllerState.Preview && (g.revision != s.controllerState.CatalogRevision || owner != currentOwner)) {
		return true
	}
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
		if s.controllerState.Connected && s.controller.SetPWM != nil {
			s.controllerPendingPWM = g
			s.runControllerOperation(func(ctx context.Context) error { return s.controller.SetPWM(ctx, g.owner, g.key, g.percent) })
		}
	} else if g.dragging {
		if g.localContext != "" {
			if g.localContext == s.controllerLocalOrderContext() {
				s.saveLocalControllerOrder(g, walk.Point{X: x, Y: y})
			}
			return true
		}
		if !s.canEditControllerChannel() {
			return true
		}
		target := controllerDropTarget(g, s.controllerChannelHits, walk.Point{X: x, Y: y})
		for _, h := range s.controllerChannelHits {
			if h.key == target {
				s.saveControllerChannel(g.key, g.revision, map[string]any{"order": h.channel.Order})
				break
			}
		}
	} else {
		for _, h := range s.controllerChannelHits {
			if h.key == g.key && pointInRect(walk.Point{X: x, Y: y}, h.header) && (pointInRect(g.origin, h.name) || pointInRect(g.origin, h.icon)) {
				s.beginControllerInline(g.key)
				if pointInRect(g.origin, h.icon) {
					s.pickControllerInlineIcon()
				}
				break
			}
		}
	}
	return true
}

func controllerDropTarget(g controllerChannelGesture, hits []controllerChannelHit, p walk.Point) string {
	if !g.dragging {
		return ""
	}
	kind := ""
	for _, h := range hits {
		if h.key == g.key {
			kind = h.channel.Kind
		}
	}
	for _, h := range hits {
		if kind != "" && h.key != g.key && h.channel.Kind == kind && pointInRect(p, h.rect) {
			return h.key
		}
	}
	return ""
}
func (s *Shell) controllerDropTarget(p walk.Point) string {
	return controllerDropTarget(s.controllerGesture, s.controllerChannelHits, p)
}

func (s *Shell) cancelControllerGestureToken(token uint64) {
	if token != 0 && s.controllerGesture.token == token {
		s.controllerGesture = controllerChannelGesture{}
		if s.surface != nil {
			s.updateControllerChannelCursor(s.visualPointer)
			s.surface.Invalidate()
		}
	}
}

func (s *Shell) saveLocalControllerOrder(g controllerChannelGesture, p walk.Point) {
	target := controllerDropTarget(g, s.controllerChannelHits, p)
	if target == "" {
		return
	}
	channels := s.orderedControllerChannels()
	from, to := -1, -1
	for i, ch := range channels {
		if ch.Key == g.key {
			from = i
		}
		if ch.Key == target {
			to = i
		}
	}
	if from < 0 || to < 0 {
		return
	}
	item := channels[from]
	channels = append(channels[:from], channels[from+1:]...)
	channels = append(channels, presentation.Channel{})
	copy(channels[to+1:], channels[to:])
	channels[to] = item
	keys := make([]string, len(channels))
	for i, ch := range channels {
		keys[i] = ch.Key
	}
	root := s.controllerRoot()
	cfg := root.Config()
	orders := make(map[string][]string, len(cfg.UI.ControllerCardOrders)+1)
	for k, v := range cfg.UI.ControllerCardOrders {
		orders[k] = append([]string(nil), v...)
	}
	orders[g.localContext] = keys
	cfg.UI.ControllerCardOrders = orders
	if err := config.Save(root.configPath, cfg); err != nil {
		root.showError("Card arrangement", err)
		return
	}
	root.ApplyConfig(cfg)
}

func (s *Shell) saveControllerChannel(key, revision string, fields map[string]any) {
	if s.controllerOwner != nil {
		s.controllerOwner.saveControllerChannel(key, revision, fields)
		s.controllerOwner.syncControllerWidgets()
		return
	}
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
					target := i
					for j, peer := range s.controllerState.Channels {
						if peer.Kind == ch.Kind && peer.Order == v {
							target = j
							break
						}
					}
					item := *ch
					s.controllerState.Channels = append(s.controllerState.Channels[:old], s.controllerState.Channels[old+1:]...)
					s.controllerState.Channels = append(s.controllerState.Channels, presentation.Channel{})
					copy(s.controllerState.Channels[target+1:], s.controllerState.Channels[target:])
					s.controllerState.Channels[target] = item
				}
				break
			}
		}
		ranks := map[string]int{}
		for i := range s.controllerState.Channels {
			kind := s.controllerState.Channels[i].Kind
			s.controllerState.Channels[i].Order = ranks[kind]
			ranks[kind]++
		}
		if s.surface != nil {
			s.surface.Invalidate()
		}
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
	var icon *walk.PushButton
	iconValue := ch.Icon
	var save *walk.PushButton
	changed := func() {
		if name != nil && icon != nil && save != nil {
			save.SetEnabled(strings.TrimSpace(name.Text()) != "" && (name.Text() != ch.Name || iconValue != ch.Icon))
		}
	}
	spec := d.Dialog{AssignTo: &dlg, Title: "Channel properties", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 440}, Size: d.Size{Width: 460, Height: 240}, Layout: d.VBox{Margins: d.Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 12}, DefaultButton: &save, Children: []d.Widget{
		d.Label{Text: ch.Key, Font: d.Font{Bold: true, PointSize: 12}},
		d.Composite{Layout: d.Grid{Columns: 2, Spacing: 12}, Children: []d.Widget{d.Label{Text: "Name"}, d.LineEdit{AssignTo: &name, Text: ch.Name, OnTextChanged: changed}, d.Label{Text: "Icon"}, d.PushButton{AssignTo: &icon, Text: "Choose icon…", OnClicked: func() {
			if selected, ok := s.chooseControllerIcon(iconValue); ok {
				iconValue = selected
				icon.SetText("Icon: " + selected)
				changed()
			}
		}}}},
		d.VSpacer{}, d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{d.HSpacer{}, d.PushButton{AssignTo: &save, Text: "Save", Enabled: false, OnClicked: func() { dlg.Accept() }}, d.PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }}}},
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
		s.saveControllerChannel(key, revision, map[string]any{"name": strings.TrimSpace(name.Text()), "icon": iconValue})
	}
}
