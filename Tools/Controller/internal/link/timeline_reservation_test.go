package link

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTimelineReservationDrainsActiveAndRejectsQueuedPresentation(t *testing.T) {
	session := NewForPort("RESERVATION", newFakePort())
	defer session.Close()
	a := &session.requestAdmission
	active, err := a.acquire(context.Background(), session.closing, RequestPriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() {
		release, err := a.acquire(context.Background(), session.closing, RequestPriorityNormal)
		if release != nil {
			release()
		}
		queued <- err
	}()
	waitForQueuedRequests(t, a, 1)
	reserved := make(chan func(), 1)
	failed := make(chan error, 1)
	go func() {
		release, err := session.ReserveTimeline(context.Background())
		if err != nil {
			failed <- err
			return
		}
		reserved <- release
	}()
	select {
	case err := <-queued:
		if !errors.Is(err, ErrTimelineReserved) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued presentation was not rejected")
	}
	select {
	case <-reserved:
		t.Fatal("reservation acknowledged before the active request drained")
	case err := <-failed:
		t.Fatal(err)
	default:
	}
	active()
	var releaseReservation func()
	select {
	case releaseReservation = <-reserved:
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("reservation did not drain")
	}
	defer releaseReservation()
	for _, priority := range []RequestPriority{RequestPriorityBackground, RequestPriorityNormal} {
		if _, err := a.acquire(context.Background(), session.closing, priority); !errors.Is(err, ErrTimelineReserved) {
			t.Fatalf("priority %d: %v", priority, err)
		}
	}
	for _, priority := range []RequestPriority{RequestPriorityTimeline, RequestPrioritySafety} {
		release, err := a.acquire(context.Background(), session.closing, priority)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	releaseReservation()
	release, err := a.acquire(context.Background(), session.closing, RequestPriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestTimelineReservationTimeoutDoesNotStrandAdmission(t *testing.T) {
	session := NewForPort("RESERVATION", newFakePort())
	defer session.Close()
	active, _ := session.requestAdmission.acquire(context.Background(), session.closing, RequestPriorityNormal)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := session.ReserveTimeline(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	active()
	release, err := session.requestAdmission.acquire(context.Background(), session.closing, RequestPriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
