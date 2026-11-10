package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCombinedDesktopToggleIsContextualAndPreservesDraft(t *testing.T) {
	model := readyModel(t, PageConsole)
	for _, item := range model.actionBarItems(model.snapshot()) {
		if item.action == "gui" {
			t.Fatal("ordinary TUI must not advertise a nonexistent GUI")
		}
	}
	calls := 0
	want := errors.New("window test failure")
	model.toggleGUI = func() error { calls++; return want }
	model.input.SetValue("unsent command")
	updated, command, handled := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	if !handled || command == nil || updated.input.Value() != "unsent command" || calls != 0 {
		t.Fatal("GUI toggle must preserve the input and run outside the UI loop")
	}
	result, ok := command().(commandResultMsg)
	if !ok || !errors.Is(result.err, want) || calls != 1 {
		t.Fatal("GUI toggle must run exactly once and report its actual error")
	}
	found := false
	for _, item := range updated.actionBarItems(updated.snapshot()) {
		found = found || item.action == "gui"
	}
	if !found {
		t.Fatal("combined mode needs a discoverable mouse/keyboard GUI action")
	}
}
