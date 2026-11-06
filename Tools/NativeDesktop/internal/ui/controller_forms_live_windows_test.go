//go:build windows

package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
)

// Test-owned HWNDs only. No device/core/transport is started; closing the test
// dialog is not input injection into a user's running application.
func TestControllerActivityFormNativeLayout(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.controller.Title = "PCController"
	backdrop, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer backdrop.Dispose()
	style := uint32(win.GetWindowLong(backdrop.Handle(), win.GWL_STYLE)) &^ uint32(win.WS_CAPTION|win.WS_THICKFRAME|win.WS_SYSMENU)
	win.SetWindowLong(backdrop.Handle(), win.GWL_STYLE, int32(style|win.WS_POPUP))
	_ = winfx.SetDWMWindowCornerPreference(uintptr(backdrop.Handle()), false)
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{})
	backdrop.SetLayout(layout)
	backdrop.ToolBar().SetVisible(false)
	backdrop.StatusBar().SetVisible(false)
	_, err = walk.NewCustomWidgetPixels(backdrop, 0, func(c *walk.Canvas, b walk.Rectangle) error {
		return c.GradientFillRectanglePixels(walk.RGB(49, 38, 76), walk.RGB(41, 75, 77), walk.Horizontal, b)
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := winfx.MonitorWorkAreaForRect(winfx.Rect{Left: 80, Top: 80, Right: 900, Bottom: 780})
	if err != nil {
		t.Fatal(err)
	}
	backdrop.SetBoundsPixels(walk.Rectangle{X: int(work.Left), Y: int(work.Top), Width: int(work.Right - work.Left), Height: int(work.Bottom - work.Top)})
	backdrop.Show()
	win.SetWindowPos(backdrop.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	s.mw.Show()
	s.controller.LoadPage = func(context.Context, string) (presentation.Page, error) {
		page := presentation.Page{Title: "RF reception & assignments", Columns: []string{"ID", "Waveform", "Protocol", "Assignment", "Live", "Received"}, Rows: [][]string{{"3", "12124016 / 24", "1", "Window · toggle", "●", "4"}, {"observed:17:24:1", "17 / 24", "1", "Unlearned", "", "2"}}, RowValues: map[string]map[string]string{"3": {"name": "Window"}}, Fields: []presentation.Field{
			{Key: "name", Label: "Assignment name", Value: "", Section: "Assignment"}, {Key: "gesture", Label: "Gesture", Value: "click", Choices: []string{"click", "hold", "hold-end"}, Section: "Assignment"},
			{Key: "timed", Label: "Timed learning", Value: "false", Choices: []string{"false", "true"}, Section: "Learning"}, {Key: "timeout", Label: "Timeout (seconds)", Value: "60", Numeric: true, Min: 1, Max: 3600, Section: "Learning"},
			{Key: "continuous", Label: "Continuous learning", Value: "false", Choices: []string{"false", "true"}, Section: "Learning"},
			{Key: "definition", Label: "Advanced binding (JSON)", Multiline: true, Value: "{\r\n  \"name\": \"Window\"\r\n}", Section: "Advanced"},
		}, Actions: []presentation.PageAction{{ID: "assign", Label: "Save assignment", Enabled: true, Section: "Assignment"}, {ID: "learn", Label: "Start learning", Enabled: true, Section: "Learning"}, {ID: "save", Label: "Save preferences", Enabled: true, Section: "Learning"}}}
		page.Owner = "offline-documentation-fixture"
		page.Receipts = map[string]presentation.RFReceipt{"3": {Identity: "12124016:24:1", Learned: true, Count: 4}, "observed:17:24:1": {Identity: "17:24:1", Count: 2}}
		return page, nil
	}
	renders := 0
	watchdog := time.AfterFunc(15*time.Second, func() { s.mw.Synchronize(func() { t.Error("activity form timed out"); win.PostQuitMessage(0) }) })
	defer watchdog.Stop()
	s.openControllerFormObserved("rf", func(dialog *walk.Dialog) {
		renders++
		tabs := findControllerFormTabs(dialog)
		if tabs == nil || tabs.Pages().Len() != 3 {
			t.Errorf("expected 3 distinct activity tabs, got %v", tabs)
		}
		var pid uint32
		win.GetWindowThreadProcessId(dialog.Handle(), &pid)
		if pid != uint32(os.Getpid()) || win.GetWindow(dialog.Handle(), win.GW_OWNER) != s.mw.Handle() {
			t.Error("activity dialog ownership changed")
		}
		var bounds win.RECT
		win.GetWindowRect(dialog.Handle(), &bounds)
		if bounds.Left < work.Left || bounds.Top < work.Top || bounds.Right > work.Right || bounds.Bottom > work.Bottom {
			t.Errorf("activity off-screen: %+v", bounds)
		}
		for _, name := range []string{"Close", "Refresh"} {
			button := controllerFixtureButton(dialog, name)
			if button == nil || !button.Visible() {
				t.Errorf("fixed %s action missing", name)
				continue
			}
			var r win.RECT
			win.GetWindowRect(button.Handle(), &r)
			if r.Left < bounds.Left || r.Right > bounds.Right || r.Top < bounds.Top || r.Bottom > bounds.Bottom {
				t.Errorf("fixed %s action clipped: %+v", name, r)
			}
		}
		// Let the modal loop paint before read-only screen capture, then close
		// the exact owned object on its UI thread.
		time.AfterFunc(250*time.Millisecond, func() {
			dialog.Synchronize(func() {
				if dir := os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS"); dir != "" {
					win.SetWindowPos(dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
					win.RedrawWindow(dialog.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
					_ = winfx.FlushComposition()
					path := filepath.Join(dir, "gui-rf-activity.png")
					title := dialog.Title()
					go func() {
						// PrintWindow needs the owned HWND's UI thread to keep
						// servicing WM_PRINT while the capture tool waits.
						if output, err := exec.Command(os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), strconv.Itoa(os.Getpid()), path, "="+title, "print", "0").CombinedOutput(); err != nil {
							t.Errorf("activity capture: %v %s", err, output)
						} else {
							t.Log(fmt.Sprintf("native activity captured: %s", path))
						}
						dialog.Synchronize(func() { dialog.Cancel() })
					}()
					return
				}
				dialog.Cancel()
			})
		})
	})
	if renders != 1 {
		t.Fatalf("native activity did not render: %d", renders)
	}
}

func controllerFixtureButton(root walk.Container, text string) *walk.PushButton {
	for i := 0; i < root.Children().Len(); i++ {
		child := root.Children().At(i)
		if button, ok := child.(*walk.PushButton); ok && button.Text() == text {
			return button
		}
		if container, ok := child.(walk.Container); ok {
			if button := controllerFixtureButton(container, text); button != nil {
				return button
			}
		}
	}
	return nil
}
