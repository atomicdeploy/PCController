package control

import "pccontroller.local/controller/internal/native"

// PWMRecallValue is confirmed, primary-owned RAM state for the current board
// connection, never an optimistic client draft or a claimed physical reading.
// Raw preserves exact duty when the presentation curve changes.
type PWMRecallValue struct {
	Known bool   `json:"known"`
	Raw   uint16 `json:"raw"`
}

func (runtime *Runtime) observePWMRecallLocked(values native.PWMValues) {
	if !values.Available || validatePWMActionReadback(values) != nil || runtime.session == nil {
		return
	}
	if runtime.pwmRecallGeneration != runtime.generation {
		runtime.pwmRecall = [16]PWMRecallValue{}
		runtime.pwmRecallGeneration = runtime.generation
	}
	for channel, raw := range values.Values {
		if raw != 0 {
			runtime.pwmRecall[channel] = PWMRecallValue{Known: true, Raw: raw}
		}
	}
}

func (runtime *Runtime) pwmRecallAtGeneration(generation uint64, channel byte) PWMRecallValue {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if runtime.session == nil || runtime.generation != generation || runtime.pwmRecallGeneration != generation || channel >= 16 {
		return PWMRecallValue{}
	}
	return runtime.pwmRecall[channel]
}
