//go:build windows

package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/traymenu"
	"rayanlamp.local/app/internal/winfx"
	"rayanlamp.local/app/presentation"
)

type controllerTelemetryPoint struct {
	at                       time.Time
	generation               uint64
	volts, amps, temperature float64
	power, thermal           bool
}

func (s *Shell) controllerRoot() *Shell {
	if s.controllerOwner != nil {
		return s.controllerOwner
	}
	return s
}

func (s *Shell) controllerWidgetLabel(id string) string {
	switch id {
	case "front-panel":
		return "Front panel"
	case "telemetry":
		return "Live charts"
	}
	for _, ch := range s.controllerState.Channels {
		if id == "channel:"+ch.Key {
			return ch.Name
		}
	}
	return ""
}

func (s *Shell) controllerWidgetMinimum() walk.Size {
	if s.controllerWidgetID == "front-panel" {
		return walk.Size{Width: 340, Height: 330}
	}
	if s.controllerWidgetID == "telemetry" {
		return walk.Size{Width: 360, Height: 360}
	}
	return walk.Size{Width: 320, Height: 290}
}

func (s *Shell) showControllerWidgetMenu(x, y int32) {
	root := s.controllerRoot()
	items := []traymenu.Item{}
	add := func(id, label string, enabled bool) {
		visible := false
		if w := root.controllerWidgets[id]; w != nil {
			visible = w.mw.Visible()
		}
		items = append(items, traymenu.Item{ID: traymenu.ActionID(id), Label: label, Checked: visible, Disabled: !enabled})
	}
	add("front-panel", "Front panel", true)
	add("telemetry", "Live charts", root.controllerState.HavePower || root.controllerState.HaveTemperature || len(root.controllerHistory) > 0)
	items = append(items, traymenu.Item{Separator: true})
	for _, ch := range root.controllerState.Channels {
		add("channel:"+ch.Key, ch.Name, true)
	}
	items = append(items, traymenu.Item{Separator: true}, traymenu.Item{ID: "snap", Label: "Magnetic window snapping", Checked: root.Config().UI.MagneticSnap})
	result, err := traymenu.ShowAtAnchor(root.controllerMenuOptions(items), traymenu.Rect{Left: x, Top: y, Right: x, Bottom: y})
	if err != nil {
		root.showError("Desktop widgets", err)
		return
	}
	if result.Cancelled {
		return
	}
	if result.Action == "snap" {
		cfg := root.Config()
		cfg.UI.MagneticSnap = !cfg.UI.MagneticSnap
		if err := config.Save(root.configPath, cfg); err != nil {
			root.showError("Desktop widgets", err)
			return
		}
		root.ApplyConfig(cfg)
		return
	}
	id := string(result.Action)
	if child := root.controllerWidgets[id]; child != nil && child.mw.Visible() {
		child.Hide()
		return
	}
	if err := root.openControllerWidget(id); err != nil {
		root.showError("Desktop widget", err)
	}
}

// One UI thread, one RPC owner and one incoming observation stream. Widgets
// own only their HWND/compositor; none creates a tray, serial client or poller.
func (s *Shell) openControllerWidget(id string) error {
	if s.controllerOwner != nil {
		return s.controllerOwner.openControllerWidget(id)
	}
	if child := s.controllerWidgets[id]; child != nil {
		child.Show()
		return nil
	}
	label := s.controllerWidgetLabel(id)
	if label == "" {
		return fmt.Errorf("unknown widget %q", id)
	}
	cfg := s.Config()
	saved, exists := cfg.UI.ControllerWidgets[id]
	cfg.UI.ControllerWidgets = nil
	cfg.UI.ViewMode, cfg.UI.CloseToTray, cfg.UI.ShowTaskbar = config.ViewCompact, true, false
	cfg.UI.TitleBarMode = config.TitleBarCustom
	cfg.UI.WindowVisible, cfg.UI.WindowState = true, config.WindowStateNormal
	b := s.mw.BoundsPixels()
	cfg.UI.WindowBounds = config.WindowBounds{X: b.X + 40, Y: b.Y + 40, Width: 400, Height: 340}
	if id == "telemetry" {
		cfg.UI.WindowBounds.Height = 440
	}
	if exists && saved.Bounds.Width > 0 {
		cfg.UI.WindowBounds = saved.Bounds
	}
	options := *s.controller
	options.Title = "PCController · " + label
	ctx, cancel := context.WithCancel(s.ctx)
	child := &Shell{controller: &options, controllerOwner: s, controllerWidgetID: id,
		core: s.core, dispatcher: s.dispatcher, configPath: s.configPath, cfg: cfg, snapshot: s.snapshot,
		controllerState: s.controllerState, controllerBusy: s.controllerBusy,
		ctx: ctx, cancel: cancel, eventDone: make(chan struct{}), log: s.log}
	child.initializeLocalization()
	if err := child.createWindow(); err != nil {
		cancel()
		child.dispose()
		return err
	}
	child.appIcon = s.appIcon
	if s.appIcon != nil {
		child.mw.SetIcon(s.appIcon)
	}
	child.installEvents()
	child.applyConfigUI(cfg, false)
	child.updateControllerSegments(s.controllerState)
	child.windowSessionTracking.Store(true)
	if s.controllerWidgets == nil {
		s.controllerWidgets = map[string]*Shell{}
	}
	s.controllerWidgets[id] = child
	child.Show()
	return nil
}

func (s *Shell) syncControllerWidgets() {
	if s.controllerOwner != nil || s.closed.Load() {
		return
	}
	for id, placement := range s.Config().UI.ControllerWidgets {
		if placement.Visible && s.controllerWidgets[id] == nil && s.controllerWidgetLabel(id) != "" {
			if err := s.openControllerWidget(id); err != nil {
				s.logf("restore widget %s: %v", id, err)
			}
		}
	}
	for _, child := range s.controllerWidgets {
		child.controllerState, child.controllerBusy = s.controllerState, s.controllerBusy
		child.updateControllerSegments(s.controllerState)
		child.surface.Invalidate()
	}
}

func (s *Shell) syncControllerWidgetAppearance() {
	if s.controllerOwner != nil {
		return
	}
	for _, child := range s.controllerWidgets {
		cfg, old := s.Config(), child.Config()
		cfg.UI.ControllerWidgets = nil
		cfg.UI.WindowBounds, cfg.UI.WindowVisible, cfg.UI.WindowState = old.UI.WindowBounds, old.UI.WindowVisible, old.UI.WindowState
		cfg.UI.ViewMode, cfg.UI.CloseToTray, cfg.UI.ShowTaskbar, cfg.UI.TitleBarMode = config.ViewCompact, true, false, config.TitleBarCustom
		child.cfgMu.Lock()
		child.cfg = cfg
		child.cfgMu.Unlock()
		child.applyConfigUI(cfg, false)
	}
}

func (s *Shell) persistControllerWidget() {
	root := s.controllerOwner
	if root == nil || !s.windowSessionTracking.Load() || root.configPath == "" {
		return
	}
	bounds, visible, _ := s.rememberedWindowSession()
	placement := config.ControllerWidget{Bounds: bounds, Visible: visible}
	root.cfgMu.Lock()
	placements := make(map[string]config.ControllerWidget, len(root.cfg.UI.ControllerWidgets)+1)
	for k, v := range root.cfg.UI.ControllerWidgets {
		placements[k] = v
	}
	placements[s.controllerWidgetID] = placement
	root.cfg.UI.ControllerWidgets = placements
	root.cfgMu.Unlock()
	_, err := config.Update(root.configPath, func(cfg *config.Config) error {
		if cfg.UI.ControllerWidgets == nil {
			cfg.UI.ControllerWidgets = map[string]config.ControllerWidget{}
		}
		cfg.UI.ControllerWidgets[s.controllerWidgetID] = placement
		return nil
	})
	if err != nil {
		root.logf("save widget placement: %v", err)
	}
}

func (s *Shell) controllerSnapNeighbours() []winfx.Rect {
	if s.controller == nil {
		return nil
	}
	root := s.controllerRoot()
	var result []winfx.Rect
	add := func(peer *Shell) {
		if peer == s || peer.mw == nil || !peer.mw.Visible() || win.IsIconic(peer.mw.Handle()) {
			return
		}
		b := peer.mw.BoundsPixels()
		result = append(result, winfx.Rect{Left: int32(b.X), Top: int32(b.Y), Right: int32(b.X + b.Width), Bottom: int32(b.Y + b.Height)})
	}
	add(root)
	for _, peer := range root.controllerWidgets {
		add(peer)
	}
	return result
}

func (s *Shell) observeControllerTelemetry(state presentation.Snapshot) {
	if state.ObservedAt.IsZero() || !state.Connected || (!state.HavePower && !state.HaveTemperature) {
		return
	}
	n := len(s.controllerHistory)
	if n > 0 && s.controllerHistory[n-1].generation != state.Generation {
		s.controllerHistory = nil
		n = 0
	}
	point := controllerTelemetryPoint{at: state.ObservedAt, generation: state.Generation, volts: float64(state.SupplyMV) / 1000, amps: float64(state.CurrentMA) / 1000, temperature: state.TemperatureC, power: state.HavePower, thermal: state.HaveTemperature}
	if n > 0 {
		last := s.controllerHistory[n-1]
		if !point.at.After(last.at) {
			return
		}
		if point.at.UnixMilli()/100 == last.at.UnixMilli()/100 {
			s.controllerHistory[n-1] = point
			return
		}
	}
	s.controllerHistory = append(s.controllerHistory, point)
	if len(s.controllerHistory) > 240 {
		s.controllerHistory = append([]controllerTelemetryPoint(nil), s.controllerHistory[len(s.controllerHistory)-240:]...)
	}
}

func (s *Shell) paintControllerWidget(c *walk.Canvas, b walk.Rectangle, p palette) error {
	if s.controllerWidgetID == "telemetry" {
		return s.paintControllerCharts(c, b, p)
	}
	key := strings.TrimPrefix(s.controllerWidgetID, "channel:")
	state := s.controllerState
	s.controllerState.Channels = nil
	for _, ch := range state.Channels {
		if ch.Key == key {
			s.controllerState.Channels = []presentation.Channel{ch}
			break
		}
	}
	defer func() { s.controllerState = state }()
	return s.paintControllerChannels(c, walk.Rectangle{X: b.X + 12, Y: b.Y + 12, Width: b.Width - 24, Height: b.Height - 24}, p)
}

func (s *Shell) paintControllerCharts(c *walk.Canvas, b walk.Rectangle, p palette) error {
	history := s.controllerRoot().controllerHistory
	status := "Live charts"
	if !s.controllerState.Connected {
		status += " · Offline"
	}
	s.controllerText(c, status, s.bodyFont, p.text, walk.Rectangle{X: b.X + 16, Y: b.Y + 8, Width: b.Width - 32, Height: 28}, false)
	if len(history) == 0 {
		return nil
	}
	last := history[len(history)-1]
	type metric struct {
		name, unit string
		color      walk.Color
		value      func(controllerTelemetryPoint) (float64, bool)
	}
	metrics := []metric{}
	if last.power {
		metrics = append(metrics, metric{"Voltage", "V", p.accent, func(v controllerTelemetryPoint) (float64, bool) { return v.volts, v.power }}, metric{"Current", "A", p.success, func(v controllerTelemetryPoint) (float64, bool) { return v.amps, v.power }})
	}
	if last.thermal {
		metrics = append(metrics, metric{"Temperature", "°C", p.danger, func(v controllerTelemetryPoint) (float64, bool) { return v.temperature, v.thermal }})
	}
	height := (b.Height - 54) / max(1, len(metrics))
	for i, m := range metrics {
		r := walk.Rectangle{X: b.X + 12, Y: b.Y + 42 + i*height, Width: b.Width - 24, Height: height - 8}
		s.controllerCard(c, r, p)
		value, _ := m.value(last)
		s.controllerText(c, fmt.Sprintf("%s   %.2f %s", m.name, value, m.unit), s.captionFont, m.color, walk.Rectangle{X: r.X + 12, Y: r.Y + 6, Width: r.Width - 24, Height: 24}, false)
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, point := range history {
			if v, ok := m.value(point); ok {
				lo = math.Min(lo, v)
				hi = math.Max(hi, v)
			}
		}
		if hi-lo < 0.01 {
			lo -= 0.5
			hi += 0.5
		}
		plot := walk.Rectangle{X: r.X + 12, Y: r.Y + 36, Width: r.Width - 24, Height: max(8, r.Height-48)}
		pen, err := walk.NewCosmeticPen(walk.PenSolid, m.color)
		if err != nil {
			continue
		}
		var previous walk.Point
		have := false
		span := last.at.Sub(history[0].at).Seconds()
		if span <= 0 {
			span = 1
		}
		for _, point := range history {
			v, ok := m.value(point)
			if !ok {
				have = false
				continue
			}
			next := walk.Point{X: plot.X + int(point.at.Sub(history[0].at).Seconds()/span*float64(plot.Width)), Y: plot.Y + plot.Height - int((v-lo)/(hi-lo)*float64(plot.Height))}
			if have {
				c.DrawLinePixels(pen, previous, next)
			} else {
				controllerFill(c, walk.Rectangle{X: next.X, Y: next.Y, Width: 2, Height: 2}, m.color, 1)
			}
			previous, have = next, true
		}
		pen.Dispose()
	}
	return nil
}
