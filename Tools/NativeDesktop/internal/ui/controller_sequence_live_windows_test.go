//go:build windows

package ui

import (
	"context"
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

func TestControllerSequenceEnglishNativeLayout(t *testing.T) {
	controllerSequenceNativeFixture(t, "en")
}
func TestControllerSequencePersianNativeLayout(t *testing.T) {
	controllerSequenceNativeFixture(t, "fa")
}

func controllerSequenceNativeFixture(t *testing.T, locale string) {
	s, _ := newControllerWindowFixture(t)
	s.controller.Title = "PCController"
	s.applyLocaleUI(locale)
	backdrop, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer backdrop.Dispose()
	style := uint32(win.GetWindowLong(backdrop.Handle(), win.GWL_STYLE)) &^ uint32(win.WS_CAPTION|win.WS_THICKFRAME|win.WS_SYSMENU)
	win.SetWindowLong(backdrop.Handle(), win.GWL_STYLE, int32(style|win.WS_POPUP))
	backdrop.SetLayout(walk.NewVBoxLayout())
	backdrop.ToolBar().SetVisible(false)
	backdrop.StatusBar().SetVisible(false)
	_, err = walk.NewCustomWidgetPixels(backdrop, 0, func(c *walk.Canvas, b walk.Rectangle) error {
		brush, err := walk.NewSolidColorBrush(walk.RGB(34, 30, 42))
		if err != nil {
			return err
		}
		defer brush.Dispose()
		return c.FillRectanglePixels(brush, b)
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := winfx.MonitorWorkAreaForRect(winfx.Rect{Left: 80, Top: 80, Right: 1000, Bottom: 1000})
	if err != nil {
		t.Fatal(err)
	}
	backdrop.SetBoundsPixels(walk.Rectangle{X: int(work.Left), Y: int(work.Top), Width: int(work.Right - work.Left), Height: int(work.Bottom - work.Top)})
	backdrop.Show()
	win.SetWindowPos(backdrop.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	s.mw.Show()
	s.controller.LoadPage = func(context.Context, string) (presentation.Page, error) {
		return presentation.Page{Title: "Sequence editor · Scene 1", Columns: []string{"Step", "Start", "Type", "Target", "Value / text", "Duration"}, Rows: [][]string{{"0", "0.000000 s", "relay", "4", "1", "0 ms"}, {"1", "0.200000 s", "pwm", "0", "500", "1200 ms"}, {"2", "0.400000 s", "tone", "0", "880 Hz", "115 ms"}, {"3", "1.000000 s", "display", "0", "Scene 1", "800 ms"}}, SelectedRow: "1", RowValues: map[string]map[string]string{"1": {"step.kind": "pwm", "step.at_us": "200000", "step.value": "500", "step.duration_ms": "1200", "step.to_value": "3000"}}, Timeline: []presentation.TimelineCue{{Key: "0", Label: "relay", Lane: "Relay", AtUS: 0}, {Key: "1", Label: "pwm", Lane: "PWM", AtUS: 200000, DurationMS: 1200}, {Key: "2", Label: "tone", Lane: "Tone", AtUS: 400000, DurationMS: 115}, {Key: "3", Label: "display", Lane: "Display", AtUS: 1000000, DurationMS: 800}}, Fields: []presentation.Field{
			{Key: "step.kind", Label: "Step type", Value: "relay", Choices: []string{"relay", "pwm", "tone", "display"}, ChoiceLabels: []string{"Relay", "PWM", "Tone", "Display"}, Section: "Step"},
			{Key: "step.at_us", Label: "Start (µs)", Value: "0", Numeric: true, Min: 0, Max: 2147483647, Section: "Step"},
			{Key: "step.value", Label: "Value / raw duty", Value: "0", Numeric: true, Min: 0, Max: 4095, WhenKey: "step.kind", WhenValues: []string{"pwm", "relay"}, Section: "Step"},
			{Key: "step.duration_ms", Label: "Duration (ms)", Value: "0", Numeric: true, Min: 0, Max: 65535, WhenKey: "step.kind", WhenValues: []string{"pwm", "tone", "display"}, Section: "Step"},
			{Key: "step.to_value", Label: "to value (optional)", Value: "", Numeric: true, OptionalNumeric: true, Min: 0, Max: 4095, WhenKey: "step.kind", WhenValues: []string{"pwm"}, Section: "Step"},
		}, Actions: []presentation.PageAction{{ID: "append", Label: "Add step", Enabled: true}, {ID: "save", Label: "Save step", Enabled: true, SelectionRequired: true}, {ID: "remove", Label: "Remove step", Enabled: true, SelectionRequired: true}}}, nil
	}
	watchdog := time.AfterFunc(15*time.Second, func() { s.mw.Synchronize(func() { t.Error("sequence fixture timed out"); win.PostQuitMessage(0) }) })
	defer watchdog.Stop()
	s.openControllerFormObserved("sequence", func(dialog *walk.Dialog) {
		if dialog.RightToLeftLayout() != (locale == "fa") {
			t.Error("native direction did not follow locale")
		}
		for _, label := range []string{"Close", "Refresh", "Add step", "Save step"} {
			if button := controllerFixtureButton(dialog, s.nativeText(label)); button == nil {
				t.Errorf("missing localized action %s", label)
			}
		}
		var edit *walk.LineEdit
		var find func(walk.Container)
		find = func(root walk.Container) {
			for i := 0; i < root.Children().Len(); i++ {
				child := root.Children().At(i)
				if input, ok := child.(*walk.LineEdit); ok && input.Text() == "200000" {
					edit = input
				}
				if container, ok := child.(walk.Container); ok {
					find(container)
				}
				if tabs, ok := child.(*walk.TabWidget); ok {
					for i := 0; i < tabs.Pages().Len(); i++ {
						find(tabs.Pages().At(i))
					}
				}
			}
		}
		find(dialog)
		if edit == nil {
			t.Error("selected step did not hydrate timing")
			dialog.Cancel()
			return
		}
		time.AfterFunc(300*time.Millisecond, func() {
			dialog.Synchronize(func() {
				if err := edit.SetText("250000"); err != nil {
					t.Error(err)
				}
				if save := controllerFixtureButton(dialog, s.nativeText("Save step")); save == nil || !save.Enabled() {
					t.Error("timing draft did not enable Save")
				}
				finish := func() { _ = edit.SetText("200000"); dialog.Cancel() }
				if directory := os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS"); directory != "" {
					path := filepath.Join(directory, "gui-sequence-"+locale+".png")
					title := dialog.Title()
					mode := "print"
					if os.Getenv("PCCONTROLLER_DESKTOP_VERIFY_SCREEN") == "1" {
						mode = "screen"
						path = filepath.Join(directory, "gui-sequence-"+locale+"-screen.png")
					}
					win.SetWindowPos(dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
					win.RedrawWindow(dialog.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW|win.RDW_ALLCHILDREN)
					go func() {
						if output, err := exec.Command(os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), strconv.Itoa(os.Getpid()), path, "="+title, mode, "0").CombinedOutput(); err != nil {
							t.Errorf("sequence capture: %v %s", err, output)
						}
						dialog.Synchronize(finish)
					}()
				} else {
					finish()
				}
			})
		})
	})
}
