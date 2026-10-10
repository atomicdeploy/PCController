package control

import (
	"context"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"testing"
	"time"
)

func TestMediaClockTimingSeparatesFeedbackGapFromPositionCorrection(t *testing.T) {
	anchor := time.Now()
	previous := MediaPlaybackSnapshot{MediaPlaybackUpdate: MediaPlaybackUpdate{
		ClientID: "test", Sequence: 3, Loaded: true, Playing: true, Rate: 1, Epoch: 5, PlanRevision: 178, PositionMS: 280},
		ClockTiming: MediaClockTiming{MaxFeedbackIntervalMS: 47, MaxForwardCorrectionMS: 2}}
	value := previous.MediaPlaybackUpdate
	value.Sequence++
	value.PositionMS = 458
	timing := mediaClockTiming(previous, value, anchor, anchor.Add(80*time.Millisecond))
	if timing.FeedbackIntervalMS != 80 || timing.PositionCorrectionMS != 98 || timing.MaxForwardCorrectionMS != 98 || timing.MaxFeedbackIntervalMS != 80 {
		t.Fatalf("incoming clock leap was not separated from receipt gap: %+v", timing)
	}
	previous.ClockTiming = timing
	value.PositionMS = 350
	timing = mediaClockTiming(previous, value, anchor, anchor.Add(80*time.Millisecond))
	if timing.PositionCorrectionMS != -10 || timing.MaxForwardCorrectionMS != 98 {
		t.Fatalf("backward correction erased prior forward evidence: %+v", timing)
	}
	previous.Rate = 2
	value.PositionMS = 440
	if timing = mediaClockTiming(previous, value, anchor, anchor.Add(80*time.Millisecond)); timing.PositionCorrectionMS != 0 {
		t.Fatalf("projection did not use the previous rate: %+v", timing)
	}
	for _, name := range []string{"pause", "resume", "epoch", "plan", "owner", "unloaded", "no-anchor"} {
		t.Run(name, func(t *testing.T) {
			before, current, received := previous, value, anchor
			switch name {
			case "pause":
				current.Playing = false
			case "resume":
				before.Playing = false
			case "epoch":
				current.Epoch++
			case "plan":
				current.PlanRevision++
			case "owner":
				current.ClientID = "replacement"
			case "unloaded":
				current.Loaded = false
			case "no-anchor":
				received = time.Time{}
			}
			if got := mediaClockTiming(before, current, received, anchor.Add(time.Second)); got != (MediaClockTiming{}) {
				t.Fatalf("prior test maxima leaked across %s: %+v", name, got)
			}
		})
	}
}

func TestMediaPlaybackUnchangedPausedHeartbeatDoesNotRearm(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	previous := MediaPlaybackUpdate{ClientID: "player:test", Sequence: 1, Loaded: true, Rate: 1, Epoch: 3, PlanRevision: 1, PositionMS: 100}
	runtime.mediaPlayback.snapshot.MediaPlaybackUpdate = previous
	runtime.mediaPlayback.received = time.Now()
	// Deliberately no timeline worker: an acknowledged, unchanged pause must
	// not require another scheduler round-trip just to renew its lease.
	runtime.mediaTimeline.status = MediaTimelineStatus{ClientID: previous.ClientID, Revision: 1, StepCount: 814,
		State: "paused", ArmedEpoch: 3, ClockSequence: 1, ClockPositionMS: 100}
	value := previous
	value.Sequence++
	snapshot, err := runtime.UpdateMediaPlayback(value)
	if err != nil || snapshot.Sequence != 2 || snapshot.Timeline.ClockSequence != 1 {
		t.Fatalf("unchanged paused heartbeat unnecessarily rearmed: %+v, %v", snapshot, err)
	}
}

func TestMediaTimelinePausedHeartbeatRequiresAcknowledgedUnchangedPause(t *testing.T) {
	value := MediaPlaybackUpdate{ClientID: "player:test", Sequence: 2, Loaded: true, Rate: 1, Epoch: 3, PlanRevision: 1, PositionMS: 100}
	previous := value
	previous.Sequence = 1
	plan := MediaTimelineStatus{ClientID: value.ClientID, Revision: 1, State: "paused", ArmedEpoch: 3, ClockSequence: 1, ClockPositionMS: 100}
	if !mediaTimelinePausedHeartbeatArmed(plan, previous, value) {
		t.Fatal("acknowledged unchanged pause not recognized")
	}
	for _, name := range []string{"initial", "playing", "seek", "epoch", "revision", "owner", "unloaded", "faulted", "ready", "unacknowledged", "unarmed", "position"} {
		t.Run(name, func(t *testing.T) {
			before, current, status := previous, value, plan
			switch name {
			case "initial":
				before.Loaded = false
			case "playing":
				before.Playing = true
			case "seek":
				current.PositionMS++
			case "epoch":
				current.Epoch++
			case "revision":
				current.PlanRevision++
			case "owner":
				current.ClientID = "other"
			case "unloaded":
				current.Loaded = false
			case "faulted":
				status.State = "faulted"
			case "ready":
				status.State = "ready"
			case "unacknowledged":
				status.ClockSequence = 0
			case "unarmed":
				status.ArmedEpoch++
			case "position":
				status.ClockPositionMS++
			}
			if mediaTimelinePausedHeartbeatArmed(status, before, current) {
				t.Fatal("required worker acknowledgement was bypassed")
			}
		})
	}
}

func TestMediaPlaybackPublishesAcceptedPauseWhenArmingFails(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	runtime.mediaTimeline.mu.Lock()
	runtime.mediaTimeline.status = MediaTimelineStatus{ClientID: "player:test", Revision: 1, StepCount: 1, State: "faulted", Error: "clock expired"}
	runtime.mediaTimeline.mu.Unlock()
	after := runtime.LatestEventID()
	value := MediaPlaybackUpdate{ClientID: "player:test", Sequence: 1, Loaded: true, Rate: 1, Epoch: 1, PlanRevision: 1}
	snapshot, err := runtime.UpdateMediaPlayback(value)
	if err == nil || snapshot.Sequence != value.Sequence || snapshot.Playing {
		t.Fatalf("accepted pause must remain visible alongside arm failure: %+v, %v", snapshot, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, "media.playback")
	if err != nil {
		t.Fatalf("accepted paused clock was not broadcast after arming failure: %v", err)
	}
	if event.State != "paused" || event.Stream != "state" || event.Source != value.ClientID || event.Metadata["sequence"] != "1" {
		t.Fatalf("wrong accepted pause event: %+v", event)
	}
}

func TestMediaPlaybackValidationAndClockPayload(t *testing.T) {
	duration := uint64(12_000)
	value := MediaPlaybackUpdate{ClientID: "player:one", Sequence: 1, Loaded: true, Playing: true, PositionMS: 10_000, DurationMS: &duration, Rate: 2}
	if err := value.validate(); err != nil {
		t.Fatal(err)
	}
	cells := mediaClockCells(value, 250*time.Millisecond)
	if cells != [4]byte{0x3F, 0x3F, 0x06, 0x3F} {
		t.Fatalf("clock cells % X", cells)
	}
	value.Playing = false
	if paused := mediaClockCells(value, time.Second); paused != [4]byte{0x3F, 0xBF, 0x06, 0x3F} {
		t.Fatalf("paused cells % X", paused)
	}
	value.Playing = true
	if capped := mediaClockCells(value, 10*time.Second); capped != [4]byte{0x3F, 0xBF, 0x06, 0x5B} {
		t.Fatalf("duration-capped cells % X", capped)
	}
	value.Rate = 0
	if value.validate() == nil {
		t.Fatal("accepted invalid speed")
	}
	value.Rate = 1
	value.Loaded = false
	if value.validate() == nil {
		t.Fatal("accepted playing unloaded media")
	}
}
func TestMediaPlaybackRejectsStaleSequenceAndCompetingOwner(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	update := MediaPlaybackUpdate{ClientID: "player:a", Sequence: 1, Loaded: true, Rate: 1}
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.UpdateMediaPlayback(update); err == nil {
		t.Fatal("accepted replay")
	}
	update.ClientID = "player:b"
	if _, err := runtime.UpdateMediaPlayback(update); err == nil {
		t.Fatal("accepted competing live owner")
	}
	update.ClientID = "player:a"
	update.Sequence = 2
	update.Loaded = false
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	update.ClientID = "player:b"
	update.Sequence = 1
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
	runtime.mediaPlayback.mu.Unlock()
	if runtime.MediaPlayback().Connected {
		t.Fatal("expired owner is still connected")
	}
}
func TestMediaPlaybackFormattedSegments(t *testing.T) {
	for _, test := range []struct {
		position uint64
		playing  bool
		cells    [4]byte
	}{
		{65_000, false, [4]byte{0x3f, 0x86, 0x3f, 0x6d}},   // 01:05, steady colon
		{65_500, true, [4]byte{0x3f, 0x06, 0x3f, 0x6d}},    // blinking colon off
		{6_000_000, true, [4]byte{0x3f, 0x86, 0x66, 0x3f}}, // 01:40 after 99:59
	} {
		p := mediaClockCells(MediaPlaybackUpdate{PositionMS: test.position, Loaded: true, Playing: test.playing, Rate: 1}, 0)
		for i, expected := range test.cells {
			if p[i] != expected {
				t.Fatalf("%d cell %d: %x != %x", test.position, i, p[i], expected)
			}
		}
	}
}
func TestMediaPlaybackBoardACKAndExpiredProgramClaim(t *testing.T) {
	runtime := New(Options{RequestTimeout: time.Second})
	defer runtime.Close()
	port := newProgramStateWirePort()
	runtime.attach(link.OpenResult{Session: link.NewForPort("MEDIA-TEST", port), Port: ports.Info{Name: "MEDIA-TEST"}, Hello: native.Hello{Name: "PCController", Capabilities: native.CapabilityProgramState | native.CapabilityScheduledSegments}})
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "player:test", Sequence: 1, Loaded: true, Playing: true, PositionMS: 65000, Rate: 1}); err != nil {
		t.Fatal(err)
	}
	waitClock := func(loaded bool) {
		t.Helper()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case frame := <-port.writes:
				if frame.Opcode == native.OpDisplayText && len(frame.Payload) >= 8 && frame.Payload[0] == native.DisplayScheduledSegments {
					if loaded && len(frame.Payload) == 12 && frame.Payload[3] == 4 && frame.Payload[4] == native.SegmentRawCells {
						return
					}
					if !loaded && len(frame.Payload) == 8 && frame.Payload[3] == 0 {
						return
					}
				}
			case <-timer.C:
				t.Fatalf("no acknowledged media display loaded=%v", loaded)
			}
		}
	}
	waitClock(true)
	deadline := time.Now().Add(time.Second)
	for !runtime.MediaPlayback().BoardSynced && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !runtime.MediaPlayback().BoardSynced {
		t.Fatal("native ACK not reported")
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
	runtime.mediaPlayback.mu.Unlock()
	runtime.mediaPlayback.wake <- struct{}{}
	waitClock(false)
	if runtime.ProgramState().Mode != ProgramIdle {
		t.Fatal("expired playback retained Running claim")
	}
	if runtime.MediaPlayback().Connected || runtime.MediaPlayback().BoardSynced {
		t.Fatal("expired playback reported live synchronization")
	}
}
