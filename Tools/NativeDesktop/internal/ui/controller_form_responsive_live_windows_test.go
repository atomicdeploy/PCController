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
	"pccontroller.local/desktop/presentation"
)

func controllerFormNamed(root walk.Container, name string) walk.Widget {
	for i := 0; i < root.Children().Len(); i++ {
		child := root.Children().At(i)
		if child.Name() == name {
			return child
		}
		if tabs, ok := child.(*walk.TabWidget); ok {
			for j := 0; j < tabs.Pages().Len(); j++ {
				if found := controllerFormNamed(tabs.Pages().At(j), name); found != nil {
					return found
				}
			}
		}
		if nested, ok := child.(walk.Container); ok {
			if found := controllerFormNamed(nested, name); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestControllerConnectionFormResponsiveDark(t *testing.T) {
	controllerResponsiveFormFixture(t, false, "dark")
}
func TestControllerConnectionFormResponsiveLight(t *testing.T) {
	controllerResponsiveFormFixture(t, false, "light")
}
func TestControllerEditorFormResponsive(t *testing.T) {
	controllerResponsiveFormFixture(t, true, "dark")
}

func controllerResponsiveFormFixture(t *testing.T, rich bool, theme string) {
	s, _ := newControllerWindowFixture(t)
	s.cfg.UI.Theme = theme
	s.controller.Title = "PCController"
	page := presentation.Page{Title: "Board connection", Fields: []presentation.Field{
		{Key: "state", Label: "Status", Value: "Disconnected", ReadOnly: true},
		{Key: "reason", Label: "Detail", Value: "closed by host", ReadOnly: true},
		{Key: "port", Label: "Port", Value: "Automatic", Choices: []string{"Automatic", "COM9 · PCController"}},
	}, Actions: []presentation.PageAction{{ID: "connect", Label: "Connect", Enabled: true}}}
	key := "connection"
	if rich {
		key = "form-fixture"
		page.Title = "Peripheral presentation"
		page.Fields = []presentation.Field{
			{Key: "name", Label: "Channel name", Value: "Window", Section: "General"},
			{Key: "mode", Label: "Mode", Value: "PWM", Choices: []string{"PWM", "Switch"}, Section: "General"},
			{Key: "level", Label: "Brightness (%)", Value: "100", Numeric: true, Min: 0, Max: 100, Section: "General"},
			{Key: "enabled", Label: "Visible", Value: "true", Choices: []string{"false", "true"}, Section: "General"},
			{Key: "definition", Label: "Definition", Value: "{\r\n  \"channel\": \"pwm.0\"\r\n}", Multiline: true, Section: "Advanced"},
		}
		page.Actions = []presentation.PageAction{{ID: "save", Label: "Save channel", Enabled: true, Section: "General"}, {ID: "reset", Label: "Reset presentation", Enabled: true, Section: "General"}, {ID: "export", Label: "Export", Enabled: true}, {ID: "validate", Label: "Validate", Enabled: true, Section: "Advanced"}}
	}
	s.controller.LoadPage = func(context.Context, string) (presentation.Page, error) { return page, nil }
	s.controller.SubmitPage = func(context.Context, string, string, map[string]string) (presentation.Page, error) {
		t.Error("layout fixture attempted mutation")
		return page, nil
	}
	watchdog := time.AfterFunc(25*time.Second, func() {
		s.mw.Synchronize(func() { t.Error("responsive dialog fixture timed out"); win.PostQuitMessage(0) })
	})
	defer watchdog.Stop()
	s.openControllerFormObserved(key, func(dialog *walk.Dialog) {
		if !rich && findControllerFormTabs(dialog) != nil {
			t.Error("single-section form must not repeat its title in a tab")
		}
		var draft *walk.LineEdit
		if rich {
			draft, _ = controllerFormNamed(dialog, "form.value.name").(*walk.LineEdit)
			if draft == nil {
				t.Error("name editor missing")
				dialog.Cancel()
				return
			}
			_ = draft.SetText("Unsaved channel name")
		}
		sizes := []walk.Size{{Width: 720, Height: 380}, {Width: 1000, Height: 620}, {Width: 460, Height: 500}}
		if rich {
			sizes = []walk.Size{{Width: 720, Height: 620}, {Width: 1020, Height: 720}, {Width: 560, Height: 640}}
		}
		index := 0
		firstWidth := 0
		var next func()
		next = func() {
			if index == len(sizes) {
				if draft != nil {
					_ = draft.SetText("Window")
				}
				dialog.Cancel()
				return
			}
			size := sizes[index]
			_ = dialog.SetSize(size)
			time.AfterFunc(250*time.Millisecond, func() {
				dialog.Synchronize(func() {
					if actual := dialog.Size().Width; actual > size.Width+8 {
						t.Errorf("dialog minimum width prevents requested resize: requested %d actual %d", size.Width, actual)
					}
					fieldKey := "port"
					if rich {
						fieldKey = "name"
					}
					field := controllerFormNamed(dialog, "form.value."+fieldKey)
					label := controllerFormNamed(dialog, "form.label."+fieldKey)
					var fieldRect, labelRect, client win.RECT
					win.GetClientRect(dialog.Handle(), &client)
					if field == nil || label == nil {
						t.Error("responsive row missing")
						dialog.Cancel()
						return
					}
					win.GetWindowRect(field.Handle(), &fieldRect)
					win.GetWindowRect(label.Handle(), &labelRect)
					if index == 0 {
						firstWidth = int(fieldRect.Right - fieldRect.Left)
					}
					if index == 1 && int(fieldRect.Right-fieldRect.Left) <= firstWidth+dialog.IntFrom96DPI(150) {
						t.Error("editor did not grow with the dialog")
					}
					if index == 1 {
						delta := (labelRect.Top + labelRect.Bottom) - (fieldRect.Top + fieldRect.Bottom)
						if delta < -2 || delta > 2 {
							t.Errorf("field/label vertical centers drift: %v %v", labelRect, fieldRect)
						}
						if !rich {
							value := controllerFormNamed(dialog, "form.value.state")
							var valueRect win.RECT
							win.GetWindowRect(value.Handle(), &valueRect)
							if valueRect.Left != fieldRect.Left {
								t.Errorf("read-only and editable values have different leading edges: %d vs %d", valueRect.Left, fieldRect.Left)
							}
						}
					}
					if index == 2 && labelRect.Bottom > fieldRect.Top {
						t.Error("narrow form did not stack its labels above the editors")
					}
					if !rich {
						viewport := controllerFormNamed(dialog, "form.viewport")
						var viewportRect win.RECT
						win.GetWindowRect(viewport.Handle(), &viewportRect)
						if fieldRect.Bottom > viewportRect.Bottom {
							t.Error("simple form hides its Port selector behind an unnecessary scrollbar")
						}
						connect, close := controllerFixtureButton(dialog, "Connect"), controllerFixtureButton(dialog, "Close")
						if connect == nil || close == nil {
							if status, ok := controllerFormNamed(dialog, "form.status").(*walk.Label); ok {
								t.Errorf("footer action missing: %s", status.Text())
							} else {
								t.Error("footer action missing")
							}
							dialog.Cancel()
							return
						}
						var a, b win.RECT
						win.GetWindowRect(connect.Handle(), &a)
						win.GetWindowRect(close.Handle(), &b)
						if delta := a.Top - b.Top; delta < -1 || delta > 1 {
							t.Error("single primary action must share the Close/Refresh footer row")
						}
					}
					for _, text := range []string{"Refresh", "Close"} {
						button := controllerFixtureButton(dialog, text)
						if button == nil {
							t.Errorf("%s missing", text)
							continue
						}
						var r win.RECT
						win.GetWindowRect(button.Handle(), &r)
						var bounds win.RECT
						win.GetWindowRect(dialog.Handle(), &bounds)
						if r.Left < bounds.Left || r.Right > bounds.Right || r.Bottom > bounds.Bottom {
							t.Errorf("%s is clipped", text)
						}
					}
					if draft != nil && draft.Text() != "Unsaved channel name" {
						t.Error("resize lost the inline draft")
					}
					if rich {
						for _, text := range []string{"Save channel", "Reset presentation", "Export"} {
							button := controllerFixtureButton(dialog, text)
							if button == nil || !button.Visible() {
								t.Errorf("visible section action %s disappeared", text)
								continue
							}
							var buttonRect, bounds win.RECT
							win.GetWindowRect(button.Handle(), &buttonRect)
							win.GetWindowRect(dialog.Handle(), &bounds)
							var parent win.RECT
							win.GetWindowRect(button.Parent().Handle(), &parent)
							if buttonRect.Top < parent.Top || buttonRect.Bottom > parent.Bottom {
								t.Errorf("%s clipped inside action container: button=%+v parent=%+v", text, buttonRect, parent)
							}
							for ancestor := win.GetParent(button.Parent().Handle()); ancestor != 0 && ancestor != dialog.Handle(); ancestor = win.GetParent(ancestor) {
								win.GetWindowRect(ancestor, &parent)
								if buttonRect.Top < parent.Top || buttonRect.Bottom > parent.Bottom || buttonRect.Left < parent.Left || buttonRect.Right > parent.Right {
									t.Errorf("%s clipped by %#x: button=%+v parent=%+v", text, ancestor, buttonRect, parent)
								}
							}
							if buttonRect.Left < bounds.Left || buttonRect.Right > bounds.Right || buttonRect.Bottom > bounds.Bottom {
								t.Errorf("section action %s clipped", text)
							}
						}
					}
					name := fmt.Sprintf("gui-form-%s-%s-%d.png", key, theme, size.Width)
					index++
					if directory := os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS"); directory != "" {
						path := filepath.Join(directory, name)
						title := dialog.Title()
						go func() {
							if output, err := exec.Command(os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), strconv.Itoa(os.Getpid()), path, "="+title, "print", "0").CombinedOutput(); err != nil {
								t.Errorf("capture: %v: %s", err, output)
							}
							dialog.Synchronize(next)
						}()
					} else {
						next()
					}
				})
			})
		}
		next()
	})
}
