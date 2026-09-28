package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"strings"
	"testing"
	"time"
)

func TestStripRetainedMacroErrorExplainsRecovery(t *testing.T) {
	original := &link.RemoteError{RequestOpcode: native.OpAddressableLED, Code: native.ErrorBusy}
	err := stripCommandError(original)
	if !errors.Is(err, original) || !strings.Contains(err.Error(), "macro buffer clear") {
		t.Fatalf("missing actionable recovery: %v", err)
	}
}

func TestStripActivityDoesNotClearOtherOutputLanes(t *testing.T) {
	runtime := New(Options{})
	runtime.setOutputActivity("melody", true)
	runtime.setOutputActivity("strip", true)
	runtime.setOutputActivity("strip", false)
	if runtime.activeUseMask.Load() != activeUseMelody {
		t.Fatalf("strip changed melody activity: %d", runtime.activeUseMask.Load())
	}
}

func (target *recordingOutputTarget) snapshotCommands() []recordedOutputCommand {
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]recordedOutputCommand(nil), target.commands...)
}

func TestStripFailureDoesNotCommitPartialFrame(t *testing.T) {
	target := &recordingOutputTarget{failAt: 2}
	outputs := NewOutputScheduler(target)
	defer outputs.Close()
	if err := outputs.sendStripFrame(context.Background(), stripRainbowFrame(100, 0)); err == nil {
		t.Fatal("expected failed chunk")
	}
	for _, command := range target.snapshotCommands() {
		if len(command.payload) == 1 && command.payload[0] == 0xFC {
			t.Fatal("committed partial frame")
		}
	}
}

func TestStripRainbowStopsWithoutMoreFrames(t *testing.T) {
	target := &recordingOutputTarget{}
	outputs := NewOutputScheduler(target)
	defer outputs.Close()
	if _, err := stripStreamCommand(context.Background(), outputs, []string{"rainbow", "100", "30"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshotCommands()) < 9 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := stripStreamCommand(context.Background(), outputs, []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	count := len(target.snapshotCommands())
	time.Sleep(50 * time.Millisecond)
	if len(target.snapshotCommands()) != count {
		t.Fatal("stream continued after stop")
	}
	if count < 9 {
		t.Fatal("rainbow never committed a frame")
	}
}
