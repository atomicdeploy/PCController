package traymenu

import (
	"math"
	"testing"
	"time"
)

func TestHoverAnimatorCrossFadesSelection(t *testing.T) {
	t.Parallel()

	animation := newHoverAnimator(3)
	changed, continuing := animation.advance(0, 16*time.Millisecond)
	if !changed || !continuing {
		t.Fatalf("first step = changed %v, continuing %v", changed, continuing)
	}
	first := animation.alpha(0, 96)
	if first == 0 || first >= 96 {
		t.Fatalf("first-row alpha after one step = %d", first)
	}

	for animation.levels[0] != hoverLevelMaximum {
		animation.advance(0, 16*time.Millisecond)
	}
	if got := animation.alpha(0, 96); got != 96 {
		t.Fatalf("settled first-row alpha = %d, want 96", got)
	}

	animation.advance(2, 16*time.Millisecond)
	if animation.levels[0] >= hoverLevelMaximum {
		t.Fatal("old row did not begin fading out")
	}
	if animation.levels[2] == 0 {
		t.Fatal("new row did not begin fading in")
	}

	for {
		_, continuing = animation.advance(2, 16*time.Millisecond)
		if !continuing {
			break
		}
	}
	if animation.levels[0] != 0 || animation.levels[2] != hoverLevelMaximum {
		t.Fatalf("settled levels = %v", animation.levels)
	}
}

func TestHoverAnimatorFadesToNoSelection(t *testing.T) {
	t.Parallel()

	animation := newHoverAnimator(1)
	for animation.levels[0] != hoverLevelMaximum {
		animation.advance(0, 16*time.Millisecond)
	}
	for {
		_, continuing := animation.advance(-1, 16*time.Millisecond)
		if !continuing {
			break
		}
	}
	if animation.levels[0] != 0 {
		t.Fatalf("level after clearing selection = %d", animation.levels[0])
	}
}

func TestHoverAnimatorUsesElapsedTimeInsteadOfFrameCount(t *testing.T) {
	t.Parallel()

	regular := newHoverAnimator(1)
	for range 4 {
		regular.advance(0, 30*time.Millisecond)
	}
	delayed := newHoverAnimator(1)
	delayed.advance(0, 120*time.Millisecond)
	if regular.levels[0] != hoverLevelMaximum ||
		delayed.levels[0] != hoverLevelMaximum {
		t.Fatalf(
			"120ms hover levels = regular %d delayed %d, want %d",
			regular.levels[0],
			delayed.levels[0],
			hoverLevelMaximum,
		)
	}
}

func TestPopupScalarMotionUsesAbsoluteTimeAndReversesContinuously(t *testing.T) {
	t.Parallel()

	start := time.Unix(300, 0)
	var motion popupScalarMotion
	motion.begin(0, 255, start, 200*time.Millisecond)
	value, changed, continuing := motion.advance(start.Add(50 * time.Millisecond))
	if !changed || !continuing || math.Abs(float64(value)-64) > 1 {
		t.Fatalf("quarter frame = (%d, %v, %v), want about 64", value, changed, continuing)
	}

	motion.begin(value, 0, start.Add(50*time.Millisecond), 200*time.Millisecond)
	if motion.current != value {
		t.Fatalf("reverse jumped from %d to %d", value, motion.current)
	}
	_, _, continuing = motion.advance(start.Add(250 * time.Millisecond))
	if motion.current != 0 || continuing {
		t.Fatalf("delayed reverse = (%d, %v), want settled zero", motion.current, continuing)
	}

	motion.begin(0, 255, start, 200*time.Millisecond)
	value, _, continuing = motion.advance(start.Add(time.Second))
	if value != 255 || continuing {
		t.Fatalf("delayed reveal = (%d, %v), want settled full", value, continuing)
	}
}

func TestPopupFrameGateCoalescesAndRetriesFailedPost(t *testing.T) {
	t.Parallel()

	var gate popupFrameGate
	posts := 0
	if !gate.request(func() bool {
		posts++
		return true
	}) {
		t.Fatal("first frame request was not posted")
	}
	if gate.request(func() bool {
		posts++
		return true
	}) {
		t.Fatal("second frame request bypassed the pending-frame gate")
	}
	if posts != 1 {
		t.Fatalf("posted %d coalesced frames, want 1", posts)
	}

	gate.consume()
	if gate.request(func() bool {
		posts++
		return false
	}) {
		t.Fatal("failed PostMessage was reported as queued")
	}
	if !gate.request(func() bool {
		posts++
		return true
	}) {
		t.Fatal("failed PostMessage left the frame gate stuck")
	}
	if posts != 3 {
		t.Fatalf("post attempts = %d, want 3", posts)
	}
}

func TestPopupFrameGateRestartIgnoresStaleGeneration(t *testing.T) {
	t.Parallel()

	oldGate := new(popupFrameGate)
	newGate := new(popupFrameGate)
	if !oldGate.request(func() bool { return true }) ||
		!newGate.request(func() bool { return true }) {
		t.Fatal("could not seed old and new pending frames")
	}

	// A stale message cannot consume the current generation's gate.
	if consumePopupAnimationFrame(true, 2, 1, newGate) {
		t.Fatal("stale animation token was accepted after restart")
	}
	if newGate.request(func() bool { return true }) {
		t.Fatal("stale token incorrectly cleared the current pending frame")
	}

	// A late request from the stopped goroutine remains isolated in oldGate
	// and cannot block or consume the current generation.
	oldGate.consume()
	if !oldGate.request(func() bool { return true }) {
		t.Fatal("could not model a late stale-generation post")
	}
	if !consumePopupAnimationFrame(true, 2, 2, newGate) {
		t.Fatal("current animation token was not consumed")
	}
	if !newGate.request(func() bool { return true }) {
		t.Fatal("current frame gate remained stuck after valid consumption")
	}
}

func TestPresentationOffsetHandlesBothDirections(t *testing.T) {
	t.Parallel()

	for _, offset := range []int32{-8, 8} {
		if got := presentationOffset(offset, 0); got != offset {
			t.Fatalf("presentationOffset(%d, 0) = %d", offset, got)
		}
		if got := presentationOffset(offset, 255); got != 0 {
			t.Fatalf("presentationOffset(%d, 255) = %d", offset, got)
		}
	}
}
