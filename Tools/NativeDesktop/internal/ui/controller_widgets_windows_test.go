//go:build windows

package ui

import (
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/winfx"
	"rayanlamp.local/app/presentation"
)

func TestControllerTelemetryHistoryIsBoundedAndObserved(t *testing.T) {
	s := &Shell{}
	state := presentation.Snapshot{Connected: true, HavePower: true, SupplyMV: 12000, ObservedAt: time.Unix(100, 0), Generation: 1}
	for i := 0; i < 300; i++ {
		state.ObservedAt = state.ObservedAt.Add(100 * time.Millisecond)
		s.observeControllerTelemetry(state)
	}
	if len(s.controllerHistory) != 240 {
		t.Fatal("history not bounded")
	}
	s.observeControllerTelemetry(state)
	if len(s.controllerHistory) != 240 {
		t.Fatal("duplicate timestamp appended")
	}
	state.Connected = false
	state.ObservedAt = state.ObservedAt.Add(time.Second)
	s.observeControllerTelemetry(state)
	if s.controllerHistory[239].at == state.ObservedAt {
		t.Fatal("disconnected sample appended")
	}
	state.Connected = true
	state.Generation++
	s.observeControllerTelemetry(state)
	if len(s.controllerHistory) != 1 {
		t.Fatal("history crossed board generation")
	}
}

func TestControllerWindowsSnapToPeers(t *testing.T) {
	var drag magneticDragSession
	peer := winfx.Rect{Left: 100, Top: 100, Right: 500, Bottom: 440}
	moving := winfx.Rect{Left: 507, Top: 104, Right: 907, Bottom: 444}
	result, snapped, _ := drag.Apply(moving, snapCursorPoint{X: 600, Y: 110}, winfx.Rect{Right: 1920, Bottom: 1080}, 96, 12, false, peer)
	if !snapped.SnappedX || !snapped.SnappedY || result.Left != 500 || result.Top != 100 {
		t.Fatalf("peer grid snap: %+v %+v", result, snapped)
	}
}

func TestControllerWidgetWindowsLifecycle(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	cfg.UI.ViewMode = config.ViewDashboard
	s.cfg = cfg
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.controllerState.Channels = []presentation.Channel{{Key: "pwm.0", Name: "Window", Kind: "mosfet", Known: true, State: "on", Percent: 35}}
	s.mw.Show()
	pumpControllerFixture(s)
	for _, id := range []string{"front-panel", "channel:pwm.0", "telemetry"} {
		if err := s.openControllerWidget(id); err != nil {
			t.Fatal(err)
		}
	}
	pumpControllerFixture(s)
	if len(s.controllerWidgets) != 3 {
		t.Fatal("missing widget windows")
	}
	for _, child := range s.controllerWidgets {
		if child.tray != nil || child.controllerOwner != s || child.controller != nil && child.controller.Updates != s.controller.Updates {
			t.Fatal("widget created another tray/observation owner")
		}
		// Persistent acrylic uses the donor's activation-independent Auto stack.
		if !child.mw.Visible() || child.Config().UI.Backdrop != config.BackdropAcrylic || (child.backdrop != winfx.BackdropAcrylic && child.backdrop != winfx.BackdropAuto) {
			t.Fatalf("widget not shown with acrylic: %s %v", child.controllerWidgetID, child.backdrop)
		}
	}
	if len(s.controllerWidgets["front-panel"].controllerSnapNeighbours()) != 3 {
		t.Fatal("snap peer inventory missing")
	}
	s.controllerState.Segments = [4]byte{1, 2, 3, 4}
	s.syncControllerWidgets()
	pumpControllerFixture(s)
	if s.controllerWidgets["front-panel"].controllerState.Segments != s.controllerState.Segments {
		t.Fatal("widget did not receive latest observation")
	}
	w := s.controllerWidgets["channel:pwm.0"]
	w.Quit()
	pumpControllerFixture(s)
	if w.mw.Visible() || !s.mw.Visible() {
		t.Fatal("widget close affected root lifecycle")
	}
	loaded, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.ControllerWidgets["channel:pwm.0"].Visible {
		t.Fatal("hidden widget visibility not persisted")
	}
	if err := s.openControllerWidget("channel:pwm.0"); err != nil {
		t.Fatal(err)
	}
	pumpControllerFixture(s)
	if w != s.controllerWidgets["channel:pwm.0"] || !w.mw.Visible() {
		t.Fatal("show duplicated the widget")
	}
	s.activateControllerPreference("compact")
	pumpControllerFixture(s)
	canvas, err := s.surface.CreateCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Dispose()
	if err := s.paint(canvas, s.surface.ClientBoundsPixels()); err != nil {
		t.Fatal(err)
	}
	if len(s.layout.controllerCards) != 1 {
		t.Fatal("compact mode contains non-front-panel cards")
	}
	for _, b := range s.layout.controllerButtons {
		if b.action < actionKey1 || b.action > actionKey4 {
			t.Fatalf("non-panel compact action %d", b.action)
		}
	}
	// Closing a widget must leave the other compositors alive.
	if s.mw.BoundsPixels().Width < 340 {
		t.Fatal("compact mode used the donor lamp's 176px geometry")
	}
}
