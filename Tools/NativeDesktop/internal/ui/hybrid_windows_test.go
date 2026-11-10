//go:build windows

package ui

import (
	"testing"

	"github.com/lxn/walk"
	"pccontroller.local/desktop/presentation"
)

func TestControllerCombinedWindowVisibilityPreservesShell(t *testing.T) {
	s, _ := newControllerWindowFixture(t)
	s.controller = &presentation.Options{HideOnClose: true}
	hwnd := s.mw.Handle()
	for _, action := range []string{"show", "hide", "show", "toggle", "toggle"} {
		before := s.mw.Visible()
		if err := s.applyControllerWindowRequest(action); err != nil {
			t.Fatal(err)
		}
		pumpControllerFixture(s)
		want := action == "show" || (action == "toggle" && !before)
		if s.mw.Visible() != want || s.mw.Handle() != hwnd || s.closed.Load() {
			t.Fatalf("%s recreated/disposed the shell or gave wrong visibility", action)
		}
	}
	if err := s.applyControllerWindowRequest("invalid"); err == nil {
		t.Fatal("unknown action accepted")
	}
	// Combined mode's close button hides without disposing the GUI/TUI session,
	// even if ordinary standalone preferences disable close-to-tray.
	s.cfg.UI.CloseToTray = false
	canceled := false
	s.onClosing(&canceled, walk.CloseReasonUnknown)
	pumpControllerFixture(s)
	if !canceled || s.mw.Visible() || s.closed.Load() {
		t.Fatal("GUI close ended the combined session")
	}
	if err := s.applyControllerWindowRequest("show"); err != nil {
		t.Fatal(err)
	}
	pumpControllerFixture(s)
	if !s.mw.Visible() || s.mw.Handle() != hwnd {
		t.Fatal("closed GUI could not be shown again")
	}
}
