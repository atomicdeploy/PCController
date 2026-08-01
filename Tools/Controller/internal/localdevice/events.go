package localdevice

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxEventBytes  = 64 << 10
	maxNoticeBytes = 4 << 10
)

var ErrInvalidEvent = errors.New("invalid local device event")

// EventType is one JSON event in the fixed Local Device v1 vocabulary.
type EventType string

const (
	EventSnapshotUpdated EventType = "snapshot.updated"
	EventActionCompleted EventType = "action.completed"
	EventDeviceNotice    EventType = "device.notice"
)

// Event is a strict JSON WebSocket event. Exactly one type-specific payload is
// permitted, preventing arbitrary upstream documents from reaching IPC.
type Event struct {
	Contract string        `json:"contract"`
	Type     EventType     `json:"type"`
	Sequence uint64        `json:"sequence"`
	At       time.Time     `json:"at"`
	Snapshot *Snapshot     `json:"snapshot,omitempty"`
	Result   *ActionResult `json:"result,omitempty"`
	Notice   string        `json:"notice,omitempty"`
}

// ParseEvent decodes and validates one bounded, fixed-shape JSON event.
func ParseEvent(payload []byte) (Event, error) {
	if len(payload) == 0 || len(payload) > maxEventBytes || !utf8.Valid(payload) {
		return Event{}, ErrInvalidEvent
	}
	var event Event
	if err := decodeStrictJSON(payload, &event); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if event.Contract != ContractVersion || event.Sequence == 0 || event.At.IsZero() {
		return Event{}, fmt.Errorf("%w: event identity", ErrInvalidEvent)
	}
	switch event.Type {
	case EventSnapshotUpdated:
		if event.Snapshot == nil || event.Result != nil || event.Notice != "" {
			return Event{}, fmt.Errorf("%w: snapshot payload shape", ErrInvalidEvent)
		}
		if err := validateSnapshot(*event.Snapshot); err != nil {
			return Event{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
		}
		copySnapshot := *event.Snapshot
		event.Snapshot = &copySnapshot
	case EventActionCompleted:
		if event.Result == nil || event.Snapshot != nil || event.Notice != "" {
			return Event{}, fmt.Errorf("%w: action payload shape", ErrInvalidEvent)
		}
		if event.Result.Contract != ContractVersion || !event.Result.Accepted ||
			!isKnownAction(event.Result.Action) || event.Result.CompletedAt.IsZero() {
			return Event{}, fmt.Errorf("%w: action result", ErrInvalidEvent)
		}
		copyResult := *event.Result
		if copyResult.Snapshot != nil {
			if err := validateSnapshot(*copyResult.Snapshot); err != nil {
				return Event{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
			}
			copySnapshot := *copyResult.Snapshot
			copyResult.Snapshot = &copySnapshot
		}
		event.Result = &copyResult
	case EventDeviceNotice:
		if event.Snapshot != nil || event.Result != nil || !validNotice(event.Notice) {
			return Event{}, fmt.Errorf("%w: notice payload shape", ErrInvalidEvent)
		}
	default:
		return Event{}, fmt.Errorf("%w: unsupported type %q", ErrInvalidEvent, event.Type)
	}
	return event, nil
}

func isKnownEvent(value EventType) bool {
	switch value {
	case EventSnapshotUpdated, EventActionCompleted, EventDeviceNotice:
		return true
	default:
		return false
	}
}

func validNotice(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || len([]byte(value)) > maxNoticeBytes {
		return false
	}
	for _, character := range value {
		if character == 0 || (character < 0x20 && character != '\n' && character != '\r' && character != '\t') || character == 0x7f {
			return false
		}
	}
	return true
}
