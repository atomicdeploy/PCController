package hostbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/pcspeaker"
	"pccontroller.local/controller/internal/shell"
)

func TestPeerSubscriptionEchoIsConsumedWithoutRepublishing(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	for _, event := range []controller.Event{
		{Kind: "status_led.changed", Source: "bridge"},
		{Kind: "message", Source: "websocket"},
		{Kind: "door", Source: "board", Metadata: map[string]string{"bridge.ingress": "peer"}},
	} {
		before := runtime.LatestEventID()
		raw, _ := json.Marshal(event)
		if !manager.ingestPeerEvent("peer", raw) {
			t.Fatal("echo must be consumed, not passed to text fallback")
		}
		if runtime.LatestEventID() != before {
			t.Fatalf("echo republished: %+v", event)
		}
	}
}

func TestAuthenticatedPeerBuzzerEventStaysStructuredAndLoopSafe(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()
	raw, _ := json.Marshal(controller.Event{
		ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board",
		Metadata: map[string]string{
			"frequency_hz": "880", "duration_ms": "125",
			"device_micros": "1000", "connection_generation": "7",
		},
	})
	if !manager.ingestPeerEvent("cafe-pc", raw) {
		t.Fatal("valid peer event was not accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, "buzzer.note")
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata["bridge.ingress"] != "cafe-pc" ||
		event.Metadata["bridge.original_source"] != "board" ||
		event.Source != "bridge" {
		t.Fatalf("peer provenance=%#v source=%q", event.Metadata, event.Source)
	}
	if bridgeEventForwardable(controller.Event{Kind: event.Kind, Metadata: event.Metadata}) {
		t.Fatal("ingressed event could be forwarded into a bridge cycle")
	}
	config := appconfig.DefaultBuzzerMirror()
	config.Enabled, config.NativeEnabled = true, true
	if job, ok := buzzerMirrorJobFor(config, controller.Event{Kind: event.Kind, Metadata: event.Metadata}); !ok ||
		job.frequencyHz != 880 || job.durationMS != 125 || !job.timed ||
		!job.haveGeneration || job.generation != 7 {
		t.Fatalf("mirrored job=%+v ok=%t", job, ok)
	}
}

func TestBuzzerMirrorJobIgnoresMalformedOptionalGeneration(t *testing.T) {
	config := appconfig.DefaultBuzzerMirror()
	config.Enabled, config.NativeEnabled = true, true
	job, ok := buzzerMirrorJobFor(config, controller.Event{
		Kind: "buzzer.note",
		Metadata: map[string]string{
			"frequency_hz": "880", "duration_ms": "125",
			"device_micros": "1000", "connection_generation": "not-a-number",
		},
	})
	if !ok || !job.timed || job.haveGeneration {
		t.Fatalf("optional malformed generation rejected the note: job=%+v ok=%t", job, ok)
	}
}

func TestBuzzerMirrorJobRequiresOptInAndValidBoardNote(t *testing.T) {
	config := appconfig.DefaultBuzzerMirror()
	event := controller.Event{Kind: "buzzer.note", Metadata: map[string]string{
		"frequency_hz": "440", "duration_ms": "220", "muted": "false",
	}}
	if _, ok := buzzerMirrorJobFor(config, event); ok {
		t.Fatal("disabled mirror accepted a note")
	}
	config.Enabled = true
	config.NativeEnabled = true
	config.DriverDirectory = `C:\optional\winring0`
	job, ok := buzzerMirrorJobFor(config, event)
	if !ok || job.frequencyHz != 440 || job.durationMS != 220 {
		t.Fatalf("job=%+v ok=%t", job, ok)
	}
	event.Metadata["muted"] = "true"
	if _, ok := buzzerMirrorJobFor(config, event); !ok {
		t.Fatal("board-silent note did not reach the independently enabled host path")
	}
	event.Metadata["frequency_hz"] = "0"
	if pause, ok := buzzerMirrorJobFor(config, event); !ok || pause.frequencyHz != 0 {
		t.Fatal("explicit board pause was not retained as a host timeline marker")
	}
	event.Metadata["duration_ms"] = "0"
	if stop, ok := buzzerMirrorJobFor(config, event); !ok || stop.frequencyHz != 0 || stop.durationMS != 0 {
		t.Fatal("explicit board stop was not retained as a host timeline marker")
	}
	event.Metadata["frequency_hz"] = "440"
	if _, ok := buzzerMirrorJobFor(config, event); ok {
		t.Fatal("zero-duration non-stop buzzer state was accepted")
	}
}

func TestBuzzerPlaybackTimelinePreservesDeviceCadenceAndBuffersTransportJitter(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	first := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 0xFFFFFF00, timed: true,
		observedAt: base, source: "board-a",
	}, base)
	if !first.start.Equal(base.Add(buzzerPlayoutDelay)) || !first.end.Equal(base.Add(buzzerPlayoutDelay+100*time.Millisecond)) {
		t.Fatalf("first plan=%+v", first)
	}
	// uint32 subtraction deliberately preserves the MCU clock across wrap.
	second := timeline.plan(buzzerMirrorJob{
		durationMS: 80, deviceMicros: 0x0001D3C0, timed: true,
		observedAt: base.Add(145 * time.Millisecond), source: "board-a",
	}, base.Add(145*time.Millisecond))
	wantStart := base.Add(buzzerPlayoutDelay + 120*time.Millisecond)
	if !second.start.Equal(wantStart) {
		t.Fatalf("second start=%v want=%v", second.start, wantStart)
	}
	remaining, ok := second.remaining(base.Add(150 * time.Millisecond))
	if !ok || remaining != 80*time.Millisecond {
		t.Fatalf("late remaining=%v ok=%t", remaining, ok)
	}
	if remaining, ok := second.remaining(base.Add(250 * time.Millisecond)); ok || remaining != 0 {
		t.Fatalf("expired note remaining=%v ok=%t", remaining, ok)
	}
}

func TestBuzzerPlaybackTimelineRebuffersFreshShortNoteAfterMissedCushion(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{source: "board-a", timed: true,
		deviceMicros: 1000, durationMS: 10, observedAt: base}, base)
	// Previously this fresh 10 ms note ended at +160 ms and was entirely
	// skipped when it arrived at +190 ms. Rebuffer, don't stretch the note.
	received := base.Add(190 * time.Millisecond)
	late := timeline.plan(buzzerMirrorJob{source: "board-a", timed: true,
		deviceMicros: 101000, durationMS: 10, observedAt: received}, received)
	if !late.start.Equal(received.Add(buzzerPlayoutDelay)) || late.end.Sub(late.start) != 10*time.Millisecond {
		t.Fatalf("fresh short note was truncated: %+v", late)
	}
	next := timeline.plan(buzzerMirrorJob{source: "board-a", timed: true,
		deviceMicros: 121000, durationMS: 10, observedAt: received.Add(time.Millisecond)}, received.Add(time.Millisecond))
	if next.start.Sub(late.start) != 20*time.Millisecond {
		t.Fatalf("device cadence lost after recovery: %+v", next)
	}
}

func TestBuzzerPlaybackTimelineDoesNotRebufferStaleQueuedNote(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{source: "board-a", timed: true,
		deviceMicros: 1000, durationMS: 10, observedAt: base}, base)
	stale := timeline.plan(buzzerMirrorJob{source: "board-a", timed: true,
		deviceMicros: 101000, durationMS: 10, observedAt: base.Add(190 * time.Millisecond)}, base.Add(time.Second))
	if _, audible := stale.remaining(base.Add(time.Second)); audible {
		t.Fatal("stale queued note was replayed as fresh")
	}
}

func TestBuzzerPlaybackTimelineReanchorsAfterDeviceRestart(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{
		durationMS: 10, deviceMicros: 9_000_000, timed: true,
		observedAt: base, source: "board-a",
	}, base)
	restarted := timeline.plan(buzzerMirrorJob{
		durationMS: 10, deviceMicros: 1_000, timed: true,
		observedAt: base.Add(time.Second), source: "board-a",
	}, base.Add(time.Second))
	if !restarted.start.Equal(base.Add(time.Second + buzzerPlayoutDelay)) {
		t.Fatalf("restart did not re-anchor to observation: %+v", restarted)
	}
}

func TestBuzzerPlaybackTimelineReanchorsSmallForwardCounterAfterGenerationChange(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 1_000, timed: true,
		generation: 1, haveGeneration: true,
		observedAt: base, source: "board-a",
	}, base)
	restarted := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 2_000, timed: true,
		generation: 2, haveGeneration: true,
		observedAt: base.Add(time.Second), source: "board-a",
	}, base.Add(time.Second))
	if !restarted.start.Equal(base.Add(time.Second+buzzerPlayoutDelay)) ||
		!restarted.end.Equal(base.Add(1100*time.Millisecond+buzzerPlayoutDelay)) {
		t.Fatalf("generation change did not re-anchor to observation: %+v", restarted)
	}
}

func TestBuzzerPlaybackTimelineBoundsMissingGenerationAndFutureSkew(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 1_000, timed: true,
		observedAt: base, source: "peer-without-generation",
	}, base)
	restarted := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 2_000, timed: true,
		observedAt: base.Add(time.Second), source: "peer-without-generation",
	}, base.Add(time.Second))
	if !restarted.start.Equal(base.Add(time.Second + buzzerPlayoutDelay)) {
		t.Fatalf("missing-generation restart did not re-anchor: %+v", restarted)
	}

	jumped := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 240_002_000, timed: true,
		observedAt: base.Add(1100 * time.Millisecond), source: "peer-without-generation",
	}, base.Add(1100*time.Millisecond))
	if !jumped.start.Equal(base.Add(1100*time.Millisecond+buzzerPlayoutDelay)) ||
		!jumped.end.Equal(base.Add(1200*time.Millisecond+buzzerPlayoutDelay)) {
		t.Fatalf("future timestamp jump was scheduled away from observation: %+v", jumped)
	}
}

func TestBuzzerPlaybackTimelineReanchorsAfterWholeMicrosWrapOfSilence(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 1_000, timed: true,
		observedAt: base, source: "board-a",
	}, base)
	observedAt := base.Add(time.Duration(uint64(1)<<32) * time.Microsecond).Add(30 * time.Second)
	fresh := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 30_001_000, timed: true,
		observedAt: observedAt, source: "board-a",
	}, observedAt)
	if !fresh.start.Equal(observedAt.Add(buzzerPlayoutDelay)) {
		t.Fatalf("full-wrap silence did not re-anchor to observation: %+v", fresh)
	}
	remaining, ok := fresh.remaining(observedAt)
	if !ok || remaining != 100*time.Millisecond {
		t.Fatalf("fresh note remaining=%v ok=%t", remaining, ok)
	}
}

func TestBuzzerDispatchReusesResolvedExternalBackend(t *testing.T) {
	manager := &Manager{
		buzzerJobs: make(chan buzzerMirrorJob, 1),
		status: Status{
			BuzzerNativeState: "ready", BuzzerNativeBackend: "external",
			BuzzerNativeExecutable: "/usr/local/bin/beep",
		},
	}
	config := appconfig.Config{}
	config.Integrations.BuzzerMirror = appconfig.DefaultBuzzerMirror()
	config.Integrations.BuzzerMirror.Enabled = true
	config.Integrations.BuzzerMirror.NativeEnabled = true
	manager.dispatchBuzzerMirror(config, controller.Event{
		Kind: "buzzer.note", Metadata: map[string]string{
			"frequency_hz": "440", "duration_ms": "80",
		},
	})
	job := <-manager.buzzerJobs
	if job.config.Backend != "external" || job.config.Executable != "/usr/local/bin/beep" {
		t.Fatalf("resolved backend was not reused: %+v", job.config)
	}
}

func TestBuzzerReconcileDoesNotProbeAnUnchangedReadyBackend(t *testing.T) {
	config := appconfig.DefaultBuzzerMirror()
	config.Enabled, config.NativeEnabled, config.Backend = true, true, "native"
	previous := config
	status := Status{BuzzerNativeState: "ready", BuzzerNativeBackend: "native"}
	calls := 0
	resolve := func(appconfig.BuzzerMirror) (pcspeaker.BackendStatus, error) {
		calls++
		return pcspeaker.BackendStatus{Backend: "native"}, nil
	}
	config.WebAudioEnabled = !config.WebAudioEnabled
	config.Path = "host"
	if _, err := reuseOrResolveNativeBuzzer(config, &previous, status, resolve); err != nil || calls != 0 {
		t.Fatalf("presentation-only change probed live speaker: calls=%d err=%v", calls, err)
	}
	config.DriverDirectory = "changed"
	if _, err := reuseOrResolveNativeBuzzer(config, &previous, status, resolve); err != nil || calls != 1 {
		t.Fatalf("changed native configuration was not resolved: calls=%d err=%v", calls, err)
	}
	config = previous
	status.BuzzerNativeState = "failed"
	if _, err := reuseOrResolveNativeBuzzer(config, &previous, status, resolve); err != nil || calls != 2 {
		t.Fatalf("failed backend did not retry resolution: calls=%d err=%v", calls, err)
	}
	if _, err := reuseOrResolveNativeBuzzer(config, nil, status, resolve); err != nil || calls != 3 {
		t.Fatalf("first startup did not resolve: calls=%d err=%v", calls, err)
	}
}

func TestBuzzerMirrorTransitionsCancelSameSourceWithoutConcurrentPlayback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	type playbackEvent struct {
		frequency int
		started   bool
	}
	events := make(chan playbackEvent, 8)
	activeCalls, maxActive := 0, 0
	manager := &Manager{
		ctx: ctx, cancel: cancel, buzzerJobs: make(chan buzzerMirrorJob, 8),
		status: Status{BuzzerNativeState: "ready"},
		buzzerPlay: func(playContext context.Context, job buzzerMirrorJob) error {
			activeCalls++
			if activeCalls > maxActive {
				maxActive = activeCalls
			}
			events <- playbackEvent{frequency: job.frequencyHz, started: true}
			<-playContext.Done()
			activeCalls--
			events <- playbackEvent{frequency: job.frequencyHz}
			return playContext.Err()
		},
	}
	manager.wait.Add(1)
	go manager.buzzerMirrorLoop()
	send := func(source string, frequency, duration int) {
		manager.buzzerJobs <- buzzerMirrorJob{
			frequencyHz: frequency, durationMS: duration,
			observedAt: time.Now(), source: source,
		}
	}
	waitEvent := func(want playbackEvent) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("playback event=%+v want=%+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for playback event %+v", want)
		}
	}

	send("board-a", 440, 5_000)
	waitEvent(playbackEvent{frequency: 440, started: true})
	send("board-a", 660, 5_000)
	waitEvent(playbackEvent{frequency: 440})
	waitEvent(playbackEvent{frequency: 660, started: true})
	send("board-a", 0, 40)
	waitEvent(playbackEvent{frequency: 660})
	select {
	case unexpected := <-events:
		t.Fatalf("pause started another playback: %+v", unexpected)
	case <-time.After(25 * time.Millisecond):
	}
	send("board-a", 880, 5_000)
	waitEvent(playbackEvent{frequency: 880, started: true})
	send("board-a", 0, 0)
	waitEvent(playbackEvent{frequency: 880})
	select {
	case unexpected := <-events:
		t.Fatalf("stop started another playback: %+v", unexpected)
	case <-time.After(25 * time.Millisecond):
	}
	if maxActive != 1 {
		t.Fatalf("maximum concurrent native playbacks=%d want=1", maxActive)
	}
	cancel()
	manager.wait.Wait()
}

func TestBuzzerMirrorStopDoesNotCancelAnotherSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	manager := &Manager{
		ctx: ctx, cancel: cancel, buzzerJobs: make(chan buzzerMirrorJob, 4),
		status: Status{BuzzerNativeState: "ready"},
		buzzerPlay: func(playContext context.Context, _ buzzerMirrorJob) error {
			started <- struct{}{}
			<-playContext.Done()
			stopped <- struct{}{}
			return playContext.Err()
		},
	}
	manager.wait.Add(1)
	go manager.buzzerMirrorLoop()
	manager.buzzerJobs <- buzzerMirrorJob{
		frequencyHz: 440, durationMS: 5_000, observedAt: time.Now(), source: "board-a",
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("source A playback did not start")
	}
	manager.buzzerJobs <- buzzerMirrorJob{
		frequencyHz: 0, durationMS: 0, observedAt: time.Now(), source: "board-b",
	}
	select {
	case <-stopped:
		t.Fatal("source B stop canceled source A")
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	manager.wait.Wait()
}

func TestBuzzerMirrorStopFlushesBufferedNotesWithoutPlayback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	played := make(chan int, 4)
	manager := &Manager{ctx: ctx, cancel: cancel, buzzerJobs: make(chan buzzerMirrorJob, 4),
		status: Status{BuzzerNativeState: "ready"},
		buzzerPlay: func(ctx context.Context, job buzzerMirrorJob) error {
			played <- job.frequencyHz
			<-ctx.Done()
			return ctx.Err()
		},
	}
	defer func() { cancel(); manager.wait.Wait() }()
	manager.wait.Add(1)
	// Queue in stream order before starting the scheduler to avoid wall-clock races.
	manager.buzzerJobs <- buzzerMirrorJob{source: "board-a", frequencyHz: 440, durationMS: 500,
		timed: true, deviceMicros: 1000, observedAt: time.Now()}
	manager.buzzerJobs <- buzzerMirrorJob{source: "board-a", frequencyHz: 660, durationMS: 500,
		timed: true, deviceMicros: 2000, observedAt: time.Now()}
	manager.buzzerJobs <- buzzerMirrorJob{source: "board-a", frequencyHz: 0, durationMS: 0,
		timed: true, deviceMicros: 3000, observedAt: time.Now()}
	go manager.buzzerMirrorLoop()
	select {
	case frequency := <-played:
		t.Fatalf("stopped buffered note replayed at %d Hz", frequency)
	case <-time.After(2 * buzzerPlayoutDelay):
	}
}

func TestBuzzerMirrorReconnectDropsPreviousGenerationBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	played := make(chan int, 4)
	manager := &Manager{ctx: ctx, cancel: cancel, buzzerJobs: make(chan buzzerMirrorJob, 4),
		status: Status{BuzzerNativeState: "ready"},
		buzzerPlay: func(ctx context.Context, job buzzerMirrorJob) error {
			played <- job.frequencyHz
			<-ctx.Done()
			return ctx.Err()
		},
	}
	defer func() { cancel(); manager.wait.Wait() }()
	manager.wait.Add(1)
	for generation := uint64(1); generation <= 2; generation++ {
		manager.buzzerJobs <- buzzerMirrorJob{source: "board-a", frequencyHz: int(generation) * 440, durationMS: 500,
			timed: true, deviceMicros: 1000, generation: generation, haveGeneration: true, observedAt: time.Now()}
	}
	go manager.buzzerMirrorLoop()
	select {
	case frequency := <-played:
		if frequency != 880 {
			t.Fatalf("old-generation buffer replayed at %d Hz", frequency)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh generation did not play")
	}
}

func TestNativeBuzzerFailuresAreStateTransitionsNotPerNoteLogSpam(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()

	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	first := runtime.LatestEventID()
	if first != after+1 || manager.Status().BuzzerNativeState != "failed" {
		t.Fatalf("first failure id=%d status=%#v", first, manager.Status())
	}
	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	if runtime.LatestEventID() != first {
		t.Fatal("identical per-note failure emitted another activity event")
	}
	manager.recordNativeBuzzerResult(nil)
	if runtime.LatestEventID() != first+1 || manager.Status().BuzzerNativeState != "ready" {
		t.Fatalf("recovery id=%d status=%#v", runtime.LatestEventID(), manager.Status())
	}
}
