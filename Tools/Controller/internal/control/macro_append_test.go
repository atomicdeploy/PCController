package control

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestAppendRecordingPreservesIdentityAndExistingSequence(t *testing.T) {
	config := appconfig.Defaults()
	prefix := []appconfig.MacroStep{{Kind: "relay", Target: 7, Value: 1, DurationMS: 1000, ActionIDs: []string{"relay.8.on"}}}
	seed := appconfig.Macro{ID: 8, Name: "existing", Category: "Cinema", Icon: "seat", Color: "blue", Mode: "host", TimingToleranceUS: 1234, KeepOutputsOnCancel: true, Steps: prefix}
	config.Macros = []appconfig.Macro{seed}
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartAppendingRecording(context.Background(), "effect:8", "automatic"); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{2, 1, 0}, ObservedAt: base})
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{2, 2, 0}, ObservedAt: base.Add(250 * time.Millisecond)})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 1 || len(macro.Steps) != 3 || !reflect.DeepEqual(macro.Steps[:1], prefix) {
		t.Fatalf("lost prefix or duplicated identity: %#v", config.Macros)
	}
	if macro.Steps[1].AtUS != 1_000_000 || macro.Steps[2].AtUS != 1_250_000 {
		t.Fatalf("wrong appended offsets: %#v", macro.Steps)
	}
	withoutSteps := macro
	withoutSteps.Steps = prefix
	if !reflect.DeepEqual(withoutSteps, seed) {
		t.Fatalf("capture changed metadata or playback policy: %#v", macro)
	}
}

func TestAppendRecordingDiscardAndClearedSequence(t *testing.T) {
	config := appconfig.Defaults()
	seed := appconfig.Macro{ID: 3, Name: "take", Color: "green", Mode: "auto", Steps: []appconfig.MacroStep{{AtUS: 500000, Kind: "pwm", Target: 1, Value: 32}}}
	config.Macros = []appconfig.Macro{seed}
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartAppendingRecording(context.Background(), "3", "automatic"); err != nil {
		t.Fatal(err)
	}
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{1, 64, 0}, ObservedAt: time.Now()})
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Macros[0], seed) {
		t.Fatal("discard overwrote original effect")
	}
	// Clearing in the existing editor is the replacement workflow; no new ID.
	config.Macros[0].Steps = nil
	if _, err := runner.StartAppendingRecording(context.Background(), "effect:3", "device-clock"); err != nil {
		t.Fatal(err)
	}
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{1, 64, 0}, Timed: true, DeviceMicros: 0xFFFFFF00})
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{1, 0, 0}, Timed: true, DeviceMicros: 0x000000F4})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.ID != 3 || macro.Mode != "auto" || len(macro.Steps) != 2 || macro.Steps[0].AtUS != 0 || macro.Steps[1].AtUS != 500 {
		t.Fatalf("cleared sequence did not start fresh: %#v", macro)
	}
}

func TestAppendRecordingDoesNotOverwriteConcurrentEditOrDeletion(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		config := appconfig.Defaults()
		config.Macros = []appconfig.Macro{{ID: 3, Name: "take", Color: "green", Mode: "auto", Steps: []appconfig.MacroStep{{Kind: "pwm", Target: 1, Value: 32}}}}
		runner := macroTestRunner(&config, nil)
		if _, err := runner.StartAppendingRecording(context.Background(), "3", "automatic"); err != nil {
			t.Fatal(err)
		}
		runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{1, 64, 0}, ObservedAt: time.Now()})
		if deleted {
			config.Macros = nil
		} else {
			config.Macros[0].Name = "edited elsewhere"
		}
		if _, err := runner.StopRecording(true); err == nil || !strings.Contains(err.Error(), "retained") {
			t.Fatalf("conflict was not retained: %v", err)
		}
		if !runner.RecordingState().Active || len(runner.RecordingState().Preview) != 2 {
			t.Fatal("failed save lost take")
		}
		if !deleted && config.Macros[0].Name != "edited elsewhere" {
			t.Fatal("overwrote concurrent edit")
		}
		if _, err := runner.StopRecording(false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendRecordingUsesFullAuthoredEnd(t *testing.T) {
	for _, kind := range []string{"relay", "pwm", "rgb"} {
		steps := []appconfig.MacroStep{{AtUS: 100000, Kind: kind, Target: 1, Value: 1, DurationMS: 100, RepeatCount: 3, RepeatIntervalMS: 250}}
		end, err := recordingSequenceEnd(steps)
		if err != nil || end != 700000 {
			t.Fatalf("%s end=%d err=%v", kind, end, err)
		}
	}
	if _, err := recordingSequenceEnd([]appconfig.MacroStep{{AtUS: 0x7fffffff, Kind: "pwm", Target: 1, DurationMS: 1}}); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestAppendRecordingBoardRetainedDownloadPreservesPrefix(t *testing.T) {
	config := appconfig.Defaults()
	prefix := []appconfig.MacroStep{{AtUS: 1000000, Kind: "pwm", Target: 1, Value: 32}}
	config.Macros = []appconfig.Macro{{ID: 3, Name: "take", Color: "green", Mode: "auto", Steps: prefix}}
	runner := macroTestRunner(&config, nil)
	// A scripted native transport only; no serial or LAN discovery is opened.
	runner.runtime.session = &link.Session{}
	runner.runtime.connectionState = "connected"
	runner.requestGeneration = func(_ context.Context, _ uint64, opcode byte, payload []byte, _ byte) (native.Frame, error) {
		if opcode == native.OpMacroStep && len(payload) > 0 && payload[0] == native.MacroQueueQueryPayload()[0] {
			return macroPlaybackStatusFrame(native.MacroStatus{State: native.MacroRecorded, TotalSteps: 2}), nil
		}
		if opcode == native.OpMacroStep && len(payload) > 0 && payload[0] == 5 {
			return native.Frame{Opcode: native.OpMacroStatus, Payload: []byte{native.EventMacro, 0x80, 0, 0, 0, 0, 0, 0, 128, 244, 1, 0, 0, 0}}, nil
		}
		return native.Frame{Opcode: native.OpACK}, nil
	}
	if _, err := runner.StartAppendingRecording(context.Background(), "effect:3", "board-retained"); err != nil {
		t.Fatal(err)
	}
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(macro.Steps) != 3 || !reflect.DeepEqual(macro.Steps[:1], prefix) || macro.Steps[1].AtUS != 1000000 || macro.Steps[2].AtUS != 1000500 {
		t.Fatalf("download replaced prefix or used wrong offsets: %#v", macro)
	}
	runner.runtime.session = nil
}
