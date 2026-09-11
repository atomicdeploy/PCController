package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func completedMacroStatus() native.MacroStatus {
	return native.MacroStatus{Schema: native.MacroQueueSchema, ID: 5, State: native.MacroCompleted, AcceptedSteps: 3, ExecutedSteps: 3, TotalSteps: 3, AcceptedBytes: 42, StartedAtUS: 12345}
}

func TestMCUCompletionPrioritizesFullEvidenceOverElapsedGrace(t *testing.T) {
	now := time.Unix(100, 0)
	window := macroCompletionWindow{watchdog: 15 * time.Second, watchdogDeadline: now.Add(15 * time.Second)}
	status := completedMacroStatus()
	if terminal, err := window.terminal(status, 1, 3, now); terminal || err != nil {
		t.Fatalf("completion did not allow late evidence: terminal=%t err=%v", terminal, err)
	}
	// The old 150 ms window must not cut off a normal UART response interval.
	if terminal, err := window.terminal(status, 2, 3, now.Add(time.Second)); terminal || err != nil {
		t.Fatalf("evidence grace too short: terminal=%t err=%v", terminal, err)
	}
	for _, elapsed := range []time.Duration{3 * time.Second, 16 * time.Second} {
		if terminal, err := window.terminal(status, 3, 3, now.Add(elapsed)); !terminal || err != nil {
			t.Fatalf("full completion falsely timed out after %s: terminal=%t err=%v", elapsed, terminal, err)
		}
	}
}

func TestMCUCompletionMissingEvidenceIsNotWatchdogOrFaithful(t *testing.T) {
	now := time.Unix(200, 0)
	window := macroCompletionWindow{watchdog: 15 * time.Second, watchdogDeadline: now.Add(15 * time.Second)}
	status := completedMacroStatus()
	_, _ = window.terminal(status, 1, 3, now)
	terminal, err := window.terminal(status, 1, 3, now.Add(macroCompletionEvidenceGrace))
	var missing *macroTimingEvidenceError
	if !terminal || !errors.As(err, &missing) || strings.Contains(err.Error(), "watchdog") || !strings.Contains(err.Error(), "timing unverified") {
		t.Fatalf("missing timestamps misreported: terminal=%t err=%v", terminal, err)
	}
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	runner.presentOnce.Do(func() { runner.present = make(chan MacroState, 1) })
	runner.state = MacroState{ID: 5, Name: "evidence", Mode: macroModeMCU, Running: true, StepCount: 3, TimingToleranceUS: 2500}
	done := make(chan struct{})
	runner.done = done
	runner.applyDeviceStatus(status)
	runner.recordEvidence(0, 984, true)
	runner.finishPlayback(done, appconfig.Macro{Steps: make([]appconfig.MacroStep, 3)}, status, 1, false, err)
	state := runner.State()
	if state.Lifecycle != "completed" || state.Step != 3 || state.EvidenceSteps != 1 || state.Device.ExecutedSteps != 3 || state.BufferFill != 0 || state.Faithful || !strings.Contains(state.LastError, "timing unverified") {
		t.Fatalf("device completion conflated with timing proof: %+v", state)
	}
	text, commandErr := macroCommand(context.Background(), runner, []string{"status"})
	if commandErr != nil || !strings.Contains(text, "step=3/3 evidence=1/3") {
		t.Fatalf("CLI conceals execution/evidence distinction: %q %v", text, commandErr)
	}
}

func TestMCUCompletionFullEvidenceFinishesFaithfully(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	runner.presentOnce.Do(func() { runner.present = make(chan MacroState, 1) })
	runner.state = MacroState{ID: 5, Name: "complete", Mode: macroModeMCU, Running: true, StepCount: 3, TimingToleranceUS: 2500}
	done := make(chan struct{})
	runner.done = done
	status := completedMacroStatus()
	runner.applyDeviceStatus(status)
	for index, delta := range []int32{904, 868, 160} {
		runner.recordEvidence(index, delta, true)
	}
	runner.finishPlayback(done, appconfig.Macro{Steps: make([]appconfig.MacroStep, 3)}, status, 3, false, nil)
	state := runner.State()
	if state.Lifecycle != "completed" || state.Step != 3 || state.EvidenceSteps != 3 || !state.Faithful || state.LastError != "" || state.MaximumTimingErrorUS != 904 {
		t.Fatalf("complete proof failed or changed timing: %+v", state)
	}
}

func TestMCUCompletionRetainsRealWatchdogAndCancellation(t *testing.T) {
	now := time.Unix(300, 0)
	window := macroCompletionWindow{watchdog: 15 * time.Second, watchdogDeadline: now.Add(15 * time.Second)}
	status := completedMacroStatus()
	status.State = native.MacroPlaying
	terminal, err := window.terminal(status, 1, 3, now.Add(16*time.Second))
	if !terminal || err == nil || !strings.Contains(err.Error(), "15s watchdog") {
		t.Fatalf("real playback timeout lost: terminal=%t err=%v", terminal, err)
	}
	status.State = native.MacroCancelled
	if terminal, err := window.terminal(status, 1, 3, now.Add(16*time.Second)); !terminal || err != nil {
		t.Fatalf("cancellation misreported: terminal=%t err=%v", terminal, err)
	}
}

func TestMCUCompletionIgnoresRegressiveStatusesAndLateACKCounts(t *testing.T) {
	completed := completedMacroStatus()
	for _, state := range []byte{native.MacroBuffering, native.MacroPlaying} {
		stale := completed
		stale.State, stale.ExecutedSteps, stale.Fill = state, 0, 42
		if got := mergeMacroDeviceStatus(completed, stale); got != completed {
			t.Fatalf("stale state regressed completed counters: %+v", got)
		}
	}
	staleEpoch := completed
	staleEpoch.StartedAtUS--
	if got := mergeMacroDeviceStatus(completed, staleEpoch); got != completed {
		t.Fatalf("previous run replaced current state: %+v", got)
	}
	if got := acknowledgeMacroDeviceStatus(completed, 1, 14); got != completed {
		t.Fatalf("late ACK regressed completed/fill state: %+v", got)
	}
	stalePlaying := completed
	stalePlaying.State = native.MacroPlaying
	if got := mergeMacroDeviceStatus(completed, stalePlaying); got != completed {
		t.Fatalf("terminal state regressed with equal counters: %+v", got)
	}
	playing := completed
	playing.State, playing.ExecutedSteps, playing.Fill = native.MacroPlaying, 0, 42
	advanced := acknowledgeMacroDeviceStatus(playing, 1, 14)
	if advanced.ExecutedSteps != 1 || advanced.Fill != 28 {
		t.Fatalf("fresh ACK did not advance estimate: %+v", advanced)
	}
	if got := mergeMacroDeviceStatus(advanced, completed); got != completed {
		t.Fatalf("authoritative final report rejected: %+v", got)
	}
}

func TestMCUCompletionDrainsQueuedEvidenceAheadOfHousekeeping(t *testing.T) {
	runtime := New(Options{})
	for index := uint64(1); index <= 300; index++ {
		runtime.eventLog = append(runtime.eventLog, Event{ID: index, Kind: "rx", Frame: native.Frame{Opcode: native.OpACK, Seq: 1}})
	}
	ack := Event{ID: 301, Frame: native.Frame{Opcode: native.OpACK, Seq: native.MacroExecutionSequence, Payload: []byte{native.OpDisplayText, 0, 1, 0, 0, 0}}}
	runtime.eventLog = append(runtime.eventLog, ack)
	got, pending := runtime.pendingMacroEvent(0, 5)
	if !pending || got.ID != ack.ID {
		t.Fatalf("queued execution proof hidden behind housekeeping: pending=%t event=%+v", pending, got)
	}
	if _, pending := runtime.pendingMacroEvent(ack.ID, 5); pending {
		t.Fatal("execution evidence would be counted twice")
	}
}
