package traymenu

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	hoverLevelMaximum  = uint8(255)
	popupHoverDuration = 120 * time.Millisecond
)

// hoverAnimator keeps the visual highlight independent from keyboard and
// pointer selection. Selection changes immediately for input semantics while
// the corresponding row fades in and the previous row fades out.
type hoverAnimator struct {
	levels []uint8
}

func newHoverAnimator(itemCount int) hoverAnimator {
	if itemCount < 0 {
		itemCount = 0
	}
	return hoverAnimator{levels: make([]uint8, itemCount)}
}

// advance moves every row toward its semantic target by real elapsed time.
// Posted frame requests are intentionally coalesced, so frame-count increments
// would make a busy popup animate more slowly than an idle one.
func (a *hoverAnimator) advance(
	target int,
	elapsed time.Duration,
) (changed, continuing bool) {
	if a == nil {
		return false, false
	}
	if elapsed <= 0 {
		elapsed = 16 * time.Millisecond
	}
	step := int(math.Round(
		float64(hoverLevelMaximum) *
			float64(elapsed) /
			float64(popupHoverDuration),
	))
	if step < 1 {
		step = 1
	}
	for index, level := range a.levels {
		wanted := uint8(0)
		if index == target {
			wanted = hoverLevelMaximum
		}
		switch {
		case level < wanted:
			next := int(level) + step
			if next > int(wanted) {
				next = int(wanted)
			}
			a.levels[index] = uint8(next)
			changed = true
		case level > wanted:
			next := int(level) - step
			if next < int(wanted) {
				next = int(wanted)
			}
			a.levels[index] = uint8(next)
			changed = true
		}
		if a.levels[index] != wanted {
			continuing = true
		}
	}
	return changed, continuing
}

func (a *hoverAnimator) settle(target int) {
	if a == nil {
		return
	}
	for index := range a.levels {
		a.levels[index] = 0
		if index == target {
			a.levels[index] = hoverLevelMaximum
		}
	}
}

func (a hoverAnimator) alpha(index int, maximum uint8) uint8 {
	if index < 0 || index >= len(a.levels) || maximum == 0 {
		return 0
	}
	level := easeByte(a.levels[index])
	return uint8((uint16(level)*uint16(maximum) + 127) / 255)
}

// easeByte is a fixed-point smoothstep. It has zero slope at both ends, which
// avoids a hard pop when a row starts highlighting or finishes fading.
func easeByte(value uint8) uint8 {
	x := uint64(value)
	numerator := x * x * (3*255 - 2*x)
	return uint8((numerator + 255*255/2) / (255 * 255))
}

func presentationOffset(offset int32, presentation uint8) int32 {
	remaining := int64(255 - easeByte(presentation))
	return int32(int64(offset) * remaining / 255)
}

// popupScalarMotion anchors presentation to a start time. Delayed timer
// dispatches therefore skip obsolete frames instead of lengthening a menu
// reveal or dismissal. Duration is scaled by remaining distance so reversing
// an in-flight transition preserves velocity.
type popupScalarMotion struct {
	from     uint8
	to       uint8
	current  uint8
	started  time.Time
	duration time.Duration
	active   bool
}

func (motion *popupScalarMotion) begin(
	current, target uint8,
	now time.Time,
	fullDuration time.Duration,
) {
	if motion == nil {
		return
	}
	motion.from = current
	motion.to = target
	motion.current = current
	motion.started = now
	distance := absInt(int(target) - int(current))
	if fullDuration <= 0 || distance == 0 {
		motion.duration = 0
		motion.current = target
		motion.active = false
		return
	}
	motion.duration = time.Duration(
		float64(fullDuration) *
			float64(distance) /
			float64(hoverLevelMaximum),
	)
	if motion.duration < time.Millisecond {
		motion.duration = time.Millisecond
	}
	motion.active = true
}

func (motion *popupScalarMotion) advance(
	now time.Time,
) (value uint8, changed, continuing bool) {
	if motion == nil {
		return 0, false, false
	}
	previous := motion.current
	if !motion.active {
		return previous, false, false
	}
	elapsed := now.Sub(motion.started)
	if elapsed <= 0 {
		return previous, false, true
	}
	if elapsed >= motion.duration {
		motion.current = motion.to
		motion.active = false
		return motion.current, motion.current != previous, false
	}
	progress := float64(elapsed) / float64(motion.duration)
	value64 := float64(motion.from) +
		float64(int(motion.to)-int(motion.from))*progress
	motion.current = uint8(math.Round(value64))
	return motion.current, motion.current != previous, true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// popupFrameGate permits at most one posted animation-frame message at a
// time. A scheduler may keep ticking while the UI thread is busy without
// flooding its queue with obsolete frames.
type popupFrameGate struct {
	pending atomic.Bool
}

func (gate *popupFrameGate) request(post func() bool) bool {
	if gate == nil || post == nil ||
		!gate.pending.CompareAndSwap(false, true) {
		return false
	}
	if post() {
		return true
	}
	gate.pending.Store(false)
	return false
}

func (gate *popupFrameGate) consume() {
	if gate != nil {
		gate.pending.Store(false)
	}
}

func (gate *popupFrameGate) reset() {
	if gate != nil {
		gate.pending.Store(false)
	}
}

// consumePopupAnimationFrame validates one coalesced frame token before
// releasing the gate. It is platform-neutral state logic even though Windows
// is currently the only popup implementation that posts the native message.
func consumePopupAnimationFrame(
	running bool,
	currentToken, messageToken uint32,
	gate *popupFrameGate,
) bool {
	if !running || currentToken == 0 ||
		messageToken != currentToken || gate == nil {
		return false
	}
	gate.consume()
	return true
}
