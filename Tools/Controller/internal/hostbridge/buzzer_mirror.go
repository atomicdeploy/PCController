package hostbridge

import (
	"context"
	"errors"
	"strconv"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/pcspeaker"
)

type buzzerMirrorJob struct {
	config      appconfig.BuzzerMirror
	frequencyHz int
	durationMS  int
}

func buzzerMirrorJobFor(config appconfig.BuzzerMirror, event controller.Event) (buzzerMirrorJob, bool) {
	if !config.Enabled || !config.NativeEnabled || event.Kind != "buzzer.note" {
		return buzzerMirrorJob{}, false
	}
	frequencyHz, frequencyErr := strconv.Atoi(event.Metadata["frequency_hz"])
	durationMS, durationErr := strconv.Atoi(event.Metadata["duration_ms"])
	if frequencyErr != nil || durationErr != nil || frequencyHz == 0 || durationMS == 0 {
		return buzzerMirrorJob{}, false
	}
	return buzzerMirrorJob{config: config, frequencyHz: frequencyHz, durationMS: durationMS}, true
}

func (manager *Manager) dispatchBuzzerMirror(config appconfig.Config, event controller.Event) {
	job, ok := buzzerMirrorJobFor(config.Integrations.BuzzerMirror, event)
	if !ok {
		return
	}
	select {
	case manager.buzzerJobs <- job:
	default:
		manager.recordNativeBuzzerResult(errors.New("native buzzer mirror queue is full; note dropped"))
	}
}

func (manager *Manager) buzzerMirrorLoop() {
	defer manager.wait.Done()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case job := <-manager.buzzerJobs:
			manager.mu.RLock()
			player := manager.buzzerPlayer
			manager.mu.RUnlock()
			if player == nil {
				player = playNativeBuzzer
			}
			if err := player(manager.ctx, job); manager.ctx.Err() == nil {
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
