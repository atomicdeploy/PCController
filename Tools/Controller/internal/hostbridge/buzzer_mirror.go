package hostbridge

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/pcspeaker"
)

type buzzerMirrorJob struct {
	config       appconfig.BuzzerMirror
	frequencyHz  int
	durationMS   int
	deviceMicros uint32
	timed        bool
	observedAt   time.Time
	source       string
}

const maxBuzzerSourceGap = 5 * time.Minute

type buzzerPlaybackPlan struct {
	start time.Time
	end   time.Time
}

type buzzerTimelineAnchor struct {
	deviceMicros uint32
	start        time.Time
}

type buzzerPlaybackTimeline struct {
	anchors map[string]buzzerTimelineAnchor
}

func newBuzzerPlaybackTimeline() *buzzerPlaybackTimeline {
	return &buzzerPlaybackTimeline{anchors: make(map[string]buzzerTimelineAnchor)}
}

func (timeline *buzzerPlaybackTimeline) plan(job buzzerMirrorJob, now time.Time) buzzerPlaybackPlan {
	observedAt := job.observedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	start := observedAt
	if job.timed {
		if previous, ok := timeline.anchors[job.source]; ok {
			delta := time.Duration(uint32(job.deviceMicros-previous.deviceMicros)) * time.Microsecond
			if delta <= maxBuzzerSourceGap {
				start = previous.start.Add(delta)
			}
		}
		timeline.anchors[job.source] = buzzerTimelineAnchor{
			deviceMicros: job.deviceMicros,
			start:        start,
		}
	}
	return buzzerPlaybackPlan{
		start: start,
		end:   start.Add(time.Duration(job.durationMS) * time.Millisecond),
	}
}

func (plan buzzerPlaybackPlan) remaining(now time.Time) (time.Duration, bool) {
	if !plan.end.After(now) {
		return 0, false
	}
	if plan.start.After(now) {
		return plan.end.Sub(plan.start), true
	}
	return plan.end.Sub(now), true
}

func buzzerMirrorJobFor(config appconfig.BuzzerMirror, event controller.Event) (buzzerMirrorJob, bool) {
	if !config.Enabled || !config.NativeEnabled || event.Kind != "buzzer.note" {
		return buzzerMirrorJob{}, false
	}
	frequencyHz, frequencyErr := strconv.Atoi(event.Metadata["frequency_hz"])
	durationMS, durationErr := strconv.Atoi(event.Metadata["duration_ms"])
	if frequencyErr != nil || durationErr != nil || frequencyHz < 0 || durationMS < 0 ||
		(durationMS == 0 && frequencyHz != 0) {
		return buzzerMirrorJob{}, false
	}
	job := buzzerMirrorJob{
		config: config, frequencyHz: frequencyHz, durationMS: durationMS,
		observedAt: event.Time, source: "local-board",
	}
	if ingress := strings.TrimSpace(event.Metadata["bridge.ingress"]); ingress != "" {
		job.source = "bridge:" + ingress
	}
	if raw := strings.TrimSpace(event.Metadata["device_micros"]); raw != "" {
		deviceMicros, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return buzzerMirrorJob{}, false
		}
		job.deviceMicros = uint32(deviceMicros)
		job.timed = true
	}
	return job, true
}

func (manager *Manager) dispatchBuzzerMirror(config appconfig.Config, event controller.Event) {
	job, ok := buzzerMirrorJobFor(config.Integrations.BuzzerMirror, event)
	if !ok {
		return
	}
	// Configure-time resolution is authoritative for auto mode. Reusing it
	// avoids probing the native device before launching the already-selected
	// optional external helper for every pushed note.
	manager.mu.RLock()
	resolvedState := manager.status.BuzzerNativeState
	resolvedBackend := manager.status.BuzzerNativeBackend
	resolvedExecutable := manager.status.BuzzerNativeExecutable
	manager.mu.RUnlock()
	if resolvedState == "ready" && resolvedBackend != "" {
		job.config.Backend = resolvedBackend
		if resolvedExecutable != "" {
			job.config.Executable = resolvedExecutable
		}
	}
	select {
	case manager.buzzerJobs <- job:
	default:
		manager.recordNativeBuzzerResult(errors.New("native buzzer mirror queue is full; note dropped"))
	}
}

func (manager *Manager) buzzerMirrorLoop() {
	defer manager.wait.Done()
	timeline := newBuzzerPlaybackTimeline()
	type scheduledPlayback struct {
		job   buzzerMirrorJob
		plan  buzzerPlaybackPlan
		order uint64
	}
	type activePlayback struct {
		source string
		cancel context.CancelFunc
		done   chan error
	}
	var pending []scheduledPlayback
	var replacement *scheduledPlayback
	var active *activePlayback
	var order uint64
	var timer *time.Timer
	var timerChannel <-chan time.Time
	player := manager.buzzerPlay
	if player == nil {
		player = playNativeBuzzer
	}
	stopTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerChannel = nil
	}
	armTimer := func(at time.Time) {
		stopTimer()
		delay := time.Until(at)
		if delay < 0 {
			delay = 0
		}
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerChannel = timer.C
	}
	start := func(next scheduledPlayback) bool {
		remaining, audible := next.plan.remaining(time.Now())
		if !audible || next.job.frequencyHz == 0 {
			return false
		}
		next.job.durationMS = int((remaining + time.Millisecond - 1) / time.Millisecond)
		playContext, cancel := context.WithDeadline(manager.ctx, next.plan.end)
		done := make(chan error, 1)
		active = &activePlayback{source: next.job.source, cancel: cancel, done: done}
		go func() { done <- player(playContext, next.job) }()
		return true
	}
	finishActive := func(err error) {
		current := active
		active = nil
		current.cancel()
		if (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) &&
			manager.ctx.Err() == nil {
			err = nil
		}
		if manager.ctx.Err() == nil {
			manager.recordNativeBuzzerResult(err)
		}
	}
	defer stopTimer()
	for {
		now := time.Now()
		if active == nil {
			if replacement != nil {
				next := *replacement
				replacement = nil
				if start(next) {
					continue
				}
			}
			if len(pending) != 0 && !pending[0].plan.start.After(now) {
				next := pending[0]
				pending = pending[1:]
				if start(next) {
					continue
				}
				continue
			}
		}

		var nextTransition time.Time
		if active == nil {
			if len(pending) != 0 {
				nextTransition = pending[0].plan.start
			}
		} else {
			for _, candidate := range pending {
				if candidate.job.source == active.source {
					nextTransition = candidate.plan.start
					break
				}
			}
		}
		if nextTransition.IsZero() {
			stopTimer()
		} else {
			armTimer(nextTransition)
		}

		var activeDone <-chan error
		if active != nil {
			activeDone = active.done
		}
		select {
		case <-manager.ctx.Done():
			if active != nil {
				active.cancel()
				<-active.done
			}
			return
		case job := <-manager.buzzerJobs:
			plan := timeline.plan(job, time.Now())
			order++
			pending = append(pending, scheduledPlayback{job: job, plan: plan, order: order})
			sort.SliceStable(pending, func(left, right int) bool {
				if pending[left].plan.start.Equal(pending[right].plan.start) {
					return pending[left].order < pending[right].order
				}
				return pending[left].plan.start.Before(pending[right].plan.start)
			})
		case err := <-activeDone:
			finishActive(err)
		case <-timerChannel:
			timerChannel = nil
			if active == nil {
				continue
			}
			now = time.Now()
			for index := 0; index < len(pending); {
				candidate := pending[index]
				if candidate.plan.start.After(now) {
					break
				}
				if candidate.job.source != active.source {
					index++
					continue
				}
				selected := candidate
				replacement = &selected
				pending = append(pending[:index], pending[index+1:]...)
			}
			if replacement != nil {
				active.cancel()
			}
		}
	}
}

func (manager *Manager) recordNativeBuzzerResult(playbackErr error) {
	manager.mu.Lock()
	previousState := manager.status.BuzzerNativeState
	previousError := manager.status.BuzzerNativeLastError
	kind, text := "buzzer.host.ready", "native host buzzer playback recovered"
	if playbackErr == nil {
		manager.status.BuzzerNativeState = "ready"
		manager.status.BuzzerNativeLastError = ""
	} else {
		text = playbackErr.Error()
		kind = "buzzer.host.failed"
		manager.status.BuzzerNativeState = "failed"
		manager.status.BuzzerNativeLastError = text
		manager.status.LastError = "native buzzer mirror: " + text
	}
	changed := previousState != manager.status.BuzzerNativeState || previousError != manager.status.BuzzerNativeLastError
	manager.mu.Unlock()
	if changed {
		manager.client.EmitHostEvent(kind, text)
	}
}

func playNativeBuzzer(parent context.Context, job buzzerMirrorJob) error {
	return pcspeaker.PlayConfigured(
		parent, job.config.DriverDirectory, job.config.Backend,
		job.config.Executable, job.frequencyHz, job.durationMS,
	)
}

func resolveNativeBuzzer(config appconfig.BuzzerMirror) (pcspeaker.BackendStatus, error) {
	return pcspeaker.ResolveBackend(config.DriverDirectory, config.Backend, config.Executable)
}
