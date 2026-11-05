//go:build windows

package ui

import (
	"context"
	"github.com/lxn/walk"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/traymenu"
	"pccontroller.local/desktop/presentation"
)

func TestControllerTrayClicksNeverDispatchDeviceActions(t *testing.T) {
	for _, button := range []walk.MouseButton{walk.LeftButton, walk.RightButton} {
		if trayToggleOnPress(true, button) || !trayMenuOnRelease(true, button) {
			t.Fatal("controller tray lost original open-menu click behavior")
		}
	}
	if !trayToggleOnPress(false, walk.LeftButton) || trayMenuOnRelease(false, walk.LeftButton) || !trayMenuOnRelease(false, walk.RightButton) {
		t.Fatal("standalone donor click behavior changed")
	}
}

func TestControllerTraySharesCompleteHostModelAcrossRenderers(t *testing.T) {
	for _, connected := range []bool{false, true} {
		host := []presentation.MenuItem{{Label: "Controller offline", Disabled: true}}
		if connected {
			host = append(host, presentation.MenuItem{Separator: true})
			for i, page := range []string{"dashboard", "controls", "workbench", "updates", "settings"} {
				host = append(host, presentation.MenuItem{ID: page, Label: page, Default: i == 0})
			}
		}
		host = append(host, presentation.MenuItem{Separator: true}, presentation.MenuItem{ID: "reconnect", Label: "Connect now"}, presentation.MenuItem{Separator: true}, presentation.MenuItem{ID: "exit", Label: "Exit"})
		s := &Shell{cfg: config.Default(), controller: &presentation.Options{Title: "PCController", InvokeHostMenu: func(context.Context, string) error { return nil }}, controllerState: presentation.Snapshot{HostMenu: host}}
		items := s.controllerMenuItems()
		if err := traymenu.Validate(traymenu.Options{Items: items}); err != nil {
			t.Fatal(err)
		}
		for i, original := range host {
			item := items[i]
			if item.Separator != original.Separator || item.Label != original.Label || item.Default != original.Default {
				t.Fatalf("host row %d changed: %+v", i, item)
			}
			if original.Separator {
				if item.ID != "" || item.Disabled || len(item.IconPNG) != 0 {
					t.Fatal("separator acquired command fields")
				}
				continue
			}
			if item.Disabled != original.Disabled {
				t.Fatal("host enabled state changed")
			}
			if original.ID != "" && item.ID != traymenu.ActionID("host:"+original.ID) {
				t.Fatal("host dispatch identity lost")
			}
		}
		for _, native := range []bool{false, true} {
			s.cfg.UI.NativeMenus = native
			options := s.controllerMenuOptions(items)
			if options.PreferNative != native || &options.Items[0] != &items[0] {
				t.Fatal("renderer replaced shared model")
			}
		}
	}
}
