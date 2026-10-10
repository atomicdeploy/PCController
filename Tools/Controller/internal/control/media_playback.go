package control

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/native"
)

const mediaPlaybackLease = 3 * time.Second

// A paused clock update must not return before the timeline worker has observed
// and armed its epoch. Clients can send Play immediately after that return.
const mediaTimelineArmAckTimeout = 250 * time.Millisecond

type MediaPlaybackUpdate struct {
	ClientID     string  `json:"client_id"`
	Sequence     uint64  `json:"sequence"`
	PositionMS   uint64  `json:"position_ms"`
	DurationMS   *uint64 `json:"duration_ms,omitempty"`
	Playing      bool    `json:"playing"`
	Loaded       bool    `json:"loaded"`
	Rate         float64 `json:"rate"`
	Epoch        uint64  `json:"epoch,omitempty"`
	PlanRevision uint64  `json:"plan_revision,omitempty"`
}
type MediaPlaybackSnapshot struct {
	MediaPlaybackUpdate
	ClockTiming      MediaClockTiming    `json:"clock_timing"`
	ReceivedAt       time.Time           `json:"received_at"`
	Connected        bool                `json:"connected"`
	BoardSynced      bool                `json:"board_synced"`
	BoardError       string              `json:"board_error,omitempty"`
	BoardSyncedAt    time.Time           `json:"board_synced_at,omitempty"`
	BoardSequence    uint64              `json:"board_sequence"`
	BoardRoundTripMS int64               `json:"board_round_trip_ms"`
	Timeline         MediaTimelineStatus `json:"timeline"`
}

// Clock corrections are measured at admission, before an executor can miss an
// intervening update. These are per uninterrupted playing epoch, not cumulative
// maxima from earlier tests. Receipt times retain Go's monotonic clock.
type MediaClockTiming struct {
	FeedbackIntervalMS     float64 `json:"feedback_interval_ms"`
	MaxFeedbackIntervalMS  float64 `json:"max_feedback_interval_ms"`
	PositionCorrectionMS   float64 `json:"position_correction_ms"`
	MaxForwardCorrectionMS float64 `json:"max_forward_correction_ms"`
}

func mediaClockTiming(previous MediaPlaybackSnapshot, value MediaPlaybackUpdate, received, now time.Time) MediaClockTiming {
	if received.IsZero() || !previous.Loaded || !value.Loaded || !previous.Playing || !value.Playing ||
		previous.ClientID != value.ClientID || previous.PlanRevision != value.PlanRevision || previous.Epoch != value.Epoch {
		return MediaClockTiming{}
	}
	timing := previous.ClockTiming
	timing.FeedbackIntervalMS = now.Sub(received).Seconds() * 1000
	timing.MaxFeedbackIntervalMS = max(timing.MaxFeedbackIntervalMS, timing.FeedbackIntervalMS)
	projected := float64(previous.PositionMS) + timing.FeedbackIntervalMS*previous.Rate
	timing.PositionCorrectionMS = float64(value.PositionMS) - projected
	timing.MaxForwardCorrectionMS = max(timing.MaxForwardCorrectionMS, timing.PositionCorrectionMS)
	return timing
}

type mediaPlaybackState struct {
	operation sync.Mutex
	authority MediaAuthorityStatus
	mu        sync.Mutex
	snapshot  MediaPlaybackSnapshot
	received  time.Time
	running   bool
	wake      chan struct{}
}

func (value MediaPlaybackUpdate) validate() error {
	if strings.TrimSpace(value.ClientID) == "" || len(value.ClientID) > 180 || value.Sequence == 0 {
		return errors.New("registered client_id and positive sequence are required")
	}
	if value.PositionMS > math.MaxUint32 || (value.DurationMS != nil && *value.DurationMS > math.MaxUint32) {
		return errors.New("media clock values must fit unsigned 32-bit milliseconds")
	}
	if math.IsNaN(value.Rate) || math.IsInf(value.Rate, 0) || value.Rate < .25 || value.Rate > 4 {
		return errors.New("media rate must be 0.25..4")
	}
	if value.Playing && !value.Loaded {
		return errors.New("unloaded media cannot be playing")
	}
	return nil
}
func copyMediaUpdate(value MediaPlaybackUpdate) MediaPlaybackUpdate {
	if value.DurationMS != nil {
		duration := *value.DurationMS
		value.DurationMS = &duration
	}
	return value
}
func (runtime *Runtime) MediaPlayback() MediaPlaybackSnapshot {
	state := &runtime.mediaPlayback
	state.mu.Lock()
	result := state.snapshot
	result.MediaPlaybackUpdate = copyMediaUpdate(result.MediaPlaybackUpdate)
	result.Connected = !state.received.IsZero() && time.Since(state.received) < mediaPlaybackLease
	if !result.Connected {
		result.BoardSynced = false
	}
	state.mu.Unlock()
	result.Timeline = runtime.MediaTimeline()
	return result
}

// Internal schedulers retain Go's monotonic anchor. UTC is for API display only.
func (runtime *Runtime) mediaTimelineClock() (MediaPlaybackUpdate, time.Time, MediaClockTiming) {
	runtime.mediaPlayback.mu.Lock()
	defer runtime.mediaPlayback.mu.Unlock()
	return copyMediaUpdate(runtime.mediaPlayback.snapshot.MediaPlaybackUpdate), runtime.mediaPlayback.received, runtime.mediaPlayback.snapshot.ClockTiming
}
func (runtime *Runtime) UpdateMediaPlayback(value MediaPlaybackUpdate) (MediaPlaybackSnapshot, error) {
	runtime.mediaPlayback.operation.Lock()
	defer runtime.mediaPlayback.operation.Unlock()
	if err := value.validate(); err != nil {
		return runtime.MediaPlayback(), err
	}
	if value.PlanRevision != 0 {
		plan := runtime.MediaTimeline()
		if plan.ClientID != value.ClientID || plan.Revision != value.PlanRevision {
			return runtime.MediaPlayback(), errors.New("media clock requires the acknowledged prepared timeline revision")
		}
		if value.Playing && plan.StepCount > 0 && (plan.State == "faulted" || plan.State == "stopped" || plan.ArmedEpoch != value.Epoch || plan.ClockSequence == 0) {
			return runtime.MediaPlayback(), errors.New("hardware timeline is not armed for this media epoch; pause and reprepare")
		}
	} else if value.Playing {
		plan := runtime.MediaTimeline()
		if plan.ClientID == value.ClientID && plan.StepCount > 0 && plan.State != "stopped" {
			return runtime.MediaPlayback(), errors.New("playing media must acknowledge its prepared hardware timeline")
		}
	}
	state := &runtime.mediaPlayback
	state.mu.Lock()
	if err := runtime.authorityAdmissionLocked(value.ClientID); err != nil {
		state.mu.Unlock()
		return runtime.MediaPlayback(), err
	}
	fresh := !state.received.IsZero() && time.Since(state.received) < mediaPlaybackLease
	previous := state.snapshot
	if fresh && previous.ClientID != value.ClientID && previous.Loaded {
		state.mu.Unlock()
		return runtime.MediaPlayback(), errors.New("another live client owns the board media clock")
	}
	if fresh && previous.ClientID == value.ClientID && value.Sequence <= previous.Sequence {
		state.mu.Unlock()
		return runtime.MediaPlayback(), errors.New("stale media playback sequence")
	}
	received := time.Now()
	clockTiming := mediaClockTiming(previous, value, state.received, received)
	state.received = received
	if value.Loaded && state.authority.OwnerID == "" {
		state.authority.OwnerID = value.ClientID
		state.authority.OwnerLabel = value.ClientID
		state.authority.Revision++
	}
	if !value.Loaded && state.authority.OwnerID == value.ClientID && !state.authority.Exclusive {
		state.authority.OwnerID = ""
		state.authority.OwnerLabel = ""
		state.authority.Revision++
	}
	state.snapshot = MediaPlaybackSnapshot{MediaPlaybackUpdate: copyMediaUpdate(value), ReceivedAt: state.received.UTC(), Connected: true,
		ClockTiming: clockTiming,
		BoardSynced: previous.BoardSynced, BoardError: previous.BoardError, BoardSyncedAt: previous.BoardSyncedAt,
		BoardSequence: previous.BoardSequence, BoardRoundTripMS: previous.BoardRoundTripMS}
	if state.wake == nil {
		state.wake = make(chan struct{}, 1)
	}
	start := !state.running
	state.running = true
	state.mu.Unlock()
	if start {
		go runtime.runMediaPlayback()
	}
	select {
	case state.wake <- struct{}{}:
	default:
	}
	// The clock state was accepted above even if the executor cannot arm it.
	// Publish that state before waiting so every client sees a requested pause.
	duration := ""
	if value.DurationMS != nil {
		duration = strconv.FormatUint(*value.DurationMS, 10)
	}
	runtime.PublishStructuredEvent(Event{Kind: "media.playback", Stream: "state", Source: value.ClientID,
		State: mediaPlaybackEventState(value),
		Metadata: map[string]string{"client_id": value.ClientID, "sequence": strconv.FormatUint(value.Sequence, 10),
			"position_ms": strconv.FormatUint(value.PositionMS, 10), "duration_ms": duration,
			"loaded": strconv.FormatBool(value.Loaded), "playing": strconv.FormatBool(value.Playing), "rate": strconv.FormatFloat(value.Rate, 'f', -1, 64)}})
	if !value.Playing && value.PlanRevision != 0 {
		plan := runtime.MediaTimeline()
		if plan.ClientID == value.ClientID && plan.Revision == value.PlanRevision && plan.StepCount > 0 &&
			!(fresh && mediaTimelinePausedHeartbeatArmed(plan, previous.MediaPlaybackUpdate, value)) {
			deadline := time.NewTimer(mediaTimelineArmAckTimeout)
			poll := time.NewTicker(time.Millisecond)
			defer deadline.Stop()
			defer poll.Stop()
			for {
				plan = runtime.MediaTimeline()
				if plan.ClientID != value.ClientID || plan.Revision != value.PlanRevision {
					return runtime.MediaPlayback(), errors.New("prepared media timeline changed while arming its paused clock")
				}
				if plan.State == "faulted" {
					return runtime.MediaPlayback(), fmt.Errorf("prepared media timeline could not arm paused clock: %s", plan.Error)
				}
				if plan.ArmedEpoch == value.Epoch && plan.ClockSequence >= value.Sequence {
					break
				}
				select {
				case <-deadline.C:
					return runtime.MediaPlayback(), fmt.Errorf("prepared media timeline did not acknowledge paused clock sequence %d / epoch %d within %s", value.Sequence, value.Epoch, mediaTimelineArmAckTimeout)
				case <-poll.C:
				}
			}
		}
	}
	return runtime.MediaPlayback(), nil
}

// An unchanged paused heartbeat renews the clock lease, not the arm operation.
// The worker has already acknowledged this exact paused epoch and position.
// Initial pause, seek, plan replacement and playing-to-paused cleanup still
// require a fresh worker acknowledgement before Play may be accepted.
func mediaTimelinePausedHeartbeatArmed(plan MediaTimelineStatus, previous, value MediaPlaybackUpdate) bool {
	return previous.Loaded && value.Loaded && !previous.Playing && !value.Playing &&
		previous.ClientID == value.ClientID && previous.PlanRevision == value.PlanRevision &&
		previous.Epoch == value.Epoch && previous.PositionMS == value.PositionMS &&
		plan.ClientID == value.ClientID && plan.Revision == value.PlanRevision &&
		plan.State == "paused" && plan.ClockSequence > 0 && plan.ArmedEpoch == value.Epoch &&
		plan.ClockPositionMS == value.PositionMS
}

func mediaPlaybackEventState(value MediaPlaybackUpdate) string {
	if !value.Loaded {
		return "unloaded"
	}
	if value.Playing {
		return "playing"
	}
	return "paused"
}
func mediaClockCells(value MediaPlaybackUpdate, age time.Duration) [4]byte {
	position := value.PositionMS
	if value.Playing {
		position += uint64(float64(age.Milliseconds()) * value.Rate)
	}
	if value.DurationMS != nil && position > *value.DurationMS {
		position = *value.DurationMS
	}
	if position > math.MaxUint32 {
		position = math.MaxUint32
	}
	seconds := position / 1000
	left, right := seconds/60, seconds%60
	if left > 99 {
		left, right = seconds/3600, (seconds/60)%60
	}
	if left > 99 {
		left = 99
	}
	digits := [10]byte{0x3f, 0x06, 0x5b, 0x4f, 0x66, 0x6d, 0x7d, 0x07, 0x7f, 0x6f}
	cells := [4]byte{digits[left/10], digits[left%10], digits[right/10], digits[right%10]}
	if !value.Playing || position%1000 < 500 {
		cells[1] |= 0x80
	}
	return cells
}
func mediaDisplayPayload(value MediaPlaybackUpdate, age time.Duration) []byte {
	if !value.Loaded {
		return native.ScheduledSegmentReleasePayload()
	}
	return native.ScheduledSegmentRawPayload(mediaClockCells(value, age), uint16(mediaPlaybackLease/time.Millisecond))
}
func (runtime *Runtime) runMediaPlayback() {
	state := &runtime.mediaPlayback
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastSent time.Time
	var lastPlaying, lastLoaded bool
	var lastGeneration uint64
	var lastSequence uint64
	var roundTrip time.Duration
	var claimed bool
	var retryAt time.Time
	for {
		select {
		case <-ticker.C:
		case <-state.wake:
		}
		state.mu.Lock()
		value, received := copyMediaUpdate(state.snapshot.MediaPlaybackUpdate), state.received
		expired := time.Since(received) >= mediaPlaybackLease
		state.mu.Unlock()
		if expired {
			value.Playing = false
			value.Loaded = false
		}
		playing := value.Playing && value.Loaded && !runtime.emergencyStop.Load()
		value.Playing = playing
		if playing != claimed {
			mode := ProgramIdle
			if playing {
				mode = ProgramRunning
			}
			_, _ = runtime.SetProgramState("media-playback", mode, "client playback")
			claimed = playing
		}
		runtime.mu.RLock()
		connected, generation, capabilities := runtime.session != nil, runtime.generation, runtime.hello.Capabilities
		runtime.mu.RUnlock()
		changed := value.Sequence != lastSequence || value.Playing != lastPlaying || value.Loaded != lastLoaded || generation != lastGeneration
		if connected && time.Now().After(retryAt) && (changed || time.Since(lastSent) >= 200*time.Millisecond) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			started := time.Now()
			// Transit estimate is bounded and its measured uncertainty stays visible.
			transit := roundTrip / 2
			if transit > 100*time.Millisecond {
				transit = 100 * time.Millisecond
			}
			var err error
			if capabilities&native.CapabilityScheduledSegments == 0 {
				err = errors.New("connected firmware does not advertise scheduled segment messages; flash the current firmware first")
			} else {
				// Playback display refresh is an owned internal presentation. It
				// must neither be recorded as a timeline action nor be rejected as
				// a competing anonymous live display command.
				commandContext := context.WithValue(WithMediaActor(ctx, value.ClientID), mediaTimelineContextKey{}, true)
				err = runtime.Command(commandContext, native.OpDisplayText, mediaDisplayPayload(value, time.Since(received)+transit))
			}
			cancel()
			lastSent = time.Now()
			lastPlaying, lastLoaded, lastGeneration, lastSequence = value.Playing, value.Loaded, generation, value.Sequence
			state.mu.Lock()
			state.snapshot.BoardSynced = err == nil
			state.snapshot.BoardError = ""
			if err != nil {
				state.snapshot.BoardError = fmt.Sprintf("media clock not acknowledged: %v", err)
				retryAt = time.Now().Add(time.Second)
			} else {
				retryAt = time.Time{}
				roundTrip = lastSent.Sub(started)
				state.snapshot.BoardRoundTripMS = roundTrip.Milliseconds()
				state.snapshot.BoardSyncedAt = lastSent.UTC()
				state.snapshot.BoardSequence = value.Sequence
			}
			state.mu.Unlock()
		} else if !connected {
			state.mu.Lock()
			state.snapshot.BoardSynced = false
			state.snapshot.BoardError = "board disconnected"
			state.mu.Unlock()
		}
		if expired {
			state.mu.Lock()
			if state.received.Equal(received) {
				state.running = false
				state.snapshot.Connected = false
				state.snapshot.Playing = false
				state.snapshot.Loaded = false
				state.mu.Unlock()
				return
			}
			state.mu.Unlock()
		}
	}
}
