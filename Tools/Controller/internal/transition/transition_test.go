package transition

import "testing"

func TestEndpointsAndSmoothMidpoint(t *testing.T) {
	if Uint16(100, 4100, SmoothStep(0)) != 100 ||
		Uint16(100, 4100, SmoothStep(1)) != 4100 ||
		Uint16(100, 4100, SmoothStep(.5)) != 2100 {
		t.Fatal("shared transition endpoints or midpoint changed")
	}
	if Uint8(255, 0, SmoothStep(.5)) != 128 {
		t.Fatal("RGB byte interpolation no longer shares the PWM curve")
	}
}
