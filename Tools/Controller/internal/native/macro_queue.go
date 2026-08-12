package native

import (
	"encoding/binary"
	"fmt"
)

const (
	MacroQueueSchema       byte = 3
	MacroExecutionSequence byte = 0xFE
	MacroQueueCapacity          = 127
	MacroAppendHeaderSize       = 5
	MacroRecordHeaderSize       = 6
	MacroMaximumFragment        = MaxPayload - MacroAppendHeaderSize
	// Board capture is retained in the MCU's bounded macro ring. The declared
	// capture total must never claim more storage than the board owns.
	MacroCaptureMaximumBytes    = MacroQueueCapacity
	MacroCaptureFetchHeaderSize = 7
	MacroCaptureFetchMaximum    = MaxPayload - MacroCaptureFetchHeaderSize
	// EventAction is intentionally smaller than an ordinary host command. It
	// covers every board-generated physical/RF action without consuming a
	// second MaximumPayload-sized AVR buffer.
	MacroBoardActionMaximumPayload = 8
)

const (
	MacroIdle byte = iota
	MacroBuffering
	MacroPlaying
	MacroCompleted
	MacroCancelled
	MacroFailed
	MacroRecording
	MacroCaptured
	MacroExported
)

const (
	MacroOptionKeepOutputsOnCancel byte = 1 << iota
	MacroOptionCaptureInputs
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
	DroppedSteps   uint16 `json:"dropped_steps"`
}

func (status MacroStatus) Free() byte {
	if int(status.Fill) >= MacroQueueCapacity {
		return 0
	}
	return byte(MacroQueueCapacity - int(status.Fill))
}

func (status MacroStatus) Active() bool {
	return status.State == MacroBuffering || status.State == MacroPlaying ||
		status.State == MacroRecording
}

func (status MacroStatus) Faithful() bool {
	return status.State == MacroCompleted && status.Underruns == 0 &&
		status.DispatchErrors == 0 && status.DroppedSteps == 0
}

// ParseMacroStatus accepts the shared [EventMacro, report] envelope. The event
// type may carry the protocol's high-bit timestamp marker; callers strip the
// trailing timestamp before parsing the report body.
func ParseMacroStatus(payload []byte) (MacroStatus, error) {
	const size = 21
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
		StartedAtUS:  binary.LittleEndian.Uint32(payload[13:17]),
		TotalSteps:   binary.LittleEndian.Uint16(payload[17:19]),
		DroppedSteps: binary.LittleEndian.Uint16(payload[19:21]),
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
		flags = MacroOptionKeepOutputsOnCancel
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

// MacroCaptureStartPayload tells schema-3 firmware to capture board-origin
// actions into its fixed in-memory ring. It deliberately advertises only the
// ring capacity; a host must not imply that an AVR retained an unbounded trace.
func MacroCaptureStartPayload(id byte) []byte {
	return []byte{MacroQueueSchema, id, MacroOptionCaptureInputs, MacroCaptureMaximumBytes, 0}
}

// MacroCaptureFetchPayload requests a bounded page from the retained
// schema-3 board-capture ring. Selector 4 is fetch; selector 3 is clear.
func MacroCaptureFetchPayload(offset uint16, maximum byte) ([]byte, error) {
	if offset > MacroCaptureMaximumBytes {
		return nil, fmt.Errorf("macro capture offset %d exceeds %d-byte ring", offset, MacroCaptureMaximumBytes)
	}
	if maximum == 0 || int(maximum) > MacroCaptureFetchMaximum {
		return nil, fmt.Errorf("macro capture fetch maximum must be 1..%d", MacroCaptureFetchMaximum)
	}
	return []byte{4, byte(offset), byte(offset >> 8), maximum}, nil
}

// MacroCaptureChunk is a bounded recovery page from the board-owned capture
// ring. Data may end in a partial stream record; callers validate only after
// the status-declared retained-byte count has been recovered.
type MacroCaptureChunk struct {
	Schema byte   `json:"schema"`
	State  byte   `json:"state"`
	ID     byte   `json:"id"`
	Offset uint16 `json:"offset"`
	Data   []byte `json:"data"`
}

func ParseMacroCaptureChunk(payload []byte) (MacroCaptureChunk, error) {
	const header = MacroCaptureFetchHeaderSize
	if len(payload) < header || payload[0] != EventMacro ||
		payload[1] != MacroQueueSchema || payload[2] != MacroExported {
		return MacroCaptureChunk{}, fmt.Errorf("invalid schema-3 macro capture export")
	}
	length := int(payload[6])
	if length > MaxPayload-header || len(payload) != header+length {
		return MacroCaptureChunk{}, fmt.Errorf(
			"macro capture chunk length %d/body %d is invalid", length, len(payload),
		)
	}
	chunk := MacroCaptureChunk{
		Schema: payload[1], State: payload[2], ID: payload[3],
		Offset: binary.LittleEndian.Uint16(payload[4:6]),
		Data:   append([]byte(nil), payload[7:]...),
	}
	if chunk.Offset > MacroCaptureMaximumBytes ||
		int(chunk.Offset)+len(chunk.Data) > MacroCaptureMaximumBytes {
		return MacroCaptureChunk{}, fmt.Errorf(
			"macro capture range %d+%d exceeds %d-byte ring", chunk.Offset, len(chunk.Data), MacroCaptureMaximumBytes,
		)
	}
	return chunk, nil
}

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
	if !MacroQueueableOpcode(opcode) {
		return nil, fmt.Errorf("opcode 0x%02X is not a queueable acknowledged command", opcode)
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

// MacroQueueableOpcode is the one host-side policy shared by persisted macro
// validation, board-origin action-event parsing, and stream compilation. The
// firmware dispatcher remains the final peripheral/safety authority.
func MacroQueueableOpcode(opcode byte) bool {
	switch opcode {
	case OpSetStream, OpSetSettings,
		OpBuzzer, OpPWMSet, OpPWMAllOff,
		OpStatusRGB, OpStatusEffect, OpStatusProfileSet,
		OpAddressableLED, OpRFTx,
		OpRFLearnStart, OpRFLearnCancel, OpRFLearnClear,
		OpRFLearnRemove, OpRFLearnReplace,
		OpMenuAction, OpRelaySet, OpRelaySide,
		OpRelayAllOff, OpRelayTest, OpMenuSetPage,
		OpDisplayText, OpRemoteKeyGesture:
		return true
	default:
		return false
	}
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
