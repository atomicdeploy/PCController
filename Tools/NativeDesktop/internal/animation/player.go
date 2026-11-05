package animation

import (
	"context"
	"image"
	"sync"
	"time"
)

// Player owns one cancelable playback at a time. PlayTransition runs the
// frames once in the requested direction and leaves the final frame visible.
type Player struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
}

func (p *Player) Stop() {
	p.mu.Lock()
	p.generation++
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (p *Player) PlayTransition(
	parent context.Context,
	animation *Animation,
	forward bool,
	onFrame func(index int, frame image.Image),
) {
	if animation == nil || onFrame == nil {
		p.Stop()
		return
	}
	count := animation.FrameCount()
	// Playback owns a shallow snapshot of any resident source images. The UI
	// may release Animation.Frames as soon as its native bitmap cache is
	// complete; retaining independent interface slots here avoids racing that
	// slice mutation while preserving the callback's existing image value.
	frames := append([]image.Image(nil), animation.Frames...)
	ctx, cancel := context.WithCancel(parent)

	p.mu.Lock()
	p.generation++
	generation := p.generation
	previousCancel := p.cancel
	p.cancel = cancel
	p.mu.Unlock()

	if previousCancel != nil {
		previousCancel()
	}

	go func() {
		defer func() {
			cancel()
			p.mu.Lock()
			if p.generation == generation {
				p.cancel = nil
			}
			p.mu.Unlock()
		}()

		if count == 0 {
			return
		}

		for step := 0; step < count; step++ {
			if !p.isCurrent(ctx, generation) {
				return
			}
			index := step
			if !forward {
				index = count - 1 - step
			}
			var frame image.Image
			if index < len(frames) {
				frame = frames[index]
			}
			onFrame(index, frame)

			if !p.isCurrent(ctx, generation) {
				return
			}
			delay := 33 * time.Millisecond
			if index < len(animation.Delays) {
				delay = animation.Delays[index]
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (p *Player) isCurrent(ctx context.Context, generation uint64) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}

	p.mu.Lock()
	current := p.generation == generation
	p.mu.Unlock()
	return current
}
