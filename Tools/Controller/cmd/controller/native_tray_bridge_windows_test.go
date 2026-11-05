//go:build windows && !no_native_gui

package main

import (
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/nativeshell"
	"testing"
)

func TestDesktopPreservesEveryHostTrayAction(t *testing.T) {
	for _, state := range []control.Snapshot{{}, {Paused: true}, {ConnectionState: "connecting"}, {ConnectionState: "reconnecting"}, {Connected: true}} {
		state.Port.Name = "COM18"
		want := nativeshell.BuildMenu(desktopTrayState(state, "PCController"))
		got := desktopHostMenu(state)
		if len(got) != len(want) {
			t.Fatal("host tray item lost")
		}
		for i, item := range want {
			if got[i].Label != item.Label || got[i].Disabled == item.Enabled || got[i].Separator != (item.Kind == nativeshell.ItemSeparator) || got[i].Default != item.Default {
				t.Fatalf("host tray changed item %d: %+v", i, got[i])
			}
			if item.Kind == nativeshell.ItemAction && got[i].ID == "" {
				t.Fatalf("host action %s lost its dispatch ID", item.Label)
			}
		}
	}
}

func TestDesktopTrayUsesOriginalStatusAndIcons(t *testing.T) {
	for _, state := range []control.Snapshot{{}, {Paused: true}, {ConnectionState: "connecting"}, {ConnectionState: "reconnecting"}, {Connected: true}} {
		state.Port.Name = "COM18"
		original := desktopTrayState(state, "Cinema Controller")
		got := desktopSnapshot(state, "127.0.0.1:8787", original.Title)
		if got.TrayIcon != string(nativeshell.StateIcon(original)) || got.TrayTooltip != nativeshell.Tooltip(original) {
			t.Fatalf("tray status lost: %+v", got)
		}
	}
}
