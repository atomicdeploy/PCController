package appconfig

import "fmt"

// Repetition describes the finite whole-sequence run. Omitted count is one;
// an explicitly supplied zero is invalid. Intervals are start-to-start.
func (macro Macro) Repetition() (count uint16, intervalUS, totalUS uint32, err error) {
	count = 1
	if macro.RepeatCount != nil {
		count = *macro.RepeatCount
	}
	if count < 1 || count > 1000 {
		return 0, 0, 0, fmt.Errorf("repeat_count must be 1..1000")
	}
	if macro.RepeatIntervalMS > 3_600_000 {
		return 0, 0, 0, fmt.Errorf("repeat_interval_ms must be 0..3600000")
	}
	var span uint64
	for _, step := range macro.Steps {
		span = max(span, uint64(step.AtUS)+uint64(step.DurationMS)*1000)
	}
	interval := uint64(macro.RepeatIntervalMS) * 1000
	if interval == 0 {
		interval = span
	}
	if interval < span {
		return 0, 0, 0, fmt.Errorf("repeat_interval_ms must cover the full base effect span")
	}
	if count > 1 && interval == 0 {
		return 0, 0, 0, fmt.Errorf("zero-span repetition requires an explicit nonzero interval")
	}
	total := span + uint64(count-1)*interval
	if span > 0x7fffffff || total > 0x7fffffff || interval > 0x7fffffff {
		return 0, 0, 0, fmt.Errorf("whole effect repetition exceeds 2147483647 us")
	}
	return count, uint32(interval), uint32(total), nil
}
