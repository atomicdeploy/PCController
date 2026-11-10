package osd

import (
	"math"
	"testing"
	"time"
)

func TestExitIsContinuousMonotonicAndBounded(t *testing.T) {
	for _, start := range []time.Duration{0, 40 * time.Millisecond, 100 * time.Millisecond, time.Second} {
		alpha, slide := Timeline(start, 5*time.Second, false)
		a, y := exitFrame(0, exitTime, alpha, slide)
		if a != alpha || y != slide {
			t.Fatal("exit jumped", a, y, alpha, slide)
		}
		for elapsed := time.Millisecond; elapsed <= exitTime; elapsed += time.Millisecond {
			next, down := exitFrame(elapsed, exitTime, alpha, slide)
			if next > a || down < y || next < 0 || down > 8 {
				t.Fatal("exit reversed", elapsed, next, down)
			}
			a, y = next, down
		}
		if a != 0 || y != 8 {
			t.Fatal("exit did not finish", a, y)
		}
	}
}

func TestHoverReversalContinuesFromCurrentValue(t *testing.T) {
	from := []float64{0, 0, 0}
	values, moving := hoverFrame(from, 0, hoverTime/2, false)
	if !moving || values[0] != .5 || values[1] != 0 {
		t.Fatal(values, moving)
	}
	reversed, _ := hoverFrame(values, 1, 0, false)
	if reversed[0] != values[0] || reversed[1] != 0 {
		t.Fatal("hover reversal jumped", reversed)
	}
	settled, moving := hoverFrame(values, 1, hoverTime, false)
	if moving || settled[0] != 0 || settled[1] != 1 {
		t.Fatal(settled, moving)
	}
	reduced, moving := hoverFrame(values, 2, 0, true)
	if moving || reduced[0] != 0 || reduced[2] != 1 {
		t.Fatal(reduced, moving)
	}
	if math.Abs(from[0]) > 0 {
		t.Fatal("mutated previous frame")
	}
}
