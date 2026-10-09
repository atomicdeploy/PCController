package main

import (
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/hostmenu"
)

const panelFallbackMinimumAnimationPeriod = 400 * time.Millisecond

// panelFallbackAnimator provides capability-driven visual feedback when the
// connected firmware exposes front-panel capture but not overlay animation.
type panelFallbackAnimator struct {
	mu            sync.Mutex
	stop          chan struct{}
	done          chan struct{}
	period        time.Duration
	minimumPeriod time.Duration
	push          func(hostmenu.Snapshot) error
	generation    uint64
}

func newPanelFallbackAnimator(push func(hostmenu.Snapshot) error) *panelFallbackAnimator {
	return &panelFallbackAnimator{
		period: panelFallbackMinimumAnimationPeriod, minimumPeriod: panelFallbackMinimumAnimationPeriod,
		push: push,
	}
}

func (animator *panelFallbackAnimator) Start(snapshot hostmenu.Snapshot) {
	animator.Stop()
	if !panelFallbackAnimated(snapshot.Panel) || animator.push == nil {
		return
	}
	animator.mu.Lock()
	animator.generation++
	generation := animator.generation
	stop := make(chan struct{})
	done := make(chan struct{})
	animator.stop, animator.done = stop, done
	period := animator.period
	if animator.minimumPeriod > 0 && period < animator.minimumPeriod {
		// Tests can use a short injected period; production never can.
		period = animator.minimumPeriod
	}
	animator.mu.Unlock()
	go animator.run(generation, snapshot, period, stop, done)
}

func (animator *panelFallbackAnimator) run(generation uint64, snapshot hostmenu.Snapshot, period time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	visible := true
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			visible = !visible
			frame := snapshot
			frame.Panel.Segments = panelFallbackSegments(snapshot.Panel, visible)
			if err := animator.push(frame); err != nil {
				return
			}
			animator.mu.Lock()
			current := animator.generation == generation
			animator.mu.Unlock()
			if !current {
				return
			}
		}
	}
}

func (animator *panelFallbackAnimator) Stop() {
	animator.mu.Lock()
	stop, done := animator.stop, animator.done
	animator.stop, animator.done = nil, nil
	animator.generation++
	if stop != nil {
		close(stop)
	}
	animator.mu.Unlock()
	if done != nil {
		<-done
	}
}

func panelFallbackAnimated(panel hostmenu.Panel) bool {
	visual := strings.ToLower(strings.TrimSpace(panel.EditVisual))
	return panel.Blink || visual == "blink" || visual == "edit-dim" ||
		visual == "alternate" || visual == "pulse"
}

func panelFallbackSegments(panel hostmenu.Panel, visible bool) string {
	if visible {
		return panel.Segments
	}
	switch strings.ToLower(strings.TrimSpace(panel.EditVisual)) {
	case "edit-dim", "alternate", "pulse":
		return "----"
	default:
		return "    "
	}
}
