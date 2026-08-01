package controller

import (
	"context"
	"testing"
	"time"
)

func TestPublicRFValidationWithoutDevice(t *testing.T) {
	client := New(Options{})
	defer client.Shutdown()
	if err := client.BeginRFLearn(context.Background(), 0); err == nil {
		t.Fatal("expected zero learn timeout to fail validation")
	}
	err := client.MapLearnedRF(context.Background(), 1, RFMapping{
		Action: RFActionRelay, Value: 8, Behavior: RFBehaviorToggle,
	})
	if err == nil {
		t.Fatal("expected invalid relay mapping to fail validation")
	}
	err = client.MapLearnedRF(context.Background(), 1, RFMapping{
		Action: RFActionRelay, Value: 0, Behavior: RFBehaviorToggle,
	})
	if err == nil {
		t.Fatal("expected unsafe R1 mapping to fail host validation")
	}
	if err := client.BeginRFLearn(context.Background(), 121*time.Second); err == nil {
		t.Fatal("expected oversized learn timeout to fail validation")
	}
}

func TestPublicPeripheralValidationWithoutDevice(t *testing.T) {
	client := New(Options{})
	defer client.Shutdown()
	ctx := context.Background()
	if err := client.SetRelay(ctx, 0, true); err == nil {
		t.Fatal("expected R0 to fail validation")
	}
	if err := client.SetRelay(ctx, 9, true); err == nil {
		t.Fatal("expected R9 to fail validation")
	}
	if err := client.SetPWMChannel(ctx, 16, 1); err == nil {
		t.Fatal("expected PWM channel 16 to fail validation")
	}
	if err := client.SetPWMChannel(ctx, 0, 4096); err == nil {
		t.Fatal("expected PWM value 4096 to fail validation")
	}
	if err := client.SetPWMMode(ctx, PWMMode(3)); err == nil {
		t.Fatal("expected PWM mode 3 to fail validation")
	}
	if err := client.PlayTone(ctx, 440, 0); err == nil {
		t.Fatal("expected zero-duration tone to fail validation")
	}
	if err := client.TransmitRF(ctx, 0, 24, 1, 350, 1); err == nil {
		t.Fatal("expected zero RF code to fail validation")
	}
	if _, err := client.StartMelody(ctx, Melody{
		Name: "bad",
		Notes: []MelodyNote{{
			FrequencyHz: 1,
			DurationMS:  1,
		}},
	}, 1); err == nil {
		t.Fatal("expected invalid melody to fail validation")
	}
	if _, err := client.StartStatusLEDEffect(ctx, StatusLEDEffect{
		Name: "bad", Kind: "breathe", PeriodMS: 100,
	}); err == nil {
		t.Fatal("expected invalid status LED effect to fail validation")
	}
}
