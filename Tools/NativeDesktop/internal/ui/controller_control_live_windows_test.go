//go:build windows

package ui

import (
	"context"
	"encoding/json"
	"github.com/lxn/walk"
	"os"
	"os/exec"
	"path/filepath"
	"pccontroller.local/desktop/presentation"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerAutomationTriggerLayout(t *testing.T) {
	controllerControlFixture(t, "automation", 0, "en")
}

func TestControllerBuzzerLifecycleRetainsLiteralDraft(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.controller.Title = "PCController"
	changes := make(chan struct{}, 1)
	var version atomic.Uint64
	var active atomic.Bool
	s.controller.PageChanges = changes
	s.controller.PageVersion = func(string) uint64 { return version.Load() }
	s.controller.LoadPage = func(context.Context, string) (presentation.Page, error) {
		p := presentation.Page{Title: "Buzzer & melodies", Fields: []presentation.Field{{Key: "frequency", Label: "Tone frequency (Hz)", Value: "880", Numeric: true, Min: 20, Max: 20000, Section: "Tone test"}}, Actions: []presentation.PageAction{{ID: "tone", Label: "Play tone", Enabled: true, Section: "Tone test"}}}
		if active.Load() {
			p.Actions = append(p.Actions, presentation.PageAction{ID: "stop", Label: "Stop buzzer", Enabled: true})
		}
		return p, nil
	}
	s.controller.SubmitPage = func(context.Context, string, string, map[string]string) (presentation.Page, error) {
		t.Fatal("lifecycle observation must not execute a command")
		return presentation.Page{}, nil
	}
	phase := 0
	watchdog := time.AfterFunc(10*time.Second, func() { s.mw.Synchronize(func() { t.Error("buzzer lifecycle update timed out"); s.mw.Close() }) })
	defer watchdog.Stop()
	s.openControllerFormObserved("buzzer", func(dialog *walk.Dialog) {
		var edit *walk.LineEdit
		var find func(walk.Container)
		find = func(c walk.Container) {
			for i := 0; i < c.Children().Len(); i++ {
				child := c.Children().At(i)
				if e, ok := child.(*walk.LineEdit); ok {
					edit = e
				}
				if tabs, ok := child.(*walk.TabWidget); ok {
					for i := 0; i < tabs.Pages().Len(); i++ {
						find(tabs.Pages().At(i))
					}
				}
				if nested, ok := child.(walk.Container); ok {
					find(nested)
				}
			}
		}
		find(dialog)
		if edit == nil {
			t.Error("tone field missing")
			dialog.Cancel()
			return
		}
		switch phase {
		case 0:
			phase++
			_ = edit.SetText("invalid draft")
			active.Store(true)
			version.Add(1)
			changes <- struct{}{}
		case 1:
			phase++
			if edit.Text() != "invalid draft" || controllerFixtureButton(dialog, "Stop buzzer") == nil {
				t.Error("start update reset draft or omitted Stop")
			}
			active.Store(false)
			version.Add(1)
			changes <- struct{}{}
		case 2:
			if edit.Text() != "invalid draft" || controllerFixtureButton(dialog, "Stop buzzer") != nil {
				t.Error("completion reset draft or retained Stop")
			}
			_ = edit.SetText("880")
			dialog.Cancel()
		}
	})
}
func TestControllerAutomationActionsLayout(t *testing.T) {
	controllerControlFixture(t, "automation", 1, "en")
}
func TestControllerMelodyEditorLayout(t *testing.T) { controllerControlFixture(t, "melody", 0, "en") }
func TestControllerPeripheralEditorLayout(t *testing.T) {
	controllerControlFixture(t, "peripheral", 0, "en")
}
func TestControllerLCDPresentationLayout(t *testing.T) { controllerControlFixture(t, "lcd", 0, "en") }
func TestControllerAutomationPersianLayout(t *testing.T) {
	controllerControlFixture(t, "automation", 1, "fa")
}

func controllerControlFixture(t *testing.T, key string, tab int, locale string) {
	directory := os.Getenv("PCCONTROLLER_NATIVE_PAGE_FIXTURES")
	if directory == "" {
		t.Skip("host adapter fixture documents not requested")
	}
	s, _ := newControllerWindowFixture(t)
	s.controller.Title = "PCController"
	s.applyLocaleUI(locale)
	data, err := os.ReadFile(filepath.Join(directory, key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var page presentation.Page
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	s.controller.LoadPage = func(context.Context, string) (presentation.Page, error) { return page, nil }
	s.controller.SubmitPage = func(context.Context, string, string, map[string]string) (presentation.Page, error) {
		t.Error("layout fixture attempted a mutation")
		return page, nil
	}
	watchdog := time.AfterFunc(15*time.Second, func() { s.mw.Synchronize(func() { t.Error("control fixture timed out"); s.mw.Close() }) })
	defer watchdog.Stop()
	s.openControllerFormObserved(key, func(dialog *walk.Dialog) {
		if tabs := findControllerFormTabs(dialog); tabs != nil && tab < tabs.Pages().Len() {
			if err := tabs.SetCurrentIndex(tab); err != nil {
				t.Error(err)
			}
		}
		if dialog.RightToLeftLayout() != (locale == "fa") {
			t.Error("wrong native layout direction")
		}
		if controllerFixtureButton(dialog, s.nativeText("Close")) == nil || controllerFixtureButton(dialog, s.nativeText("Refresh")) == nil {
			t.Error("fixed actions missing")
		}
		if key == "peripheral" && controllerFixtureButton(dialog, s.nativeText("Choose icon")) == nil {
			t.Error("native icon picker missing")
		}
		time.AfterFunc(250*time.Millisecond, func() {
			dialog.Synchronize(func() {
				if key == "automation" {
					if button := controllerFixtureButton(dialog, s.nativeText("Save action")); button == nil || button.Enabled() {
						t.Error("unchanged action Save must be inert")
					}
				}
				output := os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS")
				if output == "" {
					dialog.Cancel()
					return
				}
				path := filepath.Join(output, "gui-"+key+"-"+strconv.Itoa(tab)+"-"+locale+".png")
				title := dialog.Title()
				go func() {
					if out, err := exec.Command(os.Getenv("PCCONTROLLER_DESKTOP_CAPTURE_TOOL"), strconv.Itoa(os.Getpid()), path, "="+title, "print", "0").CombinedOutput(); err != nil {
						t.Errorf("capture: %v %s", err, out)
					}
					dialog.Synchronize(func() { dialog.Cancel() })
				}()
			})
		})
	})
}
