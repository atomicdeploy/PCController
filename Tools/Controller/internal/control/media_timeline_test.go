package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMediaTimelineBackgroundTrafficCannotBlockPreparedEdges(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	for _, opcode := range []byte{native.OpPWMGet, native.OpGetStatus, native.OpDisplayText, native.OpProgramState} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		// Internal presentation bypasses the live-output conflict check, but
		// still must not bypass the actual wire reservation.
		ctx = context.WithValue(ctx, mediaTimelineContextKey{}, true)
		_, err := runtime.Request(ctx, opcode, nil, native.OpACK)
		cancel()
		var busy *ResourceBusyError
		if !errors.As(err, &busy) || busy.Resource != "serial_channel" {
			t.Fatalf("fresh readback should report ownership conflict, got %v", err)
		}
	}
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.Acknowledged == 2 })
	if status.State == "faulted" {
		t.Fatal(status.Error)
	}
	mu.Lock()
	for _, frame := range *frames {
		if frame.Opcode == native.OpPWMGet {
			mu.Unlock()
			t.Fatal("PWM poll reached the wire during playback")
		}
	}
	mu.Unlock()
	value.Playing = false
	value.Sequence++
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	if runtime.MediaTimeline().PlayReady {
		t.Fatal("pause did not release readback")
	}
}

func TestMediaTimelineReservationLeaseUsesGrantTimeNotOldClockReceipt(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	_ = armTimeline(t, runtime, 0)
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-300 * time.Millisecond)
	runtime.mediaPlayback.mu.Unlock()
	runtime.expirePausedMediaTimelineWire()
	if !runtime.MediaTimeline().PlayReady {
		t.Fatal("old paused clock receipt prematurely expired newly granted reservation")
	}
	runtime.mediaTimeline.mu.Lock()
	runtime.mediaTimeline.wireRenewedAt = time.Now().Add(-300 * time.Millisecond)
	runtime.mediaTimeline.mu.Unlock()
	runtime.expirePausedMediaTimelineWire()
	if runtime.MediaTimeline().PlayReady {
		t.Fatal("expired reservation still advertised readiness")
	}
}

func TestMediaTimelineDoesNotAcknowledgePlayWithPWMReadbackInFlight(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	value.Playing = false
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	value.Sequence++
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		// This fixture ACKs commands, but deliberately does not provide PWM
		// values, leaving the read registered until its caller cancels it.
		_, err := runtime.Request(ctx, native.OpPWMGet, nil, native.OpPWMValues)
		done <- err
	}()
	runtime.mu.RLock()
	session := runtime.session
	runtime.mu.RUnlock()
	deadline := time.Now().Add(time.Second)
	written := false
	for !written && time.Now().Before(deadline) {
		mu.Lock()
		for _, frame := range *frames {
			written = written || frame.Opcode == native.OpPWMGet
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	if !written || !session.HasPendingRequest(native.OpPWMGet) {
		t.Fatal("PWM read did not acquire the wire")
	}
	value.PreparePlay = true
	armed := make(chan error, 1)
	go func() { _, err := runtime.UpdateMediaPlayback(value); armed <- err }()
	select {
	case err := <-armed:
		t.Fatalf("reservation returned before read drained: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if runtime.MediaPlayback().Playing || runtime.MediaTimeline().PlayReady {
		t.Fatal("draining reservation falsely acknowledged readiness or Play")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readback did not release its request slot")
	}
	select {
	case err := <-armed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reservation did not finish")
	}
	if !runtime.MediaTimeline().PlayReady {
		t.Fatal("drained reservation not acknowledged")
	}
	value.PreparePlay = false
	value.Playing = true
	value.Sequence++
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatalf("released readback still blocks explicit Play: %v", err)
	}
}

func TestMediaTimelineReportsStandaloneStripAsRetryableResourceBusy(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	runtime.setActiveUseState(activeUseStrip, true)
	_, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "test", Revision: 1})
	var busy *ResourceBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected ResourceBusyError, got %T: %v", err, err)
	}
	if busy.Resource != "addressable_strip" || busy.Owner != "standalone_strip_stream" || busy.RetryAfterMS != 2000 {
		t.Fatalf("unexpected resource conflict: %#v", busy)
	}
}

func TestMediaTimelinePausedReservationExpiresAndRequiresNewPreparePlay(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	if !runtime.MediaTimeline().PlayReady {
		t.Fatal("paused reservation missing")
	}
	waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return !status.PlayReady })
	if _, err := runtime.UpdateMediaPlayback(value); err == nil {
		t.Fatal("expired serial reservation accepted Play")
	}
}

func TestMediaTimelineEmergencyStopReleasesPausedReservation(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	_ = armTimeline(t, runtime, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.SetEmergencyStop(ctx, true, "test", "reservation regression"); err != nil {
		t.Fatal(err)
	}
	if runtime.MediaTimeline().PlayReady {
		t.Fatal("E-STOP left wire reserved")
	}
}

func TestMediaTimelineEmergencyStopBypassesReservationStillBeingGranted(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	runtime.mu.RLock()
	session := runtime.session
	runtime.mu.RUnlock()
	// Model the reservation's drain phase: the link has fenced admission but
	// no completed lease has yet been installed in mediaTimeline.releaseWire.
	release, err := session.ReserveTimeline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.SetEmergencyStop(ctx, true, "test", "pending reservation"); err != nil {
		t.Fatalf("safety cleanup was blocked by pending reservation: %v", err)
	}
}

func TestMediaTimelinePreparePlayValidation(t *testing.T) {
	for _, value := range []MediaPlaybackUpdate{
		{ClientID: "test", Sequence: 1, Rate: 1, PreparePlay: true, Loaded: false, PlanRevision: 1},
		{ClientID: "test", Sequence: 1, Rate: 1, PreparePlay: true, Loaded: true, PlanRevision: 0},
		{ClientID: "test", Sequence: 1, Rate: 1, PreparePlay: true, Loaded: true, Playing: true, PlanRevision: 1},
	} {
		if err := value.validate(); err == nil {
			t.Fatalf("invalid preparation accepted: %+v", value)
		}
	}
}

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

func TestMediaTimelineRemainingBudgetChargesDispatchDelay(t *testing.T) {
	if got := mediaTimelineRemainingBudget(50, -3); got != 50*time.Millisecond {
		t.Fatalf("early dispatch budget = %v", got)
	}
	if got := mediaTimelineRemainingBudget(50, 12.5); got != 37500*time.Microsecond {
		t.Fatalf("delayed dispatch budget = %v", got)
	}
	if got := mediaTimelineRemainingBudget(50, 50); got != 0 {
		t.Fatalf("expired dispatch budget = %v", got)
	}
}

type mediaTimelineWire struct {
	*programStateWirePort
	nack     bool
	ackDelay time.Duration
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
	if port.ackDelay > 0 {
		time.Sleep(port.ackDelay)
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
	port := &mediaTimelineWire{programStateWirePort: newProgramStateWirePort(), nack: nack, ackDelay: time.Millisecond}
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
	runtime.attach(link.OpenResult{Session: link.NewForPort("TIMELINE-TEST", port), Port: ports.Info{Name: "TIMELINE-TEST"}, Hello: native.Hello{Name: "Virtual", Capabilities: native.CapabilityProgramState | native.CapabilityScheduledSegments}})
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
	value.PreparePlay = true
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	value.Sequence++
	value.Playing = true
	value.PreparePlay = false
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
	if status.MaxDispatchLatenessMS <= 0 || status.MaxAckRoundTripMS <= 0 {
		t.Fatalf("timing phases were not reported: %+v", status)
	}
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		var edges []byte
		for _, frame := range *frames {
			if frame.Opcode == native.OpRelaySet {
				edges = append(edges, frame.Payload[1])
			}
		}
		mu.Unlock()
		if len(edges) == 2 {
			if edges[0] != 1 || edges[1] != 0 {
				t.Fatalf("wire edges %v", edges)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wire edges %v", edges)
		}
		time.Sleep(time.Millisecond)
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

func TestMediaTimelineBlockedBoardSnapshotReportsDelayWithoutExecutingExpiredCue(t *testing.T) {
	runtime, frames, mu := mediaTimelineFixture(t, false)
	value := armTimeline(t, runtime, 0)
	// Inject an accepted Play directly, while holding the board snapshot lock.
	// This deterministic source fixture does not depend on a busy real machine,
	// a client publisher or an artificially enlarged dispatch budget.
	runtime.mu.Lock()
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.snapshot.MediaPlaybackUpdate = value
	runtime.mediaPlayback.received = time.Now()
	runtime.mediaPlayback.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	runtime.mu.Unlock()
	status := waitTimeline(t, runtime, func(status MediaTimelineStatus) bool { return status.State == "faulted" })
	if status.Acknowledged != 0 || status.LastStep != "on/0" || !strings.Contains(status.Error, "not executed (worker gap") {
		t.Fatalf("expired cue lost its diagnostic evidence: %+v", status)
	}
	if max(status.MaxWorkerGapMS, status.MaxBoardReadMS) < 100 {
		t.Fatalf("snapshot contention was invisible: %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, frame := range *frames {
		if frame.Opcode == native.OpRelaySet {
			t.Fatal("expired cue reached the wire")
		}
	}
}

func TestMediaTimelineCatalogSnapshotDoesNotBlockConnectionAccess(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseCatalog := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCatalog()
	var once sync.Once
	runner := NewMacroRunner(nil, func() []appconfig.Macro {
		once.Do(func() { close(entered) })
		<-release
		return []appconfig.Macro{{ID: 7, Name: "fixture", Mode: macroModeHost,
			Steps: []appconfig.MacroStep{{Kind: "relay", Target: 7, Value: 0}}}}
	}, nil)
	runtime.setMacroRunner(runner)
	snapshotDone := make(chan Snapshot, 1)
	go func() { snapshotDone <- runtime.Snapshot() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("snapshot never entered catalog provider")
	}
	accessDone := make(chan struct{})
	go func() {
		// Model a pending connection writer followed by the executor's reader.
		runtime.mu.Lock()
		runtime.mu.Unlock()
		runtime.mu.RLock()
		runtime.mu.RUnlock()
		close(accessDone)
	}()
	select {
	case <-accessDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("catalog presentation held the shared connection lock")
	}
	releaseCatalog()
	select {
	case snapshot := <-snapshotDone:
		found := false
		for _, effect := range snapshot.Effects {
			found = found || effect.Reference == "effect:7" && effect.Name == "fixture"
		}
		if !found {
			t.Fatalf("moving catalog work dropped effects: %+v", snapshot.Effects)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot did not finish")
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
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "test", Sequence: 3, Loaded: true, Playing: true, Rate: 1, Epoch: 1, PlanRevision: 1}); err == nil || !strings.Contains(err.Error(), status.Error) {
		t.Fatalf("faulted plan resumed or its original error was masked: %v", err)
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
	value.PreparePlay = true
	if _, err := runtime.UpdateMediaPlayback(value); err != nil {
		t.Fatal(err)
	}
	value.Sequence++
	value.Playing = true
	value.PreparePlay = false
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
