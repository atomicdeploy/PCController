//go:build windows

package ui

import (
	"testing"
	"time"
)

func TestWindowFadeZeroValueIsFullyVisibleAndSettled(t *testing.T) {
	var fade windowFadeState
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)

	if fade.Active() {
		t.Fatal("zero-value fade is active")
	}
	if got := fade.Opacity(); got != 255 {
		t.Fatalf("zero-value opacity = %d, want 255", got)
	}
	opacity, done := fade.Sample(now)
	if opacity != 255 || !done {
		t.Fatalf("zero-value sample = (%d, %v), want (255, true)", opacity, done)
	}
}

func TestWindowFadeDurationIsThreeHundredMilliseconds(t *testing.T) {
	if windowFadeDuration != 300*time.Millisecond {
		t.Fatalf("window fade duration = %v, want 300ms", windowFadeDuration)
	}
}

func TestWindowFadeInUsesElapsedSmoothstep(t *testing.T) {
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	var fade windowFadeState
	fade.Begin(start, windowFadeDuration, 0, 255, false)

	if !fade.Active() {
		t.Fatal("new fade-in is not active")
	}
	if got := fade.Opacity(); got != 0 {
		t.Fatalf("initial fade-in opacity = %d, want 0", got)
	}

	tests := []struct {
		name     string
		offset   time.Duration
		want     byte
		wantDone bool
	}{
		{name: "before start", offset: -time.Millisecond, want: 0},
		{name: "start", offset: 0, want: 0},
		{name: "quarter", offset: 75 * time.Millisecond, want: 40},
		{name: "midpoint", offset: 150 * time.Millisecond, want: 128},
		{name: "three quarters", offset: 225 * time.Millisecond, want: 215},
		{name: "last millisecond", offset: 299 * time.Millisecond, want: 255},
		{name: "complete", offset: 300 * time.Millisecond, want: 255, wantDone: true},
		{name: "after complete", offset: 450 * time.Millisecond, want: 255, wantDone: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opacity, done := fade.Sample(start.Add(test.offset))
			if opacity != test.want || done != test.wantDone {
				t.Fatalf(
					"sample at %v = (%d, %v), want (%d, %v)",
					test.offset,
					opacity,
					done,
					test.want,
					test.wantDone,
				)
			}
		})
	}
	if fade.Active() {
		t.Fatal("completed fade-in remains active")
	}
}

func TestWindowFadeOutIsMonotonicDespiteIrregularTicks(t *testing.T) {
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	var fade windowFadeState
	fade.Begin(start, windowFadeDuration, 255, 0, true)

	if !fade.closing {
		t.Fatal("fade-out did not retain its closing intent")
	}
	previous := byte(255)
	for _, offset := range []time.Duration{
		0,
		17 * time.Millisecond,
		68 * time.Millisecond,
		68 * time.Millisecond,
		201 * time.Millisecond,
		299 * time.Millisecond,
	} {
		opacity, done := fade.Sample(start.Add(offset))
		if done {
			t.Fatalf("fade-out completed early at %v", offset)
		}
		if opacity > previous {
			t.Fatalf(
				"fade-out opacity increased at %v: %d after %d",
				offset,
				opacity,
				previous,
			)
		}
		previous = opacity
	}

	opacity, done := fade.Sample(start.Add(windowFadeDuration))
	if opacity != 0 || !done {
		t.Fatalf("completed fade-out = (%d, %v), want (0, true)", opacity, done)
	}
	if fade.Active() {
		t.Fatal("completed fade-out remains active")
	}
}

func TestWindowFadeSupportsArbitraryEndpointsAndContinuousRestart(t *testing.T) {
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	var fade windowFadeState
	fade.Begin(start, windowFadeDuration, 64, 192, false)

	midpoint, done := fade.Sample(start.Add(windowFadeDuration / 2))
	if midpoint != 128 || done {
		t.Fatalf("partial-range midpoint = (%d, %v), want (128, false)", midpoint, done)
	}

	restart := start.Add(180 * time.Millisecond)
	current, done := fade.Sample(restart)
	if done {
		t.Fatal("fade completed before continuous restart")
	}
	fade.Begin(restart, windowFadeDuration, current, 0, true)
	if got := fade.Opacity(); got != current {
		t.Fatalf("restarted fade jumped from %d to %d", current, got)
	}
	if opacity, done := fade.Sample(restart); opacity != current || done {
		t.Fatalf(
			"restarted fade initial sample = (%d, %v), want (%d, false)",
			opacity,
			done,
			current,
		)
	}
}

func TestWindowFadeImmediateAndSettlePaths(t *testing.T) {
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		duration time.Duration
		from     byte
		to       byte
	}{
		{name: "zero duration", duration: 0, from: 0, to: 255},
		{name: "negative duration", duration: -time.Millisecond, from: 255, to: 0},
		{name: "equal endpoints", duration: windowFadeDuration, from: 91, to: 91},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fade windowFadeState
			fade.Begin(start, test.duration, test.from, test.to, true)
			if fade.Active() {
				t.Fatal("immediate fade is active")
			}
			opacity, done := fade.Sample(start)
			if opacity != test.to || !done {
				t.Fatalf(
					"immediate sample = (%d, %v), want (%d, true)",
					opacity,
					done,
					test.to,
				)
			}
		})
	}

	var fade windowFadeState
	fade.Begin(start, windowFadeDuration, 0, 255, true)
	fade.Sample(start.Add(100 * time.Millisecond))
	fade.Set(173)
	if fade.Active() || fade.closing {
		t.Fatalf("Set left stale state: active=%v closing=%v", fade.Active(), fade.closing)
	}
	if got := fade.Opacity(); got != 173 {
		t.Fatalf("Set opacity = %d, want 173", got)
	}
	if opacity, done := fade.Sample(start.Add(time.Hour)); opacity != 173 || !done {
		t.Fatalf("settled sample = (%d, %v), want (173, true)", opacity, done)
	}
}
