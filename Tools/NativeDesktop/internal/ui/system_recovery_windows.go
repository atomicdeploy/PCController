//go:build windows

package ui

import (
	"sync"
	"time"

	"rayanlamp.local/app/internal/model"
)

const defaultSystemRecoveryDelay = 900 * time.Millisecond

type systemRecoveryReason uint8

const (
	systemRecoveryNetwork systemRecoveryReason = 1 << iota
	systemRecoveryUnlock
	systemRecoveryResume
)

// systemRecoveryCoordinator coalesces noisy adapter notifications and keeps
// all core reconnects serialized outside the window procedure. Resume forces
// one fresh connection; unlock and ordinary interface changes recover only
// when neither HTTP nor WebSocket is usable.
type systemRecoveryCoordinator struct {
	delay time.Duration

	snapshot        func() model.Snapshot
	reconnect       func() error
	onNetworkChange func()
	log             func(string, ...any)

	mu        sync.Mutex
	pending   systemRecoveryReason
	deadline  time.Time
	suspended bool
	stopped   bool

	wake chan struct{}
	stop chan struct{}
	done chan struct{}
}

func newSystemRecoveryCoordinator(
	delay time.Duration,
	snapshot func() model.Snapshot,
	reconnect func() error,
	onNetworkChange func(),
	log func(string, ...any),
) *systemRecoveryCoordinator {
	if delay <= 0 {
		delay = defaultSystemRecoveryDelay
	}
	coordinator := &systemRecoveryCoordinator{
		delay:           delay,
		snapshot:        snapshot,
		reconnect:       reconnect,
		onNetworkChange: onNetworkChange,
		log:             log,
		wake:            make(chan struct{}, 1),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
	go coordinator.run()
	return coordinator
}

func (c *systemRecoveryCoordinator) Suspend() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.suspended = true
	c.pending = 0
	c.deadline = time.Time{}
	c.mu.Unlock()
	c.signalWake()
}

func (c *systemRecoveryCoordinator) Resume() {
	c.schedule(systemRecoveryResume)
}

func (c *systemRecoveryCoordinator) Unlock() {
	c.schedule(systemRecoveryUnlock)
}

func (c *systemRecoveryCoordinator) NetworkChanged() {
	c.schedule(systemRecoveryNetwork)
}

func (c *systemRecoveryCoordinator) schedule(reason systemRecoveryReason) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	if reason == systemRecoveryResume {
		c.suspended = false
	}
	if c.suspended {
		c.mu.Unlock()
		return
	}
	c.pending |= reason
	c.deadline = time.Now().Add(c.delay)
	c.mu.Unlock()
	c.signalWake()
}

func (c *systemRecoveryCoordinator) signalWake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *systemRecoveryCoordinator) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		<-c.done
		return
	}
	c.stopped = true
	c.pending = 0
	c.mu.Unlock()
	close(c.stop)
	<-c.done
}

func (c *systemRecoveryCoordinator) run() {
	defer close(c.done)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	var timerChannel <-chan time.Time
	defer timer.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-c.wake:
			delay, armed := c.nextDelay()
			if !armed {
				stopRecoveryTimer(timer)
				timerChannel = nil
				continue
			}
			stopRecoveryTimer(timer)
			timer.Reset(delay)
			timerChannel = timer.C
		case <-timerChannel:
			reasons, remaining, ready := c.takeReady()
			if !ready {
				if remaining > 0 {
					timer.Reset(remaining)
					timerChannel = timer.C
				} else {
					timerChannel = nil
				}
				continue
			}
			timerChannel = nil
			c.execute(reasons)
		}
	}
}

func (c *systemRecoveryCoordinator) nextDelay() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.suspended || c.pending == 0 {
		return 0, false
	}
	delay := time.Until(c.deadline)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func (c *systemRecoveryCoordinator) takeReady() (
	systemRecoveryReason,
	time.Duration,
	bool,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.suspended || c.pending == 0 {
		return 0, 0, false
	}
	if remaining := time.Until(c.deadline); remaining > 0 {
		return 0, remaining, false
	}
	reasons := c.pending
	c.pending = 0
	c.deadline = time.Time{}
	return reasons, 0, true
}

func (c *systemRecoveryCoordinator) execute(reasons systemRecoveryReason) {
	if reasons&systemRecoveryNetwork != 0 && c.onNetworkChange != nil {
		c.onNetworkChange()
	}
	force := reasons&systemRecoveryResume != 0
	if !force {
		if c.snapshot == nil || !snapshotNeedsSystemRecovery(c.snapshot()) {
			return
		}
	}
	if c.reconnect == nil {
		return
	}
	if err := c.reconnect(); err != nil && c.log != nil {
		c.log("Windows lifecycle reconnect: %v", err)
	}
}

func snapshotNeedsSystemRecovery(snapshot model.Snapshot) bool {
	return !snapshot.WebSocketOnline && !snapshot.HTTPReachable
}

func stopRecoveryTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}
