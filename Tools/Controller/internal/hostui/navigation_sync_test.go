package hostui

import (
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

const (
	participantEpoch1 = "11111111111111111111111111111111"
	participantEpoch2 = "22222222222222222222222222222222"
	participantEpoch3 = "33333333333333333333333333333333"
	participantEpoch4 = "44444444444444444444444444444444"
	groupEpoch1       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	groupEpoch2       = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func follower(id, page, epoch string, revision uint64) AppInstance {
	return AppInstance{
		ID: id, Surface: "tui", Page: page, State: "active", LeaseSeconds: 45,
		Values: map[string]string{
			NavigationSyncKey: NavigationSyncFollow, NavigationGroupKey: DefaultNavigationGroup,
			NavigationEpochKey: epoch, NavigationRevisionKey: formatNavigationRevision(revision),
		},
	}
}

func webFollower(id, page, epoch string, revision uint64) AppInstance {
	value := follower(id, page, epoch, revision)
	value.Surface = "webui"
	return value
}

func independent(id, page, epoch string, revision uint64) AppInstance {
	value := follower(id, page, epoch, revision)
	value.Values[NavigationSyncKey] = NavigationSyncIndependent
	return value
}

func formatNavigationRevision(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func TestNavigationReporterChangesOnlyItsEphemeralFollowMode(t *testing.T) {
	reporter, err := NewNavigationReporter(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := reporter.NextValues()[NavigationSyncKey]; got != NavigationSyncFollow {
		t.Fatalf("initial mode=%q", got)
	}
	reporter.SetFollow(false)
	if got := reporter.NextValues()[NavigationSyncKey]; got != NavigationSyncIndependent {
		t.Fatalf("independent mode=%q", got)
	}
	reporter.SetFollow(true)
	values := reporter.NextCatchUpValues()
	if values[NavigationSyncKey] != NavigationSyncFollow || values[NavigationCatchUpKey] != "true" {
		t.Fatalf("rejoined values=%#v", values)
	}
}

func deterministicCoordinator(epochs ...string) *NavigationCoordinator {
	coordinator := NewNavigationCoordinator()
	index := 0
	coordinator.newEpoch = func() (string, error) {
		value := epochs[index]
		index++
		return value, nil
	}
	return coordinator
}

func observeJoined(
	coordinator *NavigationCoordinator,
	instance AppInstance,
	live ...AppInstance,
) []AppAction {
	return coordinator.Observe(InstanceChange{Kind: "joined", Instance: instance}, live)
}

func observeUpdated(
	coordinator *NavigationCoordinator,
	instance AppInstance,
	live ...AppInstance,
) []AppAction {
	actions := coordinator.Observe(InstanceChange{Kind: "updated", Instance: instance}, live)
	if len(actions) != 0 {
		return actions
	}
	participant, ok := parseNavigationParticipant(instance)
	if !ok {
		return nil
	}
	outcome, err := coordinator.Commit(NavigationCommand{
		Group: participant.group, Source: participant.id, Page: participant.page,
		OperationID: participant.epoch + "-" + strconv.FormatUint(participant.revision, 10),
	}, live)
	if err != nil {
		return nil
	}
	result := make([]AppAction, 0, len(outcome.Actions))
	for _, action := range outcome.Actions {
		if action.Target != participant.id {
			result = append(result, action)
		}
	}
	return result
}

func TestNavigationCoordinatorConvergesThreeFollowersWithoutEcho(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "controls", participantEpoch2, 1)
	three := follower("tui:three", "dashboard", participantEpoch3, 1)

	if actions := observeJoined(coordinator, one, one); len(actions) != 0 {
		t.Fatalf("first follower should seed, actions=%#v", actions)
	}
	if actions := observeJoined(coordinator, two, one, two); len(actions) != 1 ||
		actions[0].Target != two.ID || actions[0].Value != "dashboard" {
		t.Fatalf("second follower catch-up=%#v", actions)
	}
	if actions := observeJoined(coordinator, three, one, two, three); len(actions) != 1 ||
		actions[0].Target != three.ID || actions[0].Metadata[NavigationRevisionKey] != "1" {
		t.Fatalf("third follower catch-up=%#v", actions)
	}

	one = follower(one.ID, "events", participantEpoch1, 2)
	actions := observeUpdated(coordinator, one, one, two, three)
	if len(actions) != 2 || actions[0].Target != three.ID && actions[0].Target != two.ID {
		t.Fatalf("three-way fanout=%#v", actions)
	}
	gotTargets := []string{actions[0].Target, actions[1].Target}
	if !reflect.DeepEqual(gotTargets, []string{"tui:three", "tui:two"}) {
		t.Fatalf("deterministic targets=%#v", gotTargets)
	}
	for _, action := range actions {
		if action.Value != "events" || action.Source != "navigation-sync" ||
			action.Metadata[NavigationEpochKey] != groupEpoch1 ||
			action.Metadata[NavigationRevisionKey] != "2" ||
			action.Metadata[NavigationSourceKey] != one.ID {
			t.Fatalf("fanout action=%#v", action)
		}
	}

	// Applying the pushed page causes the receiver to report it. The canonical
	// page is already events, so this acknowledgement is intentionally silent.
	two = follower(two.ID, "events", participantEpoch2, 2)
	if echo := observeUpdated(coordinator, two, one, two, three); len(echo) != 0 {
		t.Fatalf("receiver acknowledgement echoed=%#v", echo)
	}
}

func TestNavigationCoordinatorConvergesTUIAndWebUIFollowers(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	tui := follower("tui:one", "dashboard", participantEpoch1, 1)
	web := webFollower("tab:web:one", "settings", participantEpoch2, 1)
	observeJoined(coordinator, tui, tui)
	catchUp := observeJoined(coordinator, web, tui, web)
	if len(catchUp) != 1 || catchUp[0].Target != web.ID || catchUp[0].Value != "dashboard" {
		t.Fatalf("web follower catch-up=%#v", catchUp)
	}
	outcome, err := coordinator.Commit(NavigationCommand{
		Group: DefaultNavigationGroup, Source: web.ID, Page: "events", OperationID: "web-events-1",
	}, []AppInstance{tui, web})
	if err != nil || outcome.Page != "events" || len(outcome.Actions) != 2 {
		t.Fatalf("web commit outcome=%#v err=%v", outcome, err)
	}
	if outcome.Actions[0].Target != web.ID || outcome.Actions[1].Target != tui.ID {
		t.Fatalf("cross-surface targets=%#v", outcome.Actions)
	}
}

func TestNavigationCoordinatorCommitAcknowledgesSourceAndRejectsLateLeaseRollback(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)
	outcome, err := coordinator.Commit(NavigationCommand{
		Group: DefaultNavigationGroup, Source: one.ID, Page: "events", OperationID: "op-events-1",
	}, []AppInstance{one, two})
	if err != nil || outcome.Page != "events" || outcome.Revision != 2 || outcome.OperationID != "op-events-1" || len(outcome.Actions) != 2 {
		t.Fatalf("commit outcome=%#v err=%v", outcome, err)
	}
	if outcome.Actions[0].Target != one.ID || outcome.Actions[1].Target != two.ID ||
		outcome.Actions[0].Metadata[NavigationOperationKey] != outcome.OperationID {
		t.Fatalf("ordered source/follower actions=%#v", outcome.Actions)
	}
	// The old report path is presence-only: a late dashboard lease cannot undo
	// the acknowledged events selection.
	one = follower(one.ID, "dashboard", participantEpoch1, 2)
	if actions := coordinator.Observe(InstanceChange{Kind: "updated", Instance: one}, []AppInstance{one, two}); len(actions) != 0 {
		t.Fatalf("late lease published navigation=%#v", actions)
	}
	join := follower("tui:three", "settings", participantEpoch3, 1)
	catchUp := observeJoined(coordinator, join, one, two, join)
	if len(catchUp) != 1 || catchUp[0].Value != "events" {
		t.Fatalf("late lease rolled canonical state back: %#v", catchUp)
	}
}

func TestNavigationCoordinatorCommitIsIdempotentAndRejectsConflictingReuse(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)
	command := NavigationCommand{
		Group: DefaultNavigationGroup, Source: one.ID, Page: "events", OperationID: "same-operation-1",
	}
	first, err := coordinator.Commit(command, []AppInstance{one, two})
	if err != nil || len(first.Actions) != 2 {
		t.Fatalf("first outcome=%#v err=%v", first, err)
	}
	replay, err := coordinator.Commit(command, []AppInstance{one, two})
	if err != nil || len(replay.Actions) != 0 || replay.Page != first.Page ||
		replay.Revision != first.Revision || replay.Epoch != first.Epoch {
		t.Fatalf("replay outcome=%#v err=%v", replay, err)
	}
	command.Page = "settings"
	if _, err = coordinator.Commit(command, []AppInstance{one, two}); err == nil {
		t.Fatal("conflicting operation ID reuse was accepted")
	}
	second, err := coordinator.Commit(NavigationCommand{
		Group: DefaultNavigationGroup, Source: one.ID, Page: "settings", OperationID: "newer-operation-2",
	}, []AppInstance{one, two})
	if err != nil || second.Page != "settings" {
		t.Fatalf("newer outcome=%#v err=%v", second, err)
	}
	replay, err = coordinator.Commit(NavigationCommand{
		Group: DefaultNavigationGroup, Source: one.ID, Page: "events", OperationID: "same-operation-1",
	}, []AppInstance{one, two})
	if err != nil || replay.Page != "events" || replay.Revision != first.Revision {
		t.Fatalf("older retry replay=%#v err=%v", replay, err)
	}
	if coordinator.groups[DefaultNavigationGroup].page != "settings" {
		t.Fatalf("older retry rolled canonical page back to %q", coordinator.groups[DefaultNavigationGroup].page)
	}
}

func TestNavigationCoordinatorForgetsOperationsWhenSourceLeaseLeaves(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("web:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)
	command := NavigationCommand{
		Group: DefaultNavigationGroup, Source: one.ID, Page: "events", OperationID: "source-session-1",
	}
	if _, err := coordinator.Commit(command, []AppInstance{one, two}); err != nil {
		t.Fatal(err)
	}
	coordinator.Observe(InstanceChange{Kind: "left", Instance: one}, []AppInstance{two})
	if len(coordinator.operations) != 0 || len(coordinator.operationOrder) != 0 {
		t.Fatalf("departed source operations retained: map=%d order=%d", len(coordinator.operations), len(coordinator.operationOrder))
	}
}

func TestNavigationCommitLANBudgetHarness(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)
	durations := make([]time.Duration, 0, 100)
	for index := 0; index < 100; index++ {
		page := "events"
		if index%2 == 0 {
			page = "settings"
		}
		started := time.Now()
		outcome, err := coordinator.Commit(NavigationCommand{Group: DefaultNavigationGroup, Source: one.ID, Page: page, OperationID: "lan-" + strconv.Itoa(index)}, []AppInstance{one, two})
		durations = append(durations, time.Since(started))
		if err != nil || outcome.Page != page || len(outcome.Actions) != 2 {
			t.Fatalf("iteration %d outcome=%#v err=%v", index, outcome, err)
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[(len(durations)*95+99)/100-1]
	if p95 >= 250*time.Millisecond {
		t.Fatalf("coordinator p95=%s, need <250ms", p95)
	}
}

func TestNavigationCoordinatorResendsUnacknowledgedJoinCatchUp(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "settings", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	first := observeJoined(coordinator, two, one, two)
	if len(first) != 1 || first[0].Target != two.ID || first[0].Value != "dashboard" {
		t.Fatalf("initial catch-up=%#v", first)
	}
	// Simulate an event cursor reconnect that skipped the first targeted event:
	// the next lease refresh still reports the old page. It must receive the
	// same catch-up again rather than replacing the group's canonical page.
	two = follower(two.ID, "settings", participantEpoch2, 2)
	retry := observeUpdated(coordinator, two, one, two)
	if len(retry) != 1 || retry[0].Target != two.ID || retry[0].Value != "dashboard" ||
		retry[0].Metadata[NavigationRevisionKey] != "1" {
		t.Fatalf("retried catch-up=%#v", retry)
	}
	two = follower(two.ID, "dashboard", participantEpoch2, 3)
	if echo := observeUpdated(coordinator, two, one, two); len(echo) != 0 {
		t.Fatalf("catch-up acknowledgement echoed=%#v", echo)
	}
	one = follower(one.ID, "events", participantEpoch1, 2)
	if actions := observeUpdated(coordinator, one, one, two); len(actions) != 1 ||
		actions[0].Target != two.ID || actions[0].Value != "events" {
		t.Fatalf("post-ack fanout=%#v", actions)
	}
}

func TestNavigationCoordinatorReconnectCatchesUpWithoutRollingCanonicalBack(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)
	one = follower(one.ID, "events", participantEpoch1, 2)
	if actions := observeUpdated(coordinator, one, one, two); len(actions) != 1 ||
		actions[0].Target != two.ID || actions[0].Value != "events" {
		t.Fatalf("pre-reconnect fanout=%#v", actions)
	}

	// The disconnected TUI still shows dashboard. Its authoritative event
	// session reconnect must request canonical catch-up, not publish dashboard
	// as a new navigation intent that rolls the live group backward.
	two = follower(two.ID, "dashboard", participantEpoch2, 2)
	two.Values[NavigationCatchUpKey] = "true"
	catchUp := observeUpdated(coordinator, two, one, two)
	if len(catchUp) != 1 || catchUp[0].Target != two.ID || catchUp[0].Value != "events" ||
		catchUp[0].Metadata[NavigationRevisionKey] != "2" {
		t.Fatalf("reconnect catch-up=%#v", catchUp)
	}
	three := follower("tui:three", "settings", participantEpoch3, 1)
	join := observeJoined(coordinator, three, one, two, three)
	if len(join) != 1 || join[0].Target != three.ID || join[0].Value != "events" {
		t.Fatalf("reconnect rolled canonical page backward: %#v", join)
	}
}

func TestNavigationCoordinatorRejectsSourceReplayAndEpochReplacement(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "dashboard", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)

	one = follower(one.ID, "events", participantEpoch1, 3)
	if actions := observeUpdated(coordinator, one, one, two); len(actions) != 1 {
		t.Fatalf("fresh revision actions=%#v", actions)
	}
	for _, stale := range []AppInstance{
		follower(one.ID, "settings", participantEpoch1, 3),
		follower(one.ID, "controls", participantEpoch1, 2),
		follower(one.ID, "updates", participantEpoch4, 4),
	} {
		if actions := coordinator.Observe(InstanceChange{Kind: "updated", Instance: stale}, []AppInstance{stale, two}); len(actions) != 0 {
			t.Fatalf("stale/colliding report emitted=%#v", actions)
		}
	}

	// A later valid report proves the stale registry view did not roll the
	// coordinator's canonical page or source cursor backwards.
	one = follower(one.ID, "board", participantEpoch1, 4)
	actions := observeUpdated(coordinator, one, one, two)
	if len(actions) != 1 || actions[0].Value != "board" ||
		actions[0].Metadata[NavigationRevisionKey] != "3" {
		t.Fatalf("post-replay update=%#v", actions)
	}
}

func TestNavigationCoordinatorOptOutDoesNotChangeExplicitTargeting(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	optedOut := independent("tui:private", "settings", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	if actions := observeJoined(coordinator, optedOut, one, optedOut); len(actions) != 0 {
		t.Fatalf("opted-out join actions=%#v", actions)
	}
	one = follower(one.ID, "events", participantEpoch1, 2)
	if actions := observeUpdated(coordinator, one, one, optedOut); len(actions) != 0 {
		t.Fatalf("opted-out instance received group fanout=%#v", actions)
	}

	// Explicit navigation remains an ordinary exact-target app action. It has
	// no coordinator metadata and cannot enroll or advance a follower cursor.
	explicit := AppAction{Kind: "app.page", Value: "controls", Target: optedOut.ID, Source: "ipc"}
	var cursor NavigationCursor
	if page, accepted := cursor.Accept(explicit, DefaultNavigationGroup); accepted || page != "" {
		t.Fatalf("explicit action changed sync cursor page=%q accepted=%t", page, accepted)
	}
	if explicit.Target != optedOut.ID || HasCoordinatorNavigationMetadata(explicit.Metadata) {
		t.Fatalf("explicit target contract changed=%#v", explicit)
	}
}

func TestNavigationCoordinatorJoinLeaveExpiryAndReconnectCatchUp(t *testing.T) {
	coordinator := deterministicCoordinator(groupEpoch1, groupEpoch2)
	one := follower("tui:one", "dashboard", participantEpoch1, 1)
	two := follower("tui:two", "controls", participantEpoch2, 1)
	observeJoined(coordinator, one, one)
	observeJoined(coordinator, two, one, two)

	// One follower leaves while another keeps the group canonical alive.
	coordinator.Observe(InstanceChange{Kind: "left", Instance: one}, []AppInstance{two})
	three := follower("tui:three", "settings", participantEpoch3, 1)
	catchUp := observeJoined(coordinator, three, two, three)
	if len(catchUp) != 1 || catchUp[0].Target != three.ID || catchUp[0].Value != "dashboard" ||
		catchUp[0].Metadata[NavigationEpochKey] != groupEpoch1 {
		t.Fatalf("join catch-up after leave=%#v", catchUp)
	}

	// Both remaining leases have expired. A new live report reconciles them out
	// before it seeds a fresh in-memory group epoch and canonical page.
	four := follower("tui:four", "events", participantEpoch4, 1)
	if actions := observeJoined(coordinator, four, four); len(actions) != 0 {
		t.Fatalf("first follower after expiry should seed=%#v", actions)
	}
	one = follower(one.ID, "controls", participantEpoch1, 2)
	reconnect := observeJoined(coordinator, one, four, one)
	if len(reconnect) != 1 || reconnect[0].Target != one.ID || reconnect[0].Value != "events" ||
		reconnect[0].Metadata[NavigationEpochKey] != groupEpoch2 {
		t.Fatalf("reconnect catch-up=%#v", reconnect)
	}
}

func TestNavigationCursorRejectsOutOfOrderReplayAndForeignEpoch(t *testing.T) {
	newAction := func(epoch string, revision uint64, page string) AppAction {
		return AppAction{
			Kind: "app.page", Value: page, Target: "tui:one",
			Metadata: map[string]string{
				NavigationSyncKey:  NavigationSyncGroupUpdate,
				NavigationGroupKey: DefaultNavigationGroup,
				NavigationEpochKey: epoch, NavigationRevisionKey: formatNavigationRevision(revision),
				NavigationSourceKey: "tui:two",
			},
		}
	}
	var cursor NavigationCursor
	if page, ok := cursor.Accept(newAction(groupEpoch1, 2, "events"), DefaultNavigationGroup); !ok || page != "events" {
		t.Fatalf("fresh update page=%q accepted=%t", page, ok)
	}
	for _, stale := range []AppAction{
		newAction(groupEpoch1, 2, "settings"),
		newAction(groupEpoch1, 1, "controls"),
		newAction(groupEpoch2, 3, "updates"),
	} {
		if page, ok := cursor.Accept(stale, DefaultNavigationGroup); ok || page != "" {
			t.Fatalf("stale update accepted page=%q action=%#v", page, stale)
		}
	}
	cursor.Reset()
	if page, ok := cursor.Accept(newAction(groupEpoch2, 1, "updates"), DefaultNavigationGroup); !ok || page != "updates" {
		t.Fatalf("post-reconnect epoch page=%q accepted=%t", page, ok)
	}
}

func TestNavigationCursorRejectsOlderTargetPresenceGeneration(t *testing.T) {
	action := AppAction{
		Kind: "app.page", Value: "events", Target: "tui:one",
		Metadata: map[string]string{
			NavigationSyncKey: NavigationSyncGroupUpdate, NavigationGroupKey: DefaultNavigationGroup,
			NavigationEpochKey: groupEpoch1, NavigationRevisionKey: "1", NavigationSourceKey: "tui:two",
			NavigationTargetEpochKey: participantEpoch1, NavigationTargetRevisionKey: "3",
		},
	}
	var cursor NavigationCursor
	if _, ok := cursor.AcceptFor(action, DefaultNavigationGroup, participantEpoch1, 4); ok {
		t.Fatal("action for older target report was accepted")
	}
	action.Metadata[NavigationTargetRevisionKey] = "4"
	if page, ok := cursor.AcceptFor(action, DefaultNavigationGroup, participantEpoch1, 4); !ok || page != "events" {
		t.Fatalf("current target action page=%q accepted=%t", page, ok)
	}
	action.Metadata[NavigationTargetEpochKey] = participantEpoch2
	action.Metadata[NavigationRevisionKey] = "2"
	if _, ok := cursor.AcceptFor(action, DefaultNavigationGroup, participantEpoch1, 4); ok {
		t.Fatal("foreign target epoch was accepted")
	}
}

func TestNavigationReporterUsesUniqueInstanceAndMonotonicRevisions(t *testing.T) {
	reporter, err := NewNavigationReporter(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !instanceIDPattern.MatchString(reporter.InstanceID()) || reporter.InstanceID() == "tui:" {
		t.Fatalf("instance id=%q", reporter.InstanceID())
	}
	first, second := reporter.NextValues(), reporter.NextValues()
	if first[NavigationSyncKey] != NavigationSyncFollow ||
		first[NavigationGroupKey] != DefaultNavigationGroup ||
		first[NavigationRevisionKey] != "1" || second[NavigationRevisionKey] != "2" ||
		first[NavigationEpochKey] != second[NavigationEpochKey] {
		t.Fatalf("reporter values first=%#v second=%#v", first, second)
	}
	catchUp := reporter.NextCatchUpValues()
	if catchUp[NavigationCatchUpKey] != "true" || catchUp[NavigationRevisionKey] != "3" {
		t.Fatalf("reporter catch-up values=%#v", catchUp)
	}
	if operation := reporter.NextOperationID(); operation != reporter.epoch+"-1" {
		t.Fatalf("operation id=%q", operation)
	}
	if _, revision := reporter.Identity(); revision != 3 {
		t.Fatalf("operation changed presence revision to %d", revision)
	}
}
