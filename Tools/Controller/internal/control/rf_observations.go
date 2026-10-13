package control

import (
	"time"

	"pccontroller.local/controller/internal/native"
)

const rfLastSeenLimit = 256

// Called under eventMu. Only an actual board reception frame is evidence;
// transmit, bridge, learning and catalog refresh cannot create a seen date.
func (runtime *Runtime) recordRFReceptionLocked(event Event) {
	if event.Kind != "rf.receive" || event.Source != "board" || event.Frame.Opcode != native.OpEvent {
		return
	}
	parsed, err := native.ParseDeviceEvent(event.Frame.Payload)
	if err != nil || parsed.Type != native.EventRFReceived || parsed.RFCode == 0 || parsed.RFBits == 0 || parsed.RFBits > 32 || parsed.RFProtocol == 0 || parsed.RFProtocol > 12 {
		return
	}
	if runtime.rfLastSeen == nil {
		runtime.rfLastSeen = make(map[rfGestureKey]time.Time)
	}
	key := rfGestureKey{code: parsed.RFCode, bits: parsed.RFBits, protocol: parsed.RFProtocol}
	if _, exists := runtime.rfLastSeen[key]; !exists && len(runtime.rfLastSeen) >= rfLastSeenLimit {
		var oldest rfGestureKey
		var oldestTime time.Time
		for existing, seen := range runtime.rfLastSeen {
			if oldestTime.IsZero() || seen.Before(oldestTime) {
				oldest, oldestTime = existing, seen
			}
		}
		delete(runtime.rfLastSeen, oldest)
	}
	runtime.rfLastSeen[key] = event.Time
}

// RFLastSeen retains observed reception across catalog reads, not process
// restarts. Unknown is zero; no historical time is invented or persisted.
func (runtime *Runtime) RFLastSeen(code uint32, bits, protocol byte) time.Time {
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	return runtime.rfLastSeen[rfGestureKey{code: code, bits: bits, protocol: protocol}]
}
