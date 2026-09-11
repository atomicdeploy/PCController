package control

import (
	"fmt"
	"time"

	"pccontroller.local/controller/internal/native"
)

const macroCompletionEvidenceGrace = 2 * time.Second

type macroTimingEvidenceError struct{ executed, observed, total int }

func (err *macroTimingEvidenceError) Error() string {
	return fmt.Sprintf("board completed %d/%d macro steps; timing unverified: received %d/%d execution timestamps after %s evidence grace", err.executed, err.total, err.observed, err.total, macroCompletionEvidenceGrace)
}

type macroCompletionWindow struct {
	watchdog         time.Duration
	watchdogDeadline time.Time
	evidenceDeadline time.Time
}

// Call only after draining queued macro evidence. Device completion and timing
// proof are independent; terminal success must win over an elapsed grace.
func (window *macroCompletionWindow) terminal(status native.MacroStatus, observed, total int, now time.Time) (bool, error) {
	if status.State == native.MacroCompleted {
		if observed >= total {
			return true, nil
		}
		if window.evidenceDeadline.IsZero() {
			window.evidenceDeadline = now.Add(macroCompletionEvidenceGrace)
		}
		if !now.Before(window.evidenceDeadline) {
			return true, &macroTimingEvidenceError{executed: int(status.ExecutedSteps), observed: observed, total: total}
		}
		return false, nil
	}
	if macroTerminal(status.State) {
		return true, nil
	}
	if now.After(window.watchdogDeadline) {
		return true, fmt.Errorf("macro playback exceeded its %s watchdog", window.watchdog)
	}
	return false, nil
}

// A query can overtake older status events still in the host event ring. Never
// replace authoritative completed/advanced counts with an earlier observation.
func mergeMacroDeviceStatus(current, incoming native.MacroStatus) native.MacroStatus {
	if incoming.ID != current.ID || incoming.StartedAtUS != current.StartedAtUS ||
		incoming.ExecutedSteps < current.ExecutedSteps || incoming.AcceptedBytes < current.AcceptedBytes ||
		incoming.AcceptedSteps < current.AcceptedSteps ||
		(macroTerminal(current.State) && !macroTerminal(incoming.State)) {
		return current
	}
	return incoming
}

func acknowledgeMacroDeviceStatus(status native.MacroStatus, observed, recordLength int) native.MacroStatus {
	if observed <= int(status.ExecutedSteps) {
		return status
	}
	status.ExecutedSteps = uint16(observed)
	if int(status.Fill) >= recordLength {
		status.Fill -= byte(recordLength)
	} else {
		status.Fill = 0
	}
	return status
}

// Inspect without waiting or issuing a serial request. Busy presentation/debug
// streams must not put an already-received execution ACK behind another query.
func (runtime *Runtime) pendingMacroEvent(afterID uint64, macroID byte) (Event, bool) {
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	for _, event := range runtime.eventLog {
		if event.ID <= afterID {
			continue
		}
		if event.Frame.Seq == native.MacroExecutionSequence {
			return event, true
		}
		if event.Frame.Opcode == native.OpEvent {
			deviceEvent, err := native.ParseDeviceEvent(event.Frame.Payload)
			if err == nil && deviceEvent.Macro != nil && deviceEvent.Macro.ID == macroID {
				return event, true
			}
		}
	}
	return Event{}, false
}
