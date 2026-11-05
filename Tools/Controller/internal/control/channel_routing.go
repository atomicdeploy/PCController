package control

import (
	"context"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

// ChannelRoutingBusy is safe under the configuration store lock: unlike a
// library snapshot it never calls config providers or clones a recorded take.
func (runtime *Runtime) ChannelRoutingBusy() bool {
	return runtime.activeUseMask.Load()&(activeUseMacroPlayback|activeUseMacroRecording) != 0
}

// SetRoutedOutput executes an already resolved, general-purpose physical
// binding. All writes/readback stay on one authenticated board generation and
// retain the normal E-STOP, authority, timeline and PWM cancellation guards.
func (runtime *Runtime) SetRoutedOutput(ctx context.Context, generation uint64, target string, value uint16) error {
	kind, index, ok := appconfig.RoutingTarget(target)
	if !ok || kind == "relay" && value > 1 || value > appconfig.PWMMaximumValue {
		return fmt.Errorf("invalid routed output %q or value %d", target, value)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.mu.RLock()
	current := runtime.generation == generation && runtime.session != nil && runtime.sessionUsableLocked()
	runtime.mu.RUnlock()
	if !current {
		return fmt.Errorf("connection generation %d is no longer active", generation)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	opcode := native.OpRelaySet
	payload, _ := native.RelayPayload(index, value != 0)
	if kind == "pwm" {
		opcode = native.OpPWMSet
		payload, _ = native.PWMSetPayload(index, value)
		if value == 0 {
			runtime.cancelPWMAction()
		}
		epoch := runtime.pwmActionEpoch.Load()
		release, err := runtime.acquirePWMAction(ctx)
		if err != nil {
			return err
		}
		defer release()
		bound, cleanup, err := runtime.bindPWMActionContext(ctx, epoch)
		if err != nil {
			return err
		}
		defer cleanup()
		ctx = bound
		if value == 0 {
			// Refresh the remembered nonzero value before explicit Off. Generic
			// cached telemetry may not have observed the last physical change.
			frame, err := runtime.requestAtGeneration(ctx, generation, native.OpPWMGet, nil, native.OpPWMValues)
			if err != nil {
				return err
			}
			values, err := native.ParsePWMValues(frame.Payload)
			if err != nil || !values.Available {
				return fmt.Errorf("current PWM state unavailable before Off: %v", err)
			}
		}
	}
	frame, err := runtime.requestAtGeneration(ctx, generation, opcode, payload, native.OpACK)
	if err != nil {
		return err
	}
	runtime.publishCommandEvidence(acknowledgedCommandEvidence(ctx, opcode, payload, frame))
	if kind == "relay" {
		frame, err = runtime.requestAtGeneration(ctx, generation, native.OpGetStatus, nil, native.OpStatus)
		if err != nil {
			return fmt.Errorf("output acknowledged; readback outcome unknown: %w", err)
		}
		status, err := native.ParseStatus(frame.Payload)
		if err != nil {
			return err
		}
		if (status.ActiveRelays&(1<<index) != 0) != (value != 0) {
			return fmt.Errorf("%s acknowledged but observed state differs", target)
		}
		return nil
	}
	frame, err = runtime.requestAtGeneration(ctx, generation, native.OpPWMGet, nil, native.OpPWMValues)
	if err != nil {
		return fmt.Errorf("output acknowledged; readback outcome unknown: %w", err)
	}
	values, err := native.ParsePWMValues(frame.Payload)
	if err != nil {
		return err
	}
	if !values.Available || values.Values[index] != value {
		return fmt.Errorf("%s acknowledged but observed value differs", target)
	}
	if value == 0 {
		return runtime.CommitPWMOffRecall(generation, runtime.BoardSnapshot().PWMRevision, []byte{index})
	}
	return nil
}
