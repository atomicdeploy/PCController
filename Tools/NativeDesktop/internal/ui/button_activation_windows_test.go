//go:build windows

package ui

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/lxn/walk"
)

func TestPointerButtonActivationMatchesNativeReleaseSemantics(t *testing.T) {
	var state buttonActivationState
	if !state.pointerDown(actionToggle) {
		t.Fatal("button-down did not arm the control")
	}
	if !state.pressTarget(actionToggle) {
		t.Fatal("armed control is not visually depressed")
	}
	token := state.pointerToken()
	if state.pointerDown(actionOff) || state.pointerToken() != token {
		t.Fatal("a second pointer-down replaced the active gesture")
	}

	if !state.pointerMove(actionNone) || state.pressTarget(actionToggle) {
		t.Fatal("moving outside did not temporarily raise the armed control")
	}
	// The release hit is authoritative even if a coalesced MouseMove did not
	// report re-entry first.
	armed, activate := state.pointerUp(actionToggle)
	if armed != actionToggle || !activate {
		t.Fatalf("release after coalesced re-entry = (%d,%v), want activation", armed, activate)
	}

	if !state.pointerDown(actionToggle) {
		t.Fatal("second gesture did not arm")
	}
	if !state.pointerMove(actionNone) || !state.pointerMove(actionToggle) ||
		!state.pressTarget(actionToggle) {
		t.Fatal("moving back did not depress the still-armed control")
	}

	armed, activate = state.pointerUp(actionToggle)
	if armed != actionToggle || !activate {
		t.Fatalf("release-inside = (%d,%v), want toggle activation", armed, activate)
	}
	if state.pressTarget(actionToggle) {
		t.Fatal("release left the control depressed")
	}
	if armed, activate := state.pointerUp(actionToggle); armed != actionNone || activate {
		t.Fatal("one physical release activated more than once")
	}
}

func TestPointerReleaseOutsideOrOverAnotherControlCancels(t *testing.T) {
	for _, release := range []actionID{actionNone, actionOff, actionSettings} {
		var state buttonActivationState
		state.pointerDown(actionOn)
		state.pointerMove(release)
		armed, activate := state.pointerUp(release)
		if armed != actionOn || activate {
			t.Errorf(
				"release over %d = (%d,%v), want armed on without activation",
				release,
				armed,
				activate,
			)
		}
	}

	var state buttonActivationState
	state.pointerDown(actionSettings)
	if armed := state.cancelPointer(); armed != actionSettings {
		t.Fatalf("cancel returned %d, want settings", armed)
	}
	if state.pressTarget(actionSettings) {
		t.Fatal("capture/cancel left pointer press active")
	}
}

func TestStaleCaptureCancellationCannotCancelNextPress(t *testing.T) {
	var state buttonActivationState
	state.pointerDown(actionOn)
	firstToken := state.pointerToken()
	if firstToken == 0 {
		t.Fatal("first gesture has no capture token")
	}
	if action, activate := state.pointerUp(actionOn); action != actionOn || !activate {
		t.Fatalf("first release = (%d,%v), want on activation", action, activate)
	}

	state.pointerDown(actionOff)
	secondToken := state.pointerToken()
	if secondToken == 0 || secondToken == firstToken {
		t.Fatalf(
			"second token = %d, first = %d; want a new nonzero token",
			secondToken,
			firstToken,
		)
	}
	if cancelled := state.cancelPointerToken(firstToken); cancelled != actionNone {
		t.Fatalf("stale callback cancelled %d", cancelled)
	}
	if !state.pressTarget(actionOff) {
		t.Fatal("stale callback raised the second gesture")
	}
	if cancelled := state.cancelPointerToken(secondToken); cancelled != actionOff {
		t.Fatalf("current capture callback cancelled %d, want off", cancelled)
	}
}

func TestKeyboardButtonActivationWaitsForMatchingKeyUp(t *testing.T) {
	var state buttonActivationState
	action := keyboardActivationAction(walk.KeySpace)
	if action != actionToggle || !state.keyDown(walk.KeySpace, action) {
		t.Fatal("Space did not arm Toggle")
	}
	if !state.pressTarget(actionToggle) {
		t.Fatal("held Space does not depress Toggle")
	}
	if state.keyDown(walk.KeySpace, actionToggle) {
		t.Fatal("auto-repeat armed a second activation")
	}
	if action, activate := state.keyUp(walk.KeyReturn); action != actionNone || activate {
		t.Fatal("mismatched KeyUp activated the held key")
	}
	if action, activate := state.keyUp(walk.KeySpace); action != actionToggle || !activate {
		t.Fatalf("Space KeyUp = (%d,%v), want Toggle", action, activate)
	}
	if state.pressTarget(actionToggle) {
		t.Fatal("matching KeyUp left Toggle depressed")
	}
}

func TestPointerGestureBlocksKeyboardAndPointerTakesOverHeldKey(t *testing.T) {
	var state buttonActivationState
	state.pointerDown(actionOn)
	if state.keyDown(walk.KeyF2, actionSettings) {
		t.Fatal("keyboard gesture armed while a pointer gesture was active")
	}
	state.cancelPointer()

	if !state.keyDown(walk.KeyF2, actionSettings) {
		t.Fatal("keyboard gesture did not arm")
	}
	if cancelled := state.cancelKeyboard(); cancelled != actionSettings {
		t.Fatalf("pointer takeover cancelled %d, want settings", cancelled)
	}
	if !state.pointerDown(actionOff) {
		t.Fatal("pointer did not arm after taking over the held key")
	}
	if action, activate := state.keyUp(walk.KeyF2); action != actionNone || activate {
		t.Fatal("key-up dispatched after pointer takeover")
	}
}

func TestKeyboardCancelDoesNotActivate(t *testing.T) {
	var state buttonActivationState
	state.keyDown(walk.KeyF2, actionSettings)
	if cancelled := state.cancelKeyboard(); cancelled != actionSettings {
		t.Fatalf("cancelled %d, want settings", cancelled)
	}
	if action, activate := state.keyUp(walk.KeyF2); action != actionNone || activate {
		t.Fatal("KeyUp after focus cancellation activated settings")
	}
}

func TestHeldPressLevelDoesNotDecayBeforeRelease(t *testing.T) {
	level := 0.0
	for range 20 {
		var active bool
		level, active = advanceButtonPressLevel(
			level,
			true,
			16*time.Millisecond,
		)
		if level < 1 && !active {
			t.Fatal("press animation stopped before reaching its held target")
		}
	}
	if level != 1 {
		t.Fatalf("held press level = %f, want 1", level)
	}
	for range 20 {
		level, _ = advanceButtonPressLevel(
			level,
			false,
			16*time.Millisecond,
		)
	}
	if level != 0 {
		t.Fatalf("released press level = %f, want 0", level)
	}
}

func TestSettleVisualAppearanceAppliesFinalTargetsWithoutMotion(t *testing.T) {
	t.Parallel()

	shell := &Shell{hover: actionSettings}
	if !shell.buttonActivation.pointerDown(actionToggle) {
		t.Fatal("pointerDown did not arm toggle")
	}
	for index := 1; index < len(shell.hoverLevels); index++ {
		shell.hoverLevels[index] = 0.37
		shell.pressLevels[index] = 0.63
	}
	shell.visualPhase = 0.7
	shell.visualTick = time.Now()

	shell.settleVisualAppearance()

	for index := 1; index < len(shell.hoverLevels); index++ {
		wantHover := 0.0
		if actionID(index) == actionSettings {
			wantHover = 1
		}
		if shell.hoverLevels[index] != wantHover {
			t.Errorf(
				"hover level %d = %v, want %v",
				index,
				shell.hoverLevels[index],
				wantHover,
			)
		}
		wantPress := 0.0
		if actionID(index) == actionToggle {
			wantPress = 1
		}
		if shell.pressLevels[index] != wantPress {
			t.Errorf(
				"press level %d = %v, want %v",
				index,
				shell.pressLevels[index],
				wantPress,
			)
		}
	}
	if shell.visualPhase != 0 || !shell.visualTick.IsZero() {
		t.Fatalf(
			"animation clock not settled: phase=%v tick=%v",
			shell.visualPhase,
			shell.visualTick,
		)
	}
}

func TestSurfaceButtonActionsDispatchOnlyFromReleaseHandlers(t *testing.T) {
	file := parseUIFile(t, "shell_windows.go")
	if callsSelector(findFunction(t, file, "onMouseDown"), "s", "activateSurfaceAction") {
		t.Fatal("pointer action dispatch regressed to MouseDown")
	}
	if !callsSelector(findFunction(t, file, "onMouseUp"), "s", "activateSurfaceAction") {
		t.Fatal("MouseUp no longer dispatches the armed action")
	}
	if callsSelector(findFunction(t, file, "onKeyDown"), "s", "activateSurfaceAction") {
		t.Fatal("keyboard action dispatch regressed to KeyDown")
	}
	if !callsSelector(findFunction(t, file, "onKeyUp"), "s", "activateSurfaceAction") {
		t.Fatal("KeyUp no longer dispatches the armed action")
	}

	source, err := os.ReadFile("shell_windows.go")
	if err != nil {
		t.Fatalf("read shell_windows.go: %v", err)
	}
	for _, wiring := range [][]byte{
		[]byte("surface.MouseUp().Attach(s.onMouseUp)"),
		[]byte("surface.KeyUp().Attach(s.onKeyUp)"),
		[]byte("mw.KeyUp().Attach(s.onKeyUp)"),
		[]byte("client.KeyUp().Attach(s.onKeyUp)"),
	} {
		if !bytes.Contains(source, wiring) {
			t.Errorf("release wiring %q is missing", wiring)
		}
	}
	if count := bytes.Count(source, []byte("case win.WM_KILLFOCUS:")); count < 3 {
		t.Errorf(
			"keyboard cancellation is wired to %d focus owners, want surface, client, and main window",
			count,
		)
	}
}
