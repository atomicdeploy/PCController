package hostui

import "fmt"

var byteUnits = [...]string{"B", "KiB", "MiB", "GiB", "TiB"}

// FormatBytes presents exact byte counters in compact binary units for human-facing surfaces.
func FormatBytes(value int64) string {
	if value < 0 {
		return "unknown"
	}
	unit, scaled := 0, float64(value)
	for scaled >= 1024 && unit < len(byteUnits)-1 {
		scaled /= 1024
		unit++
	}
	switch {
	case unit == 0:
		return fmt.Sprintf("%d %s", value, byteUnits[unit])
	case scaled >= 100:
		return fmt.Sprintf("%.0f %s", scaled, byteUnits[unit])
	case scaled >= 10:
		return fmt.Sprintf("%.1f %s", scaled, byteUnits[unit])
	default:
		return fmt.Sprintf("%.2f %s", scaled, byteUnits[unit])
	}
}

// FormatByteProgress preserves numerator and denominator while avoiding raw-byte noise.
func FormatByteProgress(done, total int64) string {
	return FormatBytes(done) + " / " + FormatBytes(total)
}
