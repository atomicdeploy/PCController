//go:build windows

package ui

import (
	"context"
	"errors"
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

func pumpControllerFixture(s *Shell) {
	timer := time.AfterFunc(50*time.Millisecond, func() { s.mw.Synchronize(func() { win.PostQuitMessage(0) }) })
	s.mw.Run()
	timer.Stop()
}

func TestControllerCardsRenderedInteraction(t *testing.T) {
	for _, backend := range []string{"auto", "legacy"} {
		t.Run(backend, func(t *testing.T) { testControllerCardsRenderedInteraction(t, backend) })
	}
}

func TestControllerCardOfflineDragNativeWindow(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.controllerChannels = true
	s.controllerState.Preview = false
	s.controllerState.Endpoint = "127.0.0.1:8788"
	s.controllerState.CatalogRevision = "unconfigured-revision"
	s.controllerState.Channels = []presentation.Channel{
		{Key: "relay.5", Name: "Five", Kind: "relay", Order: 0},
		{Key: "relay.6", Name: "Six", Kind: "relay", Order: 1},
		{Key: "pwm.0", Name: "Window", Kind: "mosfet", Order: 0},
	}
	s.controller.UpdateChannel = func(context.Context, string, string, map[string]any) error {
		t.Error("offline layout attempted a board-profile mutation")
		return nil
	}
	s.controller.InvokeAction = func(context.Context, presentation.ControlOwner, string) error {
		t.Error("arrangement actuated hardware")
		return nil
	}
	s.mw.Show()
	pumpControllerFixture(s)
	canvas, err := s.surface.CreateCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Dispose()
	paint := func() {
		t.Helper()
		if err := s.paint(canvas, s.surface.ClientBoundsPixels()); err != nil {
			t.Fatal(err)
		}
	}
	send := func(message uint32, p walk.Point) {
		var flags uintptr
		if message == win.WM_LBUTTONDOWN || message == win.WM_MOUSEMOVE {
			flags = win.MK_LBUTTON
		}
		win.SendMessage(s.surface.Handle(), message, flags, uintptr(uint32(uint16(p.X))|uint32(uint16(p.Y))<<16))
	}
	paint()
	first, second, other := s.controllerChannelHits[0], s.controllerChannelHits[1], s.controllerChannelHits[2]
	start := walk.Point{X: first.grip.X + first.grip.Width/2, Y: first.grip.Y + first.grip.Height/2}
	end := walk.Point{X: second.header.X + 50, Y: second.header.Y + 15}
	bad := walk.Point{X: other.header.X + 50, Y: other.header.Y + 15}
	// Exercise the no-action -> no-action early-return path that used to leave
	// the arrow cursor on the grip, and verify the actual Win32 cursor handle.
	getCursor := windows.NewLazySystemDLL("user32.dll").NewProc("GetCursor")
	assertCursor := func(id uintptr) {
		t.Helper()
		got, _, _ := getCursor.Call()
		want := win.LoadCursor(0, win.MAKEINTRESOURCE(id))
		if got != uintptr(want) {
			t.Fatalf("cursor=%x want %x (%d)", got, want, id)
		}
	}
	send(win.WM_MOUSEMOVE, start)
	assertCursor(win.IDC_SIZEALL)
	var before win.RECT
	win.GetWindowRect(s.mw.Handle(), &before)
	send(win.WM_LBUTTONDOWN, start)
	capture, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("GetCapture").Call()
	if capture != uintptr(s.surface.Handle()) {
		t.Fatal("card did not retain pointer capture")
	}
	send(win.WM_MOUSEMOVE, bad)
	assertCursor(win.IDC_NO)
	send(win.WM_MOUSEMOVE, end)
	assertCursor(win.IDC_SIZEALL)
	if s.orderedControllerChannels()[0].Key != "relay.5" {
		t.Fatal("drag persisted before release")
	}
	send(win.WM_LBUTTONUP, end)
	pumpControllerFixture(s)
	paint()
	if s.orderedControllerChannels()[0].Key != "relay.6" {
		t.Fatal("offline drag did not reorder")
	}
	if s.controllerState.Channels[0].Key != "relay.5" {
		t.Fatal("local layout overwrote authoritative catalog")
	}
	var after win.RECT
	win.GetWindowRect(s.mw.Handle(), &after)
	if before != after {
		t.Fatal("card drag moved the application window")
	}
	loaded, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.ControllerCardOrders[s.controllerLocalOrderContext()][0] != "relay.6" {
		t.Fatal("arrangement not persisted")
	}
	// Rejected kinds and capture loss must not change the saved layout.
	first = s.controllerChannelHits[0]
	start = walk.Point{X: first.grip.X + 10, Y: first.grip.Y + 16}
	send(win.WM_LBUTTONDOWN, start)
	send(win.WM_MOUSEMOVE, bad)
	send(win.WM_LBUTTONUP, bad)
	pumpControllerFixture(s)
	paint()
	if s.orderedControllerChannels()[0].Key != "relay.6" {
		t.Fatal("incompatible drop changed layout")
	}
	send(win.WM_LBUTTONDOWN, start)
	send(win.WM_MOUSEMOVE, end)
	win.ReleaseCapture()
	pumpControllerFixture(s)
	if s.controllerGesture.key != "" {
		t.Fatal("capture loss retained gesture")
	}
	// A real configured profile ignores the offline order and owns persistence.
	s.controllerState.ProfileAttached, s.controllerState.ProfileConfigured = true, true
	if s.orderedControllerChannels()[0].Key != "relay.5" {
		t.Fatal("offline layout leaked into board profile")
	}
	paint()
	first, second = s.controllerChannelHits[0], s.controllerChannelHits[1]
	start = walk.Point{X: second.grip.X + 10, Y: second.grip.Y + 16}
	end = walk.Point{X: first.header.X + 50, Y: first.header.Y + 15}
	s.controllerState.CatalogRevision = "configured-revision"
	commits := make(chan string, 1)
	s.controller.UpdateChannel = func(_ context.Context, key, revision string, fields map[string]any) error {
		if revision != "configured-revision" || fields["order"] != 0 || len(fields) != 1 {
			return errors.New("incorrect board-profile reorder request")
		}
		commits <- key
		return nil
	}
	send(win.WM_LBUTTONDOWN, start)
	send(win.WM_MOUSEMOVE, end)
	send(win.WM_LBUTTONUP, end)
	for i := 0; i < 10 && s.controllerBusy; i++ {
		pumpControllerFixture(s)
	}
	select {
	case key := <-commits:
		if key != "relay.6" {
			t.Fatalf("reordered the wrong channel: %s", key)
		}
	default:
		t.Fatalf("configured profile did not receive the reorder: %s", s.controllerState.Error)
	}
	t.Log("native window: move/no-drop cursors, capture, release, persistence and no hardware mutations verified")
}

func captureControllerFixture(t *testing.T, s *Shell, name string) {
	t.Helper()
	tool, directory := os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS")
	if tool == "" || directory == "" {
		return
	}
	win.SetWindowPos(s.mw.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	win.RedrawWindow(s.mw.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
	if s.controllerInline != nil {
		win.RedrawWindow(s.controllerInline.popup.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
	}
	_ = winfx.FlushComposition()
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".png")
	margin := os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_MARGIN")
	if margin == "" {
		margin = "12"
	}
	if output, err := exec.Command(tool, strconv.Itoa(os.Getpid()), path, s.mw.Title(), "screen", margin).CombinedOutput(); err != nil {
		t.Fatalf("screenshot: %v: %s", err, output)
	}
}

func testControllerCardsRenderedInteraction(t *testing.T, backend string) {
	s, cfg := newControllerWindowFixture(t)
	cfg.UI.RenderBackend = backend
	cfg.UI.TitleBarMode = config.TitleBarCustom
	cfg.UI.ControllerChrome = "windows"
	s.cfg = cfg
	s.controllerChannels = true
	s.controllerState.Channels = []presentation.Channel{
		{Key: "relay.5", Name: "Relay five", Kind: "relay", Known: true, State: "off", Order: 0, Actions: []presentation.Action{{ID: "off", Verb: "off"}, {ID: "on", Verb: "on"}}},
		{Key: "relay.6", Name: "Relay six", Kind: "relay", Known: true, State: "on", Order: 1},
		{Key: "pwm.0", Name: "Window", Kind: "mosfet", Known: true, State: "on", Percent: 30, Order: 0, Actions: []presentation.Action{{ID: "pwm.on", Verb: "on"}, {ID: "pwm.off", Verb: "off"}}},
		{Key: "pwm.1", Name: "Chandelier", Kind: "mosfet", Known: true, Percent: 70, Order: 1},
	}
	s.applyConfigUI(cfg, false)
	s.mw.Show()
	pumpControllerFixture(s)
	canvas, err := s.surface.CreateCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Dispose()
	paint := func() {
		t.Helper()
		if err := s.paint(canvas, s.surface.ClientBoundsPixels()); err != nil {
			t.Fatal(err)
		}
	}
	send := func(message uint32, p walk.Point) {
		var flags uintptr
		if message == win.WM_LBUTTONDOWN || message == win.WM_MOUSEMOVE {
			flags = win.MK_LBUTTON
		}
		win.SendMessage(s.surface.Handle(), message, flags, uintptr(uint32(uint16(p.X))|uint32(uint16(p.Y))<<16))
	}
	click := func(p walk.Point) {
		send(win.WM_LBUTTONDOWN, p)
		send(win.WM_LBUTTONUP, p)
		pumpControllerFixture(s)
		paint()
	}
	paint()
	captureControllerFixture(t, s, backend+"-cards")
	if s.layout.titleClose.Height != 32 || s.layout.titleClose.Width != 46 {
		t.Fatalf("Windows caption geometry=%+v", s.layout.titleClose)
	}
	for _, button := range s.layout.controllerButtons {
		if button.action == actionTitleClose {
			t.Fatal("Windows caption reused raised card button")
		}
	}
	if s.controllerActionIDs[actionChannelFirst] != "off" || s.controllerActionIDs[actionChannelFirst+1] != "on" {
		t.Fatal("rendered card must have Off left and On right")
	}
	for _, pair := range []struct {
		off, on string
		active  string
	}{{"off", "on", "off"}, {"pwm.off", "pwm.on", "pwm.on"}} {
		buttons := map[string]controllerButton{}
		for _, button := range s.layout.controllerButtons {
			id := s.controllerActionIDs[button.action]
			if id == pair.off || id == pair.on {
				buttons[id] = button
			}
		}
		off, offFound := buttons[pair.off]
		on, onFound := buttons[pair.on]
		if !offFound || !onFound || off.rect.X+off.rect.Width > on.rect.X || off.rect.Y != on.rect.Y {
			t.Fatalf("Off/On physical positions incorrect: %v", buttons)
		}
		if off.active != (pair.active == pair.off) || on.active != (pair.active == pair.on) {
			t.Fatalf("semantic active state changed while ordering buttons: %v", buttons)
		}
	}
	before := s.controllerChannelHits[0]
	for _, button := range s.layout.controllerButtons {
		if s.controllerActionIDs[button.action] == "edit:relay.5" {
			click(walk.Point{X: button.rect.X + 14, Y: button.rect.Y + 14})
			break
		}
	}
	if s.controllerInline == nil {
		t.Fatal("actual edit button did not open inline input")
	}
	if s.controllerChannelHits[0].rect != before.rect || s.controllerChannelHits[0].name != before.name {
		t.Fatal("inline edit moved/resized the card or caption")
	}
	var editTextRect win.RECT
	s.controllerInline.input.SendMessage(win.EM_GETRECT, 0, uintptr(unsafe.Pointer(&editTextRect)))
	inputOrigin := win.POINT{X: editTextRect.Left, Y: editTextRect.Top}
	win.ClientToScreen(s.controllerInline.input.Handle(), &inputOrigin)
	captionOrigin := win.POINT{X: int32(before.name.X), Y: int32(before.name.Y + (before.name.Height-controllerEditLineHeight(s.controllerInline.input))/2)}
	win.ClientToScreen(s.surface.Handle(), &captionOrigin)
	if delta := inputOrigin.X - captionOrigin.X; delta < -1 || delta > 1 {
		t.Fatalf("inline text shifted horizontally: input=%+v caption=%+v", inputOrigin, captionOrigin)
	}
	if delta := inputOrigin.Y - captionOrigin.Y; delta < -1 || delta > 1 {
		t.Fatalf("inline text shifted vertically: input=%+v caption=%+v", inputOrigin, captionOrigin)
	}
	s.controllerInline.input.SetText("Saved name")
	s.controllerInline.icon = "panel-top"
	pumpControllerFixture(s)
	paint()
	captureControllerFixture(t, s, backend+"-inline")
	var saveX, cancelX int
	for _, button := range s.layout.controllerButtons {
		switch s.controllerActionIDs[button.action] {
		case "inline-save:":
			saveX = button.rect.X
		case "inline-cancel:":
			cancelX = button.rect.X
		}
	}
	if saveX == 0 || saveX >= cancelX {
		t.Fatal("Save/Cancel order incorrect")
	}
	click(walk.Point{X: saveX + 14, Y: before.name.Y + 14})
	if s.controllerInline != nil || s.controllerState.Channels[0].Name != "Saved name" || s.controllerState.Channels[0].Icon != "panel-top" {
		t.Fatal("inline save was not applied")
	}
	// Real window messages exercise Walk's release-capture-before-MouseUp path.
	first, second := s.controllerChannelHits[0], s.controllerChannelHits[1]
	start := walk.Point{X: first.rect.X + 14, Y: first.header.Y + 14}
	end := walk.Point{X: second.rect.X + 50, Y: second.rect.Y + 60}
	send(win.WM_LBUTTONDOWN, start)
	send(win.WM_MOUSEMOVE, end)
	paint()
	if !s.controllerGesture.dragging || s.controllerDropTarget(end) != second.key {
		t.Fatal("drag never reached compatible preview")
	}
	send(win.WM_LBUTTONUP, end)
	pumpControllerFixture(s)
	paint()
	if s.controllerState.Channels[0].Key != "relay.6" {
		t.Fatal("drag release did not reorder")
	}
	// Cancellation changes neither metadata nor the window's visibility.
	s.beginControllerInline("relay.6")
	s.controllerInline.input.SetText("Discard me")
	win.SendMessage(s.controllerInline.input.Handle(), win.WM_KEYDOWN, uintptr(walk.KeyEscape), 0)
	pumpControllerFixture(s)
	if s.controllerInline != nil || s.controllerState.Channels[0].Name != "Relay six" || !s.mw.Visible() {
		t.Fatal("Escape did not cancel just the inline draft")
	}
	// Exercise the real picker controls and modal loop, with no global input.
	picker, err := s.newControllerIconPicker("lightbulb")
	if err != nil {
		t.Fatal(err)
	}
	picker.dialog.Starting().Attach(func() {
		picker.dialog.Synchronize(func() {
			picker.search.SetText("window")
			if len(picker.model.rows) == 0 {
				t.Error("picker search empty")
				picker.dialog.Cancel()
				return
			}
			picker.table.SetCurrentIndex(0)
			if !picker.accept.Enabled() {
				t.Error("picker selection did not enable acceptance")
			}
			win.SendMessage(picker.accept.Handle(), win.BM_CLICK, 0, 0)
		})
	})
	picker.dialog.Run()
	icon, accepted := picker.selection()
	picker.dispose()
	if !accepted || icon != "panel-top" {
		t.Fatalf("picker returned %q accepted=%v", icon, accepted)
	}
	// A rejected remote save must retain the draft for correction/cancel.
	s.controllerState.Preview = false
	s.controllerState.ProfileAttached, s.controllerState.ProfileConfigured = true, true
	s.controllerState.CatalogRevision = "fixture-revision"
	s.controller.UpdateChannel = func(_ context.Context, key, revision string, fields map[string]any) error {
		if key != "relay.6" || revision != "fixture-revision" || fields["name"] != "Keep draft" {
			return errors.New("wrong metadata request")
		}
		return errors.New("fixture revision conflict")
	}
	s.beginControllerInline("relay.6")
	s.controllerInline.input.SetText("Keep draft")
	s.saveControllerInline()
	for i := 0; i < 10 && s.controllerBusy; i++ {
		pumpControllerFixture(s)
	}
	if s.controllerBusy || s.controllerInline == nil || s.controllerInline.input.Text() != "Keep draft" || !s.controllerInline.input.Enabled() || s.controllerState.Error != "fixture revision conflict" {
		t.Fatal("failed save lost draft or remained busy")
	}
	s.closeControllerInline()
	// Editing and arrangement must not dispatch hardware operations.
	if s.controllerBusy {
		t.Fatal("preview triggered asynchronous hardware operation")
	}
	t.Log("native edit/save and captured drag/drop passed with stable caption geometry")
}
