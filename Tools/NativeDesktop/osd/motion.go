package osd

import "time"

const exitTime = 160 * time.Millisecond
const hoverTime = 120 * time.Millisecond

func smoothProgress(elapsed, duration time.Duration) float64 {
	if duration <= 0 || elapsed >= duration {
		return 1
	}
	if elapsed <= 0 {
		return 0
	}
	x := float64(elapsed) / float64(duration)
	return x * x * (3 - 2*x)
}

// An interrupted entrance exits from its actual opacity/position, not from a
// fabricated fully visible frame. Early expiry is never extended by a click.
func exitFrame(elapsed, duration time.Duration, opacity, slide float64) (float64, float64) {
	x := smoothProgress(elapsed, duration)
	return opacity * (1 - x), slide + (8-slide)*x
}

func hoverFrame(from []float64, target int, elapsed time.Duration, reduced bool) ([]float64, bool) {
	x := smoothProgress(elapsed, hoverTime)
	if reduced {
		x = 1
	}
	values := make([]float64, len(from))
	moving := false
	for i, start := range from {
		end := 0.0
		if i == target {
			end = 1
		}
		values[i] = start + (end-start)*x
		moving = moving || (start != end && x < 1)
	}
	return values, moving
}
