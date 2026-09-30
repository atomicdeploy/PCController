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

func TestStripStagedProtocolErrorExplainsFirmwareDrift(t *testing.T) {
	original := &link.RemoteError{RequestOpcode: native.OpAddressableLED, Code: native.ErrorBadPayload}
	err := stripCommandError(original)
	if !errors.Is(err, original) || !strings.Contains(err.Error(), "update the board firmware") {
		t.Fatalf("missing firmware-drift recovery: %v", err)
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

func TestStripEffectCatalogAndAliases(t *testing.T) {
	for _, id := range []string{"police", "white-thunder", "thunder", "lightning", "converging-red", "converge", "red-dots"} {
		definition, renderer, ok := stripEffectByID(id)
		if !ok || definition.ID == "" || renderer == nil {
			t.Fatalf("missing effect %q", id)
		}
	}
	if _, _, ok := stripEffectByID("unknown"); ok {
		t.Fatal("unknown effect was accepted")
	}
}

func TestTypedStripEffectCatalogIsLiveCapabilityGated(t *testing.T) {
	if got := SupportedStripEffectDescriptors(false, native.CapabilityAddressableLED); len(got) != 0 {
		t.Fatalf("disconnected catalog=%+v", got)
	}
	if got := SupportedStripEffectDescriptors(true, 0); len(got) != 0 {
		t.Fatalf("unsupported catalog=%+v", got)
	}
	catalog := SupportedStripEffectDescriptors(true, native.CapabilityAddressableLED)
	if len(catalog) != 3 {
		t.Fatalf("catalog=%+v", catalog)
	}
	want := []string{"police", "white-thunder", "converging-red"}
	for index, descriptor := range catalog {
		if descriptor.ID != want[index] || descriptor.Name == "" || descriptor.Description == "" ||
			descriptor.DefaultFPS < descriptor.MinFPS || descriptor.DefaultFPS > descriptor.MaxFPS ||
			descriptor.MinPixels != 1 || descriptor.MaxPixels != native.StripMaximumPixels ||
			descriptor.MinFPS != 1 || descriptor.MaxFPS != 30 {
			t.Fatalf("descriptor[%d]=%+v", index, descriptor)
		}
	}
	catalog[0].Name = "mutated"
	if StripEffectDescriptors()[0].Name != "Police" {
		t.Fatal("catalog caller mutated the canonical descriptors")
	}
}

func TestStripPoliceFrameAlternatesRedAndBlue(t *testing.T) {
	first := stripPoliceFrame(4, 0)
	if first[0] != 255 || first[2] != 0 || first[6] != 0 || first[8] != 255 {
		t.Fatalf("unexpected first police frame: %v", first)
	}
	second := stripPoliceFrame(4, 400*time.Millisecond)
	if second[0] != 0 || second[2] != 255 || second[6] != 255 || second[8] != 0 {
		t.Fatalf("unexpected swapped police frame: %v", second)
	}
}

func TestStripWhiteThunderFrameEnvelope(t *testing.T) {
	peak := stripWhiteThunderFrame(2, 0)
	if len(peak) != 6 || peak[0] != 255 || peak[1] != 255 || peak[2] != 255 {
		t.Fatalf("unexpected thunder peak: %v", peak)
	}
	dark := stripWhiteThunderFrame(2, 500*time.Millisecond)
	for _, channel := range dark {
		if channel != 0 {
			t.Fatalf("thunder should be dark between strikes: %v", dark)
		}
	}
}

func TestStripConvergingRedFrameMovesTowardCenter(t *testing.T) {
	start := stripConvergingRedFrame(10, 0)
	if start[0] != 255 || start[27] != 255 || start[12] != 0 {
		t.Fatalf("unexpected converging start: %v", start)
	}
	middle := stripConvergingRedFrame(10, 1900*time.Millisecond)
	if middle[12] == 0 || middle[15] == 0 {
		t.Fatalf("dots did not reach center: %v", middle)
	}
}

func TestStripEffectCommandListsAndStarts(t *testing.T) {
	target := &recordingOutputTarget{}
	outputs := NewOutputScheduler(target)
	defer outputs.Close()
	list, err := stripStreamCommand(context.Background(), outputs, []string{"effect", "list"})
	if err != nil || !strings.Contains(list, "converging-red") {
		t.Fatalf("effect list: %q, %v", list, err)
	}
	started, err := stripStreamCommand(context.Background(), outputs, []string{"effect", "play", "police", "8", "20"})
	if err != nil || !strings.Contains(started, "strip effect police started") {
		t.Fatalf("effect start: %q, %v", started, err)
	}
	if _, err := stripStreamCommand(context.Background(), outputs, []string{"stop"}); err != nil {
		t.Fatal(err)
	}
}

func TestStripEffectCommandRoutesThroughPublicCommandEngine(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	engine := NewCommandEngine(runtime, CommandOptions{})
	list, err := engine.Execute(context.Background(), "strip effect list")
	if err != nil || !strings.Contains(list, "white-thunder") {
		t.Fatalf("public strip effect route: %q, %v", list, err)
	}
}
