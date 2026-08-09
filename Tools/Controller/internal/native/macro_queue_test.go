package native

import (
	"encoding/binary"
	"testing"
)

func TestMacroStatusSchemaTwoRoundTripLayout(t *testing.T) {
	payload := []byte{
		EventMacro, MacroQueueSchema, MacroPlaying, 9,
		7, 0, 5, 0, 64, 0,
		42, 1, 2,
		0x78, 0x56, 0x34, 0x12,
		10, 0,
		3, 0,
	}
	status, err := ParseMacroStatus(payload)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != 9 || status.AcceptedSteps != 7 ||
		status.ExecutedSteps != 5 || status.AcceptedBytes != 64 ||
		status.Fill != 42 || status.Free() != 85 ||
		status.Underruns != 1 || status.DispatchErrors != 2 ||
		status.StartedAtUS != 0x12345678 || status.TotalSteps != 10 ||
		status.DroppedSteps != 3 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestTimedMacroStatusEventAcceptsTimestampMarker(t *testing.T) {
	payload := []byte{
		EventMacro | 0x80, MacroQueueSchema, MacroCompleted, 3,
		4, 0, 4, 0, 32, 0,
		0, 0, 0,
		0x10, 0x20, 0x30, 0x40,
		4, 0,
		0, 0,
	}
	// Timed events append the MCU clock after the 21-byte macro envelope.
	payload = append(payload, 0x78, 0x56, 0x34, 0x12)
	event, err := ParseDeviceEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !event.Timed || event.DeviceMicros != 0x12345678 || event.Macro == nil {
		t.Fatalf("unexpected timed macro event: %#v", event)
	}
	if event.Macro.State != MacroCompleted || event.Macro.ID != 3 ||
		event.Macro.ExecutedSteps != 4 || !event.Macro.Faithful() {
		t.Fatalf("unexpected macro status: %#v", event.Macro)
	}
}

func TestMacroExecutionTimestampUsesReservedSequence(t *testing.T) {
	payload := []byte{OpPWMSet, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(payload[2:], 0x89ABCDEF)
	value, ok := ResponseDeviceMicros(Frame{
		Opcode: OpACK, Seq: MacroExecutionSequence, Payload: payload,
	})
	if !ok || value != 0x89ABCDEF {
		t.Fatalf("timestamp got 0x%08X/%t", value, ok)
	}
}

func TestMacroFragmentsCarryAbsoluteStreamAndCompleteStepOffsets(t *testing.T) {
	payload, err := MacroQueueAppendPayload(123, 4, []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if payload[0] != 0 || binary.LittleEndian.Uint16(payload[1:3]) != 123 ||
		binary.LittleEndian.Uint16(payload[3:5]) != 4 ||
		len(payload) != 8 {
		t.Fatalf("unexpected APPEND payload: %v", payload)
	}
}

func TestBoardActionEventUsesOrdinaryMacroOpcodeContract(t *testing.T) {
	payload := []byte{
		EventAction | 0x80, InputSourcePhysical, OpRelaySide, 2, 1, 2,
		0x78, 0x56, 0x34, 0x12,
	}
	event, err := ParseDeviceEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != EventAction || !event.Timed ||
		event.DeviceMicros != 0x12345678 || event.Source != InputSourcePhysical ||
		event.ActionOpcode != OpRelaySide || len(event.ActionPayload) != 2 ||
		event.ActionPayload[0] != 1 || event.ActionPayload[1] != 2 {
		t.Fatalf("unexpected action event: %#v", event)
	}
}

func TestBoardActionEventRejectsControlAndOversizedPayload(t *testing.T) {
	for _, payload := range [][]byte{
		{EventAction, InputSourcePhysical, OpMacroStart, 0},
		{EventAction, InputSourceRF, OpRelaySet, MacroBoardActionMaximumPayload + 1,
			0, 0, 0, 0, 0, 0, 0, 0, 0},
	} {
		if _, err := ParseDeviceEvent(payload); err == nil {
			t.Fatalf("invalid action event accepted: % X", payload)
		}
	}
}

func TestMacroCaptureChunkUsesBoundedOffsetPages(t *testing.T) {
	payload := []byte{MacroQueueSchema, 3, 9, 5, 0, 2, 0, 3, 0xAA, 0xBB, 0xCC}
	chunk, err := ParseMacroCaptureChunk(payload)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.ID != 9 || chunk.TotalBytes != 5 || chunk.Offset != 2 ||
		len(chunk.Data) != 3 || chunk.Data[2] != 0xCC {
		t.Fatalf("unexpected capture chunk: %#v", chunk)
	}
	query := MacroCaptureQueryPayload(0x1234)
	if len(query) != 3 || query[0] != 3 || query[1] != 0x34 || query[2] != 0x12 {
		t.Fatalf("unexpected capture query: % X", query)
	}
}
