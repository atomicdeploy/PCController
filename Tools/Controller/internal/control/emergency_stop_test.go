package control

import (
	"context"
	"errors"
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestEmergencyStopOpcodeGateAllowsOnlyReadbackAndReleaseOperations(t *testing.T) {
	tests := []struct {
		name    string
		opcode  byte
		payload []byte
		allowed bool
	}{
		{"relay on", native.OpRelaySet, []byte{0, 1}, false},
		{"relay off", native.OpRelaySet, []byte{0, 0}, true},
		{"motion up", native.OpRelaySide, []byte{0, 1}, false},
		{"motion stop", native.OpRelaySide, []byte{0, 0}, true},
		{"relay test start", native.OpRelayTest, []byte{100, 0}, false},
		{"relay test stop", native.OpRelayTest, []byte{0, 0}, true},
		{"all relays off", native.OpRelayAllOff, nil, true},
		{"PWM on", native.OpPWMSet, []byte{0, 1, 0}, false},
		{"PWM zero", native.OpPWMSet, []byte{0, 0, 0}, true},
		{"all PWM off", native.OpPWMAllOff, nil, true},
		{"effect begin", native.OpMacroStart, []byte{1, 0, 1, 0}, false},
		{"effect run", native.OpMacroStep, native.MacroQueueRunPayload(), false},
		{"effect status", native.OpMacroStep, native.MacroQueueQueryPayload(), true},
		{"effect cancel safe", native.OpMacroCancel, native.MacroQueueCancelPayload(false), true},
		{"effect cancel keep outputs", native.OpMacroCancel, native.MacroQueueCancelPayload(true), false},
		{"strip frame", native.OpAddressableLED, []byte{0, 1, 2, 3, 0xFF}, false},
		{"strip clear", native.OpAddressableLED, []byte{native.AddressableLEDFill, 0, 0, 0, 0xFF}, true},
		{"status effect", native.OpStatusEffect, []byte{native.StatusEffectBreathe}, false},
		{"status effect release", native.OpStatusEffect, native.StatusEffectReleasePayload(), true},
		{"program running", native.OpProgramState, []byte{native.ProgramStateRunning}, false},
		{"program idle", native.OpProgramState, []byte{native.ProgramStateIdle}, true},
		{"menu action", native.OpMenuAction, []byte{native.MenuIncrease}, false},
		{"remote gesture", native.OpRemoteKeyGesture, []byte{1, 1}, false},
		{"status readback", native.OpGetStatus, nil, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := emergencyStopAllows(test.opcode, test.payload); got != test.allowed {
				t.Fatalf("emergencyStopAllows(0x%02X, %v)=%t, want %t", test.opcode, test.payload, got, test.allowed)
			}
		})
	}
}

func TestEmergencyStopLatchSurvivesBoardFailureAndUnlockDoesNotResume(t *testing.T) {
	runtime := New(Options{})
	state, err := runtime.SetEmergencyStop(context.Background(), true, "test", "acceptance")
	if err == nil {
		t.Fatal("engaging against a disconnected board should report reassertion failure")
	}
	if !state.Active || !runtime.EmergencyStop().Active || state.Revision != 1 {
		t.Fatalf("engaged state=%+v snapshot=%+v", state, runtime.EmergencyStop())
	}
	if _, err := runtime.SetProgramState("test", ProgramRunning, "must remain blocked"); !errors.Is(err, ErrEmergencyStopActive) {
		t.Fatalf("running state while latched error=%v", err)
	}
	released, err := runtime.SetEmergencyStop(context.Background(), false, "test", "operator reset")
	if err != nil || released.Active || released.Revision != 2 {
		t.Fatalf("released state=%+v err=%v", released, err)
	}
	if runtime.ProgramState().Mode == ProgramRunning {
		t.Fatalf("unlock unexpectedly resumed program state: %+v", runtime.ProgramState())
	}
}

func TestEmergencyStopRejectsNewEffectRecordingBeforeMutation(t *testing.T) {
	runtime := New(Options{})
	runner := NewMacroRunner(runtime, nil, nil)
	runtime.emergencyStop.Store(true)
	if _, err := runner.StartRecording("blocked", "test", "red"); !errors.Is(err, ErrEmergencyStopActive) {
		t.Fatalf("StartRecording error=%v", err)
	}
	if runner.RecordingState().Active {
		t.Fatal("recording became active while E-STOP was latched")
	}
}
