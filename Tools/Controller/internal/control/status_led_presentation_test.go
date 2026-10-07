package control

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestStatusLEDPresentationBoundsAnimationFramesAndKeepsTransitionsImmediate(t *testing.T) {
	runtime := &Runtime{}
	base := time.Unix(10, 0)
	first := native.StatusLEDState{Red: 10, Green: 20, Blue: 30, Brightness: 180, Effect: 1, Condition: 2}
	if !runtime.shouldPublishStatusLED(first, base) {
		t.Fatal("first status LED frame was not published")
	}

	nearby := first
	nearby.Red = 14
	if runtime.shouldPublishStatusLED(nearby, base.Add(16*time.Millisecond)) {
		t.Fatal("animation frame bypassed presentation interval")
	}
	if !runtime.shouldPublishStatusLED(nearby, base.Add(statusLEDPresentationInterval)) {
		t.Fatal("latest animation frame was not published at presentation interval")
	}

	effectTransition := nearby
	effectTransition.Effect++
	if !runtime.shouldPublishStatusLED(effectTransition, base.Add(55*time.Millisecond)) {
		t.Fatal("effect transition was not published immediately")
	}

	colorTransition := effectTransition
	colorTransition.Blue = 240
	if !runtime.shouldPublishStatusLED(colorTransition, base.Add(60*time.Millisecond)) {
		t.Fatal("large color transition was not published immediately")
	}
}
