package control

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

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
	if _, err := runner.StartRecording("lift", "motion", "purple"); err != nil {
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
	if macro.Steps[0].AtUS != 0 || macro.Steps[1].AtUS != 500 {
		t.Fatalf("MCU wrap delta was not preserved: %#v", macro.Steps)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != "lift" {
		t.Fatalf("recording was not persisted: %#v", config.Macros)
	}
}

func TestMacroExplicitSafeCancelOverridesBeginKeepPreference(t *testing.T) {
	if payload := native.MacroQueueCancelPayload(false); len(payload) != 1 || payload[0] != 0 {
		t.Fatalf("safe cancel must be explicit zero, got %v", payload)
	}
}

func TestMacroMetadataAndMonitorCommandsUseTheSharedRunner(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "old-name", Category: "Test", Color: "violet",
		Steps: []appconfig.MacroStep{{Kind: "relay", Target: 5, Value: 1}},
	}}
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
	ctx := context.Background()
	if output, err := macroCommand(ctx, runner, []string{"rename", "7", "new-name"}); err != nil || !strings.Contains(output, "new-name") {
		t.Fatalf("rename output=%q err=%v", output, err)
	}
	if output, err := macroCommand(ctx, runner, []string{"category", "new-name", "Diagnostics"}); err != nil || !strings.Contains(output, "Diagnostics") {
		t.Fatalf("category output=%q err=%v", output, err)
	}
	if got := config.Macros[0]; got.Name != "new-name" || got.Category != "Diagnostics" {
		t.Fatalf("metadata did not persist: %#v", got)
	}
	if output, err := macroCommand(ctx, runner, []string{"monitor"}); err != nil || !strings.Contains(output, "playback=") || !strings.Contains(output, "recording=") {
		t.Fatalf("monitor output=%q err=%v", output, err)
	}
	if _, err := macroCommand(ctx, runner, []string{"rename", "7", ""}); err == nil {
		t.Fatal("empty rename was accepted")
	}
}
