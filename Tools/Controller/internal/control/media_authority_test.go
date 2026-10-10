package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"strings"
	"sync"
	"testing"
	"time"
)

type authorityCleanupWire struct {
	*programStateWirePort
	entered chan struct{}
	release chan struct{}
	started sync.Once
	failure error
}

func (port *authorityCleanupWire) Write(data []byte) (int, error) {
	frame, err := native.Decode(data)
	if err != nil {
		return 0, err
	}
	if frame.Opcode == native.OpRelayAllOff {
		if port.failure != nil {
			return 0, port.failure
		}
		port.started.Do(func() { close(port.entered) })
		select {
		case <-port.release:
		case <-port.closed:
			return 0, errors.New("closed")
		}
	}
	return port.programStateWirePort.Write(data)
}

func TestMediaAuthorityStillRejectsCurrentGenerationCleanupFailure(t *testing.T) {
	runtime := New(Options{RequestTimeout: time.Second})
	defer runtime.Close()
	port := &authorityCleanupWire{programStateWirePort: newProgramStateWirePort(), failure: errors.New("test cleanup write failed")}
	runtime.attach(link.OpenResult{Session: link.NewForPort("AUTHORITY-TEST", port), Port: ports.Info{Name: "AUTHORITY-TEST"}, Hello: native.Hello{Name: "Virtual"}})
	if _, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "owner", Revision: 1, Actions: []MediaTimelineAction{
		{ID: "future", TimeMS: 1000, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 0}},
	}}); err != nil {
		t.Fatal(err)
	}
	status, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "owner", Operation: "release"})
	if err == nil || !strings.Contains(err.Error(), "test cleanup write failed") || status.OwnerID != "owner" {
		t.Fatalf("current-generation safety failure was ignored: %+v, %v", status, err)
	}
}

func TestMediaSnapshotsRemainAvailableDuringAuthoritySafetyCleanup(t *testing.T) {
	runtime := New(Options{RequestTimeout: time.Second})
	port := &authorityCleanupWire{programStateWirePort: newProgramStateWirePort(), entered: make(chan struct{}), release: make(chan struct{})}
	var released sync.Once
	release := func() { released.Do(func() { close(port.release) }) }
	t.Cleanup(func() { release(); runtime.Close() })
	runtime.attach(link.OpenResult{Session: link.NewForPort("AUTHORITY-TEST", port), Port: ports.Info{Name: "AUTHORITY-TEST"}, Hello: native.Hello{Name: "Virtual"}})
	if _, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "owner", Revision: 1, Actions: []MediaTimelineAction{
		{ID: "future", TimeMS: 1000, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 0}},
	}}); err != nil {
		t.Fatal(err)
	}
	cleanupDone := make(chan error, 1)
	go func() {
		_, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "owner", Operation: "release"})
		cleanupDone <- err
	}()
	select {
	case <-port.entered:
	case <-time.After(time.Second):
		t.Fatal("authority release did not enter safety cleanup")
	}
	readDone := make(chan MediaAuthorityStatus, 1)
	go func() {
		status := runtime.MediaAuthority()
		_ = runtime.MediaPlayback()
		readDone <- status
	}()
	select {
	case status := <-readDone:
		if status.OwnerID != "owner" {
			t.Fatalf("authority changed before safety cleanup completed: %+v", status)
		}
	case <-time.After(time.Second):
		t.Fatal("read-only media snapshots waited for blocked serial safety cleanup")
	}
	select {
	case err := <-cleanupDone:
		t.Fatalf("safety cleanup returned before the wire was released: %v", err)
	default:
	}
	release()
	select {
	case err := <-cleanupDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authority release did not finish after the wire was released")
	}
}

func TestMediaAuthorityRecoversFromSupersededBoardGeneration(t *testing.T) {
	for _, operation := range []string{"request", "release"} {
		t.Run(operation, func(t *testing.T) {
			runtime := New(Options{RequestTimeout: time.Second})
			port := &authorityCleanupWire{programStateWirePort: newProgramStateWirePort(), entered: make(chan struct{}), release: make(chan struct{})}
			t.Cleanup(func() { close(port.release); runtime.Close() })
			runtime.attach(link.OpenResult{Session: link.NewForPort("AUTHORITY-TEST", port), Port: ports.Info{Name: "AUTHORITY-TEST"}, Hello: native.Hello{Name: "Virtual"}})
			old, err := runtime.PrepareMediaTimeline(MediaTimelinePlan{ClientID: "owner", Revision: 1, Actions: []MediaTimelineAction{
				{ID: "future", TimeMS: 1000, Step: appconfig.MacroStep{Kind: "relay", Target: 7, Value: 0}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			// Simulate the same generation invalidation used on session recovery.
			// Keep the new board connected: stale cleanup must neither gate its
			// next publisher nor send output commands to this replacement session.
			runtime.mu.Lock()
			runtime.generation++
			runtime.mu.Unlock()
			client := "owner"
			if operation == "request" {
				client = "new-publisher"
				runtime.mediaPlayback.mu.Lock()
				runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
				runtime.mediaPlayback.mu.Unlock()
			}
			status, err := runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: client, Operation: operation})
			if err != nil {
				t.Fatalf("superseded generation blocked authority %s: %v", operation, err)
			}
			want := ""
			if operation == "request" {
				want = client
			}
			if status.OwnerID != want {
				t.Fatalf("authority = %+v, want owner %q", status, want)
			}
			select {
			case <-port.entered:
				t.Fatal("old generation cleanup targeted the replacement board")
			default:
			}
			if runtime.MediaTimeline().Generation != old.Generation {
				t.Fatal("old plan was silently retargeted instead of requiring reprepare")
			}
		})
	}
}

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
	if _, err = runtime.ChangeMediaAuthority(MediaAuthorityRequest{ClientID: "a", Operation: "unlock"}); err != nil {
		t.Fatal(err)
	}
	if runtime.MediaAuthority().OwnerID != "" {
		t.Fatal("expired unlocked lease remained reserved")
	}
}

func TestMediaAuthorityFreshLeaseResetsRestartedPublisherSequence(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	request := MediaAuthorityRequest{ClientID: "player:stable", Label: "Production", Operation: "request"}
	if _, err := runtime.ChangeMediaAuthority(request); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: request.ClientID, Sequence: 7578, Loaded: true, Rate: 1}); err != nil {
		t.Fatal(err)
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-mediaPlaybackLease - time.Second)
	runtime.mediaPlayback.mu.Unlock()

	status, err := runtime.ChangeMediaAuthority(request)
	if err != nil || status.OwnerID != request.ClientID {
		t.Fatalf("fresh lease: %+v %v", status, err)
	}
	reset := runtime.MediaPlayback()
	if reset.ClientID != request.ClientID || reset.Sequence != 0 || reset.Loaded || reset.Playing || reset.BoardSynced {
		t.Fatalf("old publisher state survived fresh lease: %+v", reset)
	}
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: request.ClientID, Sequence: 1, Loaded: true, Rate: 1}); err != nil {
		t.Fatalf("restarted publisher sequence rejected: %v", err)
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
