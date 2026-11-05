//go:build windows

package winfx

import (
	"math"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/design"
)

func TestDialogEaseIsClampedSmoothAndMonotonic(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-1, 0},
		{0, 0},
		{0.25, 0.15625},
		{0.5, 0.5},
		{0.75, 0.84375},
		{1, 1},
		{2, 1},
	}
	for _, test := range tests {
		if got := dialogEase(test.input); math.Abs(got-test.want) > 1e-9 {
			t.Fatalf("dialogEase(%g) = %g, want %g", test.input, got, test.want)
		}
	}

	previous := dialogEase(0)
	for step := 1; step <= 100; step++ {
		current := dialogEase(float64(step) / 100)
		if current < previous {
			t.Fatalf(
				"dialogEase regressed at step %d: %g after %g",
				step,
				current,
				previous,
			)
		}
		previous = current
	}
}

func TestDialogMixColorInterpolatesCOLORREFChannelsAndClamps(t *testing.T) {
	first := dialogButtonRGB(10, 20, 30)
	second := dialogButtonRGB(110, 220, 230)

	tests := []struct {
		amount float64
		want   uint32
	}{
		{-1, first},
		{0, first},
		{0.25, dialogButtonRGB(35, 70, 80)},
		{0.5, dialogButtonRGB(60, 120, 130)},
		{0.75, dialogButtonRGB(85, 170, 180)},
		{1, second},
		{2, second},
	}
	for _, test := range tests {
		if got := dialogMixColor(first, second, test.amount); got != test.want {
			t.Fatalf(
				"dialogMixColor(%#08x, %#08x, %g) = %#08x, want %#08x",
				first,
				second,
				test.amount,
				got,
				test.want,
			)
		}
	}
}

func TestDialogMotionPalettesHaveVisibleIntermediateFrames(t *testing.T) {
	for _, dark := range []bool{false, true} {
		buttons := dialogButtonColors(dark)
		buttonMid := dialogMixColor(
			buttons.NormalFill,
			buttons.NormalHot,
			dialogEase(0.5),
		)
		if buttonMid == buttons.NormalFill || buttonMid == buttons.NormalHot {
			t.Fatalf(
				"dark=%v button midpoint %#08x equals an endpoint",
				dark,
				buttonMid,
			)
		}

		tabs := dialogTabColors(dark, true)
		tabMid := dialogMixColor(
			tabs.Inactive,
			tabs.Hot,
			dialogEase(0.5),
		)
		if tabMid == tabs.Inactive || tabMid == tabs.Hot {
			t.Fatalf(
				"dark=%v tab midpoint %#08x equals an endpoint",
				dark,
				tabMid,
			)
		}
	}
}

func TestAdvanceDialogMotionEntersReversesAndSettles(t *testing.T) {
	value, active := advanceDialogMotion(0, 1, 48*time.Millisecond, 120)
	if math.Abs(value-0.4) > 1e-9 || !active {
		t.Fatalf("enter frame = (%g, %v), want (0.4, true)", value, active)
	}

	// Reversing while the enter transition is in flight must continue from
	// the current value rather than jumping to either endpoint.
	value, active = advanceDialogMotion(value, 0, 24*time.Millisecond, 120)
	if math.Abs(value-0.2) > 1e-9 || !active {
		t.Fatalf("reversed exit frame = (%g, %v), want (0.2, true)", value, active)
	}
	value, active = advanceDialogMotion(value, 0, 24*time.Millisecond, 120)
	if value != 0 || active {
		t.Fatalf("settled exit frame = (%g, %v), want (0, false)", value, active)
	}

	// A delayed timer settles according to wall-clock time. Extending motion
	// after a stall makes the UI feel sluggish and violates VIS-013.
	value, active = advanceDialogMotion(0, 1, time.Second, 120)
	if value != 1 || active {
		t.Fatalf(
			"delayed frame = (%g, %v), want (1, false)",
			value,
			active,
		)
	}

	value, active = advanceDialogMotion(0, 1, 0, 120)
	frames := 0
	for active {
		value, active = advanceDialogMotion(
			value,
			1,
			dialogMotionTimerInterval,
			120,
		)
		frames++
		if frames > 20 {
			t.Fatal("motion never settled")
		}
	}
	if value != 1 {
		t.Fatalf("settled enter value = %g, want 1", value)
	}
}

func TestAdvanceDialogMotionFallbackAndDisabledDuration(t *testing.T) {
	value, active := advanceDialogMotion(0, 1, 0, 160)
	if math.Abs(value-0.1) > 1e-9 || !active {
		t.Fatalf("zero-elapsed fallback = (%g, %v), want (0.1, true)", value, active)
	}
	value, active = advanceDialogMotion(0.4, 1, 10*time.Millisecond, 0)
	if value != 1 || active {
		t.Fatalf("disabled duration = (%g, %v), want (1, false)", value, active)
	}
	value, active = advanceDialogMotion(-4, 3, 10*time.Millisecond, 120)
	if value <= 0 || value >= 1 || !active {
		t.Fatalf("clamped endpoints = (%g, %v), want an intermediate frame", value, active)
	}
}

func TestDialogButtonAnimatedVisualInterpolatesHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, primary := range []bool{false, true} {
			palette := dialogButtonColors(dark)
			normal := dialogButtonAnimatedVisualFor(
				palette,
				primary,
				0,
				false,
				false,
				true,
			)
			middle := dialogButtonAnimatedVisualFor(
				palette,
				primary,
				0.5,
				false,
				false,
				true,
			)
			hot := dialogButtonAnimatedVisualFor(
				palette,
				primary,
				1,
				false,
				false,
				true,
			)
			if middle.Fill == normal.Fill || middle.Fill == hot.Fill {
				t.Fatalf(
					"dark=%v primary=%v hover midpoint fill %#08x equals an endpoint",
					dark,
					primary,
					middle.Fill,
				)
			}
			if middle.Border == normal.Border || middle.Border == hot.Border {
				t.Fatalf(
					"dark=%v primary=%v hover midpoint border %#08x equals an endpoint",
					dark,
					primary,
					middle.Border,
				)
			}

			pressed := dialogButtonAnimatedVisualFor(
				palette,
				primary,
				0.5,
				true,
				false,
				true,
			)
			wantPressed := dialogButtonVisualFor(
				palette,
				primary,
				false,
				true,
				false,
				true,
			)
			if pressed != wantPressed {
				t.Fatalf(
					"dark=%v primary=%v pressed hover leaked: got %+v, want %+v",
					dark,
					primary,
					pressed,
					wantPressed,
				)
			}
		}
	}
}

func TestDialogTabIndicatorPositionAndRectInterpolateSelection(t *testing.T) {
	state := dialogTabThemeState{
		IndicatorFrom: 0,
		IndicatorTo:   2,
	}
	tests := []struct {
		progress float64
		want     float64
	}{
		{0, 0},
		{0.25, 0.3125},
		{0.5, 1},
		{0.75, 1.6875},
		{1, 2},
	}
	for _, test := range tests {
		state.IndicatorProgress = test.progress
		if got := dialogTabIndicatorPosition(state); math.Abs(got-test.want) > 1e-9 {
			t.Fatalf(
				"indicator position at %g = %g, want %g",
				test.progress,
				got,
				test.want,
			)
		}
	}

	rects := []dialogTabRect{
		{Left: 0, Top: 0, Right: 100, Bottom: 40},
		{Left: 100, Top: 0, Right: 260, Bottom: 40},
	}
	state.IndicatorFrom = 0
	state.IndicatorTo = 1
	state.IndicatorProgress = 0.5
	got, ok := dialogTabIndicatorRect(rects, []bool{true, true}, state, 96)
	want := dialogTabRect{Left: 58, Top: 37, Right: 172, Bottom: 40}
	if !ok || got != want {
		t.Fatalf("mid-transition indicator = (%+v, %v), want (%+v, true)", got, ok, want)
	}

	if _, ok := dialogTabIndicatorRect(
		rects,
		[]bool{true, false},
		state,
		96,
	); ok {
		t.Fatal("indicator accepted an invalid destination tab rectangle")
	}
}

func TestSettleDialogTabMotionRestoresExactTargets(t *testing.T) {
	state := dialogTabThemeState{
		Hot:               2,
		Selected:          1,
		HoverLevels:       []float64{0.2, 0.7, 0.4},
		SelectionLevels:   []float64{0.8, 0.1, 0.3},
		IndicatorFrom:     0.4,
		IndicatorTo:       1,
		IndicatorProgress: 0.6,
		LastTick:          time.Now(),
	}
	settleDialogTabMotion(&state)
	wantHover := []float64{0, 0, 1}
	wantSelection := []float64{0, 1, 0}
	for index := range wantHover {
		if state.HoverLevels[index] != wantHover[index] {
			t.Fatalf(
				"settled hover[%d] = %g, want %g",
				index,
				state.HoverLevels[index],
				wantHover[index],
			)
		}
		if state.SelectionLevels[index] != wantSelection[index] {
			t.Fatalf(
				"settled selection[%d] = %g, want %g",
				index,
				state.SelectionLevels[index],
				wantSelection[index],
			)
		}
	}
	if state.IndicatorFrom != 1 ||
		state.IndicatorTo != 1 ||
		state.IndicatorProgress != 1 ||
		!state.LastTick.IsZero() {
		t.Fatalf("settled indicator/tick = %+v", state)
	}
}

func TestDialogButtonAnimationStateAdvancesFromHoverTarget(t *testing.T) {
	const hwnd = uintptr(0x4242)
	start := time.Unix(100, 0)
	dialogButtonMu.Lock()
	dialogButtonThemes[hwnd] = dialogButtonThemeState{
		Hot:      true,
		Animate:  true,
		LastTick: start,
	}
	dialogButtonMu.Unlock()
	t.Cleanup(func() {
		dialogButtonMu.Lock()
		delete(dialogButtonThemes, hwnd)
		dialogButtonMu.Unlock()
	})

	if !advanceDialogButtonAnimation(hwnd, start.Add(30*time.Millisecond)) {
		t.Fatal("button hover animation settled before its first intermediate frame")
	}
	state, exists := dialogButtonTheme(hwnd)
	if !exists || math.Abs(state.Hover-0.25) > 1e-9 {
		t.Fatalf("button hover state = (%+v, %v), want progress 0.25", state, exists)
	}
}

func TestDialogTabAnimationAdvancesHoverSelectionAndIndicatorTogether(
	t *testing.T,
) {
	const hwnd = uintptr(0x4343)
	start := time.Unix(200, 0)
	dialogTabMu.Lock()
	dialogTabThemes[hwnd] = dialogTabThemeState{
		Hot:               1,
		Selected:          2,
		Animate:           true,
		HoverLevels:       []float64{0, 0, 0},
		SelectionLevels:   []float64{1, 0, 0},
		IndicatorFrom:     0,
		IndicatorTo:       2,
		IndicatorProgress: 0,
		LastTick:          start,
	}
	dialogTabMu.Unlock()
	t.Cleanup(func() {
		dialogTabMu.Lock()
		delete(dialogTabThemes, hwnd)
		dialogTabMu.Unlock()
	})

	if !advanceDialogTabAnimation(hwnd, start.Add(30*time.Millisecond)) {
		t.Fatal("tab animation settled before its first intermediate frame")
	}
	state, exists := dialogTabTheme(hwnd)
	if !exists {
		t.Fatal("tab animation state disappeared")
	}
	if math.Abs(state.HoverLevels[1]-0.25) > 1e-9 {
		t.Fatalf("tab hover progress = %g, want 0.25", state.HoverLevels[1])
	}
	wantSelectionStep := 30.0 / float64(design.Default.Motion.NormalMS)
	if math.Abs(state.SelectionLevels[0]-(1-wantSelectionStep)) > 1e-9 ||
		math.Abs(state.SelectionLevels[2]-wantSelectionStep) > 1e-9 ||
		math.Abs(state.IndicatorProgress-wantSelectionStep) > 1e-9 {
		t.Fatalf("tab selection/indicator state = %+v", state)
	}
}

func TestDialogTabSchedulerRestartRejectsStaleFrame(t *testing.T) {
	const hwnd = uintptr(0x4545)
	old := &dialogTabAnimationScheduler{
		token: 1,
		stop:  make(chan struct{}),
	}
	current := &dialogTabAnimationScheduler{
		token: 2,
		stop:  make(chan struct{}),
	}
	current.pending.Store(true)

	dialogTabSchedulerMu.Lock()
	dialogTabSchedulers[hwnd] = current
	dialogTabSchedulerMu.Unlock()
	t.Cleanup(func() {
		dialogTabSchedulerMu.Lock()
		delete(dialogTabSchedulers, hwnd)
		dialogTabSchedulerMu.Unlock()
		close(old.stop)
		close(current.stop)
	})

	if consumeDialogTabAnimationFrame(hwnd, old.token) {
		t.Fatal("stale tab-animation token was accepted after restart")
	}
	if !current.pending.Load() {
		t.Fatal("stale token consumed the current generation's pending frame")
	}
	if !consumeDialogTabAnimationFrame(hwnd, current.token) {
		t.Fatal("current tab-animation token was rejected")
	}
	if current.pending.Load() {
		t.Fatal("current tab-animation frame remained pending after consume")
	}
}
