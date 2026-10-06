package control

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMediaTimelineCompilerFreezesOffsetsAndRejectsInvalidPlans(t *testing.T) {
	plan := MediaTimelinePlan{ClientID: "test", Revision: 1, Actions: []MediaTimelineAction{
		{ID: "on", TimeMS: 10000, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 1}},
		{ID: "off", TimeMS: 11000, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 0}},
	}}
	steps, err := compileMediaTimeline(plan, nil)
	if err != nil || len(steps) != 2 || steps[0].dueMS != 10000 || steps[1].dueMS != 11000 || steps[0].payload[0] != 7 {
		t.Fatalf("compile: %v %+v", err, steps)
	}
	plan.Actions[1].ID = "on"
	if _, err = compileMediaTimeline(plan, nil); err == nil {
		t.Fatal("duplicate item accepted")
	}
	plan.Actions[1].ID = "off"
	plan.Actions[1].TimeMS = 1 << 32
	if _, err = compileMediaTimeline(plan, nil); err == nil {
		t.Fatal("overflow accepted")
	}
	plan.Actions[1].TimeMS = 11000
	plan.Actions[1].Step.Kind = "missing"
	if _, err = compileMediaTimeline(plan, nil); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestMediaTimelineResumeDoesNotReplayOneShots(t *testing.T) {
	steps := []mediaTimelineStep{
		{"on", 0, native.OpRelaySet, []byte{8, 1}},
		{"rf", 5, native.OpRFTx, []byte{1}},
		{"off", 10, native.OpRelaySet, []byte{8, 0}},
		{"pwm", 15, native.OpPWMSet, []byte{3, 1, 0}},
	}
	restored := mediaTimelineRestore(steps, 4)
	if len(restored) != 2 || restored[0].id != "off" || restored[1].id != "pwm" {
		t.Fatalf("restore %+v", restored)
	}
}

func TestMediaTimelinePreservesExplicitMCUExecutionPolicy(t *testing.T) {
	macro := appconfig.Macro{ID: 7, Name: "Timing policy", Mode: macroModeMCU,
		Steps: []appconfig.MacroStep{{Kind: "relay", Target: 7, Value: 1}}}
	runner := NewMacroRunner(nil, func() []appconfig.Macro { return []appconfig.Macro{macro} }, nil)
	plan := MediaTimelinePlan{ClientID: "test", Revision: 1,
		Cues: []MediaTimelineCue{{ID: "cue", Reference: "effect:7", TimeMS: 1000}}}
	if _, err := compileMediaTimeline(plan, runner); err == nil || !strings.Contains(err.Error(), "MCU execution") {
		t.Fatalf("explicit MCU policy must not silently become host execution: %v", err)
	}
	macro.Mode = macroModeHost
	steps, err := compileMediaTimeline(plan, runner)
	if err != nil || len(steps) != 1 || steps[0].dueMS != 1000 {
		t.Fatalf("explicit host policy should compile: %v %+v", err, steps)
	}
}

type mediaTimelineWire struct {
	*programStateWirePort
	nack bool
}

func (port *mediaTimelineWire) Write(data []byte) (int, error) {
	frame, err := native.Decode(data)
	if err != nil {
		return 0, err
	}
	port.writes <- frame
	status := byte(0)
	if port.nack && frame.Opcode == native.OpRelaySet {
		status = 1
	}
	ack, err := native.Encode(native.Frame{Opcode: native.OpACK, Seq: frame.Seq, Payload: []byte{frame.Opcode, status}})
	if err != nil {
		return 0, err
	}
	port.reads <- ack
	return len(data), nil
}

func mediaTimelineFixture(t *testing.T, nack bool) (*Runtime, *[]native.Frame, *sync.Mutex) {
	t.Helper()
	runtime := New(Options{RequestTimeout: time.Second})
	port := &mediaTimelineWire{programStateWirePort: newProgramStateWirePort(), nack: nack}
	var frames []native.Frame
	var mu sync.Mutex
	go func() {
		for {
			select {
			case frame := <-port.writes:
				mu.Lock()
				frames = append(frames, frame)
				mu.Unlock()
			case <-port.closed:
				return
			}
		}
	}()
	runtime.attach(link.OpenResult{Session: link.NewForPort("TIMELINE-TEST", port), Port: ports.Info{Name: "TIMELINE-TEST"}, Hello: native.Hello{Name: "Virtual"}})
	t.Cleanup(func() { runtime.Close() })
	return runtime, &frames, &mu
}
func waitTimeline(t *testing.T, runtime *Runtime, predicate func(MediaTimelineStatus) bool) MediaTimelineStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := runtime.MediaTimeline()
		if predicate(status) {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeline status %+v", runtime.MediaTimeline())
	return MediaTimelineStatus{}
}
func armTimeline(t *testing.T, runtime *Runtime, at uint64) MediaPlaybackUpdate {
	t.Helper()
	plan := MediaTimelinePlan{ClientID: "test", Revision: 1, MaxLatenessMS: 100, Actions: []MediaTimelineAction{
		{ID: "on", TimeMS: 80, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 1}},
		{ID: "off", TimeMS: 150, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 0}},
	}}
	if _, err := runtime.PrepareMediaTimeline(plan); err != nil {
		t.Fatal(err)
	}
	value := MediaPlaybackUpdate{ClientID: "test", Sequence: 1, Loaded: true, Playing: true, Rate: 1, Epoch: 1, PlanRevision: 1}
	if _, err := runtime.UpdateMediaPlayback(value); err == nil {
		t.Fatal("playing accepted before paused arming ACK")
	}
	value.Playing = false
	value.PositionMS = at
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.ClockSequence == 1 && status.ArmedEpoch == 1 })
	value.Sequence++
	value.Playing = true
	return value
}
func TestMediaTimelineExecutesBothEdgesWithoutUIOrRepeatedCueRPC(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.Acknowledged == 2 })
	if status.State == "faulted" || status.MaxAckLatenessMS > 100 {
		t.Fatalf("not faithful: %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	var edges []byte
	for _, frame := range *frames {
		if frame.Opcode == native.OpRelaySet {
			edges = append(edges, frame.Payload[1])
		}
	}
	if len(edges) != 2 || edges[0] != 1 || edges[1] != 0 {
		t.Fatalf("wire edges %v", edges)
	}
}
func TestMediaTimelineLateActionFaultsWithoutSendingIt(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	value.PositionMS = 400
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.State == "faulted" })
	if status.Acknowledged != 0 || status.LastStep != "on/0" {
		t.Fatalf("lost failed cue: %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, frame := range *frames {
		if frame.Opcode == native.OpRelaySet {
			t.Fatal("late cue was sent")
		}
	}
}
func TestMediaTimelineNACKNeverAdvancesLedgerAndAttemptsCleanup(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, true)
	value := armTimeline(t, runtime, 0)
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.State == "faulted" })
	if status.Acknowledged != 0 {
		t.Fatal("NACK counted as successful")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := false
		for _, frame := range *frames {
			found = found || frame.Opcode == native.OpRelayAllOff
		}
		mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("ambiguous failure did not attempt safe cleanup")
}
func TestMediaTimelineClockLeaseFaultsInsteadOfSkippingPendingCue(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.State == "faulted" })
	if status.Acknowledged != 2 {
		t.Fatalf("unexpected ACK ledger: %+v", status)
	}
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "test", Sequence: 3, Loaded: true, Playing: true, Rate: 1, Epoch: 1, PlanRevision: 1}); err == nil {
		t.Fatal("faulted plan resumed")
	}
}

func TestMediaTimelinePauseResumeRestoresLatchedValueAndSeekRequiresArming(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	waitTimeline(t, runtime, func(s MediaTimelineStatus) bool { return s.Acknowledged == 1 })
	value.Sequence++
	value.Playing = false
	value.PositionMS = 100
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	waitTimeline(t, runtime, func(s MediaTimelineStatus) bool { return s.State == "paused" && s.ClockSequence == value.Sequence })
	value.Sequence++
	value.Playing = true
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	waitTimeline(t, runtime, func(s MediaTimelineStatus) bool { return s.Acknowledged == 2 })
	value.Sequence++
	value.Epoch = 2
	if _, err := runtime.UpdateMediaPlayback(value); err == nil {
		t.Fatal("unarmed seek accepted while playing")
	}
	value.Playing = false
	value.PositionMS = 200
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	waitTimeline(t, runtime, func(s MediaTimelineStatus) bool { return s.ArmedEpoch == 2 && s.RebasedSteps == 2 })
}
