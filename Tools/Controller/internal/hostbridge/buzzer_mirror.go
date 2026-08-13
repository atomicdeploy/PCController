package hostbridge

import (
	"context"
	"errors"
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
	if frequencyErr != nil || durationErr != nil || frequencyHz < 0 || durationMS <= 0 {
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
	for {
		select {
		case <-manager.ctx.Done():
			return
		case job := <-manager.buzzerJobs:
			plan := timeline.plan(job, time.Now())
			if delay := time.Until(plan.start); delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-manager.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			remaining, audible := plan.remaining(time.Now())
			if !audible || job.frequencyHz == 0 {
				continue
			}
			job.durationMS = int((remaining + time.Millisecond - 1) / time.Millisecond)
			playContext, cancel := context.WithDeadline(manager.ctx, plan.end)
			err := playNativeBuzzer(playContext, job)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) && manager.ctx.Err() == nil {
				// The absolute source deadline intentionally cuts off backend
				// startup/command overhead instead of extending the note.
				err = nil
			}
			if manager.ctx.Err() == nil {
				manager.recordNativeBuzzerResult(err)
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
