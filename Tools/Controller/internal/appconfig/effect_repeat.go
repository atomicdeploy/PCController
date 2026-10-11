package appconfig

import "fmt"

// Step repeats are independent, finite start-to-start schedules. Zero interval
// follows the shared length (or 1ms for an instantaneous command). Overlapping
// instances retain authored ordering, including their release commands.
func (step MacroStep) Repetition() (count uint16, intervalUS, spanUS uint32, err error) {
	count = 1
	if step.RepeatCount != nil {
		count = *step.RepeatCount
	}
	if count < 1 || count > 1000 || step.RepeatIntervalMS > 3_600_000 {
		return 0, 0, 0, fmt.Errorf("step repeat count must be 1..1000 and interval 0..3600000 ms")
	}
	interval := uint64(step.RepeatIntervalMS) * 1000
	if interval == 0 {
		interval = uint64(max(step.DurationMS, 1)) * 1000
	}
	span := uint64(step.DurationMS)*1000 + uint64(count-1)*interval
	if span+uint64(step.AtUS) > 0x7fffffff || interval > 0x7fffffff {
		return 0, 0, 0, fmt.Errorf("step repetition exceeds 2147483647 us")
	}
	return count, uint32(interval), uint32(span), nil
}

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
	for index, step := range macro.Steps {
		_, _, stepSpan, stepErr := step.Repetition()
		if stepErr != nil {
			return 0, 0, 0, fmt.Errorf("step %d: %w", index+1, stepErr)
		}
		span = max(span, uint64(step.AtUS)+uint64(stepSpan))
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
