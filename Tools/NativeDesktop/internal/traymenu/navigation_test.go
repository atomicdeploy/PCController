package traymenu

import "testing"

func TestNavigatorSkipsDisabledAndSeparators(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "disabled", Label: "Disabled", Disabled: true},
		{Separator: true},
		{ID: "on", Label: "On"},
		{ID: "off", Label: "Off", Disabled: true},
		{ID: "toggle", Label: "Toggle"},
	}
	n := newNavigator(items)
	if n.selected != 2 {
		t.Fatalf("initial selection = %d, want 2", n.selected)
	}
	if !n.move(1) || n.selected != 4 {
		t.Fatalf("move down selected %d, want 4", n.selected)
	}
	if !n.move(1) || n.selected != 2 {
		t.Fatalf("wrap selected %d, want 2", n.selected)
	}
	if !n.move(-1) || n.selected != 4 {
		t.Fatalf("move up selected %d, want 4", n.selected)
	}
	if !n.home() || n.selected != 2 {
		t.Fatalf("home selected %d, want 2", n.selected)
	}
	if !n.end() || n.selected != 4 {
		t.Fatalf("end selected %d, want 4", n.selected)
	}
	if action, ok := n.action(); !ok || action != "toggle" {
		t.Fatalf("action = (%q,%v), want toggle", action, ok)
	}
}

func TestNavigatorMouseAndMnemonic(t *testing.T) {
	t.Parallel()
	n := newNavigator([]Item{
		{ID: "on", Label: "&On"},
		{ID: "off", Label: "Off"},
		{ID: "settings", Label: "Settings"},
	})
	if !n.selectIndex(2) || n.selected != 2 {
		t.Fatalf("selectIndex selected %d", n.selected)
	}
	if !n.matchRune('o') || n.selected != 0 {
		t.Fatalf("first mnemonic selected %d, want 0", n.selected)
	}
	if !n.matchRune('O') || n.selected != 1 {
		t.Fatalf("cycled mnemonic selected %d, want 1", n.selected)
	}
	if n.matchRune('x') {
		t.Fatal("unknown mnemonic reported a match")
	}
	n.selectIndex(-1)
	if _, ok := n.action(); ok {
		t.Fatal("action available with no selection")
	}
}

func TestNavigatorWithNoSelectableItems(t *testing.T) {
	t.Parallel()
	n := newNavigator([]Item{
		{Separator: true},
		{ID: "disabled", Label: "Disabled", Disabled: true},
	})
	if n.selected != -1 || n.move(1) {
		t.Fatalf("navigator = %#v", n)
	}
}
