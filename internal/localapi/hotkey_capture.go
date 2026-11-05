package localapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	hotkeyCaptureLeaseTTL  = 30 * time.Second
	maxHotkeyCaptureLeases = 32
)

var (
	errHotkeyCaptureUnavailable = errors.New("localapi: hotkey capture is unavailable")
	errHotkeyProbeUnavailable   = errors.New("localapi: hotkey probing is unavailable")
	errHotkeyCaptureClosed      = errors.New("localapi: hotkey capture is closed")
	errHotkeyLeaseNotFound      = errors.New("localapi: hotkey capture lease was not found")
	errHotkeyLeaseLimit         = errors.New("localapi: too many hotkey capture leases")
	errHotkeyCaptureTransition  = errors.New("localapi: hotkey capture transition failed")
)

type hotkeyCaptureLease struct {
	ID        string
	ExpiresAt time.Time
}

type hotkeyProbeOutcome struct {
	Lease      hotkeyCaptureLease
	ProbeError error
}

// hotkeyCaptureCoordinator keeps browser recorders from racing each other or
// permanently suspending the native RegisterHotKey bindings. The callback is
// reference-counted: only the first lease enables capture and only the final
// release or expiry disables it.
type hotkeyCaptureCoordinator struct {
	// ops serializes lease transitions and native callbacks. State protected by
	// mu is always unlocked before a callback runs, so callback latency never
	// blocks the expiry timer's state inspection.
	ops sync.Mutex
	mu  sync.Mutex

	capture func(context.Context, bool) error
	probe   func(context.Context, string) error
	ttl     time.Duration
	limit   int
	now     func() time.Time

	leases map[string]time.Time
	wake   chan struct{}
	done   chan struct{}
	closed bool
}

func newHotkeyCaptureCoordinator(
	capture func(context.Context, bool) error,
	probe func(context.Context, string) error,
	ttl time.Duration,
	limit int,
) *hotkeyCaptureCoordinator {
	if ttl <= 0 {
		ttl = hotkeyCaptureLeaseTTL
	}
	if limit <= 0 {
		limit = maxHotkeyCaptureLeases
	}
	coordinator := &hotkeyCaptureCoordinator{
		capture: capture,
		probe:   probe,
		ttl:     ttl,
		limit:   limit,
		now:     time.Now,
		leases:  make(map[string]time.Time),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	if capture != nil {
		go coordinator.expiryLoop()
	}
	return coordinator
}

func (c *hotkeyCaptureCoordinator) captureAvailable() bool {
	return c != nil && c.capture != nil
}

func (c *hotkeyCaptureCoordinator) probeAvailable() bool {
	return c != nil && c.capture != nil && c.probe != nil
}

func (c *hotkeyCaptureCoordinator) begin(
	ctx context.Context,
) (hotkeyCaptureLease, error) {
	if !c.captureAvailable() {
		return hotkeyCaptureLease{}, errHotkeyCaptureUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.ops.Lock()
	defer c.ops.Unlock()
	c.pruneExpired()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return hotkeyCaptureLease{}, errHotkeyCaptureClosed
	}
	if len(c.leases) >= c.limit {
		c.mu.Unlock()
		return hotkeyCaptureLease{}, errHotkeyLeaseLimit
	}

	id, err := c.uniqueLeaseIDLocked()
	if err != nil {
		c.mu.Unlock()
		return hotkeyCaptureLease{}, err
	}
	enable := len(c.leases) == 0
	c.mu.Unlock()
	if enable {
		if err := c.capture(ctx, true); err != nil {
			return hotkeyCaptureLease{}, fmt.Errorf(
				"%w: enable: %v",
				errHotkeyCaptureTransition,
				err,
			)
		}
	}
	expires := c.now().UTC().Add(c.ttl)
	c.mu.Lock()
	c.leases[id] = expires
	c.mu.Unlock()
	c.signalWake()
	return hotkeyCaptureLease{ID: id, ExpiresAt: expires}, nil
}

func (c *hotkeyCaptureCoordinator) renew(
	ctx context.Context,
	id string,
) (hotkeyCaptureLease, error) {
	if !c.captureAvailable() {
		return hotkeyCaptureLease{}, errHotkeyCaptureUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.ops.Lock()
	defer c.ops.Unlock()
	c.pruneExpired()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return hotkeyCaptureLease{}, errHotkeyCaptureClosed
	}
	if _, ok := c.leases[id]; !ok {
		return hotkeyCaptureLease{}, errHotkeyLeaseNotFound
	}
	expires := c.now().UTC().Add(c.ttl)
	c.leases[id] = expires
	c.signalWake()
	return hotkeyCaptureLease{ID: id, ExpiresAt: expires}, nil
}

func (c *hotkeyCaptureCoordinator) release(
	ctx context.Context,
	id string,
) error {
	if !c.captureAvailable() {
		return errHotkeyCaptureUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.ops.Lock()
	defer c.ops.Unlock()
	c.pruneExpired()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errHotkeyCaptureClosed
	}
	expires, ok := c.leases[id]
	if !ok {
		c.mu.Unlock()
		return errHotkeyLeaseNotFound
	}
	delete(c.leases, id)
	disable := len(c.leases) == 0
	c.mu.Unlock()
	if disable {
		if err := c.capture(ctx, false); err != nil {
			// A request-scoped callback error means the native transition was
			// not applied (most commonly because the tab's context was
			// canceled before the UI thread accepted it). Retain this lease so
			// the browser can retry and the Background expiry path remains a
			// final cleanup owner. Native restore warnings after an applied
			// transition must be handled inside the callback, not returned.
			c.mu.Lock()
			if !c.closed {
				c.leases[id] = expires
			}
			c.mu.Unlock()
			c.signalWake()
			return fmt.Errorf(
				"%w: disable: %v",
				errHotkeyCaptureTransition,
				err,
			)
		}
	}
	c.signalWake()
	return nil
}

func (c *hotkeyCaptureCoordinator) probeChord(
	ctx context.Context,
	id string,
	chord string,
) (hotkeyProbeOutcome, error) {
	if !c.probeAvailable() {
		return hotkeyProbeOutcome{}, errHotkeyProbeUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.ops.Lock()
	defer c.ops.Unlock()
	c.pruneExpired()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return hotkeyProbeOutcome{}, errHotkeyCaptureClosed
	}
	if _, ok := c.leases[id]; !ok {
		c.mu.Unlock()
		return hotkeyProbeOutcome{}, errHotkeyLeaseNotFound
	}

	expires := c.now().UTC().Add(c.ttl)
	c.leases[id] = expires
	c.mu.Unlock()
	c.signalWake()
	return hotkeyProbeOutcome{
		Lease: hotkeyCaptureLease{
			ID:        id,
			ExpiresAt: expires,
		},
		ProbeError: c.probe(ctx, chord),
	}, nil
}

func (c *hotkeyCaptureCoordinator) close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	c.ops.Lock()
	defer c.ops.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	hadLeases := len(c.leases) != 0
	clear(c.leases)
	close(c.done)
	c.mu.Unlock()

	var callbackErr error
	if hadLeases && c.capture != nil {
		// Shutdown is terminal and never resurrects lease state after a
		// best-effort native restore reports an error.
		if err := c.capture(ctx, false); err != nil {
			callbackErr = fmt.Errorf(
				"%w: disable on close: %v",
				errHotkeyCaptureTransition,
				err,
			)
		}
	}
	return callbackErr
}

func (c *hotkeyCaptureCoordinator) uniqueLeaseIDLocked() (string, error) {
	for attempts := 0; attempts < 4; attempts++ {
		id, err := randomToken()
		if err != nil {
			return "", fmt.Errorf("localapi: create hotkey lease id: %w", err)
		}
		if _, exists := c.leases[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("localapi: unable to create unique hotkey lease id")
}

// pruneExpired removes stale leases and performs the final disable transition.
// The caller must hold ops; this method deliberately releases mu before
// invoking application code.
func (c *hotkeyCaptureCoordinator) pruneExpired() {
	c.mu.Lock()
	disable := c.pruneExpiredLocked(c.now())
	c.mu.Unlock()
	if disable && c.capture != nil {
		// Expiry is fail-safe cleanup. A callback error cannot resurrect an
		// expired lease, and a later begin reasserts capture(true). Never let a
		// canceled request context win lease removal and skip native cleanup.
		_ = c.capture(context.Background(), false)
	}
}

func (c *hotkeyCaptureCoordinator) pruneExpiredLocked(now time.Time) bool {
	if len(c.leases) == 0 {
		return false
	}
	hadLeases := len(c.leases) != 0
	for id, expires := range c.leases {
		if !now.Before(expires) {
			delete(c.leases, id)
		}
	}
	return hadLeases && len(c.leases) == 0
}

func (c *hotkeyCaptureCoordinator) expiryLoop() {
	timer := time.NewTimer(c.ttl)
	defer timer.Stop()

	for {
		wait := c.nextExpiryDelay()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-timer.C:
			c.expireLeases()
		case <-c.wake:
		case <-c.done:
			return
		}
	}
}

func (c *hotkeyCaptureCoordinator) nextExpiryDelay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.leases) == 0 {
		return c.ttl
	}
	now := c.now()
	wait := c.ttl
	for _, expires := range c.leases {
		candidate := expires.Sub(now)
		if candidate < wait {
			wait = candidate
		}
	}
	if wait < 0 {
		return 0
	}
	return wait
}

func (c *hotkeyCaptureCoordinator) expireLeases() {
	c.ops.Lock()
	defer c.ops.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.pruneExpired()
}

func (c *hotkeyCaptureCoordinator) signalWake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
