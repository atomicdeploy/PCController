package control

import (
	"context"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/native"
)

type RFLearnOptions struct {
	Timeout    time.Duration
	Indefinite bool
	Multiple   bool
}

type RFLearnState struct {
	Active     bool      `json:"active"`
	Indefinite bool      `json:"indefinite"`
	Multiple   bool      `json:"multiple"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	EndsAt     time.Time `json:"ends_at,omitempty"`
	Learned    uint32    `json:"learned"`
	Reason     string    `json:"reason,omitempty"`
}

func (runtime *Runtime) RFLearnState() RFLearnState {
	runtime.rfLearnMu.RLock()
	defer runtime.rfLearnMu.RUnlock()
	return runtime.rfLearnState
}

func (runtime *Runtime) StartRFLearning(
	ctx context.Context,
	options RFLearnOptions,
) (RFLearnState, error) {
	if options.Timeout <= 0 && !options.Indefinite {
		options.Timeout = 15 * time.Second
	}
	if options.Timeout > 24*time.Hour {
		return RFLearnState{}, fmt.Errorf("RF learning timeout exceeds 24 hours")
	}
	afterID := runtime.LatestEventID()
	chunk := rfLearnChunk(options)
	if err := runtime.armRFLearn(ctx, chunk); err != nil {
		return RFLearnState{}, err
	}

	runtime.rfLearnMu.Lock()
	if runtime.rfLearnCancel != nil {
		runtime.rfLearnCancel()
	}
	runtime.rfLearnGeneration++
	generation := runtime.rfLearnGeneration
	sessionContext, cancel := context.WithCancel(context.Background())
	runtime.rfLearnCancel = cancel
	now := time.Now()
	state := RFLearnState{
		Active: true, Indefinite: options.Indefinite,
		Multiple: options.Multiple, StartedAt: now,
	}
	if !options.Indefinite {
		state.EndsAt = now.Add(options.Timeout)
	}
	runtime.rfLearnState = state
	runtime.rfLearnMu.Unlock()

	mode := "single"
	if options.Multiple {
		mode = "multi"
	}
	duration := options.Timeout.String()
	if options.Indefinite {
		duration = "indefinite"
	}
	runtime.PublishHostEvent(
		"rf.learn.started",
		fmt.Sprintf("RF learning started mode=%s duration=%s", mode, duration),
	)
	go runtime.runRFLearning(sessionContext, generation, options, afterID)
	return state, nil
}

func (runtime *Runtime) CancelRFLearning(
	ctx context.Context,
	reason string,
) error {
	if reason == "" {
		reason = "cancelled by host"
	}
	err := runtime.Command(ctx, native.OpRFLearnCancel, nil)
	runtime.finishRFLearning(0, reason)
	return err
}

func (runtime *Runtime) armRFLearn(ctx context.Context, duration time.Duration) error {
	seconds := int64((duration + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	if seconds > int64(native.MaxRFLearnSeconds) {
		seconds = int64(native.MaxRFLearnSeconds)
	}
	payload, err := native.RFLearnStartPayload(byte(seconds))
	if err != nil {
		return err
	}
	return runtime.Command(ctx, native.OpRFLearnStart, payload)
}

func rfLearnChunk(options RFLearnOptions) time.Duration {
	maximum := time.Duration(native.MaxRFLearnSeconds) * time.Second
	if options.Indefinite || options.Timeout > maximum {
		return maximum
	}
	if options.Timeout <= 0 {
		return 15 * time.Second
	}
	return options.Timeout
}

func (runtime *Runtime) runRFLearning(
	ctx context.Context,
	generation uint64,
	options RFLearnOptions,
	afterID uint64,
) {
	deadline := time.Time{}
	if !options.Indefinite {
		deadline = time.Now().Add(options.Timeout)
	}
	for ctx.Err() == nil {
		chunk := time.Duration(native.MaxRFLearnSeconds) * time.Second
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				runtime.finishRFLearning(generation, "timeout elapsed")
				return
			}
			if remaining < chunk {
				chunk = remaining
			}
		}
		waitContext, cancel := context.WithTimeout(ctx, chunk)
		var learned Event
		for waitContext.Err() == nil {
			event, err := runtime.WaitEvent(waitContext, afterID, "")
			if err != nil {
				break
			}
			afterID = event.ID
			if event.Kind == "rf.learn" {
				learned = event
				break
			}
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		if learned.ID != 0 {
			count := runtime.incrementRFLearned(generation)
			runtime.PublishHostEvent(
				"rf.learn.mapping-required",
				fmt.Sprintf(
					"RF entry captured (%s); no default action is assigned; choose a mapping",
					learned.Text,
				),
			)
			if !options.Multiple {
				runtime.finishRFLearning(
					generation,
					fmt.Sprintf("completed after %d capture", count),
				)
				return
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			runtime.finishRFLearning(generation, "timeout elapsed")
			return
		}
		armContext, armCancel := context.WithTimeout(ctx, 3*time.Second)
		err := runtime.armRFLearn(armContext, rfLearnChunk(options))
		armCancel()
		if err != nil {
			runtime.finishRFLearning(
				generation,
				"re-arm failed: "+err.Error(),
			)
			return
		}
	}
}

func (runtime *Runtime) incrementRFLearned(generation uint64) uint32 {
	runtime.rfLearnMu.Lock()
	defer runtime.rfLearnMu.Unlock()
	if generation == runtime.rfLearnGeneration && runtime.rfLearnState.Active {
		runtime.rfLearnState.Learned++
	}
	return runtime.rfLearnState.Learned
}

func (runtime *Runtime) finishRFLearning(generation uint64, reason string) {
	runtime.rfLearnMu.Lock()
	if !runtime.rfLearnState.Active ||
		(generation != 0 && generation != runtime.rfLearnGeneration) {
		runtime.rfLearnMu.Unlock()
		return
	}
	if runtime.rfLearnCancel != nil {
		runtime.rfLearnCancel()
	}
	runtime.rfLearnCancel = nil
	runtime.rfLearnState.Active = false
	runtime.rfLearnState.Reason = reason
	state := runtime.rfLearnState
	runtime.rfLearnMu.Unlock()
	runtime.PublishHostEvent(
		"rf.learn.ended",
		fmt.Sprintf("RF learning ended: %s (captured=%d)", reason, state.Learned),
	)
}
