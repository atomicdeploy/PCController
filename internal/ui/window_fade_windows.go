//go:build windows

package ui

import "time"

const windowFadeDuration = 300 * time.Millisecond

type windowFadeBackend uint8

const (
	windowFadeBackendNone windowFadeBackend = iota
	windowFadeBackendPerPixel
	windowFadeBackendDirectX
	windowFadeBackendUniform
)

// windowFadeState is deliberately driven by elapsed time instead of timer
// ticks. A busy UI thread can therefore skip frames without making a startup
// or shutdown transition take longer than requested.
type windowFadeState struct {
	initialized bool
	active      bool
	closing     bool
	started     time.Time
	duration    time.Duration
	from        byte
	to          byte
	current     byte
}

func (fade *windowFadeState) Begin(
	now time.Time,
	duration time.Duration,
	from byte,
	to byte,
	closing bool,
) {
	fade.initialized = true
	fade.closing = closing
	fade.started = now
	fade.duration = duration
	fade.from = from
	fade.to = to
	fade.current = from
	fade.active = duration > 0 && from != to
	if !fade.active {
		fade.current = to
	}
}

func (fade *windowFadeState) Sample(now time.Time) (opacity byte, done bool) {
	if !fade.initialized {
		return 255, true
	}
	if !fade.active {
		return fade.current, true
	}
	elapsed := now.Sub(fade.started)
	if elapsed <= 0 {
		fade.current = fade.from
		return fade.current, false
	}
	if elapsed >= fade.duration {
		fade.current = fade.to
		fade.active = false
		return fade.current, true
	}

	progress := float64(elapsed) / float64(fade.duration)
	// Smoothstep keeps both ends gentle while preserving an exact midpoint.
	progress = progress * progress * (3 - 2*progress)
	value := float64(fade.from) +
		(float64(fade.to)-float64(fade.from))*progress
	if value <= 0 {
		fade.current = 0
	} else if value >= 255 {
		fade.current = 255
	} else {
		fade.current = byte(value + 0.5)
	}
	return fade.current, false
}

func (fade *windowFadeState) Opacity() byte {
	if !fade.initialized {
		return 255
	}
	return fade.current
}

func (fade *windowFadeState) Active() bool {
	return fade != nil && fade.active
}

func (fade *windowFadeState) Set(opacity byte) {
	fade.initialized = true
	fade.active = false
	fade.closing = false
	fade.from = opacity
	fade.to = opacity
	fade.current = opacity
}
