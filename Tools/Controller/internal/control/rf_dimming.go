package control

import (
	"context"
	"errors"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
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
		if state.wireSession != nil {
			runtime.mu.RLock()
			currentOwner := runtime.session == state.wireSession && runtime.sessionUsableLocked()
			runtime.mu.RUnlock()
			if !currentOwner {
				return errRFRollReleased
			}
			progress, known := state.wireSession.RFReceiveProgress(key.code, key.bits, key.protocol)
			if !known || time.Since(progress.ReceiptSeen) > rfReleaseAfter || progress.QuietFor(rfReleaseAfter) {
				return errRFRollReleased
			}
		}
		return nil
	}
}

func (runtime *Runtime) probeRFReleaseClock(state *rfGestureState) {
	defer runtime.rfClockProbe.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A read-only heartbeat is pinned to the original link. Never infer release
	// from a timed-out request, or send an output mutation as a clock probe.
	_, _ = state.wireSession.RequestWithPriority(ctx, link.RequestPriorityBackground, native.OpGetStatus, nil, native.OpStatus)
}

func withRFRollGuard(ctx context.Context, guard func() error) context.Context {
	return context.WithValue(ctx, rfRollGuardKey{}, guard)
}
