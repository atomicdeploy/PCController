package native

import (
	"encoding/binary"
	"fmt"
)

const (
	MacroQueueSchema       byte = 2
	MacroExecutionSequence byte = 0xFE
	MacroQueueCapacity          = 127
	MacroAppendHeaderSize       = 5
	MacroRecordHeaderSize       = 6
	MacroMaximumFragment        = MaxPayload - MacroAppendHeaderSize
)

const (
	MacroIdle byte = iota
	MacroBuffering
	MacroPlaying
	MacroCancelled
	MacroCompleted
	MacroFailed
)

// MacroStatus is the common schema-2 envelope returned by MACRO_STATUS and
// emitted for lifecycle changes. Free bytes are derived from Fill.
type MacroStatus struct {
	Schema         byte   `json:"schema"`
	State          byte   `json:"state"`
	ID             byte   `json:"id"`
	AcceptedSteps  uint16 `json:"accepted_steps"`
	ExecutedSteps  uint16 `json:"executed_steps"`
	AcceptedBytes  uint16 `json:"accepted_bytes"`
	Fill           byte   `json:"fill"`
	Underruns      byte   `json:"underruns"`
	DispatchErrors byte   `json:"dispatch_errors"`
	StartedAtUS    uint32 `json:"started_at_us"`
	TotalSteps     uint16 `json:"total_steps"`
	// DroppedSteps is retained in the durable JSON shape so recovery markers
	// written by the former schema-3 recorder remain readable. The schema-2
	// UART report has no such field, so freshly parsed reports leave it zero.
	DroppedSteps uint16 `json:"dropped_steps,omitempty"`
}

func (status MacroStatus) Free() byte {
	if int(status.Fill) >= MacroQueueCapacity {
		return 0
	}
	return byte(MacroQueueCapacity - int(status.Fill))
}

func (status MacroStatus) Active() bool {
	return status.State == MacroBuffering || status.State == MacroPlaying
}

func (status MacroStatus) Faithful() bool {
	return status.State == MacroCompleted && status.Underruns == 0 &&
		status.DispatchErrors == 0 && status.DroppedSteps == 0
}

// ParseMacroStatus accepts the shared [EventMacro, report] envelope. The event
// type may carry the protocol's high-bit timestamp marker; callers strip the
// trailing timestamp before parsing the report body.
func ParseMacroStatus(payload []byte) (MacroStatus, error) {
	const size = 19
	if len(payload) != size {
		return MacroStatus{}, fmt.Errorf("MACRO_STATUS payload is %d bytes, need %d", len(payload), size)
	}
	if payload[0]&0x7F != EventMacro || payload[1] != MacroQueueSchema {
		return MacroStatus{}, fmt.Errorf("unsupported MACRO_STATUS envelope %d/schema %d", payload[0], payload[1])
	}
	return MacroStatus{
		Schema: payload[1], State: payload[2], ID: payload[3],
		AcceptedSteps: binary.LittleEndian.Uint16(payload[4:6]),
		ExecutedSteps: binary.LittleEndian.Uint16(payload[6:8]),
		AcceptedBytes: binary.LittleEndian.Uint16(payload[8:10]),
		Fill:          payload[10], Underruns: payload[11], DispatchErrors: payload[12],
		StartedAtUS: binary.LittleEndian.Uint32(payload[13:17]),
		TotalSteps:  binary.LittleEndian.Uint16(payload[17:19]),
	}, nil
}

// MacroQueueStartPayload begins a host-streamed macro. Exact timing tolerance
// is host-owned and checked against timestamped 0xFE execution evidence.
func MacroQueueStartPayload(id byte, totalSteps uint16, keepOnCancel bool) ([]byte, error) {
	if totalSteps == 0 {
		return nil, fmt.Errorf("macro must contain at least one step")
	}
	flags := byte(0)
	if keepOnCancel {
		flags = 1
	}
	return []byte{MacroQueueSchema, id, flags, byte(totalSteps), byte(totalSteps >> 8)}, nil
}

func MacroQueueAppendPayload(offset, completeSteps uint16, fragment []byte) ([]byte, error) {
	if len(fragment) == 0 || len(fragment) > MacroMaximumFragment {
		return nil, fmt.Errorf("macro fragment is %d bytes, need 1..%d", len(fragment), MacroMaximumFragment)
	}
	payload := make([]byte, MacroAppendHeaderSize+len(fragment))
	payload[0] = 0
	binary.LittleEndian.PutUint16(payload[1:3], offset)
	binary.LittleEndian.PutUint16(payload[3:5], completeSteps)
	copy(payload[5:], fragment)
	return payload, nil
}

func MacroQueueRunPayload() []byte   { return []byte{1} }
func MacroQueueQueryPayload() []byte { return []byte{2} }

func MacroQueueCancelPayload(keepOutputs bool) []byte {
	if keepOutputs {
		return []byte{1}
	}
	// An explicit zero overrides any BEGIN keep-on-cancel preference. This is
	// the normal user/API cancel path and guarantees the safe-stop default.
	return []byte{0}
}

// EncodeMacroRecord stores one ordinary opcode command against an MCU-clock
// due offset. Macro-control recursion is rejected before it reaches firmware.
func EncodeMacroRecord(dueUS uint32, opcode byte, payload []byte) ([]byte, error) {
	if opcode >= OpMacroStart && opcode <= OpMacroStep {
		return nil, fmt.Errorf("macro control opcode 0x%02X cannot be queued recursively", opcode)
	}
	if len(payload) > MaxPayload {
		return nil, ErrPayloadTooLong
	}
	record := make([]byte, MacroRecordHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(record[0:4], dueUS)
	record[4] = opcode
	record[5] = byte(len(payload))
	copy(record[6:], payload)
	return record, nil
}

// ResponseDeviceMicros extracts the schema-2 MCU timestamp appended to ACKs
// and reserved-sequence execution errors.
func ResponseDeviceMicros(frame Frame) (uint32, bool) {
	if frame.Opcode == OpACK && len(frame.Payload) >= 6 {
		return binary.LittleEndian.Uint32(frame.Payload[len(frame.Payload)-4:]), true
	}
	if frame.Opcode == OpError && frame.Seq == MacroExecutionSequence && len(frame.Payload) >= 6 {
		return binary.LittleEndian.Uint32(frame.Payload[len(frame.Payload)-4:]), true
	}
	return 0, false
}
