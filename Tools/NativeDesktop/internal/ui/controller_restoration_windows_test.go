//go:build windows

package ui

import (
	"reflect"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
)

func TestControllerStartupPreservesSavedFeatures(t *testing.T) {
	cfg := config.Default()
	cfg.UI.ViewMode = config.ViewCompact
	cfg.Hotkeys.Show = "CTRL+ALT+F10"
	cfg.Notifications.Enabled, cfg.Notifications.Sound = true, true
	cfg.Automation.Enabled = true
	cfg.Integration.StartWithWindows, cfg.Integration.LocalAPIEnabled = true, true
	want := cfg
	want.Device.BaseURL = "http://127.0.0.1:1"
	if got := controllerStartupConfig(cfg, false); !reflect.DeepEqual(got, want) {
		t.Fatal("startup discarded explicit desktop preferences")
	}
	first := controllerStartupConfig(config.Default(), true)
	if first.Hotkeys != (config.HotkeysConfig{}) || first.UI.ViewMode != config.ViewDashboard {
		t.Fatal("first launch enabled device shortcuts or wrong view")
	}
}

func TestControllerCompactRestoresDashboardDimensions(t *testing.T) {
	full := config.Default()
	full.UI.ViewMode = config.ViewDashboard
	full.UI.WindowBounds = config.WindowBounds{X: 90, Y: 130, Width: 1020, Height: 880}
	compact := full
	compact.UI.ViewMode = config.ViewCompact
	compact = controllerViewConfig(full, compact)
	if compact.UI.WindowBounds.Width != 400 || compact.UI.WindowBounds.Height != 340 || compact.UI.ControllerExpandedBounds != full.UI.WindowBounds {
		t.Fatalf("compact geometry: %+v", compact.UI)
	}
	// Settings Cancel can supply a stale baseline without the new saved field.
	expanded := controllerViewConfig(compact, full)
	if expanded.UI.WindowBounds != full.UI.WindowBounds {
		t.Fatal("expand lost dashboard dimensions")
	}
	if same := controllerViewConfig(compact, compact); same.UI.WindowBounds != compact.UI.WindowBounds {
		t.Fatal("same-mode preview resized compact view")
	}
}

func TestControllerTaskbarUsesBoardKeysAndAuthorityGates(t *testing.T) {
	s := &Shell{controller: &presentation.Options{Title: "PCController"}, controllerState: presentation.Snapshot{Connected: true, CanNavigate: true}}
	for i, button := range s.currentTaskbarButtons(taskbarButtonIcons{}) {
		action, ok := controllerTaskbarAction(uintptr(taskbarThumbnailClicked)<<16 | uintptr(button.ID))
		if !ok || action != actionKey1+actionID(i) || button.Disabled {
			t.Fatalf("button %d: %+v", i, button)
		}
	}
	if _, ok := controllerTaskbarAction(uintptr(controllerTaskbarFirst)); ok {
		t.Fatal("ordinary command interpreted as thumbnail click")
	}
	for _, state := range []presentation.Snapshot{{}, {Connected: true}, {Connected: true, CanNavigate: true, Busy: true}} {
		s.controllerState = state
		for _, button := range s.currentTaskbarButtons(taskbarButtonIcons{}) {
			if !button.Disabled {
				t.Fatal("taskbar bypassed board navigation gate")
			}
		}
	}
	s.controllerState.Error = "Update failed"
	if s.currentTaskbarPresentation().progress != winfx.TaskbarProgressError {
		t.Fatal("taskbar lost failure status")
	}
}

func TestControllerAutomationModesAreObservedNotInferred(t *testing.T) {
	for _, state := range []presentation.Snapshot{{Mode: "running"}, {Connected: true, Mode: "idle"}, {Preview: true, Connected: true, HaveStatus: true, Mode: "running"}} {
		if controllerAutomationSnapshot(state).State != model.StateUnknown {
			t.Fatal("fabricated observed program mode")
		}
	}
	for mode, want := range map[string]model.OutputState{"running": model.StateOn, "IDLE": model.StateOff, "unknown": model.StateUnknown} {
		if controllerAutomationSnapshot(presentation.Snapshot{Connected: true, HaveStatus: true, Mode: mode}).State != want {
			t.Fatal(mode)
		}
	}
}

func TestControllerRestoredSettingsWindowLifecycle(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.mw.Show()
	s.OpenSettings()
	pumpControllerFixture(s)
	if s.settings == nil || s.settings.IsDisposed() {
		t.Fatal("desktop settings were not opened")
	}
	first := s.settings
	s.OpenSettings()
	pumpControllerFixture(s)
	if s.settings != first {
		t.Fatal("repeated open duplicated modeless settings")
	}
	first.Dispose()
	pumpControllerFixture(s)
	if !s.mw.Visible() {
		t.Fatal("settings close hid owner")
	}
}

func TestControllerTaskbarNativeIconsDoNotRequireDeviceResources(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	if err := s.rebuildTaskbarButtonIcons(); err != nil {
		t.Fatal(err)
	}
	before := s.taskbarButtonIcons
	for i, icon := range before.controller {
		if icon == 0 {
			t.Fatalf("missing front-panel icon %d", i)
		}
	}
	if before.toggle != 0 || before.on != 0 || before.off != 0 {
		t.Fatal("controller retained device thumbnail actions")
	}
	if err := s.rebuildTaskbarButtonIcons(); err != nil {
		t.Fatal(err)
	}
	if s.taskbarButtonIcons != before {
		t.Fatal("unchanged DPI rebuilt taskbar resources")
	}
}
