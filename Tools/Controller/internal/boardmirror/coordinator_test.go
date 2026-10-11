package boardmirror

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/fleet"
)

type fakeAdapter struct {
	states map[fleet.BoardID]State
	writes []Write
	fail   bool
}

func (fake *fakeAdapter) Inspect(_ context.Context, member fleet.MirrorMember) (State, error) {
	state := fake.states[member.BoardID]
	values := map[string]fleet.MirrorCommand{}
	for key, value := range state.Values {
		values[key] = value
	}
	state.Values = values
	return state, nil
}
func (fake *fakeAdapter) Apply(_ context.Context, member fleet.MirrorMember, write Write) (Result, error) {
	fake.writes = append(fake.writes, write)
	if fake.fail {
		return Result{}, errors.New("transport failed")
	}
	state := fake.states[member.BoardID]
	state.Values[write.Command.Key()] = write.Command
	fake.states[member.BoardID] = state
	return Result{RouteID: write.Route.RouteID, BoardID: member.BoardID, Acknowledged: true, Verified: write.Command.Kind != "buzzer"}, nil
}
func newTestCoordinator(t *testing.T) (*Coordinator, *fakeAdapter) {
	t.Helper()
	fake := &fakeAdapter{states: map[fleet.BoardID]State{}}
	for _, board := range []fleet.BoardID{"one", "two"} {
		fake.states[board] = State{BoardID: board, Connected: true, Generation: 1, Values: map[string]fleet.MirrorCommand{"pwm.0": {Kind: "pwm", Channel: 0, Value: 0}, "buzzer": {Kind: "buzzer", DurationMS: 1}}}
	}
	coordinator := New(fake)
	config := fleet.MirrorConfig{LocalBoardID: "one", CoordinatorID: "owner", Groups: []fleet.MirrorGroup{{ID: "pair", Members: []fleet.MirrorMember{{BoardID: "one"}, {BoardID: "two", Peer: "remote"}}, Policy: fleet.MirrorPolicy{Domains: []string{"pwm", "buzzer"}, PWMChannels: []int{0}}}}}
	if err := coordinator.Configure(config); err != nil {
		t.Fatal(err)
	}
	return coordinator, fake
}
func TestMirrorFullDuplexStateAndNoEcho(t *testing.T) {
	coordinator, fake := newTestCoordinator(t)
	ctx := context.Background()
	coordinator.Poll(ctx)
	if len(fake.writes) != 0 {
		t.Fatal("initial baseline actuated outputs")
	}
	fake.states["one"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 123}
	coordinator.Poll(ctx)
	coordinator.Poll(ctx)
	coordinator.Poll(ctx)
	if len(fake.writes) != 1 || fake.states["two"].Values["pwm.0"].Value != 123 {
		t.Fatalf("forward mirror/echo: %+v", fake.writes)
	}
	fake.states["two"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 456}
	coordinator.Poll(ctx)
	coordinator.Poll(ctx)
	if len(fake.writes) != 2 || fake.states["one"].Values["pwm.0"].Value != 456 {
		t.Fatalf("reverse mirror failed: %+v", fake.writes)
	}
	for _, write := range fake.writes {
		if err := write.Route.ValidateAt(time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMirrorImmediateReverseChangeUsesVerifiedAckBaseline(t *testing.T) {
	coordinator, fake := newTestCoordinator(t)
	ctx := context.Background()
	coordinator.Poll(ctx)
	fake.states["one"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 123}
	coordinator.Poll(ctx)
	// Target changes back before any subsequent polling observation.
	fake.states["two"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 0}
	coordinator.Poll(ctx)
	if len(fake.writes) != 2 || fake.states["one"].Values["pwm.0"].Value != 0 {
		t.Fatalf("reverse change lost: %+v", fake.writes)
	}
}
func TestMirrorDisconnectRebaselineAndExplicitResync(t *testing.T) {
	coordinator, fake := newTestCoordinator(t)
	ctx := context.Background()
	coordinator.Poll(ctx)
	state := fake.states["two"]
	state.Connected = false
	fake.states["two"] = state
	fake.states["one"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 123}
	coordinator.Poll(ctx)
	state.Connected = true
	state.Generation++
	fake.states["two"] = state
	coordinator.Poll(ctx)
	coordinator.Poll(ctx)
	if len(fake.writes) != 0 {
		t.Fatal("reconnect replayed actuation")
	}
	results, err := coordinator.Resync(ctx, "pair", "one")
	if err != nil || len(results) != 1 || !results[0].Verified || fake.states["two"].Values["pwm.0"].Value != 123 {
		t.Fatalf("resync: %+v %v", results, err)
	}
}
func TestMirrorBuzzerEchoAndExpiredQueue(t *testing.T) {
	coordinator, fake := newTestCoordinator(t)
	ctx := context.Background()
	coordinator.Poll(ctx)
	command := fleet.MirrorCommand{Kind: "buzzer", FrequencyHz: 440, DurationMS: 50}
	coordinator.note(ctx, tone{"one", command, time.Now()})
	if coordinator.ObserveNote("two", command) {
		t.Fatal("mirrored note was accepted for host playback")
	}
	if len(fake.writes) != 1 {
		t.Fatal("buzzer echo recirculated")
	}
	if !coordinator.ObserveNote("two", command) {
		t.Fatal("genuine repeated note was discarded")
	}
	coordinator.note(ctx, <-coordinator.notes)
	if len(fake.writes) != 2 {
		t.Fatal("new note on second board not accepted")
	}
	coordinator.note(ctx, tone{"one", command, time.Now().Add(-time.Second)})
	if len(fake.writes) != 2 {
		t.Fatal("stale note played")
	}
}
func TestMirrorQuarantinesIdentityAndKeepsFailureHonest(t *testing.T) {
	coordinator, fake := newTestCoordinator(t)
	ctx := context.Background()
	coordinator.Poll(ctx)
	state := fake.states["two"]
	state.BoardID = "wrong"
	fake.states["two"] = state
	fake.states["one"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 123}
	coordinator.Poll(ctx)
	if len(fake.writes) != 0 || coordinator.Status()[0].Errors["two"] == "" {
		t.Fatal("identity mismatch did not quarantine")
	}
	state.BoardID = "two"
	fake.states["two"] = state
	coordinator.Poll(ctx)
	fake.fail = true
	fake.states["one"].Values["pwm.0"] = fleet.MirrorCommand{Kind: "pwm", Value: 456}
	coordinator.Poll(ctx)
	results := coordinator.Status()[0].Results
	if len(results) != 1 || results[0].Acknowledged || results[0].Verified || results[0].Error == "" {
		t.Fatalf("false success: %+v", results)
	}
}
