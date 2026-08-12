package tui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// formatEngineering chooses a readable SI prefix without throwing away the
// caller-selected precision. baseScale is the number of raw units in one base
// unit (1000 for mV->V, mA->A, and mW->W).
func formatEngineering(raw int64, baseScale float64, baseUnit string, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	if decimals > 4 {
		decimals = 4
	}
	value := float64(raw) / baseScale
	abs := math.Abs(value)
	prefix := ""
	switch {
	case abs >= 1_000_000:
		value /= 1_000_000
		prefix = "M"
	case abs >= 1_000:
		value /= 1_000
		prefix = "k"
	case abs > 0 && abs < 0.001:
		value *= 1_000_000
		prefix = "u"
	case abs > 0 && abs < 1:
		value *= 1_000
		prefix = "m"
	}
	return fmt.Sprintf("%.*f %s%s", decimals, value, prefix, baseUnit)
}

func formatVoltage(millivolts int32, decimals int) string {
	return formatEngineering(int64(millivolts), 1000, "V", decimals)
}

func formatCurrent(milliamps int32, decimals int) string {
	return formatEngineering(int64(milliamps), 1000, "A", decimals)
}

func formatPower(milliwatts int32, decimals int) string {
	return formatEngineering(int64(milliwatts), 1000, "W", decimals)
}

func formatTemperature(centiCelsius int16, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	if decimals > 2 {
		decimals = 2
	}
	return fmt.Sprintf("%.*f °C", decimals, float64(centiCelsius)/100)
}

func formatUptime(milliseconds uint32) string {
	duration := time.Duration(milliseconds) * time.Millisecond
	if duration < time.Second {
		return fmt.Sprintf("%d ms", milliseconds)
	}
	return duration.Truncate(time.Second).String()
}

// freshnessLabel uses the host-owned window so every local or remote TUI
// renders the same live/stale boundary as the WebUI.
func freshnessLabel(updated, now time.Time, window time.Duration) string {
	if updated.IsZero() {
		return "waiting for device"
	}
	if window <= 0 {
		window = 1500 * time.Millisecond
	}
	age := now.Sub(updated)
	if age < 0 {
		age = 0
	}
	if age < window {
		return "live"
	}
	if age < 10*time.Second {
		return fmt.Sprintf("%.1f s ago", age.Seconds())
	}
	if age < time.Minute {
		return fmt.Sprintf("%d s ago", int(age.Round(time.Second)/time.Second))
	}
	return age.Round(time.Minute).String() + " ago"
}

func bluetoothAudioState(value byte) string {
	switch value {
	case 0:
		return "BT Audio · off / indicator dark"
	case 1:
		return "BT Audio · connected (solid indicator)"
	case 2:
		return "BT Audio · disconnected / pairing (blinking indicator)"
	default:
		return fmt.Sprintf("BT Audio · unknown state %d", value)
	}
}

func boolWord(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}

func sparkline(values []float64, width int) string {
	const levels = "▁▂▃▄▅▆▇█"
	if width < 1 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	span := maximum - minimum
	var result strings.Builder
	for _, value := range values {
		index := 3
		if span > 0 {
			index = int(math.Round((value - minimum) / span * 7))
		}
		result.WriteString(string([]rune(levels)[index]))
	}
	return result.String()
}
