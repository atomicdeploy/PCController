package control

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
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

func macroTestRunner(config *appconfig.Config, saveError *error) *MacroRunner {
	runtime := New(Options{})
	runner := NewMacroRunner(runtime, func() []appconfig.Macro { return config.Macros }, func() appconfig.Config { return *config }, func(change func(*appconfig.Config) error) error {
		if saveError != nil && *saveError != nil {
			return *saveError
		}
		candidate := *config
		candidate.Macros = append([]appconfig.Macro(nil), config.Macros...)
		if err := change(&candidate); err != nil {
			return err
		}
		if err := candidate.Validate(); err != nil {
			return err
		}
		*config = candidate
		return nil
	})
	runtime.setMacroRunner(runner)
	return runner
}

func TestHostRecordingMixedOutputsRoundTripsNamedPlayback(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("mixed prototype", "tests", "green"); err != nil {
		t.Fatal(err)
	}
	text, _ := native.DisplayTextPayload(native.DisplaySegments, 100, "TEST")
	scheduled, _ := native.ScheduledSegmentPayload(native.ScheduledSegmentOptions{SpeedMS: 220, HoldMS: 500, ForceScroll: true}, "SCROLL")
	inputs := []CommandEvidence{
		{Opcode: native.OpRelaySet, Payload: []byte{5, 1}},
		{Opcode: native.OpPWMSet, Payload: []byte{2, 0, 8}},
		{Opcode: native.OpBuzzer, Payload: native.BuzzerPayload(880, 25)},
		{Opcode: native.OpDisplayText, Payload: text},
		{Opcode: native.OpDisplayText, Payload: scheduled},
		{Opcode: native.OpRelayAllOff},
		{Opcode: native.OpPWMAllOff},
	}
	base := time.Now()
	for index := range inputs {
		inputs[index].ObservedAt = base.Add(time.Duration(index) * time.Millisecond)
		runner.runtime.publishCommandEvidence(inputs[index])
	}
	if got := runner.Snapshot().Recording.Steps; got != len(inputs) {
		t.Fatalf("live snapshot steps=%d", got)
	}
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.Steps[2].Kind != "beep" {
		t.Fatalf("tone step=%#v", macro.Steps[2])
	}
	if _, err := macroCommand(context.Background(), runner, []string{"rename", "mixed prototype", "renamed take"}); err != nil {
		t.Fatal(err)
	}
	if _, err := macroCommand(context.Background(), runner, []string{"category", "renamed take", "cinema"}); err != nil {
		t.Fatal(err)
	}
	if config.Macros[0].Name != "renamed take" || config.Macros[0].Category != "cinema" {
		t.Fatal(config.Macros)
	}
	if text, err := macroCommand(context.Background(), runner, []string{"monitor"}); err != nil || !strings.Contains(text, "steps=7") {
		t.Fatalf("monitor=%s err=%v", text, err)
	}
	compiled, err := compileMacro(config.Macros[0])
	if err != nil {
		t.Fatal(err)
	}
	var actual []CommandEvidence
	count, err := runHostMacro(context.Background(), compiled, func(_ context.Context, opcode byte, payload []byte) error {
		actual = append(actual, CommandEvidence{Opcode: opcode, Payload: append([]byte(nil), payload...)})
		return nil
	}, func(int, int32, bool) {})
	if err != nil || count != len(inputs) {
		t.Fatalf("count=%d err=%v", count, err)
	}
	for index := range inputs {
		if actual[index].Opcode != inputs[index].Opcode || !reflect.DeepEqual(actual[index].Payload, inputs[index].Payload) {
			t.Fatalf("step %d replay mismatch", index)
		}
		if macro.Steps[index].AtUS != uint32(index*1000) {
			t.Fatalf("step %d lost delta", index)
		}
	}
}

func TestMacroSaveFailureAndEmptySaveRetainTake(t *testing.T) {
	config := appconfig.Defaults()
	var saveErr error
	runner := macroTestRunner(&config, &saveErr)
	if _, err := runner.StartRecording("retained", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.StopRecording(true); err == nil || !runner.RecordingState().Active {
		t.Fatal("empty save discarded active take")
	}
	runner.runtime.publishCommandEvidence(CommandEvidence{Opcode: native.OpRelayAllOff})
	saveErr = errors.New("disk unavailable")
	if _, err := runner.StopRecording(true); err == nil || !runner.RecordingState().Active {
		t.Fatal("failed save discarded take")
	}
	saveErr = nil
	if macro, err := runner.StopRecording(true); err != nil || len(macro.Steps) != 1 {
		t.Fatalf("retry=%#v err=%v", macro, err)
	}
	if runner.RecordingState().Active || runner.RecordingState().LastError != "" {
		t.Fatal(runner.RecordingState())
	}
}

func TestHostPlaybackDoesNotDispatchAfterCancellation(t *testing.T) {
	compiled, err := compileMacro(appconfig.Macro{Mode: "host", Steps: []appconfig.MacroStep{{Kind: "relays-off"}, {Kind: "pwm-off"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	count, err := runHostMacro(ctx, compiled, func(context.Context, byte, []byte) error { t.Fatal("cancelled playback dispatched"); return nil }, func(int, int32, bool) {})
	if count != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestHostCancelCleanupFailureRemainsVisible(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	done := make(chan struct{})
	runner.done = done
	runner.state = MacroState{Running: true, Name: "test", Mode: "host"}
	runner.finishHostPlayback(done, appconfig.Macro{}, 0, true, errors.New("safe-stop failed"))
	state := runner.State()
	if state.Lifecycle != "failed" || !strings.Contains(state.LastError, "safe-stop failed") {
		t.Fatal(state)
	}
}

func TestMacroRecordingCannotCapturePlayback(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	runner.state.Running = true
	if _, err := runner.StartRecording("recursive", "", ""); err == nil {
		t.Fatal("started recording playback")
	}
}

func TestRawMacroMotionRequiresSamePermission(t *testing.T) {
	for _, step := range []appconfig.MacroStep{
		{Kind: "opcode", Opcode: native.OpRelaySet, PayloadHex: "0001"},
		{Kind: "raw", Opcode: native.OpRelaySide, PayloadHex: "0102"},
	} {
		if !macroNeedsMotionPermission(appconfig.Macro{Steps: []appconfig.MacroStep{step}}) {
			t.Fatalf("raw safety bypass: %#v", step)
		}
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
