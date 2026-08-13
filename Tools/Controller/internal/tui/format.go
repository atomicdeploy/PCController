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

func validVoltageReading(millivolts int32) bool {
	return millivolts >= 0 && millivolts <= 100_000
}

func validCurrentReading(milliamps int32) bool {
	return milliamps >= -1_000_000 && milliamps <= 1_000_000
}

func validPowerReading(milliwatts int32) bool {
	return milliwatts >= 0 && milliwatts <= 1_000_000_000
}

func validTemperatureReading(centiCelsius int16) bool {
	// DS18B20's specified measurement range is -55..125 C. This also rejects
	// the firmware's INT16_MIN missing-reading sentinel before it is formatted.
	return centiCelsius >= -5_500 && centiCelsius <= 12_500
}

func formatUptime(milliseconds uint32) string {
	duration := time.Duration(milliseconds) * time.Millisecond
	if duration < time.Second {
		return fmt.Sprintf("%d ms", milliseconds)
	}
	return duration.Truncate(time.Second).String()
}

// freshnessLiveThreshold covers the four-Hz remote convergence path with
// enough scheduling and network headroom. A 500 ms threshold previously made
// a healthy remote view alternate between "live" and a fractional age on every
// poll. Once this window expires, the age is actionable and shown.
const freshnessLiveThreshold = 1500 * time.Millisecond

// freshnessLabel reports data as live while it remains within the expected
// convergence window. Once stale, the age is stable enough to be actionable.
func freshnessLabel(updated, now time.Time) string {
	if updated.IsZero() {
		return "waiting for device"
	}
	age := now.Sub(updated)
	if age < 0 {
		age = 0
	}
	if age < freshnessLiveThreshold {
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

const remoteClockSkewWarningThreshold = 3 * time.Second

// remoteClockSkewWarning keeps clock diagnostics separate from freshness.
// Remote status timestamps cross a JSON boundary and cannot carry Go's
// monotonic clock reading; using them directly for age makes clock skew look
// like transport lag. The offset remains useful as an explicit warning.
func remoteClockSkewWarning(offset time.Duration) string {
	if offset > -remoteClockSkewWarningThreshold && offset < remoteClockSkewWarningThreshold {
		return ""
	}
	direction := "ahead"
	magnitude := offset
	if magnitude < 0 {
		direction = "behind"
		magnitude = -magnitude
	}
	var formatted string
	switch {
	case magnitude < 10*time.Second:
		formatted = fmt.Sprintf("%.1f s", magnitude.Seconds())
	case magnitude < time.Minute:
		formatted = fmt.Sprintf("%d s", int(magnitude.Round(time.Second)/time.Second))
	default:
		formatted = magnitude.Round(time.Minute).String()
	}
	return fmt.Sprintf("Clock skew · remote ≈%s %s · check time sync", formatted, direction)
}

func bluetoothAudioState(value byte) string {
	switch value {
	case 0:
		return "off · indicator dark"
	case 1:
		return "connected · solid indicator"
	case 2:
		return "disconnected or pairing · blinking indicator"
	default:
		return fmt.Sprintf("unknown state %d", value)
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
