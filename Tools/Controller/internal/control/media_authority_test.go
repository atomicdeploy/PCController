package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

func TestMediaAuthorityNegotiationRequiresOwnerConsentAndPause(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	request := func(id, op, requester string) (MediaAuthorityStatus, error) {
		return runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: id, Label: id, Operation: op, RequesterID: requester})
	}
	if _, err := request("a", "request", ""); err != nil {
		t.Fatal(err)
	}
	update := MediaPlaybackUpdate{ClientID: "a", Sequence: 1, Loaded: true, Playing: true, Rate: 1}
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	status, err := request("b", "request", "")
	if err != nil || status.OwnerID != "a" || len(status.Pending) != 1 {
		t.Fatalf("request: %+v %v", status, err)
	}
	if _, err = request("b", "accept", "b"); err == nil {
		t.Fatal("requester stole authority")
	}
	if _, err = request("a", "accept", "b"); err == nil {
		t.Fatal("handoff accepted while playing")
	}
	update.Sequence++
	update.Playing = false
	if _, err = runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	status, err = request("a", "accept", "b")
	if err != nil || status.OwnerID != "b" || len(status.Pending) != 0 {
		t.Fatalf("handoff: %+v %v", status, err)
	}
	if _, err = runtime.UpdateMediaPlayback(update); err == nil {
		t.Fatal("old publisher still admitted")
	}
	if runtime.MediaPlayback().Loaded {
		t.Fatal("old media clock remained loaded after transfer")
	}
	if _, err = runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "b", Sequence: 1, Loaded: true, Rate: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestMediaAuthorityExclusiveReservationSurvivesClockExpiry(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	_, _ = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Label: "Production", Operation: "request"})
	_, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
	runtime.mediaPlayback.mu.Unlock()
	status, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "b", Operation: "request"})
	var denied *MediaAuthorityError
	if !errors.As(err, &denied) || status.OwnerID != "a" || !status.Exclusive {
		t.Fatalf("lock: %+v %v", status, err)
	}
	if _, err = runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "b", Sequence: 1, Loaded: true, Rate: 1}); !errors.As(err, &denied) {
		t.Fatalf("clock bypassed lock: %v", err)
	}
	if _, err = runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "b", Revision: 1}); !errors.As(err, &denied) {
		t.Fatalf("prepare bypassed lock: %v", err)
	}
	if err = runtime.rejectExclusiveMediaControl(context.Background(), native.OpRelaySet); !errors.As(err, &denied) {
		t.Fatal("anonymous live output bypassed production lock")
	}
	if err = runtime.rejectExclusiveMediaControl(WithMediaActor(context.Background(), "a"), native.OpRelaySet); err != nil {
		t.Fatal(err)
	}
	if err = runtime.rejectExclusiveMediaControl(context.Background(), native.OpMediaClock); err != nil {
		t.Fatal("maintenance clock blocked")
	}
	if _, err = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "unlock"}); err != nil {
		t.Fatal(err)
	}
	if runtime.MediaAuthority().OwnerID != "" {
		t.Fatal("expired unlocked lease remained reserved")
	}
}

func TestMediaAuthorityRejectExpireAndSnapshotIsolation(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	_, _ = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "request"})
	_, _ = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "b", Operation: "request"})
	status := runtime.MediaAuthority()
	status.Pending[0].ClientID = "tampered"
	if runtime.MediaAuthority().Pending[0].ClientID != "b" {
		t.Fatal("snapshot mutated authority state")
	}
	if _, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "reject", RequesterID: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.MediaAuthority().Pending) != 0 {
		t.Fatal("rejected request remains")
	}
	_, _ = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "b", Operation: "request"})
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.authority.Pending[0].RequestedAt = time.Now().Add(-time.Minute)
	runtime.mediaPlayback.mu.Unlock()
	if _, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "accept", RequesterID: "b"}); err == nil {
		t.Fatal("expired handoff accepted")
	}
}

func TestMediaAuthorityPreparedPlanReservesPublisherBeforeClock(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	if _, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "a", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	var denied *MediaAuthorityError
	if _, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "b", Revision: 1}); !errors.As(err, &denied) {
		t.Fatalf("prepared plan replaced: %v", err)
	}
	if err := runtime.rejectExclusiveMediaControl(WithMediaActor(context.Background(), "b"), native.OpRelaySet); err != nil {
		t.Fatalf("idle observer cannot control: %v", err)
	}
	_, _ = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "lock"})
	runtime.emergencyStop.Store(true)
	if err := runtime.rejectExclusiveMediaControl(context.Background(), native.OpRelayAllOff); err != nil {
		t.Fatalf("E-STOP cleanup blocked: %v", err)
	}
}
