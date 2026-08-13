package hostui

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	ActionCapabilitiesKey = "app_actions"
	TUIActionCapabilities = "app.osc,app.page,app.progress,app.title"
	WebActionCapabilities = "app.page,app.progress,app.title"

	ActionStateQueued   = "queued"
	ActionStateApplied  = "applied"
	ActionStateRejected = "rejected"
	ActionStateTimeout  = "timeout"
	ActionStatePartial  = "partial"

	DefaultActionTimeout     = 5 * time.Second
	MaximumActionTimeout     = 30 * time.Second
	MaximumActionOperations  = 256
	ActionOperationRetention = 2 * time.Minute
)

var supportedActionCapabilities = map[string]struct{}{
	"app.page": {}, "app.title": {}, "app.progress": {}, "app.osc": {},
}

// ActionCapabilities is the canonical bounded presence value advertised by a
// client. Unknown capability names are rejected rather than silently becoming
// an unbounded extension bag.
func ActionCapabilities(values ...string) (string, error) {
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := supportedActionCapabilities[value]; !ok {
			return "", fmt.Errorf("unsupported app action capability %q", raw)
		}
		seen[value] = struct{}{}
	}
	ordered := make([]string, 0, len(seen))
	for value := range seen {
		ordered = append(ordered, value)
	}
	sort.Strings(ordered)
	return strings.Join(ordered, ","), nil
}

type ActionTargetOutcome struct {
	InstanceID string    `json:"instance_id"`
	Surface    string    `json:"surface"`
	State      string    `json:"state"`
	Reason     string    `json:"reason,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ActionOperation struct {
	OperationID string                `json:"operation_id"`
	Kind        string                `json:"kind"`
	Source      string                `json:"source,omitempty"`
	Selector    string                `json:"selector"`
	State       string                `json:"state"`
	Reason      string                `json:"reason,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	ExpiresAt   time.Time             `json:"expires_at"`
	Targets     []ActionTargetOutcome `json:"targets"`
}

type ActionAck struct {
	OperationID string `json:"operation_id"`
	InstanceID  string `json:"instance_id"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
}

type ActionOutcomeChange struct {
	OperationID string    `json:"operation_id"`
	Kind        string    `json:"kind"`
	InstanceID  string    `json:"instance_id,omitempty"`
	Surface     string    `json:"surface,omitempty"`
	State       string    `json:"state"`
	Reason      string    `json:"reason,omitempty"`
	At          time.Time `json:"at"`
}

type actionOperationRecord struct {
	operation   ActionOperation
	fingerprint string
	retainUntil time.Time
	timer       *time.Timer
}

// ActionCoordinator freezes one selector to the exact currently-live client
// set, correlates client acknowledgements, and keeps only a bounded recent
// operation history. Ordinary delivery is push-only; timers exist solely to
// turn an unacknowledged queued delivery into a truthful terminal outcome.
type ActionCoordinator struct {
	mu       sync.Mutex
	registry *InstanceRegistry
	publish  func(AppAction) error
	now      func() time.Time
	newID    func() (string, error)
	values   map[string]*actionOperationRecord
	observer func(ActionOutcomeChange)
	closed   bool
}

func NewActionCoordinator(registry *InstanceRegistry, publish func(AppAction) error) *ActionCoordinator {
	return &ActionCoordinator{
		registry: registry, publish: publish, now: time.Now,
		newID: newActionOperationID, values: make(map[string]*actionOperationRecord),
	}
}

func (coordinator *ActionCoordinator) SetObserver(observer func(ActionOutcomeChange)) {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	coordinator.observer = observer
	coordinator.mu.Unlock()
}

func (coordinator *ActionCoordinator) Submit(action AppAction, timeout time.Duration) (ActionOperation, error) {
	if coordinator == nil || coordinator.registry == nil || coordinator.publish == nil {
		return ActionOperation{}, errors.New("app action outcome coordinator is unavailable")
	}
	if HasCoordinatorNavigationMetadata(action.Metadata) {
		return ActionOperation{}, errors.New("navigation synchronization metadata is coordinator-owned")
	}
	if timeout == 0 {
		timeout = DefaultActionTimeout
	}
	if timeout < 0 || timeout > MaximumActionTimeout {
		return ActionOperation{}, fmt.Errorf("app action timeout must be 1..%d milliseconds", MaximumActionTimeout.Milliseconds())
	}
	normalized, err := NormalizeAppAction(action)
	if err != nil {
		return ActionOperation{}, err
	}
	selector := normalized.Target
	if selector == "" {
		selector = "*"
	}
	normalized.Target = selector
	fingerprint := actionFingerprint(normalized, timeout)
	now := coordinator.now().UTC()

	coordinator.mu.Lock()
	if coordinator.closed {
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action outcome coordinator is closed")
	}
	coordinator.pruneLocked(now)
	if normalized.OperationID != "" {
		if existing, ok := coordinator.values[normalized.OperationID]; ok {
			if existing.fingerprint != fingerprint {
				coordinator.mu.Unlock()
				return ActionOperation{}, errors.New("app action operation_id was already used for a different command")
			}
			operation := cloneActionOperation(existing.operation)
			coordinator.mu.Unlock()
			return operation, nil
		}
	} else {
		normalized.OperationID, err = coordinator.newID()
		if err != nil {
			coordinator.mu.Unlock()
			return ActionOperation{}, fmt.Errorf("create app action operation id: %w", err)
		}
		fingerprint = actionFingerprint(normalized, timeout)
	}
	if len(coordinator.values) >= MaximumActionOperations {
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action outcome history is full; retry after pending operations settle")
	}

	operation := ActionOperation{
		OperationID: normalized.OperationID, Kind: normalized.Kind,
		Source: normalized.Source, Selector: selector, State: ActionStateQueued,
		CreatedAt: now, ExpiresAt: now.Add(timeout), Targets: []ActionTargetOutcome{},
	}
	live := coordinator.registry.List()
	matched := resolveActionTargets(live, selector)
	if len(matched) == 0 {
		operation.State = ActionStateRejected
		operation.Reason = "target_not_live"
	}
	for _, instance := range matched {
		state, reason := actionDeliveryState(instance, normalized.Kind)
		operation.Targets = append(operation.Targets, ActionTargetOutcome{
			InstanceID: instance.ID, Surface: instance.Surface,
			State: state, Reason: reason, UpdatedAt: now,
		})
	}
	operation.State, operation.Reason = aggregateActionOperation(operation)
	record := &actionOperationRecord{
		operation: operation, fingerprint: fingerprint,
		retainUntil: operation.ExpiresAt.Add(ActionOperationRetention),
	}
	coordinator.values[operation.OperationID] = record
	changes := operationChanges(operation)
	if hasQueuedActionTarget(operation) {
		record.timer = time.AfterFunc(timeout, func() { coordinator.timeout(operation.OperationID) })
	}
	observer := coordinator.observer
	coordinator.mu.Unlock()
	notifyActionChanges(observer, changes)

	for _, target := range operation.Targets {
		if target.State != ActionStateQueued {
			continue
		}
		delivery := cloneAppAction(normalized)
		delivery.Target = target.InstanceID
		if publishErr := coordinator.publish(delivery); publishErr != nil {
			coordinator.rejectDelivery(operation.OperationID, target.InstanceID, "delivery_unavailable")
		}
	}
	return coordinator.Outcome(operation.OperationID)
}

func (coordinator *ActionCoordinator) Ack(value ActionAck) (ActionOperation, error) {
	if coordinator == nil || coordinator.registry == nil {
		return ActionOperation{}, errors.New("app action outcome coordinator is unavailable")
	}
	value.OperationID = strings.TrimSpace(value.OperationID)
	value.InstanceID = strings.TrimSpace(value.InstanceID)
	value.State = strings.ToLower(strings.TrimSpace(value.State))
	value.Reason = strings.TrimSpace(value.Reason)
	if !instanceIDPattern.MatchString(value.OperationID) || !instanceIDPattern.MatchString(value.InstanceID) {
		return ActionOperation{}, errors.New("app action acknowledgement identity is invalid")
	}
	if value.State != ActionStateApplied && value.State != ActionStateRejected {
		return ActionOperation{}, errors.New("app action acknowledgement state must be applied or rejected")
	}
	if err := validateActionReason(value.Reason); err != nil {
		return ActionOperation{}, err
	}
	if _, live := coordinator.registry.Get(value.InstanceID); !live {
		return ActionOperation{}, fmt.Errorf("app action target %q is not live", value.InstanceID)
	}
	now := coordinator.now().UTC()
	coordinator.mu.Lock()
	coordinator.pruneLocked(now)
	record, ok := coordinator.values[value.OperationID]
	if !ok {
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action operation is unknown or expired")
	}
	index := actionTargetIndex(record.operation.Targets, value.InstanceID)
	if index < 0 {
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action acknowledgement instance was not an exact operation target")
	}
	current := record.operation.Targets[index]
	if current.State != ActionStateQueued {
		if current.State == value.State && current.Reason == value.Reason {
			operation := cloneActionOperation(record.operation)
			coordinator.mu.Unlock()
			return operation, nil
		}
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action target already has a different terminal outcome")
	}
	record.operation.Targets[index].State = value.State
	record.operation.Targets[index].Reason = value.Reason
	record.operation.Targets[index].UpdatedAt = now
	record.operation.State, record.operation.Reason = aggregateActionOperation(record.operation)
	if !hasQueuedActionTarget(record.operation) && record.timer != nil {
		record.timer.Stop()
		record.timer = nil
	}
	change := actionChange(record.operation, record.operation.Targets[index])
	operation := cloneActionOperation(record.operation)
	observer := coordinator.observer
	coordinator.mu.Unlock()
	notifyActionChanges(observer, []ActionOutcomeChange{change})
	return operation, nil
}

func (coordinator *ActionCoordinator) Outcome(operationID string) (ActionOperation, error) {
	if coordinator == nil {
		return ActionOperation{}, errors.New("app action outcome coordinator is unavailable")
	}
	operationID = strings.TrimSpace(operationID)
	if !instanceIDPattern.MatchString(operationID) {
		return ActionOperation{}, errors.New("app action operation_id is invalid")
	}
	coordinator.mu.Lock()
	coordinator.pruneLocked(coordinator.now().UTC())
	record, ok := coordinator.values[operationID]
	if !ok {
		coordinator.mu.Unlock()
		return ActionOperation{}, errors.New("app action operation is unknown or expired")
	}
	operation := cloneActionOperation(record.operation)
	coordinator.mu.Unlock()
	return operation, nil
}

func (coordinator *ActionCoordinator) Close() {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	coordinator.closed = true
	for _, record := range coordinator.values {
		if record.timer != nil {
			record.timer.Stop()
		}
	}
	coordinator.values = make(map[string]*actionOperationRecord)
	coordinator.mu.Unlock()
}

func (coordinator *ActionCoordinator) timeout(operationID string) {
	now := coordinator.now().UTC()
	coordinator.mu.Lock()
	record, ok := coordinator.values[operationID]
	if !ok || coordinator.closed {
		coordinator.mu.Unlock()
		return
	}
	var changes []ActionOutcomeChange
	for index := range record.operation.Targets {
		if record.operation.Targets[index].State != ActionStateQueued {
			continue
		}
		record.operation.Targets[index].State = ActionStateTimeout
		record.operation.Targets[index].Reason = "ack_timeout"
		record.operation.Targets[index].UpdatedAt = now
		changes = append(changes, actionChange(record.operation, record.operation.Targets[index]))
	}
	record.operation.State, record.operation.Reason = aggregateActionOperation(record.operation)
	record.timer = nil
	observer := coordinator.observer
	coordinator.mu.Unlock()
	notifyActionChanges(observer, changes)
}

func (coordinator *ActionCoordinator) rejectDelivery(operationID, instanceID, reason string) {
	now := coordinator.now().UTC()
	coordinator.mu.Lock()
	record, ok := coordinator.values[operationID]
	if !ok {
		coordinator.mu.Unlock()
		return
	}
	index := actionTargetIndex(record.operation.Targets, instanceID)
	if index < 0 || record.operation.Targets[index].State != ActionStateQueued {
		coordinator.mu.Unlock()
		return
	}
	record.operation.Targets[index].State = ActionStateRejected
	record.operation.Targets[index].Reason = reason
	record.operation.Targets[index].UpdatedAt = now
	record.operation.State, record.operation.Reason = aggregateActionOperation(record.operation)
	if !hasQueuedActionTarget(record.operation) && record.timer != nil {
		record.timer.Stop()
		record.timer = nil
	}
	change := actionChange(record.operation, record.operation.Targets[index])
	observer := coordinator.observer
	coordinator.mu.Unlock()
	notifyActionChanges(observer, []ActionOutcomeChange{change})
}

func (coordinator *ActionCoordinator) pruneLocked(now time.Time) {
	for id, record := range coordinator.values {
		if now.Before(record.retainUntil) {
			continue
		}
		if record.timer != nil {
			record.timer.Stop()
		}
		delete(coordinator.values, id)
	}
}

func resolveActionTargets(live []AppInstance, selector string) []AppInstance {
	result := make([]AppInstance, 0, len(live))
	for _, instance := range live {
		if !TargetsInstance(selector, instance.ID, instance.Surface) {
			continue
		}
		// A wildcard means every live client which has an advertised or known
		// delivery path. Infrastructure-only registry entries such as the
		// coordinator bridge remain exact-targetable and truthfully reject.
		if selector == "*" && !knownActionDeliverySurface(instance.Surface) &&
			strings.TrimSpace(instance.Values[ActionCapabilitiesKey]) == "" {
			continue
		}
		result = append(result, instance)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func actionDeliveryState(instance AppInstance, kind string) (string, string) {
	advertised := strings.TrimSpace(instance.Values[ActionCapabilitiesKey])
	if advertised == "" {
		if knownActionDeliverySurface(instance.Surface) {
			return ActionStateQueued, ""
		}
		return ActionStateRejected, "delivery_not_supported"
	}
	for _, value := range strings.Split(advertised, ",") {
		if strings.EqualFold(strings.TrimSpace(value), kind) {
			return ActionStateQueued, ""
		}
	}
	return ActionStateRejected, "capability_not_advertised"
}

func knownActionDeliverySurface(surface string) bool {
	switch strings.ToLower(strings.TrimSpace(surface)) {
	case "tui", "webui":
		return true
	default:
		return false
	}
}

func aggregateActionOperation(operation ActionOperation) (string, string) {
	if len(operation.Targets) == 0 {
		return ActionStateRejected, firstNonemptyString(operation.Reason, "target_not_live")
	}
	counts := make(map[string]int)
	for _, target := range operation.Targets {
		counts[target.State]++
	}
	if counts[ActionStateQueued] > 0 {
		return ActionStateQueued, ""
	}
	if counts[ActionStateApplied] == len(operation.Targets) {
		return ActionStateApplied, ""
	}
	if counts[ActionStateRejected] == len(operation.Targets) {
		return ActionStateRejected, "all_targets_rejected"
	}
	if counts[ActionStateTimeout] == len(operation.Targets) {
		return ActionStateTimeout, "ack_timeout"
	}
	return ActionStatePartial, "mixed_target_outcomes"
}

func operationChanges(operation ActionOperation) []ActionOutcomeChange {
	if len(operation.Targets) == 0 {
		return []ActionOutcomeChange{{
			OperationID: operation.OperationID, Kind: operation.Kind,
			State: operation.State, Reason: operation.Reason, At: operation.CreatedAt,
		}}
	}
	result := make([]ActionOutcomeChange, 0, len(operation.Targets))
	for _, target := range operation.Targets {
		result = append(result, actionChange(operation, target))
	}
	return result
}

func actionChange(operation ActionOperation, target ActionTargetOutcome) ActionOutcomeChange {
	return ActionOutcomeChange{
		OperationID: operation.OperationID, Kind: operation.Kind,
		InstanceID: target.InstanceID, Surface: target.Surface,
		State: target.State, Reason: target.Reason, At: target.UpdatedAt,
	}
}

func notifyActionChanges(observer func(ActionOutcomeChange), values []ActionOutcomeChange) {
	if observer == nil {
		return
	}
	for _, value := range values {
		observer(value)
	}
}

func actionTargetIndex(values []ActionTargetOutcome, instanceID string) int {
	for index := range values {
		if values[index].InstanceID == instanceID {
			return index
		}
	}
	return -1
}

func hasQueuedActionTarget(operation ActionOperation) bool {
	for _, target := range operation.Targets {
		if target.State == ActionStateQueued {
			return true
		}
	}
	return false
}

func cloneActionOperation(value ActionOperation) ActionOperation {
	value.Targets = append([]ActionTargetOutcome(nil), value.Targets...)
	return value
}

func validateActionReason(value string) error {
	if len(value) > 256 {
		return errors.New("app action acknowledgement reason exceeds 256 characters")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("app action acknowledgement reason contains a control character")
		}
	}
	return nil
}

func actionFingerprint(action AppAction, timeout time.Duration) string {
	keys := make([]string, 0, len(action.Metadata))
	for key := range action.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, value := range []string{action.Kind, action.Value, action.Source, action.Target} {
		builder.WriteString(value)
		builder.WriteByte(0)
	}
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(action.Metadata[key])
		builder.WriteByte(0)
	}
	builder.WriteString(timeout.String())
	value := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(value[:])
}

func newActionOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func firstNonemptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
