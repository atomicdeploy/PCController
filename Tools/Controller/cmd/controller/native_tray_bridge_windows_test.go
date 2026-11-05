//go:build windows && !no_native_gui

package main

import (
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/nativeshell"
	"testing"
)

func TestDesktopPreservesEveryHostTrayAction(t *testing.T) {
	for _, connected := range []bool{false, true} {
		state := control.Snapshot{Connected: connected}
		want := nativeshell.BuildMenu(nativeshell.State{Connected: connected})
		got := desktopHostMenu(state)
		if len(got) != len(want) {
			t.Fatal("host tray item lost")
		}
		for i, item := range want {
			if got[i].Label != item.Label || got[i].Disabled == item.Enabled || got[i].Separator != (item.Kind == nativeshell.ItemSeparator) {
				t.Fatalf("host tray changed item %d: %+v", i, got[i])
			}
			if item.Kind == nativeshell.ItemAction && got[i].ID == "" {
				t.Fatalf("host action %s lost its dispatch ID", item.Label)
			}
		}
	}
}
