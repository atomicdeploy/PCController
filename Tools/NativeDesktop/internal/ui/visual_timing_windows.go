//go:build windows

package ui

import "time"

func visualFrameElapsed(previous, now time.Time) time.Duration {
	elapsed := now.Sub(previous)
	if previous.IsZero() || elapsed <= 0 {
		return 16 * time.Millisecond
	}
	// The next frame must catch up after a busy message queue, not extend the
	// transition by pretending the missed display interval never elapsed.
	return elapsed
}
