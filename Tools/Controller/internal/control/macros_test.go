package control

import (
	"encoding/binary"
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
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySet, Payload: []byte{5, 1},
		Source:       native.InputSourceHost,
		DeviceMicros: 0xFFFFFF00, Timed: true,
	})
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{2, 0x00, 0x08},
		Source:       native.InputSourceHost,
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

func TestMacroRecorderCombinesPanelAndRFWithoutDuplicatingHostEcho(t *testing.T) {
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
	if _, err := runner.StartRecording("mixed", "motion", "green"); err != nil {
		t.Fatal(err)
	}
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 1},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1000, Timed: true,
	})
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{4, 0xFF, 0x0F},
		Source: native.InputSourceRF, SourceID: 3, BoardOrigin: true,
		DeviceMicros: 2750, Timed: true,
	})
	// This is the board echo for a host ACK, not a third activation.
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{4, 0xFF, 0x0F},
		Source: native.InputSourceHost, BoardOrigin: true,
		DeviceMicros: 2750, Timed: true,
	})
	state := runner.RecordingState()
	if state.Steps != 2 || state.PanelSteps != 1 || state.RFSteps != 1 ||
		state.HostSteps != 0 {
		t.Fatalf("unexpected mixed-source recorder state: %#v", state)
	}
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(macro.Steps) != 2 || macro.Steps[0].AtUS != 0 ||
		macro.Steps[1].AtUS != 1750 || macro.Steps[0].Kind != "motion" ||
		macro.Steps[1].Kind != "pwm" {
		t.Fatalf("unexpected mixed-source macro: %#v", macro)
	}
}

func TestBoardRecordingStatusAutoStartsProvisionalHostMacro(t *testing.T) {
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
	runtime.publishMacroStatus(native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroRecording, ID: 7,
	})
	state := runner.RecordingState()
	if !state.Active || !state.BoardOwned || state.BoardID != 7 ||
		state.ID != 7 || state.Name != "Board capture 7" || state.Category != "board" {
		t.Fatalf("unexpected board recording state: %#v", state)
	}
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{1, 2},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 50, Timed: true,
	})
	if state = runner.RecordingState(); state.Steps != 1 || state.PanelSteps != 1 {
		t.Fatalf("board action was not captured: %#v", state)
	}
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureStreamDecodeMergeAndMetadataUpdate(t *testing.T) {
	first, err := native.EncodeMacroRecord(100, native.OpRelaySide, []byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := native.EncodeMacroRecord(250, native.OpPWMSet, []byte{2, 0x00, 0x08})
	if err != nil {
		t.Fatal(err)
	}
	steps, err := decodeMacroCaptureStream(append(first, second...))
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeRecordedMacroSteps(steps[:1], steps)
	if len(merged) != 2 || merged[0].AtUS != 100 || merged[1].AtUS != 250 ||
		merged[0].Kind != "motion" || merged[1].Kind != "pwm" {
		t.Fatalf("unexpected recovered steps: %#v", merged)
	}

	runtime := New(Options{})
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "Board capture 7", Category: "board", Color: "green",
		RecordingSource: "board", Steps: merged,
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
	updated, err := runner.UpdateMetadata("7", "Night lift", "motion", "purple")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Night lift" || updated.Category != "motion" ||
		updated.Color != "violet" || updated.RecordingSource != "board" ||
		len(updated.Steps) != 2 {
		t.Fatalf("metadata update changed capture data: %#v", updated)
	}
}

func TestMacroExplicitSafeCancelOverridesBeginKeepPreference(t *testing.T) {
	if payload := native.MacroQueueCancelPayload(false); len(payload) != 1 || payload[0] != 0 {
		t.Fatalf("safe cancel must be explicit zero, got %v", payload)
	}
}
