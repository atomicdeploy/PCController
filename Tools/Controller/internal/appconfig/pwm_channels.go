package appconfig

import (
	"fmt"
	"math"
	"strings"
)

const (
	PWMChannelCount = 16
	PWMMaximumValue = 4095
	PWMMinimumGamma = 0.10
	PWMMaximumGamma = 5.00
)

// PWMChannelConfig describes the operator-facing meaning of one physical PWM
// channel. Percent is always logical intensity; Raw is always the exact board
// duty. Curve conversion happens once, at the host/board boundary.
type PWMChannelConfig struct {
	OutputType string  `json:"output_type"`
	Icon       string  `json:"icon"`
	Curve      string  `json:"curve"`
	Gamma      float64 `json:"gamma"`
}

func DefaultPWMChannels() map[string]PWMChannelConfig {
	result := make(map[string]PWMChannelConfig, PWMChannelCount)
	for channel := 0; channel < PWMChannelCount; channel++ {
		config := PWMChannelConfig{OutputType: "general", Icon: "sliders-horizontal", Curve: "linear", Gamma: 1}
		switch {
		case channel <= 7 || channel == 11:
			config = PWMChannelConfig{OutputType: "lighting", Icon: "lightbulb", Curve: "gamma", Gamma: 2.2}
		case channel >= 12:
			config = PWMChannelConfig{OutputType: "indicator", Icon: "led", Curve: "gamma", Gamma: 2.2}
		}
		result[PWMChannelKey(channel)] = config
	}
	return result
}

func PWMChannelKey(channel int) string { return fmt.Sprintf("pwm.%d", channel) }

func PWMChannel(config UI, channel int) PWMChannelConfig {
	defaults := DefaultPWMChannels()
	value := defaults[PWMChannelKey(channel)]
	if configured, ok := config.PWMChannels[PWMChannelKey(channel)]; ok {
		value = configured
	}
	return normalizePWMChannel(value)
}

func normalizePWMChannel(value PWMChannelConfig) PWMChannelConfig {
	value.OutputType = strings.ToLower(strings.TrimSpace(value.OutputType))
	value.Icon = strings.TrimSpace(value.Icon)
	value.Curve = strings.ToLower(strings.TrimSpace(value.Curve))
	if value.Curve == "linear" {
		value.Gamma = 1
	}
	return value
}

func ValidatePWMChannels(channels map[string]PWMChannelConfig) error {
	if len(channels) > PWMChannelCount {
		return fmt.Errorf("ui.pwm_channels may contain at most %d entries", PWMChannelCount)
	}
	for key, raw := range channels {
		var channel int
		if _, err := fmt.Sscanf(key, "pwm.%d", &channel); err != nil || key != PWMChannelKey(channel) || channel < 0 || channel >= PWMChannelCount {
			return fmt.Errorf("ui.pwm_channels key %q must be pwm.0..pwm.15", key)
		}
		value := normalizePWMChannel(raw)
		switch value.OutputType {
		case "lighting", "indicator", "motor", "general":
		default:
			return fmt.Errorf("ui.pwm_channels[%q].output_type must be lighting, indicator, motor, or general", key)
		}
		if value.Icon == "" || len(value.Icon) > 48 || !printableASCII(value.Icon) {
			return fmt.Errorf("ui.pwm_channels[%q].icon must be 1..48 printable ASCII bytes", key)
		}
		switch value.Curve {
		case "linear":
		case "gamma":
			if math.IsNaN(value.Gamma) || math.IsInf(value.Gamma, 0) || value.Gamma < PWMMinimumGamma || value.Gamma > PWMMaximumGamma {
				return fmt.Errorf("ui.pwm_channels[%q].gamma must be %.2f..%.2f", key, PWMMinimumGamma, PWMMaximumGamma)
			}
		default:
			return fmt.Errorf("ui.pwm_channels[%q].curve must be linear or gamma", key)
		}
	}
	return nil
}

func PWMRawFromPercent(percent float64, config PWMChannelConfig) uint16 {
	if math.IsNaN(percent) || percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return PWMMaximumValue
	}
	config = normalizePWMChannel(config)
	logical := percent / 100
	if config.Curve == "gamma" {
		logical = perceptualPWMForward(logical, config.Gamma)
	}
	raw := uint16(math.Round(logical * PWMMaximumValue))
	// A non-zero operator request must never disappear into the first quantized
	// code. This preserves a usable 0.1..1% toe even on a 12-bit driver.
	if raw == 0 {
		return 1
	}
	return raw
}

func PWMPercentFromRaw(raw uint16, config PWMChannelConfig) float64 {
	if raw == 0 {
		return 0
	}
	if raw >= PWMMaximumValue {
		return 100
	}
	config = normalizePWMChannel(config)
	duty := float64(raw) / PWMMaximumValue
	if config.Curve == "gamma" {
		duty = perceptualPWMInverse(duty, config.Gamma)
	}
	return duty * 100
}

// The linear toe preserves several real 12-bit codes below 4% instead of
// collapsing them to zero. The upper section is a tangent-continuous power
// curve, so neither the value nor the fade speed jumps at the join.
func perceptualPWMForward(logical, gamma float64) float64 {
	const toe = 0.04
	offset := (gamma - 1) * toe
	junction := math.Pow(gamma*toe/(1+offset), gamma)
	if logical <= toe {
		return logical * junction / toe
	}
	return math.Pow((logical+offset)/(1+offset), gamma)
}

func perceptualPWMInverse(duty, gamma float64) float64 {
	const toe = 0.04
	offset := (gamma - 1) * toe
	junction := math.Pow(gamma*toe/(1+offset), gamma)
	if duty <= junction {
		return duty * toe / junction
	}
	return math.Pow(duty, 1/gamma)*(1+offset) - offset
}

// ApplyPWMChannelConfig adds channel semantics to the shared descriptor
// catalog without changing profile-owned names and groups.
func ApplyPWMChannelConfig(peripherals []PeripheralDescriptor, controls []ControlDescriptor, ui UI) {
	for index := range peripherals {
		if peripherals[index].Kind != "pwm" {
			continue
		}
		config := PWMChannel(ui, peripherals[index].Index)
		peripherals[index].OutputType = config.OutputType
		peripherals[index].Curve = config.Curve
		peripherals[index].Gamma = config.Gamma
		if peripherals[index].Icon == "" {
			peripherals[index].Icon = config.Icon
		}
	}
	for index := range controls {
		if !strings.HasPrefix(controls[index].Key, "pwm.") {
			continue
		}
		var channel int
		if _, err := fmt.Sscanf(controls[index].Key, "pwm.%d", &channel); err != nil {
			continue
		}
		config := PWMChannel(ui, channel)
		controls[index].OutputType = config.OutputType
		controls[index].Curve = config.Curve
		controls[index].Gamma = config.Gamma
		if controls[index].Icon == "" {
			controls[index].Icon = config.Icon
		}
	}
}
