package link

import (
	"sync"
	"time"

	"pccontroller.local/controller/internal/native"
)

type rfWireKey struct {
	code           uint32
	bits, protocol byte
}

const RFContinuityGap = 250 * time.Millisecond

type rfWireReceipt struct {
	first  uint32
	micros uint32
	timed  bool
	seen   time.Time
}
type rfWireState struct {
	mu        sync.Mutex
	receipts  map[rfWireKey]rfWireReceipt
	clock     uint32
	known     bool
	clockSeen time.Time
}

// RFClockObservation is receive-pump evidence, independent of UI/history
// dispatch. Counters are compared with modular arithmetic within one session.
type RFClockObservation struct {
	FirstReceiptMicros uint32
	ReceiptMicros      uint32
	ReceiptTimed       bool
	ReceiptSeen        time.Time
	ClockMicros        uint32
	ClockKnown         bool
	ClockSeen          time.Time
}

func (value RFClockObservation) ContainsReceipt(micros uint32) bool {
	span := uint32(value.ReceiptMicros - value.FirstReceiptMicros)
	return value.ReceiptTimed && span < 1<<31 && uint32(micros-value.FirstReceiptMicros) <= span
}

func (value RFClockObservation) QuietFor(gap time.Duration) bool {
	if !value.ReceiptTimed || !value.ClockKnown {
		return false
	}
	elapsed := uint32(value.ClockMicros - value.ReceiptMicros)
	return elapsed < 1<<31 && uint64(elapsed) >= uint64(gap/time.Microsecond)
}

func (s *Session) RFReceiveProgress(code uint32, bits, protocol byte) (RFClockObservation, bool) {
	s.rfWire.mu.Lock()
	defer s.rfWire.mu.Unlock()
	r, ok := s.rfWire.receipts[rfWireKey{code, bits, protocol}]
	return RFClockObservation{r.first, r.micros, r.timed, r.seen, s.rfWire.clock, s.rfWire.known, s.rfWire.clockSeen}, ok
}

// This runs before response/event fan-out in the wire decoder. No callbacks,
// configuration, presentation, disk access or runtime snapshots are allowed.
func (s *Session) observeRFClock(frame native.Frame) {
	var event native.DeviceEvent
	var micros uint32
	var timed, rf bool
	switch frame.Opcode {
	case native.OpEvent:
		parsed, err := native.ParseDeviceEvent(frame.Payload)
		if err != nil {
			return
		}
		event, micros, timed = parsed, parsed.DeviceMicros, parsed.Timed
		rf = parsed.Type == native.EventRFReceived && parsed.RFCode != 0
	case native.OpStatus:
		status, err := native.ParseStatus(frame.Payload)
		if err != nil {
			return
		}
		micros, timed = status.UptimeMS*1000, true
	default:
		micros, timed = native.ResponseDeviceMicros(frame)
	}
	if !timed && !rf {
		return
	}
	now := time.Now()
	s.rfWire.mu.Lock()
	defer s.rfWire.mu.Unlock()
	if timed && (!s.rfWire.known || uint32(micros-s.rfWire.clock) < 1<<31) {
		s.rfWire.clock, s.rfWire.known, s.rfWire.clockSeen = micros, true, now
	}
	if !rf {
		return
	}
	if s.rfWire.receipts == nil {
		s.rfWire.receipts = make(map[rfWireKey]rfWireReceipt)
	}
	key := rfWireKey{event.RFCode, event.RFBits, event.RFProtocol}
	if _, present := s.rfWire.receipts[key]; !present && len(s.rfWire.receipts) >= 256 {
		var oldest rfWireKey
		var oldestTime time.Time
		for k, r := range s.rfWire.receipts {
			if oldestTime.IsZero() || r.seen.Before(oldestTime) {
				oldest, oldestTime = k, r.seen
			}
		}
		delete(s.rfWire.receipts, oldest)
	}
	first := event.DeviceMicros
	if previous, present := s.rfWire.receipts[key]; present && previous.timed && event.Timed {
		delta := uint32(event.DeviceMicros - previous.micros)
		if delta == 0 || delta >= 1<<31 {
			return
		}
		if delta <= uint32(RFContinuityGap/time.Microsecond) {
			first = previous.first
		}
	}
	s.rfWire.receipts[key] = rfWireReceipt{first, event.DeviceMicros, event.Timed, now}
}
