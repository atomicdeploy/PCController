package control

import (
	"math"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestVirtualSeatTravelFollowsIndependentAppliedRelays(t *testing.T) {
	runtime := New(Options{})
	now := time.Now()
	runtime.hello.Capabilities = native.CapabilityRelayMotion
	runtime.reconcileMotionLocked(0, now)
	if runtime.virtualSeats.valid {
		t.Fatal("physical board acquired simulated position")
	}
	runtime.hello.BuildFeatures = native.BuildFeatureNativeHAL
	travel := runtime.options.VirtualSeatTravel
	if travel != 3*time.Second {
		t.Fatalf("default full travel = %s", travel)
	}
	check := func(elapsed time.Duration, left, right float64) {
		t.Helper()
		got := runtime.virtualSeats.snapshot(now.Add(elapsed), travel)
		if math.Abs(got.LeftPosition-left) > 1e-9 ||
			math.Abs(got.RightPosition-right) > 1e-9 {
			t.Fatalf("%s: got (%f,%f), want (%f,%f)", elapsed, got.LeftPosition, got.RightPosition, left, right)
		}
	}
	// Left up, right down. A direction bit without enable holds position.
	runtime.reconcileMotionLocked(0b1110, now)
	check(600*time.Millisecond, 0.7, 0.3)
	runtime.reconcileMotionLocked(0b1101, now.Add(600*time.Millisecond))
	check(900*time.Millisecond, 0.7, 0.2)
	runtime.reconcileMotionLocked(0b0011, now.Add(900*time.Millisecond))
	check(1200*time.Millisecond, 0.6, 0.2)
	check(9*time.Second, 0, 0.2)
	runtime.clearMotionLocked()
	if runtime.virtualSeats.valid {
		t.Fatal("simulated position survived connection reset")
	}
}

func TestVirtualSeatTravelChangesRateWithoutPositionJump(t *testing.T) {
	now := time.Now()
	var seats virtualSeatPositions
	seats.observe(0b0010, now, 3*time.Second)
	seats.advance(now.Add(600*time.Millisecond), 3*time.Second)
	got := seats.snapshot(now.Add(1200*time.Millisecond), 6*time.Second)
	if math.Abs(got.LeftPosition-0.8) > 1e-9 || got.RightPosition != 0.5 || got.FullTravelMS != 6000 {
		t.Fatalf("changed rate: %+v", got)
	}
}
