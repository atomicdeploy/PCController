package native

//go:generate go run ./cmd/genmacrocontract -protocol ../../../../Project/ProtocolContract.h -actions ../../../../Project/MacroActions.inc.h -output macro_contract_generated.go

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	MacroQueueSchema         byte = 3
	MacroExecutionSequence   byte = 0xFE
	MacroQueueCapacity            = 127
	MacroAppendHeaderSize         = 5
	MacroRecordHeaderSize         = 6
	MacroMaximumFragment          = MaxPayload - MacroAppendHeaderSize
	MacroCaptureChunkMaximum      = 40
	// EventAction is intentionally smaller than an ordinary host command. It
	// covers every board-generated physical/RF action without consuming a
	// second MaximumPayload-sized AVR buffer.
	MacroBoardActionMaximumPayload = 8
	MacroCaptureInputsFlag         = 1 << 1
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

// MacroStatus is the common schema-3 envelope returned by MACRO_STATUS and
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

// MacroCaptureStartPayload asks schema-3 firmware to record accepted ordinary
// board/host actions into its retained circular buffer.
func MacroCaptureStartPayload(id byte) []byte {
	return []byte{MacroQueueSchema, id, MacroCaptureInputsFlag, 0, 0}
}

// MacroCaptureStopPayload seals a recording. Selector 5 with one byte is
// distinct from identity-guarded CLEAR_CAPTURE [5,id,startedAtUS LE32].
func MacroCaptureStopPayload() []byte { return []byte{5} }

// MacroCaptureQueryPayload requests the retained schema-3 board-capture ring
// from an absolute byte offset. It does not alter capture/playback state.
func MacroCaptureQueryPayload(id byte, offset uint16) []byte {
	return []byte{3, id, byte(offset), byte(offset >> 8)}
}

// MacroCaptureAcknowledgePayload marks one identity-guarded retained capture
// durably exported. Firmware keeps the ring replayable/fetchable; only the
// explicit user-only command 5 may clear it.
func MacroCaptureAcknowledgePayload(id byte, startedAtUS uint32) []byte {
	return []byte{
		4, id,
		byte(startedAtUS), byte(startedAtUS >> 8),
		byte(startedAtUS >> 16), byte(startedAtUS >> 24),
	}
}

// MacroCaptureClearPayload explicitly destroys one retained capture only when
// both its reusable uint8 ID and MCU-start epoch still match.
func MacroCaptureClearPayload(id byte, startedAtUS uint32) []byte {
	return []byte{
		5, id,
		byte(startedAtUS), byte(startedAtUS >> 8),
		byte(startedAtUS >> 16), byte(startedAtUS >> 24),
	}
}

// MacroCaptureChunk is a bounded recovery page from the board-owned capture
// ring. Data contains whole/partial stream bytes; callers concatenate pages
// and validate complete records only after TotalBytes have arrived.
type MacroCaptureChunk struct {
	Schema     byte   `json:"schema"`
	Command    byte   `json:"command"`
	ID         byte   `json:"id"`
	TotalBytes uint16 `json:"total_bytes"`
	Offset     uint16 `json:"offset"`
	Data       []byte `json:"data"`
}

func ParseMacroCaptureChunk(payload []byte) (MacroCaptureChunk, error) {
	const header = 8
	if len(payload) < header || payload[0] != MacroQueueSchema || payload[1] != 3 {
		return MacroCaptureChunk{}, fmt.Errorf("invalid schema-3 macro capture chunk")
	}
	length := int(payload[7])
	if length > MaxPayload-header || len(payload) != header+length {
		return MacroCaptureChunk{}, fmt.Errorf(
			"macro capture chunk length %d/body %d is invalid", length, len(payload),
		)
	}
	chunk := MacroCaptureChunk{
		Schema: payload[0], Command: payload[1], ID: payload[2],
		TotalBytes: binary.LittleEndian.Uint16(payload[3:5]),
		Offset:     binary.LittleEndian.Uint16(payload[5:7]),
		Data:       append([]byte(nil), payload[8:]...),
	}
	if chunk.TotalBytes > MacroQueueCapacity || len(chunk.Data) > MacroCaptureChunkMaximum {
		return MacroCaptureChunk{}, fmt.Errorf(
			"macro capture exceeds ring/page bounds: total=%d page=%d",
			chunk.TotalBytes, len(chunk.Data),
		)
	}
	if chunk.Offset > chunk.TotalBytes ||
		int(chunk.Offset)+len(chunk.Data) > int(chunk.TotalBytes) {
		return MacroCaptureChunk{}, fmt.Errorf(
			"macro capture range %d+%d exceeds %d", chunk.Offset, len(chunk.Data), chunk.TotalBytes,
		)
	}
	if chunk.Offset < chunk.TotalBytes && len(chunk.Data) == 0 {
		return MacroCaptureChunk{}, errors.New("macro capture returned an empty non-terminal chunk")
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
	if !MacroPlaybackPayloadSemanticallyValid(opcode, payload) {
		return nil, fmt.Errorf("opcode 0x%02X payload length %d violates the macro action contract", opcode, len(payload))
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

// MacroPlaybackPayloadValid mirrors MacroAction::validPlaybackPayload. Fixed
// board-action rows must match exactly; 0xFF rows remain variable-length host
// actions and are semantically checked by their ordinary command parser.
func MacroPlaybackPayloadValid(opcode byte, payload []byte) bool {
	contract, exists := macroActionContracts[opcode]
	if !exists || len(payload) > MaxPayload {
		return false
	}
	return contract.captureLength == 0xFF || len(payload) == int(contract.captureLength)
}

// MacroPlaybackPayloadSemanticallyValid applies the ordinary command shape to
// variable actions as well. Host ACK recording may retain these larger
// payloads, while the board's compact Action evidence remains fixed and <=8.
func MacroPlaybackPayloadSemanticallyValid(opcode byte, payload []byte) bool {
	if !MacroPlaybackPayloadValid(opcode, payload) {
		return false
	}
	switch opcode {
	case OpBuzzer:
		frequency := binary.LittleEndian.Uint16(payload[0:2])
		duration := binary.LittleEndian.Uint16(payload[2:4])
		return (frequency == 0 && duration == 0) ||
			(frequency >= 20 && frequency <= 20000 && duration != 0)
	case OpStatusEffect:
		if len(payload) == 1 {
			return payload[0] == StatusEffectNone
		}
		if len(payload) != 12 || payload[0] < StatusEffectBreathe ||
			payload[0] > StatusEffectTransition || payload[8] > payload[7] {
			return false
		}
		period := binary.LittleEndian.Uint16(payload[9:11])
		return period >= StatusEffectMinimumPeriodMS && period <= StatusEffectMaximumPeriodMS
	case OpDisplayText:
		if len(payload) < 4 || payload[0] > 5 || payload[3] > 40 {
			return false
		}
		textLength := int(payload[3])
		if payload[0] == 5 {
			return len(payload) == 8+textLength &&
				payload[4]&0x7C == 0 && payload[4]&0x03 <= 2 &&
				(payload[4]&0x03 != 2 || payload[7] != 0)
		}
		if len(payload) != 4+textLength ||
			(payload[0] == 3 && (textLength < 4 || textLength > 36)) ||
			(payload[0] == 4 && textLength != 0) {
			return false
		}
	}
	return true
}

type macroActionContract struct {
	captureLength byte
}

// MacroPlaybackAllowed is generated from Project/MacroActions.inc.h and is the
// exact safe ordinary-action allowlist enforced by the production AVR queue.
func MacroPlaybackAllowed(opcode byte) bool {
	_, exists := macroActionContracts[opcode]
	return exists
}

// MacroBoardActionPayloadLength returns the exact fixed evidence prefix. A
// 0xFF canonical row is playback-only/variable and cannot enter board capture.
func MacroBoardActionPayloadLength(opcode byte) (byte, bool) {
	contract, exists := macroActionContracts[opcode]
	if !exists || contract.captureLength == 0xFF ||
		contract.captureLength > MacroBoardActionMaximumPayload {
		return 0, false
	}
	return contract.captureLength, true
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
