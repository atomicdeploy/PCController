package hostbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

type statusLEDBudgetTarget struct{ send func(context.Context) error }

func (target statusLEDBudgetTarget) SetStatusRGBBase(ctx context.Context, _, _, _, _ byte) error {
	return target.send(ctx)
}

func (target statusLEDBudgetTarget) SetStatusRGB(ctx context.Context, _, _, _, _ byte) error {
	return target.send(ctx)
}

func TestStatusLEDRequestBudgetTracksConfiguration(t *testing.T) {
	arbiter := newStatusLEDArbiter(context.Background(), nil, nil, nil)
	policy := appconfig.DefaultStatusLEDPolicy()
	for _, budget := range []time.Duration{0, 1200 * time.Millisecond, 1800 * time.Millisecond} {
		arbiter.Observe(policy, controller.Snapshot{}, controller.Event{}, budget)
		_, _, _, _, _, _, actual := arbiter.currentObservation()
		expected := budget
		if expected == 0 {
			expected = time.Duration(appconfig.Defaults().Connection.RequestTimeoutMS) * time.Millisecond
		}
		if actual != expected {
			t.Fatalf("request budget=%s, want %s", actual, expected)
		}
	}
}

func TestStatusLEDAllowsReplyBeyondOldHalfSecondLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	reported := make(chan error, 1)
	target := statusLEDBudgetTarget{send: func(request context.Context) error {
		timer := time.NewTimer(650 * time.Millisecond)
		defer timer.Stop()
		var err error
		select {
		case <-timer.C:
		case <-request.Done():
			err = request.Err()
		}
		result <- err
		cancel()
		return err
	}}
	arbiter := newStatusLEDArbiter(ctx, target, nil, func(err error) { reported <- err })
	arbiter.Observe(appconfig.DefaultStatusLEDPolicy(), controller.Snapshot{Connected: true, HaveStatus: true}, controller.Event{}, 3*time.Second)
	done := make(chan struct{})
	go func() { arbiter.Run(); close(done) }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("valid reply rejected before configured budget: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status request did not finish")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("arbiter did not stop")
	}
	select {
	case err := <-reported:
		t.Fatalf("valid delayed reply produced an error: %v", err)
	default:
	}
}

func TestStatusLEDStillReportsRealTransportFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failure := errors.New("serial write failed")
	reported := make(chan error, 1)
	target := statusLEDBudgetTarget{send: func(context.Context) error { cancel(); return failure }}
	arbiter := newStatusLEDArbiter(ctx, target, nil, func(err error) { reported <- err })
	arbiter.Observe(appconfig.DefaultStatusLEDPolicy(), controller.Snapshot{Connected: true, HaveStatus: true}, controller.Event{}, time.Second)
	done := make(chan struct{})
	go func() { arbiter.Run(); close(done) }()
	select {
	case err := <-reported:
		if !errors.Is(err, failure) {
			t.Fatalf("wrong failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual transport failure was suppressed")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("arbiter did not stop")
	}
}
