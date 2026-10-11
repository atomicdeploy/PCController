package control

import (
	"errors"
	"pccontroller.local/controller/internal/appconfig"
	"sort"
)

func expandEffectTimeline(macro appconfig.Macro) ([]appconfig.MacroStep, error) {
	count, interval, _, err := macro.Repetition()
	if err != nil {
		return nil, err
	}
	base, err := expandMacroTimeline(macro.Steps)
	if err != nil {
		return nil, err
	}
	if len(base)*int(count) > 65535 {
		return nil, errors.New("repeated effect exceeds 65535 runtime commands")
	}
	result := make([]appconfig.MacroStep, 0, len(base)*int(count))
	for n := uint16(0); n < count; n++ {
		for _, step := range base {
			step.AtUS += uint32(n) * interval // Repetition validated the complete span.
			result = append(result, step)
		}
	}
	// At a shared boundary, the preceding iteration's release precedes the
	// next iteration's start. Stable order preserves the authored sequence.
	sort.SliceStable(result, func(i, j int) bool { return result[i].AtUS < result[j].AtUS })
	return result, nil
}
