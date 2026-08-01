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
	}
	status, err := ParseMacroStatus(payload)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != 9 || status.AcceptedSteps != 7 ||
		status.ExecutedSteps != 5 || status.AcceptedBytes != 64 ||
		status.Fill != 42 || status.Free() != 85 ||
		status.Underruns != 1 || status.DispatchErrors != 2 ||
		status.StartedAtUS != 0x12345678 || status.TotalSteps != 10 {
		t.Fatalf("unexpected status: %#v", status)
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
