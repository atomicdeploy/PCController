// Package transition owns small, allocation-free interpolation primitives
// shared by host-rendered RGB and streamed PWM/MOSFET transitions.
package transition

import "math"

func SmoothStep(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value * value * (3 - 2*value)
}

func Uint8(from, to byte, amount float64) byte {
	return byte(Uint16(uint16(from), uint16(to), amount))
}

func Uint16(from, to uint16, amount float64) uint16 {
	if amount <= 0 {
		return from
	}
	if amount >= 1 {
		return to
	}
	return uint16(math.Round(float64(from) + (float64(to)-float64(from))*amount))
}
