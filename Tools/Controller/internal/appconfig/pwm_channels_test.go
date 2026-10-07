package appconfig

import (
	"math"
	"testing"
)

func TestPWMChannelDefaultsSeparateLightingAndMotorSafeControl(t *testing.T) {
	ui := Defaults().UI
	for _, channel := range []int{0, 7, 11} {
		config := PWMChannel(ui, channel)
		if config.OutputType != "lighting" || config.Curve != "gamma" || config.Gamma != 2.2 {
			t.Fatalf("channel %d lighting default = %+v", channel, config)
		}
	}
	for _, channel := range []int{8, 9, 10} {
		config := PWMChannel(ui, channel)
		if config.OutputType != "general" || config.Curve != "linear" || config.Gamma != 1 {
			t.Fatalf("channel %d general default = %+v", channel, config)
		}
	}
}

func TestPWMGammaMappingIsMonotonicAndRoundTrips(t *testing.T) {
	config := PWMChannelConfig{OutputType: "lighting", Icon: "lightbulb", Curve: "gamma", Gamma: 2.2}
	previous := uint16(0)
	for percent := 0.0; percent <= 100; percent += 0.1 {
		raw := PWMRawFromPercent(percent, config)
		if raw < previous {
			t.Fatalf("mapping decreased at %.1f%%: %d < %d", percent, raw, previous)
		}
		previous = raw
		roundTrip := PWMPercentFromRaw(raw, config)
		if raw != 0 && math.Abs(roundTrip-percent) > 1.5 {
			t.Fatalf("round trip %.1f%% -> %d -> %.3f%%", percent, raw, roundTrip)
		}
	}
	if PWMRawFromPercent(0, config) != 0 || PWMRawFromPercent(100, config) != PWMMaximumValue {
		t.Fatal("gamma endpoints are not exact")
	}
	if PWMRawFromPercent(15, config) >= PWMRawFromPercent(15, PWMChannelConfig{Curve: "linear", Gamma: 1}) {
		t.Fatal("lighting curve did not add low-end control resolution")
	}
}

func TestPWMChannelValidationRejectsUnsafeCurve(t *testing.T) {
	err := ValidatePWMChannels(map[string]PWMChannelConfig{
		"pwm.8": {OutputType: "motor", Icon: "settings", Curve: "gamma", Gamma: 9},
	})
	if err == nil {
		t.Fatal("expected out-of-range gamma rejection")
	}
}
