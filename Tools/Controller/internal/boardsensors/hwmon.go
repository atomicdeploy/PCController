package boardsensors

import (
	"fmt"
	"math"
	"time"
)

// HWMonFrame uses Linux hwmon units. TTL is remaining source freshness, not a
// new lease on every poll. No unavailable reading is exported as a healthy zero.
func (s Snapshot) HWMonFrame(now time.Time) (string, error) {
	ids := []string{"supply", "bus", "current", "power", "temperature_led", "temperature_bt_audio"}
	units := []string{"V", "V", "A", "W", "C", "C"}
	scales := []float64{1000, 1000, 1000, 1000000, 1000, 1000}
	minimum := []float64{0, 0, -100, -10000, -55, -55}
	maximum := []float64{100, 100, 100, 10000, 125, 125}
	values := [6]int64{}
	mask := 0
	ttl := s.ExpiresAt.Sub(now).Milliseconds()
	if !s.Connected || !s.Fresh || s.Source != "pccontroller-board-status" || s.ObservedAt.IsZero() || s.ObservedAt.After(now) || !s.ExpiresAt.Equal(s.ObservedAt.Add(MaxAge)) || ttl <= 0 || ttl > MaxAge.Milliseconds() {
		return "1 0 0 0 0 0 0 0 0\n", nil
	}
	seen := map[string]bool{}
	for _, r := range s.Readings {
		if seen[r.ID] {
			return "", fmt.Errorf("duplicate board sensor %s", r.ID)
		}
		seen[r.ID] = true
		for i, id := range ids {
			if r.ID != id || r.Value == nil {
				continue
			}
			if r.Unit != units[i] || math.IsNaN(*r.Value) || math.IsInf(*r.Value, 0) || *r.Value < minimum[i] || *r.Value > maximum[i] {
				return "", fmt.Errorf("invalid board sensor %s", id)
			}
			values[i] = int64(math.Round(*r.Value * scales[i]))
			mask |= 1 << i
		}
	}
	return fmt.Sprintf("1 %d %d %d %d %d %d %d %d\n", ttl, mask, values[0], values[1], values[2], values[3], values[4], values[5]), nil
}
