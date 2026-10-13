package control

import (
	"context"
	"errors"
	"time"
)

type rfRollGuardKey struct{}

var errRFRollReleased = errors.New("RF hold ended before PWM write")

func (runtime *Runtime) rfRollWriteGuard(event Event) func() error {
	key := rfGestureKey{code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol}
	runtime.rfMu.Lock()
	state := runtime.rfGestures[key]
	runtime.rfMu.Unlock()
	return func() error {
		runtime.rfMu.Lock()
		defer runtime.rfMu.Unlock()
		if state == nil || runtime.rfGestures[key] != state || !state.held || time.Since(state.lastSeen) > rfReleaseAfter {
			return errRFRollReleased
		}
		return nil
	}
}

func withRFRollGuard(ctx context.Context, guard func() error) context.Context {
	return context.WithValue(ctx, rfRollGuardKey{}, guard)
}
