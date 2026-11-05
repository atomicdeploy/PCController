//go:build windows

package ui

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/winfx"
	"rayanlamp.local/app/presentation"
)

// Documentation-only, explicitly offline sample data. No core is started and
// no command/transport callbacks exist. The owned backdrop excludes other apps
// and private desktop content from the acrylic screenshots.
func TestControllerDocumentationGallery(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	if os.Getenv("RAYANLAMP_SCREENSHOTS") == "" {
		t.Skip("gallery output not requested")
	}
	s.controller.Title = "PCController — Preview"
	cfg.UI.Animations = false
	cfg.UI.ControllerChrome = "macos"
	cfg.UI.WindowBounds = config.WindowBounds{X: 80, Y: 80, Width: 900, Height: 780}
	s.cfg = cfg
	s.applyConfigUI(cfg, false)
	s.mw.SetTitle(s.controller.Title)
	backdrop, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backdrop.Dispose)
	backdrop.SetTitle("PCController documentation backdrop")
	// A borderless, square opaque window covers the entire capture rectangle;
	// native invisible resize borders/rounded corners would expose the desktop.
	style := uint32(win.GetWindowLong(backdrop.Handle(), win.GWL_STYLE)) &^ uint32(win.WS_CAPTION|win.WS_THICKFRAME|win.WS_SYSMENU)
	win.SetWindowLong(backdrop.Handle(), win.GWL_STYLE, int32(style|win.WS_POPUP))
	_ = winfx.SetDWMWindowCornerPreference(uintptr(backdrop.Handle()), false)
	win.SetWindowPos(backdrop.Handle(), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED)
	backdrop.ToolBar().SetVisible(false)
	backdrop.StatusBar().SetVisible(false)
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{})
	backdrop.SetLayout(layout)
	_, err = walk.NewCustomWidgetPixels(backdrop, 0, func(c *walk.Canvas, b walk.Rectangle) error {
		return c.GradientFillRectanglePixels(walk.RGB(49, 38, 76), walk.RGB(41, 75, 77), walk.Horizontal, b)
	})
	if err != nil {
		t.Fatal(err)
	}
	backdrop.SetBoundsPixels(walk.Rectangle{X: 40, Y: 40, Width: 1280, Height: 900})
	backdrop.Show()
	win.SetWindowPos(backdrop.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	s.controllerState.Channels = []presentation.Channel{}
	for i, item := range []struct {
		key, name, kind, icon string
		percent               float64
	}{{"relay.5", "Garden", "relay", "trees", 100}, {"relay.6", "Stage light", "relay", "sun", 0}, {"pwm.0", "Window", "mosfet", "panel-top", 35}, {"pwm.1", "Chandelier", "mosfet", "lamp-ceiling", 70}} {
		state := "off"
		if item.percent > 0 {
			state = "on"
		}
		ch := presentation.Channel{Key: item.key, Name: item.name, Kind: item.kind, Icon: item.icon, Known: true, State: state, Percent: item.percent, Order: i % 2, Actions: []presentation.Action{{ID: item.key + ".on", Verb: "on", Label: "On"}, {ID: item.key + ".off", Verb: "off", Label: "Off"}}}
		if item.kind == "mosfet" {
			ch.Actions = append(ch.Actions, presentation.Action{ID: item.key + ".decrease", Verb: "decrease", Label: "Decrease"}, presentation.Action{ID: item.key + ".increase", Verb: "increase", Label: "Increase"})
		}
		s.controllerState.Channels = append(s.controllerState.Channels, ch)
	}
	s.mw.Show()
	pumpControllerFixture(s)
	captureControllerFixture(t, s, "gui-dashboard")
	s.controllerChannels = true
	s.surface.Invalidate()
	pumpControllerFixture(s)
	captureControllerFixture(t, s, "gui-channels")
	s.activateControllerPreference("compact")
	pumpControllerFixture(s)
	captureControllerFixture(t, s, "gui-compact")
	s.activateControllerPreference("compact")
	backdrop.SetBoundsPixels(walk.Rectangle{Width: int(win.GetSystemMetrics(win.SM_CXSCREEN)), Height: int(win.GetSystemMetrics(win.SM_CYSCREEN))})
	s.OpenSettings()
	pumpControllerFixture(s)
	if s.settings == nil || s.settings.IsDisposed() {
		t.Fatal("restored settings did not open")
	}
	settingsHandle := win.HWND(s.settings.HWND())
	var settingsPID uint32
	win.GetWindowThreadProcessId(settingsHandle, &settingsPID)
	if settingsPID != uint32(os.Getpid()) {
		t.Fatal("settings capture does not own HWND")
	}
	win.SetWindowPos(settingsHandle, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	pumpControllerFixture(s)
	win.RedrawWindow(settingsHandle, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
	_ = winfx.FlushComposition()
	settingsPath := filepath.Join(os.Getenv("RAYANLAMP_SCREENSHOTS"), "gui-settings.png")
	if output, err := exec.Command(os.Getenv("RAYANLAMP_CAPTURE_TOOL"), strconv.Itoa(os.Getpid()), settingsPath, "Desktop settings — PCController", "screen", "0").CombinedOutput(); err != nil {
		t.Fatalf("settings capture: %v %s", err, output)
	}
	s.settings.Dispose()
	pumpControllerFixture(s)
	backdrop.SetBoundsPixels(walk.Rectangle{X: 40, Y: 40, Width: 1280, Height: 900})
	for i := 0; i < 120; i++ {
		s.controllerHistory = append(s.controllerHistory, controllerTelemetryPoint{at: time.Unix(100+int64(i), 0), power: true, thermal: true, volts: 12 + 0.04*math.Sin(float64(i)/8), amps: 1.4 + 0.3*math.Sin(float64(i)/12), temperature: 36 + 1.5*math.Sin(float64(i)/24)})
	}
	for i, id := range []string{"front-panel", "channel:pwm.1", "telemetry"} {
		if err := s.openControllerWidget(id); err != nil {
			t.Fatal(err)
		}
		pumpControllerFixture(s)
		child := s.controllerWidgets[id]
		child.mw.SetBoundsPixels(walk.Rectangle{X: 64 + i*408, Y: 64, Width: 400, Height: 480})
		win.SetWindowPos(child.mw.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	}
	s.Hide()
	pumpControllerFixture(s)
	backdrop.SetBoundsPixels(walk.Rectangle{X: 40, Y: 40, Width: 1280, Height: 528})
	pumpControllerFixture(s)
	tool := os.Getenv("RAYANLAMP_CAPTURE_TOOL")
	path := filepath.Join(os.Getenv("RAYANLAMP_SCREENSHOTS"), "gui-widgets.png")
	if output, err := exec.Command(tool, strconv.Itoa(os.Getpid()), path, backdrop.Title(), "screen", "0").CombinedOutput(); err != nil {
		t.Fatalf("gallery capture: %v %s", err, output)
	}
}
