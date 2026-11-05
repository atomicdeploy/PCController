//go:build windows

package ui

import (
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

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
		if (b.action < actionKey1 || b.action > actionKey4) && b.action != actionCompact {
			t.Fatalf("non-panel compact action %d", b.action)
		}
	}
	// Closing a widget must leave the other compositors alive.
	if s.mw.BoundsPixels().Width < 340 {
		t.Fatal("compact mode used the donor lamp's 176px geometry")
	}
	for _, action := range []actionID{actionSettings, actionChannels} {
		if s.Config().UI.ViewMode != config.ViewCompact {
			s.activateControllerPreference("compact")
			pumpControllerFixture(s)
		}
		s.activateControllerAction(action)
		pumpControllerFixture(s)
		if s.Config().UI.ViewMode != config.ViewDashboard || s.mw.BoundsPixels().Width < 560 {
			t.Fatal("full page remained in the compact front-panel window")
		}
	}
}

func TestControllerWidgetsRestoreAndFollowCatalog(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	cfg.UI.ControllerWidgets = map[string]config.ControllerWidget{
		"front-panel":   {Visible: true, Bounds: config.WindowBounds{X: 120, Y: 120, Width: 400, Height: 340}},
		"telemetry":     {Visible: true, Bounds: config.WindowBounds{X: 530, Y: 120, Width: 400, Height: 440}},
		"channel:pwm.0": {Visible: true, Bounds: config.WindowBounds{X: 120, Y: 470, Width: 400, Height: 300}},
		"channel:pwm.1": {Visible: false},
	}
	s.cfg = cfg
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.syncControllerWidgets()
	pumpControllerFixture(s)
	if len(s.controllerWidgets) != 2 {
		t.Fatal("offline front-panel/chart windows did not restore before the channel catalog")
	}
	for _, item := range s.controllerWidgetMenuItems() {
		if item.ID == "telemetry" && (!item.Checked || item.Disabled) {
			t.Fatal("restored chart cannot be hidden without live telemetry")
		}
	}
	s.controllerState.Channels = []presentation.Channel{
		{Key: "pwm.0", Name: "Window", Kind: "mosfet"},
		{Key: "pwm.1", Name: "Garden", Kind: "mosfet"},
	}
	s.syncControllerWidgets()
	pumpControllerFixture(s)
	w := s.controllerWidgets["channel:pwm.0"]
	if w == nil || !w.mw.Visible() || s.controllerWidgets["channel:pwm.1"] != nil {
		t.Fatal("catalog restoration lost saved visibility")
	}
	b := w.mw.BoundsPixels()
	if b.X != 120 || b.Y != 470 || b.Width != 400 || b.Height != 300 {
		t.Fatalf("saved widget placement changed: %+v", b)
	}
	s.controllerState.Channels[0].Name = "Window lights"
	s.syncControllerWidgets()
	if !strings.Contains(w.mw.Title(), "Window lights") {
		t.Fatal("detached channel title did not follow rename")
	}
}

func TestControllerWidgetNativeMoveSnapsAndPersists(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	if controlModifierDown() {
		t.Skip("Ctrl is held; native snapping is explicitly bypassed")
	}
	cfg.UI.MagneticSnap = true
	s.cfg = cfg
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.controllerState.Channels = []presentation.Channel{{Key: "pwm.0", Name: "Window", Kind: "mosfet"}}
	for _, id := range []string{"front-panel", "channel:pwm.0"} {
		if err := s.openControllerWidget(id); err != nil {
			t.Fatal(err)
		}
	}
	pumpControllerFixture(s)
	peer, moving := s.controllerWidgets["front-panel"], s.controllerWidgets["channel:pwm.0"]
	peer.mw.SetBoundsPixels(walk.Rectangle{X: 100, Y: 100, Width: 400, Height: 340})
	win.SendMessage(moving.mw.Handle(), wmEnterSizeMove, 0, 0)
	proposal := winfx.Rect{Left: 507, Top: 104, Right: 907, Bottom: 404}
	if result := win.SendMessage(moving.mw.Handle(), wmMoving, 0, uintptr(unsafe.Pointer(&proposal))); result == 0 {
		t.Fatal("WM_MOVING did not handle peer snapping")
	}
	if proposal.Left != 500 || proposal.Top != 100 {
		t.Fatalf("native move did not align the widget grid: %+v", proposal)
	}
	moving.mw.SetBoundsPixels(walk.Rectangle{X: int(proposal.Left), Y: int(proposal.Top), Width: 400, Height: 300})
	win.SendMessage(moving.mw.Handle(), wmExitSizeMove, 0, 0)
	pumpControllerFixture(s)
	loaded, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	placement := loaded.UI.ControllerWidgets["channel:pwm.0"]
	if !placement.Visible || placement.Bounds.X != 500 || placement.Bounds.Y != 100 {
		t.Fatalf("snapped placement not persisted: %+v", placement)
	}
}
