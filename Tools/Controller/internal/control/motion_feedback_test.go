package control

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func motionFeedbackRuntime() *Runtime {
	return New(Options{})
}

func drainMotionFeedbackEvents(runtime *Runtime) {
	for {
		select {
		case <-runtime.events:
		default:
			return
		}
	}
}

func TestMotionIntentCommandContract(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		side    byte
		motion  string
		ok      bool
	}{
		{[]byte{0, 0}, 0, "stop", true},
		{[]byte{0, 1}, 0, "up", true},
		{[]byte{1, 2}, 1, "down", true},
		{[]byte{2, 1}, 0, "", false},
		{[]byte{0, 3}, 0, "", false},
	} {
		side, motion, ok := motionIntentFromCommand(native.OpRelaySide, test.payload)
		if side != test.side || motion != test.motion || ok != test.ok {
			t.Fatalf("payload %v => side=%d motion=%q ok=%t", test.payload, side, motion, ok)
		}
	}
	if _, _, ok := motionIntentFromCommand(native.OpRelaySet, []byte{0, 1}); ok {
		t.Fatal("generic relay command was treated as semantic motion")
	}
}

func TestMotionFeedbackStartsImmediatelyAndSettlesFromBoard(t *testing.T) {
	runtime := motionFeedbackRuntime()
	runtime.beginMotionIntent(0, "up")
	started := runtime.Snapshot().Motion.Left
	if started.Requested != "up" || started.Applied != "stop" || !started.Transitioning || started.Revision == 0 {
		t.Fatalf("started motion = %#v", started)
	}
	first := <-runtime.events
	if first.Kind != "motion.changed" || first.Stream != EventStreamState || first.Source != "host" ||
		first.Metadata["requested"] != "up" || first.Metadata["transitioning"] != "true" {
		t.Fatalf("start event = %#v", first)
	}

	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0b00000010, time.Now())
	runtime.mu.Unlock()
	settled := runtime.Snapshot().Motion.Left
	if settled.Requested != "up" || settled.Applied != "up" || settled.Transitioning || settled.Revision <= started.Revision {
		t.Fatalf("settled motion = %#v", settled)
	}
	second := <-runtime.events
	if second.Source != "board" || second.Metadata["applied"] != "up" || second.Metadata["transitioning"] != "false" {
		t.Fatalf("settled event = %#v", second)
	}
}

func TestMotionReversalKeepsSemanticDirectionThroughSafeBreak(t *testing.T) {
	runtime := motionFeedbackRuntime()
	now := time.Now()
	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0b00000010, now)
	runtime.mu.Unlock()
	drainMotionFeedbackEvents(runtime) // initial authoritative state for both sides

	runtime.beginMotionIntent(0, "down")
	drainMotionFeedbackEvents(runtime)

	// The physical enable drops before direction changes. This raw edge remains
	// observable, while semantic presentation stays on the requested direction.
	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0, now.Add(time.Millisecond))
	runtime.mu.Unlock()
	broken := runtime.Snapshot().Motion.Left
	if broken.Requested != "down" || broken.Applied != "stop" || !broken.Transitioning {
		t.Fatalf("break state = %#v", broken)
	}
	drainMotionFeedbackEvents(runtime)

	// Direction-only is still electrically stopped and must not create a second
	// semantic event or alter the requested presentation.
	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0b00000001, now.Add(2*time.Millisecond))
	runtime.mu.Unlock()
	select {
	case event := <-runtime.events:
		t.Fatalf("direction-only edge emitted duplicate semantic event: %#v", event)
	default:
	}

	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0b00000011, now.Add(3*time.Millisecond))
	runtime.mu.Unlock()
	settled := runtime.Snapshot().Motion.Left
	if settled.Requested != "down" || settled.Applied != "down" || settled.Transitioning {
		t.Fatalf("reversal settled state = %#v", settled)
	}
}

func TestMotionFailureRollbackCannotOverrideNewerIntent(t *testing.T) {
	runtime := motionFeedbackRuntime()
	first := runtime.beginMotionIntent(0, "up")
	second := runtime.beginMotionIntent(0, "down")
	runtime.rollbackMotionIntent(0, first)
	if got := runtime.Snapshot().Motion.Left; got.Requested != "down" || !got.Transitioning {
		t.Fatalf("older failure overwrote newer intent: %#v", got)
	}
	runtime.rollbackMotionIntent(0, second)
	if got := runtime.Snapshot().Motion.Left; got.Requested != "stop" || got.Applied != "stop" || got.Transitioning {
		t.Fatalf("latest failure did not return to applied state: %#v", got)
	}
}

func TestMotionFeedbackTracksBothSidesAndExpiresUnconfirmedIntent(t *testing.T) {
	runtime := motionFeedbackRuntime()
	base := time.Now()
	runtime.mu.Lock()
	runtime.reconcileMotionLocked(0b00001110, base)
	runtime.mu.Unlock()
	snapshot := runtime.Snapshot().Motion
	if snapshot.Left.Applied != "up" || snapshot.Right.Applied != "down" {
		t.Fatalf("initial sides = %#v", snapshot)
	}

	runtime.beginMotionIntent(1, "up")
	runtime.mu.Lock()
	runtime.motionIntentDeadline[1] = base
	runtime.reconcileMotionLocked(0b00001110, base.Add(time.Millisecond))
	runtime.mu.Unlock()
	if got := runtime.Snapshot().Motion.Right; got.Requested != "down" || got.Applied != "down" || got.Transitioning {
		t.Fatalf("expired intent = %#v", got)
	}
}

func TestMotionChangedUsesStateStream(t *testing.T) {
	if got := EventStreamForKind("motion.changed"); got != EventStreamState {
		t.Fatalf("motion.changed stream = %q", got)
	}
}
