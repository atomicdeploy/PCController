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

func TestTimelinePriorityLeaseFollowsReservationAndClose(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		session := NewForPort("PRIORITY", newFakePort())
		acquired, released := 0, 0
		session.acquirePlaybackPriority = func() (func() error, error) { acquired++; return func() error { released++; return nil }, nil }
		release, err := session.ReserveTimeline(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if acquired != 1 || released != 0 {
			t.Fatal("priority not held by reservation")
		}
		if closeFirst {
			if err = session.Close(); err != nil {
				t.Fatal(err)
			}
		}
		release()
		release()
		if err = session.Close(); err != nil {
			t.Fatal(err)
		}
		if released != 1 {
			t.Fatalf("priority released %d times", released)
		}
	}
}

func TestTimelinePriorityFailureAndDrainTimeoutUnwind(t *testing.T) {
	session := NewForPort("PRIORITY", newFakePort())
	defer session.Close()
	failure := errors.New("priority denied")
	session.acquirePlaybackPriority = func() (func() error, error) { return nil, failure }
	if release, err := session.ReserveTimeline(context.Background()); release != nil || !errors.Is(err, failure) {
		t.Fatalf("release present=%v error=%v", release != nil, err)
	}
	if session.requestAdmission.reserved {
		t.Fatal("failed priority acquisition reserved wire")
	}
	released := 0
	session.acquirePlaybackPriority = func() (func() error, error) { return func() error { released++; return nil }, nil }
	active, err := session.requestAdmission.acquire(context.Background(), session.closing, RequestPriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = session.ReserveTimeline(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	active()
	if released != 1 || session.requestAdmission.reserved {
		t.Fatal("drain timeout retained priority/wire")
	}
}

func TestTimelinePriorityCleanupDoesNotBlockSafetyAdmission(t *testing.T) {
	session := NewForPort("PRIORITY", newFakePort())
	defer session.Close()
	entered, resume := make(chan struct{}), make(chan struct{})
	session.acquirePlaybackPriority = func() (func() error, error) {
		return func() error { close(entered); <-resume; return nil }, nil
	}
	release, err := session.ReserveTimeline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { release(); close(done) }()
	<-entered
	admitted := make(chan error, 1)
	go func() {
		releaseSafety, err := session.requestAdmission.acquire(context.Background(), session.closing, RequestPrioritySafety)
		if releaseSafety != nil {
			releaseSafety()
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		close(resume)
		<-done
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(resume)
		<-done
		t.Fatal("scheduling cleanup held safety admission")
	}
}
