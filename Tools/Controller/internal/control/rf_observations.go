package control

import (
	"sort"
	"strconv"
	"time"

	"pccontroller.local/controller/internal/native"
)

const rfLastSeenLimit = 256

// RFReception is volatile, bounded observation, not an EEPROM assignment.
type RFReception struct {
	Code          uint32    `json:"code"`
	Bits          byte      `json:"bits"`
	Protocol      byte      `json:"protocol"`
	PulseUS       uint16    `json:"pulse_us"`
	ReceivedCount uint64    `json:"received_count"`
	LastSeen      time.Time `json:"last_seen"`
	Generation    uint64    `json:"connection_generation"`
}

// Called under eventMu. Only an actual board reception frame is evidence;
// transmit, bridge, learning and catalog refresh cannot create a seen date.
func (runtime *Runtime) recordRFReceptionLocked(event *Event) {
	if event.Kind != "rf.receive" || event.Source != "board" || event.Frame.Opcode != native.OpEvent {
		return
	}
	parsed, err := native.ParseDeviceEvent(event.Frame.Payload)
	if err != nil || parsed.Type != native.EventRFReceived || parsed.RFCode == 0 || parsed.RFBits == 0 || parsed.RFBits > 32 || parsed.RFProtocol == 0 || parsed.RFProtocol > 12 {
		return
	}
	if runtime.rfLastSeen == nil {
		runtime.rfLastSeen = make(map[rfGestureKey]time.Time)
		runtime.rfReceptions = make(map[rfGestureKey]RFReception)
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
		delete(runtime.rfReceptions, oldest)
	}
	runtime.rfLastSeen[key] = event.Time
	if runtime.rfReceptions == nil {
		runtime.rfReceptions = make(map[rfGestureKey]RFReception)
	}
	previous := runtime.rfReceptions[key]
	generation, _ := strconv.ParseUint(event.Metadata["connection_generation"], 10, 64)
	if previous.Generation != generation {
		previous.ReceivedCount = 0
	}
	value := RFReception{parsed.RFCode, parsed.RFBits, parsed.RFProtocol, parsed.RFPulseUS, previous.ReceivedCount + 1, event.Time, generation}
	runtime.rfReceptions[key] = value
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}
	event.Metadata["received_count"] = strconv.FormatUint(value.ReceivedCount, 10)
}

func (runtime *Runtime) RFReceptions() []RFReception {
	runtime.mu.RLock()
	generation := runtime.generation
	runtime.mu.RUnlock()
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	values := make([]RFReception, 0, len(runtime.rfReceptions))
	for _, value := range runtime.rfReceptions {
		if value.Generation == generation {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].LastSeen.After(values[j].LastSeen) })
	return values
}

// RFLastSeen retains observed reception across catalog reads, not process
// restarts. Unknown is zero; no historical time is invented or persisted.
func (runtime *Runtime) RFLastSeen(code uint32, bits, protocol byte) time.Time {
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	return runtime.rfLastSeen[rfGestureKey{code: code, bits: bits, protocol: protocol}]
}
