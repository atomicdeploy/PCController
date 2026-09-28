package fleet

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultHopLimit uint8 = 8
	MaximumHopLimit uint8 = 16

	routeIDBytes        = 16
	maximumArguments    = 32
	maximumArgumentRaw  = 4096
	maximumArgumentsRaw = 16 * 1024
)

var (
	boardIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,179}$`)
	selectorIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,179}$`)
	operationPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,179}$`)
	semanticPattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	argumentPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

var (
	ErrDeadlineExpired = errors.New("route deadline expired")
	ErrHopLimitReached = errors.New("route hop limit reached")
)

// BoardID is the canonical host-level identity of one physical or proxied
// board. Port names, peer names, and display labels are aliases, not BoardID.
// BoardID is intentionally absent from the legacy UART/COBS envelope.
type BoardID string

// Destination selects exactly one board, host instance, or mirror group.
type Destination struct {
	BoardID    BoardID `json:"board_id,omitempty"`
	InstanceID string  `json:"instance_id,omitempty"`
	GroupID    string  `json:"group_id,omitempty"`
}

// CoordinatorPosition orders one mirror group's semantic intents without
// relying on wall-clock comparisons.
type CoordinatorPosition struct {
	ID       string `json:"id,omitempty"`
	Epoch    uint64 `json:"epoch,omitempty"`
	Sequence uint64 `json:"sequence,omitempty"`
}

// Intent is a validated semantic operation. It is never a copied UART frame.
type Intent struct {
	Domain    string                     `json:"domain"`
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments,omitempty"`
}

// RouteEnvelope carries host-to-host routing state outside semantic command
// parameters. Authentication and authorization are deliberately not fields:
// every hop must establish and evaluate them in its own transport context.
type RouteEnvelope struct {
	RouteID          string              `json:"route_id"`
	OperationID      string              `json:"operation_id"`
	TraceID          string              `json:"trace_id"`
	OriginInstanceID string              `json:"origin_instance_id"`
	OriginBoardID    BoardID             `json:"origin_board_id,omitempty"`
	Target           Destination         `json:"target"`
	Coordinator      CoordinatorPosition `json:"coordinator,omitempty"`
	HopCount         uint8               `json:"hop_count"`
	HopLimit         uint8               `json:"hop_limit"`
	Deadline         time.Time           `json:"deadline"`
	Intent           Intent              `json:"intent"`
}

// UnmarshalJSON tolerates safe additive fields while decoding the living route
// contract. Writers emit only the current semantic fields.
func (route *RouteEnvelope) UnmarshalJSON(content []byte) error {
	type livingRoute RouteEnvelope
	var decoded livingRoute
	if err := json.Unmarshal(content, &decoded); err != nil {
		return err
	}
	*route = RouteEnvelope(decoded)
	return nil
}

// ValidateAt validates the bounded route shape at one deterministic instant.
// A route at its hop limit remains a valid received observation but cannot be
// advanced to another host.
func (route RouteEnvelope) ValidateAt(now time.Time) error {
	if err := validateRouteID(route.RouteID); err != nil {
		return err
	}
	if !canonicalMatch(operationPattern, route.OperationID) {
		return errors.New("operation id is invalid")
	}
	if !canonicalMatch(operationPattern, route.TraceID) {
		return errors.New("trace id is invalid")
	}
	if !canonicalMatch(selectorIDPattern, route.OriginInstanceID) {
		return errors.New("origin instance id is invalid")
	}
	if route.OriginBoardID != "" && !ValidBoardID(route.OriginBoardID) {
		return errors.New("origin board id is invalid")
	}
	if err := route.Target.Validate(); err != nil {
		return err
	}
	if route.HopLimit == 0 || route.HopLimit > MaximumHopLimit {
		return fmt.Errorf("route hop limit must be 1..%d", MaximumHopLimit)
	}
	if route.HopCount > route.HopLimit {
		return errors.New("route is already over its hop limit")
	}
	if route.Deadline.IsZero() {
		return errors.New("route deadline is required")
	}
	if !now.Before(route.Deadline) {
		return ErrDeadlineExpired
	}
	if err := route.Intent.Validate(); err != nil {
		return err
	}
	if err := route.Coordinator.validate(route.Target.GroupID != ""); err != nil {
		return err
	}
	return nil
}

// Advance validates and consumes exactly one network hop. It returns a copy so
// callers cannot mutate the received envelope in place before auditing it.
func (route RouteEnvelope) Advance(now time.Time) (RouteEnvelope, error) {
	if err := route.ValidateAt(now); err != nil {
		return RouteEnvelope{}, err
	}
	if route.HopCount >= route.HopLimit {
		return RouteEnvelope{}, ErrHopLimitReached
	}
	route.HopCount++
	route.Intent.Arguments = cloneArguments(route.Intent.Arguments)
	return route, nil
}

func ValidBoardID(value BoardID) bool {
	return canonicalMatch(boardIDPattern, string(value))
}

func (target Destination) Validate() error {
	selected := 0
	if target.BoardID != "" {
		selected++
		if !ValidBoardID(target.BoardID) {
			return errors.New("target board id is invalid")
		}
	}
	if target.InstanceID != "" {
		selected++
		if !canonicalMatch(selectorIDPattern, target.InstanceID) {
			return errors.New("target instance id is invalid")
		}
	}
	if target.GroupID != "" {
		selected++
		if !canonicalMatch(selectorIDPattern, target.GroupID) {
			return errors.New("target group id is invalid")
		}
	}
	if selected != 1 {
		return errors.New("route must select exactly one destination")
	}
	return nil
}

func (position CoordinatorPosition) validate(required bool) error {
	haveID := position.ID != ""
	haveEpoch := position.Epoch != 0
	haveSequence := position.Sequence != 0
	if !haveID && !haveEpoch && !haveSequence {
		if required {
			return errors.New("group route requires coordinator position")
		}
		return nil
	}
	if !haveID || !haveEpoch || !haveSequence {
		return errors.New("coordinator id, epoch, and sequence must be set together")
	}
	if !canonicalMatch(selectorIDPattern, position.ID) {
		return errors.New("coordinator id is invalid")
	}
	return nil
}

func (intent Intent) Validate() error {
	if !canonicalMatch(semanticPattern, intent.Domain) {
		return errors.New("intent domain is invalid")
	}
	if !canonicalMatch(semanticPattern, intent.Name) {
		return errors.New("intent name is invalid")
	}
	if len(intent.Arguments) > maximumArguments {
		return fmt.Errorf("intent arguments exceed %d entries", maximumArguments)
	}
	total := 0
	for key, value := range intent.Arguments {
		if !canonicalMatch(argumentPattern, key) {
			return fmt.Errorf("intent argument key %q is invalid", key)
		}
		if len(value) > maximumArgumentRaw {
			return fmt.Errorf("intent argument %q exceeds %d bytes", key, maximumArgumentRaw)
		}
		if !json.Valid(value) {
			return fmt.Errorf("intent argument %q is not valid JSON", key)
		}
		total += len(key) + len(value)
		if total > maximumArgumentsRaw {
			return fmt.Errorf("intent arguments exceed %d bytes", maximumArgumentsRaw)
		}
	}
	return nil
}

func validateRouteID(value string) error {
	if len(value) != routeIDBytes*2 {
		return errors.New("route id must be 32 hexadecimal characters")
	}
	if value != strings.ToLower(value) {
		return errors.New("route id must use canonical lowercase hexadecimal")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != routeIDBytes {
		return errors.New("route id must be 32 hexadecimal characters")
	}
	return nil
}

func canonicalMatch(pattern *regexp.Regexp, value string) bool {
	return value == strings.TrimSpace(value) && pattern.MatchString(value)
}

func cloneArguments(source map[string]json.RawMessage) map[string]json.RawMessage {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}
