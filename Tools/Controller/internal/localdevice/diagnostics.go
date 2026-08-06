package localdevice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	InspectCapabilities = "capabilities"
	InspectSnapshot     = "snapshot"
)

var ErrUnsupportedInspection = errors.New("unsupported local device inspection")

// CapabilityInspection is a safe allowlisted projection. It cannot contain
// headers, cookies, credentials, response bodies, or unknown upstream fields.
type CapabilityInspection struct {
	Contract string       `json:"contract"`
	DeviceID string       `json:"device_id"`
	Name     string       `json:"name,omitempty"`
	Model    string       `json:"model,omitempty"`
	Firmware string       `json:"firmware,omitempty"`
	Actions  []ActionType `json:"actions"`
	Events   []EventType  `json:"events,omitempty"`
}

// SnapshotInspection omits display text and exposes only bounded state facts.
type SnapshotInspection struct {
	Contract              string     `json:"contract"`
	DeviceID              string     `json:"device_id"`
	Sequence              uint64     `json:"sequence"`
	Power                 PowerState `json:"power"`
	DisplayMessagePresent bool       `json:"display_message_present"`
	DisplayMessageBytes   int        `json:"display_message_bytes"`
	AlertPulses           int        `json:"alert_pulses,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// Inspect fetches only the two fixed safe projections.
func (client *Client) Inspect(ctx context.Context, resource string) (any, error) {
	switch resource {
	case InspectCapabilities:
		capabilities, err := client.Capabilities(ctx)
		if err != nil {
			return nil, err
		}
		return InspectCapabilityDocument(capabilities), nil
	case InspectSnapshot:
		snapshot, err := client.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		return InspectSnapshotDocument(snapshot), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedInspection, resource)
	}
}

// InspectCapabilityDocument makes a deterministic copy of safe fields only.
func InspectCapabilityDocument(value Capabilities) CapabilityInspection {
	actions := append([]ActionType(nil), value.Actions...)
	events := append([]EventType(nil), value.Events...)
	sort.Slice(actions, func(left, right int) bool { return actions[left] < actions[right] })
	sort.Slice(events, func(left, right int) bool { return events[left] < events[right] })
	return CapabilityInspection{
		Contract: value.Contract,
		DeviceID: value.DeviceID,
		Name:     value.Name,
		Model:    value.Model,
		Firmware: value.Firmware,
		Actions:  actions,
		Events:   events,
	}
}

// InspectSnapshotDocument intentionally represents display content by presence
// and byte length rather than retaining or returning the message itself.
func InspectSnapshotDocument(value Snapshot) SnapshotInspection {
	return SnapshotInspection{
		Contract:              value.Contract,
		DeviceID:              value.DeviceID,
		Sequence:              value.Sequence,
		Power:                 value.Power,
		DisplayMessagePresent: value.DisplayMessage != "",
		DisplayMessageBytes:   len([]byte(value.DisplayMessage)),
		AlertPulses:           value.AlertPulses,
		UpdatedAt:             value.UpdatedAt,
	}
}
