package control

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func TestCompileMacroEncodesOrdinaryOpcodesWithExactOffsets(t *testing.T) {
	compiled, err := compileMacro(appconfig.Macro{
		ID: 7, Name: "demo",
		Steps: []appconfig.MacroStep{
			{AtUS: 0, Kind: "relay", Target: 5, Value: 1},
			{AtUS: 1250, Kind: "pwm", Target: 2, Value: 2048},
			{AtUS: 2500, Kind: "buzzer", FrequencyHz: 880, DurationMS: 25},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.durationUS != 2500 || len(compiled.steps) != 3 {
		t.Fatalf("unexpected compile summary: %#v", compiled)
	}
	offset := 0
	wantOpcodes := []byte{native.OpRelaySet, native.OpPWMSet, native.OpBuzzer}
	wantDue := []uint32{0, 1250, 2500}
	for index := range wantOpcodes {
		due := binary.LittleEndian.Uint32(compiled.stream[offset : offset+4])
		opcode := compiled.stream[offset+4]
		length := int(compiled.stream[offset+5])
		if due != wantDue[index] || opcode != wantOpcodes[index] {
			t.Fatalf("record %d got due/opcode %d/0x%02X", index, due, opcode)
		}
		offset += native.MacroRecordHeaderSize + length
		if compiled.completeSteps(offset) != index+1 {
			t.Fatalf("completeSteps(%d) did not include record %d", offset, index)
		}
	}
	if offset != len(compiled.stream) {
		t.Fatalf("decoded %d of %d bytes", offset, len(compiled.stream))
	}
}

func TestMacroRecorderUsesWrappingMCUAcknowledgementDeltas(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	if _, err := runner.StartMCURecording("lift", "motion", "purple"); err != nil {
		t.Fatal(err)
	}
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpRelaySet, Payload: []byte{5, 1},
		DeviceMicros: 0xFFFFFF00, Timed: true,
	})
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{2, 0x00, 0x08},
		DeviceMicros: 0x000000F4, Timed: true,
	})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.Color != "violet" || len(macro.Steps) != 2 {
		t.Fatalf("unexpected recorded macro: %#v", macro)
	}
	if macro.Mode != macroModeMCU {
		t.Fatalf("expected explicit MCU mode, got %q", macro.Mode)
	}
	if macro.Steps[0].AtUS != 0 || macro.Steps[1].AtUS != 500 {
		t.Fatalf("MCU wrap delta was not preserved: %#v", macro.Steps)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != "lift" {
		t.Fatalf("recording was not persisted: %#v", config.Macros)
	}
}

func TestBasicHostRecorderIgnoresHousekeepingAndUsesObservedDeltas(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	state, err := runner.StartRecording("quick-motion", "prototype", "green")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != macroModeHost {
		t.Fatalf("default recorder mode = %q, want host", state.Mode)
	}
	base := time.Now()
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpStatusRGB, Payload: []byte{1, 2, 3, 4},
		DeviceMicros: 100, Timed: true, ObservedAt: base,
	})
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpSetStream, Payload: []byte{1, 0},
		DeviceMicros: 200, Timed: true, ObservedAt: base.Add(10 * time.Millisecond),
	})
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpRelaySet, Payload: []byte{5, 1},
		ObservedAt: base.Add(25 * time.Millisecond),
	})
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 0},
		ObservedAt: base.Add(100 * time.Millisecond),
	})
	runner.captureCommand(CommandEvidence{
		Opcode: native.OpRelayAllOff, ObservedAt: base.Add(175 * time.Millisecond),
	})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.Mode != macroModeHost || macro.TimingToleranceUS != defaultHostMacroToleranceUS {
		t.Fatalf("unexpected host mode metadata: %#v", macro)
	}
	if len(macro.Steps) != 3 {
		t.Fatalf("housekeeping was not filtered: %#v", macro.Steps)
	}
	want := []uint32{0, 75000, 150000}
	for index, step := range macro.Steps {
		if step.AtUS != want[index] {
			t.Fatalf("step %d offset = %d, want %d", index, step.AtUS, want[index])
		}
	}
}

func TestRunHostMacroSchedulesCompiledOrdinaryCommands(t *testing.T) {
	compiled, err := compileMacro(appconfig.Macro{
		ID: 4, Name: "quick", Mode: macroModeHost,
		Steps: []appconfig.MacroStep{
			{Kind: "relay", Target: 5, Value: 1},
			{AtUS: 2000, Kind: "relay", Target: 5, Value: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var opcodes []byte
	var payloads [][]byte
	var evidence int
	observed, err := runHostMacro(
		context.Background(),
		compiled,
		func(_ context.Context, opcode byte, payload []byte) error {
			opcodes = append(opcodes, opcode)
			payloads = append(payloads, append([]byte(nil), payload...))
			return nil
		},
		func(index int, _ int32, succeeded bool) {
			if index != evidence || !succeeded {
				t.Fatalf("unexpected evidence index=%d succeeded=%t", index, succeeded)
			}
			evidence++
		},
	)
	if err != nil || observed != 2 || evidence != 2 {
		t.Fatalf("host playback result observed=%d evidence=%d err=%v", observed, evidence, err)
	}
	if len(opcodes) != 2 || opcodes[0] != native.OpRelaySet || opcodes[1] != native.OpRelaySet {
		t.Fatalf("unexpected opcodes: %v", opcodes)
	}
	if len(payloads[0]) != 2 || payloads[0][0] != 5 || payloads[0][1] != 1 || payloads[1][1] != 0 {
		t.Fatalf("unexpected relay payloads: %v", payloads)
	}
}

func TestMacroExplicitSafeCancelOverridesBeginKeepPreference(t *testing.T) {
	if payload := native.MacroQueueCancelPayload(false); len(payload) != 1 || payload[0] != 0 {
		t.Fatalf("safe cancel must be explicit zero, got %v", payload)
	}
}
