package control

import (
	"strconv"
	"time"
)

// MotionSimulationSnapshot is a travel estimate from the native board's
// applied relay feedback. A position of 0 is fully down and 1 is fully up.
type MotionSimulationSnapshot struct {
	LeftPosition  float64   `json:"left_position"`
	RightPosition float64   `json:"right_position"`
	FullTravelMS  int64     `json:"full_travel_ms"`
	SampledAt     time.Time `json:"sampled_at"`
}

// Each connection starts at mid-travel. Advance using the old applied mask
// before replacing it, so the break-before-make interval contributes no travel.
type virtualSeatPositions struct {
	valid     bool
	mask      byte
	position  [2]float64
	published [2]float64
	at        time.Time
}

func (seats *virtualSeatPositions) observe(mask byte, now time.Time, travel time.Duration) {
	if !seats.valid {
		seats.valid = true
		seats.position = [2]float64{0.5, 0.5}
		seats.published = seats.position
		seats.at = now
	}
	seats.advance(now, travel)
	seats.mask = mask
}

func (seats *virtualSeatPositions) advance(now time.Time, travel time.Duration) {
	if !seats.valid || !now.After(seats.at) {
		return
	}
	delta := float64(now.Sub(seats.at)) / float64(travel)
	for side := range seats.position {
		switch motionAppliedFromMask(seats.mask, byte(side)) {
		case "up":
			seats.position[side] = min(1, seats.position[side]+delta)
		case "down":
			seats.position[side] = max(0, seats.position[side]-delta)
		}
	}
	seats.at = now
}

func (seats virtualSeatPositions) snapshot(now time.Time, travel time.Duration) *MotionSimulationSnapshot {
	seats.advance(now, travel)
	return &MotionSimulationSnapshot{
		LeftPosition: seats.position[0], RightPosition: seats.position[1],
		FullTravelMS: travel.Milliseconds(), SampledAt: seats.at,
	}
}

func (seats virtualSeatPositions) traveling() bool {
	for side, position := range seats.position {
		switch motionAppliedFromMask(seats.mask, byte(side)) {
		case "up":
			if position < 1 {
				return true
			}
		case "down":
			if position > 0 {
				return true
			}
		}
	}
	return false
}

const virtualSeatSampleInterval = 100 * time.Millisecond

// Existing motion.changed events publish stops and interlock edges immediately.
// Schedule only while the position can change, including the last limit sample.
func (runtime *Runtime) scheduleVirtualSeatSampleLocked() {
	if runtime.virtualSeats.mask&0x0a == 0 && runtime.virtualSeatTimer != nil {
		runtime.virtualSeatTimer.Stop()
		runtime.virtualSeatTimer = nil
	}
	if runtime.virtualSeatTimer != nil || !runtime.virtualSeats.traveling() {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(virtualSeatSampleInterval, func() {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		if runtime.virtualSeatTimer != timer {
			return
		}
		runtime.virtualSeatTimer = nil
		if !runtime.sessionUsableLocked() {
			return
		}
		runtime.virtualSeats.advance(time.Now(), runtime.options.VirtualSeatTravel)
		if runtime.virtualSeats.published != runtime.virtualSeats.position {
			runtime.virtualSeats.published = runtime.virtualSeats.position
			runtime.publishEvent(Event{
				Kind: "motion.simulation", Stream: EventStreamState,
				Source: "host", Target: "app.clients", MessageType: "event",
				Metadata: map[string]string{"connection_generation": strconv.FormatUint(runtime.generation, 10)},
			})
		}
		runtime.scheduleVirtualSeatSampleLocked()
	})
	runtime.virtualSeatTimer = timer
}
