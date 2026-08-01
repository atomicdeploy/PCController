package hostui

import (
	"testing"
	"time"
)

func TestKeyboardStateSuppressesRepeatAndRetainsControlChoiceUntilRelease(t *testing.T) {
	bindings := []KeyboardBinding{{Name: "side-a-up", Key: "K"}}
	keys, err := validateKeyboardBindings(bindings)
	if err != nil {
		t.Fatal(err)
	}
	state := newKeyboardState(bindings, keys)
	now := time.Unix(1, 0)
	events := state.handle('K', true, true, true, now)
	if len(events) != 1 || !events[0].Down || !events[0].Control {
		t.Fatalf("keydown=%#v", events)
	}
	if repeated := state.handle('K', true, true, true, now); len(repeated) != 0 {
		t.Fatalf("auto-repeat was not suppressed: %#v", repeated)
	}
	events = state.handle('K', false, false, true, now.Add(time.Millisecond))
	if len(events) != 1 || events[0].Down || !events[0].Control {
		t.Fatalf("keyup lost keydown action choice: %#v", events)
	}
}

func TestKeyboardStateFailSafeReleaseBlocksHeldKeyUntilPhysicalKeyUp(t *testing.T) {
	bindings := []KeyboardBinding{{Name: "side-b-up", Key: "A"}}
	keys, err := validateKeyboardBindings(bindings)
	if err != nil {
		t.Fatal(err)
	}
	state := newKeyboardState(bindings, keys)
	now := time.Unix(2, 0)
	if events := state.handle('A', true, false, true, now); len(events) != 1 {
		t.Fatalf("keydown=%#v", events)
	}
	events := state.releaseAll("disconnect", now.Add(time.Millisecond))
	if len(events) != 1 || events[0].Down || events[0].Reason != "disconnect" {
		t.Fatalf("fail-safe release=%#v", events)
	}
	if events := state.handle('A', true, false, true, now.Add(2*time.Millisecond)); len(events) != 0 {
		t.Fatalf("held key retriggered after fail-safe: %#v", events)
	}
	if events := state.handle('A', false, false, true, now.Add(3*time.Millisecond)); len(events) != 0 {
		t.Fatalf("unmatched physical keyup emitted action: %#v", events)
	}
	if events := state.handle('A', true, false, true, now.Add(4*time.Millisecond)); len(events) != 1 {
		t.Fatalf("fresh keydown after physical release=%#v", events)
	}
}

func TestKeyboardStateFocusLossReleasesEveryMotionKey(t *testing.T) {
	bindings := []KeyboardBinding{
		{Name: "side-b-up", Key: "A"},
		{Name: "side-a-down", Key: "L"},
	}
	keys, err := validateKeyboardBindings(bindings)
	if err != nil {
		t.Fatal(err)
	}
	state := newKeyboardState(bindings, keys)
	now := time.Unix(3, 0)
	state.handle('A', true, false, true, now)
	state.handle('L', true, false, true, now)
	events := state.handle('X', true, false, false, now.Add(time.Millisecond))
	if len(events) != 2 || events[0].Reason != "focus-lost" || events[1].Reason != "focus-lost" {
		t.Fatalf("focus release=%#v", events)
	}
	if state.active() != 0 {
		t.Fatalf("active keys remain after focus loss: %d", state.active())
	}
}

func TestKeyboardBindingValidationRejectsModifiersAndDuplicates(t *testing.T) {
	if _, err := validateKeyboardBindings([]KeyboardBinding{{Name: "one", Key: "CTRL"}}); err == nil {
		t.Fatal("modifier-only binding accepted")
	}
	if _, err := validateKeyboardBindings([]KeyboardBinding{
		{Name: "one", Key: "1"}, {Name: "two", Key: "1"},
	}); err == nil {
		t.Fatal("duplicate physical key accepted")
	}
	if key, canonical, err := ParseKeyboardKey("f13"); err != nil || key != 0x7C || canonical != "F13" {
		t.Fatalf("F13=%X/%q err=%v", key, canonical, err)
	}
}
