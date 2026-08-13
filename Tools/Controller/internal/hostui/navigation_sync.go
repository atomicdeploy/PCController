package hostui

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	NavigationSyncKey           = "navigation_sync"
	NavigationGroupKey          = "navigation_group"
	NavigationEpochKey          = "navigation_epoch"
	NavigationRevisionKey       = "navigation_revision"
	NavigationSourceKey         = "navigation_source"
	NavigationCatchUpKey        = "navigation_catch_up"
	NavigationOperationKey      = "navigation_operation_id"
	NavigationTargetEpochKey    = "navigation_target_epoch"
	NavigationTargetRevisionKey = "navigation_target_revision"

	NavigationSyncFollow      = "follow"
	NavigationSyncIndependent = "independent"
	NavigationSyncGroupUpdate = "group"
	DefaultNavigationGroup    = "default"
)

var navigationMetadataKeys = map[string]struct{}{
	NavigationSyncKey: {}, NavigationGroupKey: {}, NavigationEpochKey: {},
	NavigationRevisionKey: {}, NavigationSourceKey: {}, NavigationCatchUpKey: {},
	NavigationOperationKey: {}, NavigationTargetEpochKey: {}, NavigationTargetRevisionKey: {},
}

// HasCoordinatorNavigationMetadata reports whether an action contains state
// owned by the in-memory navigation coordinator. Network callers must use the
// explicit controller.app.navigate method instead of manufacturing this data.
func HasCoordinatorNavigationMetadata(metadata map[string]string) bool {
	for key := range metadata {
		if _, ok := navigationMetadataKeys[strings.ToLower(strings.TrimSpace(key))]; ok {
			return true
		}
	}
	return false
}

// NavigationReporter supplies one process-session epoch and a monotonically
// increasing source revision for an ephemeral client instance lease. It does not
// persist page selection or any terminal/editor state.
type NavigationReporter struct {
	mu        sync.Mutex
	epoch     string
	group     string
	mode      string
	revision  uint64
	operation uint64
}

func NewNavigationReporter(follow bool, group string) (*NavigationReporter, error) {
	epoch, err := newNavigationEpoch()
	if err != nil {
		return nil, err
	}
	group = strings.ToLower(strings.TrimSpace(group))
	if group == "" {
		group = DefaultNavigationGroup
	}
	if !instanceValuePattern.MatchString(group) {
		return nil, errors.New("navigation group is invalid")
	}
	mode := NavigationSyncIndependent
	if follow {
		mode = NavigationSyncFollow
	}
	return &NavigationReporter{epoch: epoch, group: group, mode: mode}, nil
}

// InstanceID returns a globally unique, bounded ID for this TUI process. The
// endpoint is intentionally excluded: two clients of one endpoint are still
// distinct instances.
func (reporter *NavigationReporter) InstanceID() string {
	if reporter == nil {
		return ""
	}
	return "tui:" + reporter.epoch
}

// SetFollow changes only this client instance's ephemeral membership. It does
// not persist UI navigation or alter another instance's opt-out choice.
func (reporter *NavigationReporter) SetFollow(follow bool) {
	if reporter == nil {
		return
	}
	reporter.mu.Lock()
	if follow {
		reporter.mode = NavigationSyncFollow
	} else {
		reporter.mode = NavigationSyncIndependent
	}
	reporter.mu.Unlock()
}

func (reporter *NavigationReporter) NextValues() map[string]string {
	return reporter.nextValues(false)
}

// NextCatchUpValues marks a report made after the remote event session has
// reconnected. The coordinator must answer with its canonical page instead of
// interpreting a possibly stale local page as a new navigation intent.
func (reporter *NavigationReporter) NextCatchUpValues() map[string]string {
	return reporter.nextValues(true)
}

// NextOperationID creates a bounded session-scoped correlation ID for a
// coordinator command. It is distinct from lease reports and survives neither
// process restart nor a reconnect to a different primary epoch.
func (reporter *NavigationReporter) NextOperationID() string {
	if reporter == nil {
		return ""
	}
	reporter.mu.Lock()
	reporter.operation++
	value := reporter.epoch + "-" + strconv.FormatUint(reporter.operation, 10)
	reporter.mu.Unlock()
	return value
}

// Identity returns the current report identity used to reject deliveries
// queued before a newer reconnect/catch-up presence report.
func (reporter *NavigationReporter) Identity() (string, uint64) {
	if reporter == nil {
		return "", 0
	}
	reporter.mu.Lock()
	epoch, revision := reporter.epoch, reporter.revision
	reporter.mu.Unlock()
	return epoch, revision
}

func (reporter *NavigationReporter) nextValues(catchUp bool) map[string]string {
	if reporter == nil {
		return nil
	}
	reporter.mu.Lock()
	reporter.revision++
	values := map[string]string{
		NavigationSyncKey: reporter.mode, NavigationGroupKey: reporter.group,
		NavigationEpochKey:    reporter.epoch,
		NavigationRevisionKey: strconv.FormatUint(reporter.revision, 10),
	}
	if catchUp && reporter.mode == NavigationSyncFollow {
		values[NavigationCatchUpKey] = "true"
	}
	reporter.mu.Unlock()
	return values
}

type navigationParticipant struct {
	id       string
	group    string
	epoch    string
	revision uint64
	page     string
	catchUp  bool
}

func parseNavigationParticipant(instance AppInstance) (navigationParticipant, bool) {
	surface := strings.ToLower(strings.TrimSpace(instance.Surface))
	if (surface != "tui" && surface != "webui") ||
		!strings.EqualFold(strings.TrimSpace(instance.Values[NavigationSyncKey]), NavigationSyncFollow) {
		return navigationParticipant{}, false
	}
	group := strings.ToLower(strings.TrimSpace(instance.Values[NavigationGroupKey]))
	if group == "" {
		group = DefaultNavigationGroup
	}
	epoch := strings.ToLower(strings.TrimSpace(instance.Values[NavigationEpochKey]))
	revision, err := strconv.ParseUint(strings.TrimSpace(instance.Values[NavigationRevisionKey]), 10, 64)
	page := strings.ToLower(strings.TrimSpace(instance.Page))
	if !instanceValuePattern.MatchString(group) || !validNavigationEpoch(epoch) ||
		err != nil || revision == 0 || page == "" || !instancePagePattern.MatchString(page) {
		return navigationParticipant{}, false
	}
	return navigationParticipant{
		id: instance.ID, group: group, epoch: epoch, revision: revision, page: page,
		catchUp: strings.EqualFold(
			strings.TrimSpace(instance.Values[NavigationCatchUpKey]), "true",
		),
	}, true
}

type navigationMember struct {
	group             string
	epoch             string
	revision          uint64
	page              string
	pending           bool
	operationID       string
	operationPage     string
	operationEpoch    string
	operationRevision uint64
}

type navigationGroup struct {
	epoch    string
	revision uint64
	page     string
	source   string
	members  map[string]struct{}
}

// NavigationCommand is an explicit, correlated request to change one group’s
// canonical page. Instance leases only establish membership and presence.
type NavigationCommand struct {
	Group       string `json:"group"`
	Source      string `json:"source"`
	Page        string `json:"page"`
	OperationID string `json:"operation_id"`
}

// NavigationOutcome is returned to the requesting client before it treats an
// optimistic page change as settled. Actions are the exact ordered deliveries
// the primary must publish to the source and every follower.
type NavigationOutcome struct {
	Group       string      `json:"group"`
	Epoch       string      `json:"group_epoch"`
	Revision    uint64      `json:"revision"`
	OperationID string      `json:"operation_id"`
	Page        string      `json:"page"`
	Actions     []AppAction `json:"-"`
}

// NavigationCoordinator serializes the canonical active page for each live
// follower group. Its entire lifetime is the primary process lifetime; active
// pages are deliberately never written to host configuration.
type NavigationCoordinator struct {
	mu       sync.Mutex
	groups   map[string]*navigationGroup
	members  map[string]navigationMember
	newEpoch func() (string, error)
}

func NewNavigationCoordinator() *NavigationCoordinator {
	return &NavigationCoordinator{
		groups: make(map[string]*navigationGroup), members: make(map[string]navigationMember),
		newEpoch: newNavigationEpoch,
	}
}

// Observe consumes one registry change after live has been pruned. Returned
// actions are exact-instance deliveries, which keeps opted-out clients out of
// normal group fan-out while leaving explicit controller.app.navigate intact.
func (coordinator *NavigationCoordinator) Observe(
	change InstanceChange,
	live []AppInstance,
) []AppAction {
	if coordinator == nil {
		return nil
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.reconcile(live)

	participant, follows := parseNavigationParticipant(change.Instance)
	if change.Kind == "left" || !follows {
		coordinator.removeMember(change.Instance.ID)
		return nil
	}
	// A stale report cannot resurrect an instance that was already pruned from
	// the authoritative live set.
	liveParticipant, liveNow := findNavigationParticipant(live, participant.id)
	if !liveNow || liveParticipant.epoch != participant.epoch ||
		liveParticipant.revision != participant.revision {
		return nil
	}

	previous, existed := coordinator.members[participant.id]
	if existed {
		// Instance IDs are process-session IDs. An epoch/group replacement under
		// one still-live ID is therefore a replay or identity collision.
		if previous.epoch != participant.epoch || previous.group != participant.group ||
			participant.revision <= previous.revision {
			return nil
		}
		previous.revision = participant.revision
		// A lease refresh is presence only. In particular, a late terminal-title
		// callback must never mutate canonical navigation or roll its source back.
		previous.page = participant.page
		group := coordinator.groups[participant.group]
		if group == nil {
			return coordinator.seed(participant)
		}
		if participant.catchUp {
			previous.pending = participant.page != group.page
			coordinator.members[participant.id] = previous
			return []AppAction{coordinator.action(group, participant.group, participant.id)}
		}
		if previous.pending {
			if group.page == participant.page {
				previous.pending = false
				coordinator.members[participant.id] = previous
				return nil
			}
			coordinator.members[participant.id] = previous
			return []AppAction{coordinator.action(group, participant.group, participant.id)}
		}
		coordinator.members[participant.id] = previous
		return nil
	}

	group := coordinator.groups[participant.group]
	if group == nil || len(group.members) == 0 {
		return coordinator.seed(participant)
	}
	coordinator.members[participant.id] = navigationMember{
		group: participant.group, epoch: participant.epoch,
		revision: participant.revision, page: participant.page,
		pending: participant.page != group.page,
	}
	group.members[participant.id] = struct{}{}
	// Every late join receives the current epoch/revision, even when its
	// default page happens to match, so its acceptance cursor is initialized.
	return []AppAction{coordinator.action(group, participant.group, participant.id)}
}

// Commit serializes a source command and returns an acknowledgement plus the
// immediate source-and-follower deliveries. It deliberately does not read a
// lease’s page field, which makes delayed lease reports harmless.
func (coordinator *NavigationCoordinator) Commit(command NavigationCommand, live []AppInstance) (NavigationOutcome, error) {
	if coordinator == nil {
		return NavigationOutcome{}, errors.New("navigation coordinator is unavailable")
	}
	command.Group = strings.ToLower(strings.TrimSpace(command.Group))
	if command.Group == "" {
		command.Group = DefaultNavigationGroup
	}
	command.Source = strings.TrimSpace(command.Source)
	command.Page = strings.ToLower(strings.TrimSpace(command.Page))
	command.OperationID = strings.TrimSpace(command.OperationID)
	if !instanceValuePattern.MatchString(command.Group) || !instanceIDPattern.MatchString(command.Source) ||
		!instancePagePattern.MatchString(command.Page) || command.Page == "" ||
		!instanceValuePattern.MatchString(command.OperationID) {
		return NavigationOutcome{}, errors.New("navigation command is invalid")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.reconcile(live)
	member, ok := coordinator.members[command.Source]
	if !ok || member.group != command.Group {
		return NavigationOutcome{}, errors.New("navigation source is not an active follower in this group")
	}
	group := coordinator.groups[command.Group]
	if group == nil {
		return NavigationOutcome{}, errors.New("navigation group is unavailable")
	}
	if member.operationID == command.OperationID {
		if member.operationPage != command.Page {
			return NavigationOutcome{}, errors.New("navigation operation ID was reused with a different page")
		}
		return NavigationOutcome{
			Group: command.Group, Epoch: member.operationEpoch,
			Revision: member.operationRevision, OperationID: command.OperationID,
			Page: member.operationPage,
		}, nil
	}
	changed := group.page != command.Page
	if changed {
		group.page = command.Page
		group.source = command.Source
		group.revision++
	}
	outcome := NavigationOutcome{Group: command.Group, Epoch: group.epoch, Revision: group.revision, OperationID: command.OperationID, Page: group.page}
	member.operationID = command.OperationID
	member.operationPage = outcome.Page
	member.operationEpoch = outcome.Epoch
	member.operationRevision = outcome.Revision
	coordinator.members[command.Source] = member
	if !changed {
		return outcome, nil
	}
	targets := make([]string, 0, len(group.members))
	for id := range group.members {
		targets = append(targets, id)
	}
	sort.Strings(targets)
	outcome.Actions = make([]AppAction, 0, len(targets))
	for _, target := range targets {
		action := coordinator.action(group, command.Group, target)
		action.Metadata[NavigationOperationKey] = command.OperationID
		outcome.Actions = append(outcome.Actions, action)
	}
	return outcome, nil
}

func (coordinator *NavigationCoordinator) seed(participant navigationParticipant) []AppAction {
	epoch, err := coordinator.newEpoch()
	if err != nil {
		return nil
	}
	group := &navigationGroup{
		epoch: epoch, revision: 1, page: participant.page, source: participant.id,
		members: map[string]struct{}{participant.id: {}},
	}
	coordinator.groups[participant.group] = group
	coordinator.members[participant.id] = navigationMember{
		group: participant.group, epoch: participant.epoch,
		revision: participant.revision, page: participant.page,
	}
	return nil
}

func (coordinator *NavigationCoordinator) reconcile(live []AppInstance) {
	followers := make(map[string]navigationParticipant)
	for _, instance := range live {
		if participant, ok := parseNavigationParticipant(instance); ok {
			followers[participant.id] = participant
		}
	}
	for id := range coordinator.members {
		_, ok := followers[id]
		if !ok {
			coordinator.removeMember(id)
		}
	}
}

func (coordinator *NavigationCoordinator) removeMember(id string) {
	member, ok := coordinator.members[id]
	if !ok {
		return
	}
	delete(coordinator.members, id)
	group := coordinator.groups[member.group]
	if group == nil {
		return
	}
	delete(group.members, id)
	if len(group.members) == 0 {
		delete(coordinator.groups, member.group)
	}
}

func (coordinator *NavigationCoordinator) fanout(
	group *navigationGroup,
	groupName, source string,
) []AppAction {
	targets := make([]string, 0, len(group.members))
	for id := range group.members {
		if id != source {
			targets = append(targets, id)
		}
	}
	sort.Strings(targets)
	actions := make([]AppAction, 0, len(targets))
	for _, target := range targets {
		actions = append(actions, coordinator.action(group, groupName, target))
	}
	return actions
}

func (coordinator *NavigationCoordinator) action(
	group *navigationGroup,
	groupName, target string,
) AppAction {
	member := coordinator.members[target]
	return AppAction{
		Kind: "app.page", Value: group.page, Source: "navigation-sync", Target: target,
		Metadata: map[string]string{
			NavigationSyncKey: NavigationSyncGroupUpdate, NavigationGroupKey: groupName,
			NavigationEpochKey:          group.epoch,
			NavigationRevisionKey:       strconv.FormatUint(group.revision, 10),
			NavigationSourceKey:         group.source,
			NavigationTargetEpochKey:    member.epoch,
			NavigationTargetRevisionKey: strconv.FormatUint(member.revision, 10),
		},
	}
}

func findNavigationParticipant(values []AppInstance, id string) (navigationParticipant, bool) {
	for _, instance := range values {
		if instance.ID == id {
			return parseNavigationParticipant(instance)
		}
	}
	return navigationParticipant{}, false
}

type NavigationUpdate struct {
	Page, Group, Epoch, Source, TargetEpoch string
	Revision, TargetRevision                uint64
}

func ParseNavigationUpdate(action AppAction) (NavigationUpdate, bool) {
	if !strings.EqualFold(strings.TrimSpace(action.Kind), "app.page") ||
		!strings.EqualFold(strings.TrimSpace(action.Metadata[NavigationSyncKey]), NavigationSyncGroupUpdate) {
		return NavigationUpdate{}, false
	}
	revision, err := strconv.ParseUint(strings.TrimSpace(action.Metadata[NavigationRevisionKey]), 10, 64)
	targetEpoch := strings.ToLower(strings.TrimSpace(action.Metadata[NavigationTargetEpochKey]))
	targetRevisionText := strings.TrimSpace(action.Metadata[NavigationTargetRevisionKey])
	hasTargetIdentity := targetEpoch != "" || targetRevisionText != ""
	var targetRevision uint64
	var targetErr error
	if targetEpoch != "" || targetRevisionText != "" {
		targetRevision, targetErr = strconv.ParseUint(targetRevisionText, 10, 64)
	}
	update := NavigationUpdate{
		Page:   strings.ToLower(strings.TrimSpace(action.Value)),
		Group:  strings.ToLower(strings.TrimSpace(action.Metadata[NavigationGroupKey])),
		Epoch:  strings.ToLower(strings.TrimSpace(action.Metadata[NavigationEpochKey])),
		Source: strings.TrimSpace(action.Metadata[NavigationSourceKey]), Revision: revision,
		TargetEpoch: targetEpoch, TargetRevision: targetRevision,
	}
	if err != nil || update.Revision == 0 || !instancePagePattern.MatchString(update.Page) ||
		update.Page == "" || !instanceValuePattern.MatchString(update.Group) ||
		!validNavigationEpoch(update.Epoch) || !instanceIDPattern.MatchString(update.Source) ||
		(hasTargetIdentity && (targetErr != nil || !validNavigationEpoch(update.TargetEpoch) || update.TargetRevision == 0)) {
		return NavigationUpdate{}, false
	}
	return update, true
}

// NavigationCursor rejects duplicates and out-of-order deliveries. A group
// epoch is immutable for a connected process; Reset must be called only after
// an authoritative primary-session reconnect before accepting a new epoch.
type NavigationCursor struct {
	Epoch    string
	Revision uint64
}

func (cursor *NavigationCursor) Accept(action AppAction, group string) (string, bool) {
	return cursor.AcceptFor(action, group, "", 0)
}

// AcceptFor additionally rejects a pushed action created for an older
// presence generation. Missing target identity remains accepted for rolling
// compatibility with an older coordinator.
func (cursor *NavigationCursor) AcceptFor(
	action AppAction, group, targetEpoch string, targetRevision uint64,
) (string, bool) {
	update, ok := ParseNavigationUpdate(action)
	if !ok || !strings.EqualFold(strings.TrimSpace(group), update.Group) {
		return "", false
	}
	if cursor.Epoch != "" && cursor.Epoch != update.Epoch {
		return "", false
	}
	if cursor.Epoch == update.Epoch && update.Revision <= cursor.Revision {
		return "", false
	}
	targetEpoch = strings.ToLower(strings.TrimSpace(targetEpoch))
	if update.TargetEpoch != "" && (targetEpoch == "" || update.TargetEpoch != targetEpoch ||
		update.TargetRevision < targetRevision) {
		return "", false
	}
	cursor.Epoch, cursor.Revision = update.Epoch, update.Revision
	return update.Page, true
}

func (cursor *NavigationCursor) Reset() {
	cursor.Epoch = ""
	cursor.Revision = 0
}

func newNavigationEpoch() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validNavigationEpoch(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}
