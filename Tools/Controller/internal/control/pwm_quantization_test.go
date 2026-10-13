package control

import (
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func TestPWMRelativeQuantizationAlwaysMovesInRequestedDirection(t *testing.T) {
	tiny := 0.000001
	for _, config := range []appconfig.PWMChannelConfig{
		{Curve: "linear", Gamma: 1, StepPercent: &tiny},
		{Curve: "gamma", Gamma: 2.2, StepPercent: &tiny},
		{Curve: "gamma", Gamma: 2.4, StepPercent: &tiny},
		{Curve: "gamma", Gamma: 5},
	} {
		for value := 0; value <= appconfig.PWMMaximumValue; value++ {
			raw := uint16(value)
			up, upErr := pwmActionRaw(raw, "increase", config)
			down, downErr := pwmActionRaw(raw, "decrease", config)
			if upErr != nil || downErr != nil || up > appconfig.PWMMaximumValue || down > appconfig.PWMMaximumValue {
				t.Fatalf("invalid duty %d config=%+v up=%d down=%d errors=%v/%v", raw, config, up, down, upErr, downErr)
			}
			if raw < appconfig.PWMMaximumValue && up <= raw || raw == appconfig.PWMMaximumValue && up != raw {
				t.Fatalf("increase stalled at %d: %d config=%+v", raw, up, config)
			}
			if raw > 0 && down >= raw || raw == 0 && down != 0 {
				t.Fatalf("decrease stalled at %d: %d config=%+v", raw, down, config)
			}
			for _, verb := range []string{"on", "off"} {
				percent, _ := pwmActionPercent(raw, verb, config)
				got, err := pwmActionRaw(raw, verb, config)
				if err != nil || got != appconfig.PWMRawFromPercent(percent, config) {
					t.Fatal("absolute action conversion changed")
				}
			}
		}
	}
}

func TestPWMRepeatedRelativeStepsUseRealReadbackWithoutStalling(t *testing.T) {
	tiny := 0.000001
	for _, gamma := range []float64{2.2, 2.4, 5} {
		config := appconfig.PWMChannelConfig{Curve: "gamma", Gamma: gamma, StepPercent: &tiny}
		values := native.PWMValues{Available: true}
		values.Values[15] = 1234
		reads, writes := 0, 0
		apply := func(verb string) {
			got, err := applyPWMActionWithIO(0, verb, config, func() (native.PWMValues, error) { reads++; return values, nil }, func(raw uint16) error { writes++; values.Values[0] = raw; return nil })
			if err != nil || got != values || got.Values[15] != 1234 {
				t.Fatalf("lost authoritative/untouched state: %+v %v", got, err)
			}
		}
		for step := 1; step <= 4; step++ {
			apply("increase")
			if values.Values[0] != uint16(step) {
				t.Fatalf("repeated increase stalled: %d", values.Values[0])
			}
		}
		for step := 3; step >= 0; step-- {
			apply("decrease")
			if values.Values[0] != uint16(step) {
				t.Fatalf("repeated decrease stalled: %d", values.Values[0])
			}
		}
		if reads != 16 || writes != 8 {
			t.Fatalf("read/write/readback phases lost: %d/%d", reads, writes)
		}
	}
}
