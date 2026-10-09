package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

type RFLearnMode string

const (
	RFLearnIndefinite RFLearnMode = "indefinite"
	RFLearnTimer      RFLearnMode = "timer"
)

type RFLearnOptions struct {
	Mode    RFLearnMode
	Timeout time.Duration
}

type RFLearnState struct {
	Active       bool        `json:"active"`
	Mode         RFLearnMode `json:"mode"`
	ConfiguredMS int64       `json:"configured_ms"`
	RemainingMS  int64       `json:"remaining_ms"`
	StartedAt    time.Time   `json:"started_at,omitempty"`
	EndsAt       time.Time   `json:"ends_at,omitempty"`
	Learned      uint32      `json:"learned"`
	Reason       string      `json:"reason,omitempty"`
}

// ParseRFLearnMode normalizes the two current modes and the documented timer aliases.
func ParseRFLearnMode(value string) (RFLearnMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "indefinite":
		return RFLearnIndefinite, nil
	case "timer", "single", "one-shot":
		return RFLearnTimer, nil
	default:
		return "", fmt.Errorf("RF learn mode must be indefinite or timer (aliases: single, one-shot)")
	}
}

func (runtime *Runtime) RFLearnState() RFLearnState {
	runtime.rfLearnMu.RLock()
	defer runtime.rfLearnMu.RUnlock()
	state := runtime.rfLearnState
	if state.Active && state.Mode == RFLearnTimer && !state.EndsAt.IsZero() {
		remaining := time.Until(state.EndsAt)
		if remaining < 0 {
			remaining = 0
		}
		state.RemainingMS = remaining.Milliseconds()
	}
	return state
}

func (runtime *Runtime) StartRFLearning(
	ctx context.Context,
	options RFLearnOptions,
) (RFLearnState, error) {
	mode, err := ParseRFLearnMode(string(options.Mode))
	if err != nil {
		return RFLearnState{}, err
	}
	options.Mode = mode
	if options.Mode == RFLearnTimer && options.Timeout <= 0 {
		options.Timeout = 15 * time.Second
	}
	maximum := time.Duration(native.MaxRFLearnSeconds) * time.Second
	if options.Mode == RFLearnTimer {
		options.Timeout = ((options.Timeout + time.Second - 1) / time.Second) * time.Second
		if options.Timeout > maximum {
			return RFLearnState{}, fmt.Errorf("RF learning timer exceeds %s", maximum)
		}
	}
	if options.Mode == RFLearnIndefinite {
		options.Timeout = 0
	}
	now := time.Now()
	state := RFLearnState{
		Active: true, Mode: options.Mode, StartedAt: now,
		ConfiguredMS: options.Timeout.Milliseconds(),
		RemainingMS:  options.Timeout.Milliseconds(),
	}
	if options.Mode == RFLearnTimer {
		state.EndsAt = now.Add(options.Timeout)
	}
	runtime.rfLearnMu.Lock()
	runtime.rfLearnState = state
	runtime.rfLearnMu.Unlock()
	if err := runtime.armRFLearn(ctx, options); err != nil {
		runtime.rfLearnMu.Lock()
		runtime.rfLearnState.Active = false
		runtime.rfLearnState.Reason = "start failed: " + err.Error()
		runtime.rfLearnMu.Unlock()
		return RFLearnState{}, err
	}
	return state, nil
}

func (runtime *Runtime) CancelRFLearning(
	ctx context.Context,
	reason string,
) error {
	if reason == "" {
		reason = "cancelled by host"
	}
	if err := runtime.Command(ctx, native.OpRFLearnCancel, nil); err != nil {
		return err
	}
	// The board publishes the authoritative cancelled lifecycle event. Keep a
	// bounded local fallback for transports that acknowledge before delivering it.
	runtime.rfLearnMu.Lock()
	if runtime.rfLearnState.Active {
		runtime.rfLearnState.Active = false
		runtime.rfLearnState.RemainingMS = 0
		runtime.rfLearnState.Reason = reason
	}
	runtime.rfLearnMu.Unlock()
	return nil
}

func (runtime *Runtime) armRFLearn(ctx context.Context, options RFLearnOptions) error {
	mode, seconds := native.RFLearnModeIndefinite, byte(0)
	if options.Mode == RFLearnTimer {
		mode = native.RFLearnModeTimer
		seconds = byte(options.Timeout / time.Second)
	}
	payload, err := native.RFLearnStartPayload(mode, seconds)
	if err != nil {
		return err
	}
	return runtime.Command(ctx, native.OpRFLearnStart, payload)
}

// observeRFLearningEvent projects MCU lifecycle events into the shared snapshot.
// It also returns whether a newly captured code needs an explicit host mapping.
func (runtime *Runtime) observeRFLearningEvent(event native.DeviceEvent) (mappingRequired bool, captured uint32) {
	runtime.rfLearnMu.Lock()
	defer runtime.rfLearnMu.Unlock()
	state := &runtime.rfLearnState
	if event.Type == native.EventRFLearned {
		if state.Active {
			state.Learned++
		}
		return true, state.Learned
	}
	if event.Type != native.EventRFLearning {
		return false, state.Learned
	}
	now := time.Now()
	if event.RFLearnMode == native.RFLearnModeTimer {
		state.Mode = RFLearnTimer
	} else {
		state.Mode = RFLearnIndefinite
	}
	state.ConfiguredMS = int64(event.RFLearnTotalSeconds) * 1000
	state.RemainingMS = int64(event.RFLearnRemainingSeconds) * 1000
	if state.Mode == RFLearnTimer && event.RFLearnRemainingSeconds != 0 {
		state.EndsAt = now.Add(time.Duration(event.RFLearnRemainingSeconds) * time.Second)
	} else {
		state.EndsAt = time.Time{}
	}
	switch event.RFLearnState {
	case native.RFLearningStarted:
		state.Active = true
		state.StartedAt = now
		state.Learned = 0
		state.Reason = ""
	case native.RFLearningProgress:
		state.Active = true
		if state.StartedAt.IsZero() {
			state.StartedAt = now
		}
	case native.RFLearningEnded:
		state.Active = false
		state.RemainingMS = 0
		state.Reason = "timer elapsed"
	case native.RFLearningCancelled:
		state.Active = false
		state.RemainingMS = 0
		state.Reason = "cancelled"
	case native.RFLearningFull:
		state.Active = false
		state.RemainingMS = 0
		state.Reason = "storage full"
	}
	return false, state.Learned
}

func (runtime *Runtime) publishRFMappingRequired(event native.DeviceEvent, captured uint32) {
	runtime.PublishStructuredEvent(Event{
		Kind: "rf.learn.mapping-required",
		Text: fmt.Sprintf(
			"RF entry %d captured; captured=%d; no default action is assigned; choose a mapping",
			event.RFID, captured,
		),
		Source: "board", Target: "host", MessageType: "action-required",
		RFID: event.RFID, HaveRFID: true,
		Metadata: map[string]string{
			"mapping": "unmapped", "captured": fmt.Sprintf("%d", captured),
		},
	})
}

func (runtime *Runtime) markRFLearningDisconnected(reason string) {
	runtime.rfLearnMu.Lock()
	defer runtime.rfLearnMu.Unlock()
	if !runtime.rfLearnState.Active {
		return
	}
	runtime.rfLearnState.Active = false
	runtime.rfLearnState.RemainingMS = 0
	runtime.rfLearnState.Reason = reason
}
