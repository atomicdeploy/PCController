package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func (runtime *Runtime) releaseMediaTimelineWire() {
	state := &runtime.mediaTimeline
	state.mu.Lock()
	release := state.releaseWire
	state.releaseWire = nil
	state.wireRenewedAt = time.Time{}
	state.status.PlayReady = false
	state.mu.Unlock()
	if release != nil {
		release()
	}
}

func (runtime *Runtime) expirePausedMediaTimelineWire() {
	state := &runtime.mediaTimeline
	state.mu.Lock()
	if state.releaseWire == nil || time.Since(state.wireRenewedAt) <= mediaTimelineArmAckTimeout {
		state.mu.Unlock()
		return
	}
	release := state.releaseWire
	state.releaseWire = nil
	state.wireRenewedAt = time.Time{}
	state.status.PlayReady = false
	state.mu.Unlock()
	release()
}

// The timeline owns maintenance too. Refresh host presence/display only in a
// large cue-free gap; never let a separate presentation worker occupy the wire
// across a cue. Dense timelines themselves keep firmware host activity alive.
func (runtime *Runtime) serviceTimelinePresentation(ctx context.Context, value MediaPlaybackUpdate, anchor time.Time, generation uint64) {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := runtime.requestAtGenerationPriority(ctx, generation, link.RequestPriorityTimeline,
		native.OpProgramState, native.ProgramStatePayload(true), native.OpACK)
	if err == nil {
		_, err = runtime.requestAtGenerationPriority(ctx, generation, link.RequestPriorityTimeline,
			native.OpDisplayText, mediaDisplayPayload(value, time.Since(anchor)), native.OpACK)
	}
	state := &runtime.mediaPlayback
	state.mu.Lock()
	defer state.mu.Unlock()
	state.snapshot.BoardSynced = err == nil
	if err == nil {
		state.snapshot.BoardSyncedAt = time.Now().UTC()
		state.snapshot.BoardSequence = value.Sequence
		state.snapshot.BoardError = ""
	} else {
		state.snapshot.BoardError = fmt.Sprintf("timeline presentation not acknowledged: %v", err)
	}
}

// Called only after the executor acknowledged the paused epoch. Media playback
// operations serialize this with other clock updates; terminal executor state
// is rechecked after draining, because cancellation can happen concurrently.
func (runtime *Runtime) reserveMediaTimelineWire(ctx context.Context, value MediaPlaybackUpdate) error {
	state := &runtime.mediaTimeline
	state.mu.Lock()
	if state.releaseWire != nil && state.status.PlayReady {
		state.wireRenewedAt = time.Now()
		state.mu.Unlock()
		return nil
	}
	generation := state.status.Generation
	state.mu.Unlock()
	runtime.mu.RLock()
	session := runtime.session
	valid := session != nil && runtime.generation == generation && runtime.sessionUsableLocked()
	runtime.mu.RUnlock()
	if !valid {
		return errors.New("prepared board session changed before serial reservation")
	}
	release, err := session.ReserveTimeline(ctx)
	if err != nil {
		return err
	}
	runtime.mu.RLock()
	current := runtime.session == session && runtime.generation == generation && runtime.sessionUsableLocked()
	runtime.mu.RUnlock()
	state.mu.Lock()
	status := state.status
	if status.ClientID != value.ClientID || status.Revision != value.PlanRevision ||
		status.ArmedEpoch != value.Epoch || status.State != "paused" || runtime.emergencyStop.Load() || !current {
		state.mu.Unlock()
		release()
		return errors.New("prepared paused epoch changed while reserving serial channel")
	}
	state.releaseWire = release
	state.wireRenewedAt = time.Now()
	state.status.PlayReady = true
	state.mu.Unlock()
	return nil
}
