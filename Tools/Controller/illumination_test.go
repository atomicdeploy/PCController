package controller

import (
	"testing"
	"time"
)

func TestIlluminationPWMIsBoundedAndMonotonic(t *testing.T) {
	previous := uint16(0)
	for brightness := 0; brightness <= 255; brightness++ {
		got := illuminationPWM(byte(brightness))
		if got > 4095 {
			t.Fatalf("brightness %d produced out-of-range PWM %d", brightness, got)
		}
		if brightness != 0 && got <= previous {
			t.Fatalf("brightness %d produced non-increasing PWM %d after %d", brightness, got, previous)
		}
		previous = got
	}
	if illuminationPWM(0) != 0 || illuminationPWM(255) != 4095 {
		t.Fatalf("unexpected endpoints: 0=%d 255=%d", illuminationPWM(0), illuminationPWM(255))
	}
}

func TestIlluminationStateUsesDoorPolicyAndExactAppliedPWM(t *testing.T) {
	settings := Settings{
		LightMode: 1, OnBrightness: 128, OffBrightness: 16, Persisted: true,
	}
	pwm := PWMValues{Available: true}
	pwm.Values[enclosureIlluminationPWMChannel] = illuminationPWM(64)
	status := Status{PWMAvailable: true, DoorOpen: false}
	closed := illuminationState(settings, status, pwm, time.Unix(1, 0))
	if !closed.Available || closed.TargetBrightness != 16 || closed.TargetPWM != illuminationPWM(16) ||
		closed.AppliedBrightness != 64 || closed.AppliedPWM != illuminationPWM(64) || !closed.Persisted {
		t.Fatalf("closed auto illumination=%+v", closed)
	}
	status.DoorOpen = true
	opened := illuminationState(settings, status, pwm, time.Unix(2, 0))
	if opened.TargetBrightness != 128 || opened.TargetPWM != illuminationPWM(128) || opened.AtTarget {
		t.Fatalf("open auto illumination=%+v", opened)
	}
	settings.LightMode = 2
	pwm.Values[enclosureIlluminationPWMChannel] = illuminationPWM(128)
	forced := illuminationState(settings, Status{PWMAvailable: true}, pwm, time.Unix(3, 0))
	if forced.TargetBrightness != 128 || !forced.AtTarget {
		t.Fatalf("forced-on illumination=%+v", forced)
	}
}

func TestIlluminationStateChangeIgnoresObservationTimestamp(t *testing.T) {
	left := IlluminationState{Available: true, Mode: 1, AppliedPWM: 1024, UpdatedAt: time.Unix(1, 0)}
	right := left
	right.UpdatedAt = time.Unix(2, 0)
	if !sameIlluminationState(left, right) {
		t.Fatal("timestamp-only observations must not publish duplicate state events")
	}
	right.AppliedPWM++
	if sameIlluminationState(left, right) {
		t.Fatal("applied PWM changes must publish a state event")
	}
}
