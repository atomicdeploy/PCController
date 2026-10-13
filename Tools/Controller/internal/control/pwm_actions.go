package control

import (
	"context"
	"errors"
	"fmt"
	"math"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

// PWMActionConfirmationError separates an acknowledged mutation from a
// missing/invalid confirmation; callers must not retry the write blindly.
type PWMActionConfirmationError struct{ Cause error }

func (err *PWMActionConfirmationError) Error() string {
	if err.Cause == nil {
		return "PWM write acknowledged but confirmation unavailable; output outcome is unknown"
	}
	return "PWM write acknowledged but confirmation unavailable; output outcome is unknown: " + err.Cause.Error()
}
func (err *PWMActionConfirmationError) Unwrap() error { return err.Cause }

func (runtime *Runtime) acquirePWMAction(ctx context.Context) (func(), error) {
	runtime.pwmActionOnce.Do(func() { runtime.pwmActionGate = make(chan struct{}, 1) })
	select {
	case runtime.pwmActionGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-runtime.pwmActionGate
			return nil, err
		}
		return func() { <-runtime.pwmActionGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Zeroing writes supersede queued percentage changes and cancel an in-flight
// compound operation. They never wait for the percentage-action gate.
func (runtime *Runtime) cancelPWMAction() {
	runtime.pwmCancelMu.Lock()
	runtime.pwmActionEpoch.Add(1)
	cancel := runtime.pwmActionCancel
	runtime.pwmCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (runtime *Runtime) bindPWMActionContext(ctx context.Context, epoch uint64) (context.Context, func(), error) {
	runtime.pwmCancelMu.Lock()
	if epoch != runtime.pwmActionEpoch.Load() {
		runtime.pwmCancelMu.Unlock()
		return nil, nil, errors.New("PWM action superseded by a zeroing request")
	}
	bound, cancel := context.WithCancel(ctx)
	runtime.pwmActionCancel = cancel
	runtime.pwmCancelMu.Unlock()
	return bound, func() {
		runtime.pwmCancelMu.Lock()
		runtime.pwmActionCancel = nil
		runtime.pwmCancelMu.Unlock()
		cancel()
	}, nil
}

func pwmActionPercent(raw uint16, verb string, config appconfig.PWMChannelConfig) (float64, error) {
	step := appconfig.PWMStepPercent(config)
	if math.IsNaN(step) || math.IsInf(step, 0) || step <= 0 || step > 100 {
		return 0, errors.New("PWM step must be finite and greater than 0, at most 100 percent")
	}
	switch verb {
	case "on":
		return 100, nil
	case "off":
		return 0, nil
	case "increase":
		return math.Min(100, appconfig.PWMPercentFromRaw(raw, config)+step), nil
	case "decrease":
		return math.Max(0, appconfig.PWMPercentFromRaw(raw, config)-step), nil
	default:
		return 0, fmt.Errorf("unknown PWM action %q", verb)
	}
}

func pwmActionRaw(raw uint16, verb string, config appconfig.PWMChannelConfig) (uint16, error) {
	percent, err := pwmActionPercent(raw, verb, config)
	if err != nil {
		return 0, err
	}
	target := appconfig.PWMRawFromPercent(percent, config)
	// Relative intent must make progress at the actual 12-bit resolution.
	// Re-reading unchanged duty would otherwise lose every sub-quantum step.
	// Do not change absolute percent conversion or invent an observed setpoint.
	if verb == "increase" && raw < appconfig.PWMMaximumValue && target <= raw {
		target = raw + 1
	} else if verb == "decrease" && raw > 0 && target >= raw {
		target = raw - 1
	}
	return target, nil
}

func applyPWMActionWithIO(channel byte, verb string, config appconfig.PWMChannelConfig, read func() (native.PWMValues, error), write func(uint16) error) (native.PWMValues, error) {
	if channel >= appconfig.PWMChannelCount {
		return native.PWMValues{}, errors.New("PWM channel must be 0..15")
	}
	values, err := read()
	if err != nil {
		return native.PWMValues{}, err
	}
	if err := validatePWMActionReadback(values); err != nil {
		return native.PWMValues{}, err
	}
	raw, err := pwmActionRaw(values.Values[channel], verb, config)
	if err != nil {
		return native.PWMValues{}, err
	}
	if err := write(raw); err != nil {
		return native.PWMValues{}, err
	}
	values, err = read()
	if err != nil {
		return native.PWMValues{}, &PWMActionConfirmationError{Cause: err}
	}
	if err := validatePWMActionReadback(values); err != nil {
		return native.PWMValues{}, &PWMActionConfirmationError{Cause: err}
	}
	return values, nil
}

func validatePWMActionReadback(values native.PWMValues) error {
	if !values.Available {
		return errors.New("PWM driver is unavailable")
	}
	if values.SelectedChannel >= appconfig.PWMChannelCount {
		return errors.New("PWM selected-channel readback exceeds 15")
	}
	for _, raw := range values.Values {
		if raw > appconfig.PWMMaximumValue {
			return errors.New("PWM readback exceeds 4095")
		}
	}
	return nil
}

// ApplyPWMAction pins the complete read/modify/write/readback to one session.
// Host PWM requests share a cancellable gate; autonomous firmware is not a
// host-side compare-and-swap and may independently change its owned outputs.
func (runtime *Runtime) ApplyPWMAction(ctx context.Context, generation uint64, channel byte, verb string, config appconfig.PWMChannelConfig) (native.PWMValues, error) {
	if err := ctx.Err(); err != nil {
		return native.PWMValues{}, err
	}
	if channel >= appconfig.PWMChannelCount {
		return native.PWMValues{}, errors.New("PWM channel must be 0..15")
	}
	if err := appconfig.ValidatePWMChannels(map[string]appconfig.PWMChannelConfig{appconfig.PWMChannelKey(int(channel)): config}); err != nil {
		return native.PWMValues{}, err
	}
	if _, err := pwmActionPercent(0, verb, config); err != nil {
		return native.PWMValues{}, err
	}
	if runtime.options.RequestTimeout <= 0 {
		return native.PWMValues{}, errors.New("PWM action requires a positive serial request timeout")
	}
	ctx, finish := context.WithTimeout(ctx, 3*runtime.options.RequestTimeout)
	defer finish()
	if verb == "off" {
		runtime.cancelPWMAction()
	}
	epoch := runtime.pwmActionEpoch.Load()
	release, err := runtime.acquirePWMAction(ctx)
	if err != nil {
		return native.PWMValues{}, err
	}
	defer release()
	bound, cleanup, err := runtime.bindPWMActionContext(ctx, epoch)
	if err != nil {
		return native.PWMValues{}, err
	}
	defer cleanup()
	ctx = bound
	request := func(opcode byte, payload []byte, expected byte) (native.Frame, error) {
		requestCtx, cancel := context.WithTimeout(ctx, runtime.options.RequestTimeout)
		defer cancel()
		return runtime.requestAtGeneration(requestCtx, generation, opcode, payload, expected)
	}
	read := func() (native.PWMValues, error) {
		frame, err := request(native.OpPWMGet, nil, native.OpPWMValues)
		if err != nil {
			return native.PWMValues{}, err
		}
		values, err := native.ParsePWMValues(frame.Payload)
		if err == nil && !values.Available {
			err = errors.New("PWM driver is unavailable")
		}
		return values, err
	}
	values, err := applyPWMActionWithIO(channel, verb, config, read, func(raw uint16) error {
		payload, err := native.PWMSetPayload(channel, raw)
		if err != nil {
			return err
		}
		frame, err := request(native.OpPWMSet, payload, native.OpACK)
		if err == nil {
			runtime.publishCommandEvidence(acknowledgedCommandEvidence(ctx, native.OpPWMSet, payload, frame))
		}
		return err
	})
	if err != nil {
		return native.PWMValues{}, err
	}
	// requestAtGeneration already admits the complete readback through the
	// shared revisioned publisher. Do not emit a duplicate, unrevisioned event.
	return values, nil
}
