package control

import (
	"context"
	"encoding/binary"
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

type MediaPlaybackUpdate struct {
	ClientID   string  `json:"client_id"`
	Sequence   uint64  `json:"sequence"`
	PositionMS uint64  `json:"position_ms"`
	DurationMS *uint64 `json:"duration_ms,omitempty"`
	Playing    bool    `json:"playing"`
	Loaded     bool    `json:"loaded"`
	Rate       float64 `json:"rate"`
}
type MediaPlaybackSnapshot struct {
	MediaPlaybackUpdate
	ReceivedAt       time.Time `json:"received_at"`
	Connected        bool      `json:"connected"`
	BoardSynced      bool      `json:"board_synced"`
	BoardError       string    `json:"board_error,omitempty"`
	BoardSyncedAt    time.Time `json:"board_synced_at,omitempty"`
	BoardSequence    uint64    `json:"board_sequence"`
	BoardRoundTripMS int64     `json:"board_round_trip_ms"`
}
type mediaPlaybackState struct {
	mu       sync.Mutex
	snapshot MediaPlaybackSnapshot
	received time.Time
	running  bool
	wake     chan struct{}
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
	defer state.mu.Unlock()
	result := state.snapshot
	result.MediaPlaybackUpdate = copyMediaUpdate(result.MediaPlaybackUpdate)
	result.Connected = !state.received.IsZero() && time.Since(state.received) < mediaPlaybackLease
	if !result.Connected {
		result.BoardSynced = false
	}
	return result
}
func (runtime *Runtime) UpdateMediaPlayback(value MediaPlaybackUpdate) (MediaPlaybackSnapshot, error) {
	if err := value.validate(); err != nil {
		return runtime.MediaPlayback(), err
	}
	state := &runtime.mediaPlayback
	state.mu.Lock()
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
	state.received = time.Now()
	state.snapshot = MediaPlaybackSnapshot{MediaPlaybackUpdate: copyMediaUpdate(value), ReceivedAt: state.received.UTC(), Connected: true,
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
	duration := ""
	if value.DurationMS != nil {
		duration = strconv.FormatUint(*value.DurationMS, 10)
	}
	runtime.PublishStructuredEvent(Event{Kind: "media.playback", Stream: "state", Source: value.ClientID,
		State: mediaPlaybackEventState(value),
		Metadata: map[string]string{"client_id": value.ClientID, "sequence": strconv.FormatUint(value.Sequence, 10),
			"position_ms": strconv.FormatUint(value.PositionMS, 10), "duration_ms": duration,
			"loaded": strconv.FormatBool(value.Loaded), "playing": strconv.FormatBool(value.Playing), "rate": strconv.FormatFloat(value.Rate, 'f', -1, 64)}})
	return runtime.MediaPlayback(), nil
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
func mediaClockPayload(value MediaPlaybackUpdate, age time.Duration) []byte {
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
	payload := make([]byte, 14)
	payload[0] = 3
	if value.Loaded {
		payload[1] |= 1
	}
	if value.Playing {
		payload[1] |= 2
	}
	binary.LittleEndian.PutUint32(payload[2:6], uint32(position))
	binary.LittleEndian.PutUint16(payload[6:8], uint16(math.Round(value.Rate*256)))
	binary.LittleEndian.PutUint16(payload[8:10], uint16(mediaPlaybackLease/time.Millisecond))
	seconds := position / 1000
	left, right := seconds/60, seconds%60
	if left > 99 {
		left, right = seconds/3600, (seconds/60)%60
	}
	if left > 99 {
		left = 99
	}
	digits := [10]byte{0x3f, 0x06, 0x5b, 0x4f, 0x66, 0x6d, 0x7d, 0x07, 0x7f, 0x6f}
	payload[10], payload[11], payload[12], payload[13] = digits[left/10], digits[left%10], digits[right/10], digits[right%10]
	if !value.Playing || position%1000 < 500 {
		payload[11] |= 0x80
	}
	return payload
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
		connected, generation := runtime.session != nil, runtime.generation
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
			err := runtime.Command(ctx, native.OpMediaClock, mediaClockPayload(value, time.Since(received)+transit))
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
