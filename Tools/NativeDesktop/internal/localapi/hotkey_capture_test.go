package localapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type hotkeyCaptureRecorder struct {
	mu     sync.Mutex
	states []bool
	failOn map[bool]error
}

func (r *hotkeyCaptureRecorder) capture(
	_ context.Context,
	enabled bool,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, enabled)
	return r.failOn[enabled]
}

func (r *hotkeyCaptureRecorder) snapshot() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.states...)
}

func TestHotkeyCaptureCoordinatorReferenceCountsLeases(t *testing.T) {
	recorder := &hotkeyCaptureRecorder{}
	coordinator := newHotkeyCaptureCoordinator(
		recorder.capture,
		nil,
		time.Hour,
		2,
	)
	defer coordinator.close(context.Background())

	first, err := coordinator.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || !validRandomToken(first.ID) ||
		!validRandomToken(second.ID) {
		t.Fatalf("lease IDs are not unique opaque tokens: %q / %q", first.ID, second.ID)
	}
	if got := recorder.snapshot(); len(got) != 1 || !got[0] {
		t.Fatalf("capture transitions after two begins = %#v, want [true]", got)
	}

	time.Sleep(time.Millisecond)
	renewed, err := coordinator.renew(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.ID != first.ID || !renewed.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("renewed lease = %#v, original = %#v", renewed, first)
	}

	if _, err := coordinator.begin(context.Background()); !errors.Is(
		err,
		errHotkeyLeaseLimit,
	) {
		t.Fatalf("third begin error = %v, want lease limit", err)
	}
	if err := coordinator.release(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if got := recorder.snapshot(); len(got) != 1 {
		t.Fatalf("non-final release transitions = %#v, want [true]", got)
	}
	if err := coordinator.release(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if got := recorder.snapshot(); len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("capture transitions = %#v, want [true false]", got)
	}
	if err := coordinator.release(
		context.Background(),
		second.ID,
	); !errors.Is(err, errHotkeyLeaseNotFound) {
		t.Fatalf("repeated release error = %v, want lease not found", err)
	}
}

func TestHotkeyCaptureCoordinatorExpiresAndRenews(t *testing.T) {
	const ttl = 80 * time.Millisecond
	transitions := make(chan bool, 4)
	var coordinator *hotkeyCaptureCoordinator
	capture := func(_ context.Context, enabled bool) error {
		// Native callbacks are application code. They must not run under the
		// coordinator state mutex; this would deadlock if they inspect state.
		_ = coordinator.nextExpiryDelay()
		transitions <- enabled
		return nil
	}
	coordinator = newHotkeyCaptureCoordinator(capture, nil, ttl, 4)
	defer coordinator.close(context.Background())

	lease, err := coordinator.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case enabled := <-transitions:
		if !enabled {
			t.Fatal("first transition disabled capture")
		}
	case <-time.After(time.Second):
		t.Fatal("capture enable callback deadlocked")
	}

	time.Sleep(ttl / 3)
	renewed, err := coordinator.renew(context.Background(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("renewal did not extend expiry: %#v -> %#v", lease, renewed)
	}
	select {
	case enabled := <-transitions:
		t.Fatalf("lease expired before renewed deadline; transition = %v", enabled)
	case <-time.After(ttl / 2):
	}
	select {
	case enabled := <-transitions:
		if enabled {
			t.Fatal("expiry unexpectedly enabled capture")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("final lease expiry did not disable capture")
	}
	if _, err := coordinator.renew(
		context.Background(),
		lease.ID,
	); !errors.Is(err, errHotkeyLeaseNotFound) {
		t.Fatalf("renew expired lease error = %v, want lease not found", err)
	}
}

func TestHotkeyCaptureExpiryCleanupIgnoresRequestCancellation(t *testing.T) {
	now := time.Now().UTC()
	var cleanupContextErr error
	cleanupCalls := 0
	coordinator := &hotkeyCaptureCoordinator{
		capture: func(ctx context.Context, enabled bool) error {
			if !enabled {
				cleanupCalls++
				cleanupContextErr = ctx.Err()
			}
			return nil
		},
		ttl:    time.Minute,
		limit:  4,
		now:    func() time.Time { return now },
		leases: make(map[string]time.Time),
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	lease, err := coordinator.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := coordinator.renew(canceled, lease.ID); !errors.Is(
		err,
		errHotkeyLeaseNotFound,
	) {
		t.Fatalf("renew expired lease = %v, want lease not found", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("expiry cleanup calls = %d, want 1", cleanupCalls)
	}
	if cleanupContextErr != nil {
		t.Fatalf("expiry cleanup inherited canceled request context: %v", cleanupContextErr)
	}
	if err := coordinator.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHotkeyCaptureCoordinatorProbeRenewsLease(t *testing.T) {
	probeErr := errors.New("already registered")
	var (
		probeMu sync.Mutex
		probed  []string
	)
	coordinator := newHotkeyCaptureCoordinator(
		func(context.Context, bool) error { return nil },
		func(_ context.Context, chord string) error {
			probeMu.Lock()
			probed = append(probed, chord)
			probeMu.Unlock()
			return probeErr
		},
		time.Hour,
		4,
	)
	defer coordinator.close(context.Background())

	if _, err := coordinator.probeChord(
		context.Background(),
		"missing",
		"Ctrl+K",
	); !errors.Is(err, errHotkeyLeaseNotFound) {
		t.Fatalf("probe without lease error = %v, want lease not found", err)
	}
	lease, err := coordinator.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	outcome, err := coordinator.probeChord(
		context.Background(),
		lease.ID,
		"Ctrl+Alt+K",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(outcome.ProbeError, probeErr) {
		t.Fatalf("probe outcome error = %v, want %v", outcome.ProbeError, probeErr)
	}
	if !outcome.Lease.ExpiresAt.After(lease.ExpiresAt) {
		t.Fatalf("probe did not renew lease: %#v -> %#v", lease, outcome.Lease)
	}
	probeMu.Lock()
	defer probeMu.Unlock()
	if len(probed) != 1 || probed[0] != "Ctrl+Alt+K" {
		t.Fatalf("probed chords = %#v", probed)
	}
}

func TestHotkeyCaptureCoordinatorFailureCleanupAndClose(t *testing.T) {
	t.Run("enable failure grants no lease", func(t *testing.T) {
		recorder := &hotkeyCaptureRecorder{
			failOn: map[bool]error{true: errors.New("enable failed")},
		}
		coordinator := newHotkeyCaptureCoordinator(
			recorder.capture,
			nil,
			time.Hour,
			4,
		)
		defer coordinator.close(context.Background())

		if _, err := coordinator.begin(context.Background()); !errors.Is(
			err,
			errHotkeyCaptureTransition,
		) {
			t.Fatalf("begin error = %v, want transition failure", err)
		}
		coordinator.mu.Lock()
		active := len(coordinator.leases)
		coordinator.mu.Unlock()
		if active != 0 {
			t.Fatalf("active leases after failed enable = %d", active)
		}
	})

	t.Run("request disable failure retains a retryable lease", func(t *testing.T) {
		recorder := &hotkeyCaptureRecorder{
			failOn: map[bool]error{false: errors.New("disable failed")},
		}
		coordinator := newHotkeyCaptureCoordinator(
			recorder.capture,
			nil,
			time.Hour,
			4,
		)
		first, err := coordinator.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := coordinator.release(
			context.Background(),
			first.ID,
		); !errors.Is(err, errHotkeyCaptureTransition) {
			t.Fatalf("release error = %v, want transition failure", err)
		}
		if _, err := coordinator.renew(
			context.Background(),
			first.ID,
		); err != nil {
			t.Fatalf("lease was not retained after unapplied transition: %v", err)
		}

		recorder.mu.Lock()
		delete(recorder.failOn, false)
		recorder.mu.Unlock()
		if err := coordinator.release(context.Background(), first.ID); err != nil {
			t.Fatalf("retry release: %v", err)
		}
		if err := coordinator.close(context.Background()); err != nil {
			t.Fatalf("close after release = %v, want nil", err)
		}
		if err := coordinator.close(context.Background()); err != nil {
			t.Fatalf("second close = %v, want idempotent nil", err)
		}
		if _, err := coordinator.begin(context.Background()); !errors.Is(
			err,
			errHotkeyCaptureClosed,
		) {
			t.Fatalf("begin after close error = %v, want closed", err)
		}
		if got := recorder.snapshot(); len(got) != 3 ||
			!got[0] || got[1] || got[2] {
			t.Fatalf(
				"failure cleanup transitions = %#v, want [true false false]",
				got,
			)
		}
	})

	t.Run("expiry failure never resurrects a stale lease", func(t *testing.T) {
		recorder := &hotkeyCaptureRecorder{
			failOn: map[bool]error{false: errors.New("restore failed")},
		}
		now := time.Now().UTC()
		coordinator := &hotkeyCaptureCoordinator{
			capture: recorder.capture,
			ttl:     time.Minute,
			limit:   4,
			now:     func() time.Time { return now },
			leases:  make(map[string]time.Time),
			wake:    make(chan struct{}, 1),
			done:    make(chan struct{}),
		}
		first, err := coordinator.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(2 * time.Minute)
		coordinator.expireLeases()
		if _, err := coordinator.renew(
			context.Background(),
			first.ID,
		); !errors.Is(err, errHotkeyLeaseNotFound) {
			t.Fatalf("expired lease survived restore failure: %v", err)
		}

		second, err := coordinator.begin(context.Background())
		if err != nil {
			t.Fatalf("begin after failed expiry cleanup: %v", err)
		}
		if second.ID == first.ID {
			t.Fatal("new begin reused the expired opaque lease ID")
		}
		if err := coordinator.close(context.Background()); !errors.Is(
			err,
			errHotkeyCaptureTransition,
		) {
			t.Fatalf("terminal close error = %v, want transition failure", err)
		}
		if err := coordinator.close(context.Background()); err != nil {
			t.Fatalf("second terminal close = %v", err)
		}
		if _, err := coordinator.begin(context.Background()); !errors.Is(
			err,
			errHotkeyCaptureClosed,
		) {
			t.Fatalf("begin after failed terminal close = %v", err)
		}
		if got := recorder.snapshot(); len(got) != 4 ||
			!got[0] || got[1] || !got[2] || got[3] {
			t.Fatalf(
				"expiry failure transitions = %#v, want [true false true false]",
				got,
			)
		}
	})
}

func TestHotkeyCaptureCoordinatorUnavailable(t *testing.T) {
	coordinator := newHotkeyCaptureCoordinator(nil, nil, time.Hour, 4)
	defer coordinator.close(context.Background())
	if _, err := coordinator.begin(context.Background()); !errors.Is(
		err,
		errHotkeyCaptureUnavailable,
	) {
		t.Fatalf("begin error = %v, want unavailable", err)
	}
	if _, err := coordinator.probeChord(
		context.Background(),
		"lease",
		"Ctrl+K",
	); !errors.Is(err, errHotkeyProbeUnavailable) {
		t.Fatalf("probe error = %v, want unavailable", err)
	}
}
