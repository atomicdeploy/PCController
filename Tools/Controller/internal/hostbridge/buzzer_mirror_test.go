package hostbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestAuthenticatedPeerBuzzerEventStaysStructuredAndLoopSafe(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()
	raw, _ := json.Marshal(controller.Event{
		ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board",
		Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"},
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
	if job, ok := buzzerMirrorJobFor(config, controller.Event{Kind: event.Kind, Metadata: event.Metadata}); !ok || job.frequencyHz != 880 || job.durationMS != 125 {
		t.Fatalf("mirrored job=%+v ok=%t", job, ok)
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

func TestBuzzerPlaybackTimelinePreservesDeviceCadenceAndTrimsLateNotes(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	first := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 0xFFFFFF00, timed: true,
		observedAt: base, source: "board-a",
	}, base)
	if !first.start.Equal(base) || !first.end.Equal(base.Add(100*time.Millisecond)) {
		t.Fatalf("first plan=%+v", first)
	}
	// uint32 subtraction deliberately preserves the MCU clock across wrap.
	second := timeline.plan(buzzerMirrorJob{
		durationMS: 80, deviceMicros: 0x0001D3C0, timed: true,
		observedAt: base.Add(145 * time.Millisecond), source: "board-a",
	}, base.Add(145*time.Millisecond))
	wantStart := base.Add(120 * time.Millisecond)
	if !second.start.Equal(wantStart) {
		t.Fatalf("second start=%v want=%v", second.start, wantStart)
	}
	remaining, ok := second.remaining(base.Add(150 * time.Millisecond))
	if !ok || remaining != 50*time.Millisecond {
		t.Fatalf("late remaining=%v ok=%t", remaining, ok)
	}
	if remaining, ok := second.remaining(base.Add(250 * time.Millisecond)); ok || remaining != 0 {
		t.Fatalf("expired note remaining=%v ok=%t", remaining, ok)
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
	if !restarted.start.Equal(base.Add(time.Second)) {
		t.Fatalf("restart did not re-anchor to observation: %+v", restarted)
	}
}

func TestBuzzerPlaybackTimelineReanchorsAfterWholeMicrosWrapOfSilence(t *testing.T) {
	timeline := newBuzzerPlaybackTimeline()
	base := time.Unix(1700000000, 0)
	_ = timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 10_000, timed: true,
		observedAt: base, source: "board-a",
	}, base)
	observedAt := base.Add(time.Duration(uint64(1)<<32) * time.Microsecond).Add(30 * time.Second)
	fresh := timeline.plan(buzzerMirrorJob{
		durationMS: 100, deviceMicros: 30_010_000, timed: true,
		observedAt: observedAt, source: "board-a",
	}, observedAt)
	if !fresh.start.Equal(observedAt) {
		t.Fatalf("fresh note after a whole micros wrap did not re-anchor: %+v", fresh)
	}
	if remaining, ok := fresh.remaining(observedAt); !ok || remaining != 100*time.Millisecond {
		t.Fatalf("fresh note after a whole micros wrap was trimmed: remaining=%v ok=%t", remaining, ok)
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
