package traymenu

import "testing"

func TestPopupLifecycleRequiresCommittedPreparationBeforeActivate(t *testing.T) {
	t.Parallel()

	var state popupLifecycleState
	if state.activate() {
		t.Fatal("cold popup activated before preparation")
	}
	if !state.beginPreparation() || state != popupLifecyclePreparing {
		t.Fatalf("beginPreparation state = %d", state)
	}
	if state.activate() {
		t.Fatal("preparing popup activated before its first frame was committed")
	}
	if !state.commitPreparation() || state != popupLifecycleHiddenPrepared {
		t.Fatalf("commitPreparation state = %d", state)
	}
	if state.visible() {
		t.Fatal("prepared popup became visible before activation")
	}
	if !state.activate() || state != popupLifecycleActive || !state.visible() {
		t.Fatalf("activate state = %d, visible = %v", state, state.visible())
	}
}

func TestPopupLifecycleReuseRequiresFreshHiddenPreparation(t *testing.T) {
	t.Parallel()

	state := preparedActivePopup(t)
	if !state.beginHiding() || state != popupLifecycleHiding || !state.visible() {
		t.Fatalf("beginHiding state = %d, visible = %v", state, state.visible())
	}
	if choice := state.completeHiding(true); choice != popupCloseHide {
		t.Fatalf("completeHiding reuse choice = %d, want hide", choice)
	}
	if state != popupLifecycleHiddenReusable || !state.reusable() || state.visible() {
		t.Fatalf(
			"reusable state = %d, reusable = %v, visible = %v",
			state,
			state.reusable(),
			state.visible(),
		)
	}

	if state.activate() {
		t.Fatal("cached popup revealed its stale frame")
	}
	if !state.beginPreparation() || !state.commitPreparation() || !state.activate() {
		t.Fatalf("cached popup did not reopen after fresh preparation: state %d", state)
	}
}

func TestPopupLifecycleDisabledReuseDestroysAfterExit(t *testing.T) {
	t.Parallel()

	state := preparedActivePopup(t)
	if !state.beginHiding() {
		t.Fatal("active popup did not start hiding")
	}
	if choice := state.completeHiding(false); choice != popupCloseDestroy {
		t.Fatalf("completeHiding choice = %d, want destroy", choice)
	}
	if state != popupLifecycleDestroyed || state.visible() || state.reusable() {
		t.Fatalf("destroyed popup retained live state: %d", state)
	}
	if state.beginPreparation() || state.commitPreparation() || state.activate() {
		t.Fatal("destroyed popup accepted another lifecycle transition")
	}
}

func TestPopupLifecycleFailureInvalidatesCachedWindow(t *testing.T) {
	t.Parallel()

	state := popupLifecycleHiddenReusable
	if choice := state.invalidate(); choice != popupCloseDestroy {
		t.Fatalf("invalidate choice = %d, want destroy", choice)
	}
	if state != popupLifecycleDestroyed {
		t.Fatalf("invalidate state = %d, want destroyed", state)
	}
	if choice := state.invalidate(); choice != popupCloseNone {
		t.Fatalf("second invalidate choice = %d, want none", choice)
	}
	if choice := state.completeHiding(true); choice != popupCloseNone {
		t.Fatalf("invalid completeHiding choice = %d, want none", choice)
	}
}

func preparedActivePopup(t *testing.T) popupLifecycleState {
	t.Helper()
	var state popupLifecycleState
	if !state.beginPreparation() || !state.commitPreparation() || !state.activate() {
		t.Fatalf("could not prepare active popup: state %d", state)
	}
	return state
}
