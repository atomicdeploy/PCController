package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type LampState uint8

const (
	StateUnknown LampState = iota
	StateOff
	StateOn
)

func ParseLampState(s string) LampState {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ON", "1", "TRUE", "HIGH":
		return StateOn
	case "OFF", "0", "FALSE", "LOW":
		return StateOff
	default:
		return StateUnknown
	}
}

func (s LampState) String() string {
	switch s {
	case StateOn:
		return "on"
	case StateOff:
		return "off"
	default:
		return "unknown"
	}
}

func (s LampState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON accepts the stable string representation emitted by the IPC
// and local HTTP API. Keeping this on the shared model lets every controller
// consume a Snapshot without maintaining a transport-specific shadow type.
func (s *LampState) UnmarshalJSON(data []byte) error {
	if s == nil {
		return fmt.Errorf("lamp state: cannot unmarshal into nil receiver")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = StateUnknown
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("lamp state must be a string: %w", err)
	}
	parsed := ParseLampState(value)
	if parsed == StateUnknown &&
		!strings.EqualFold(strings.TrimSpace(value), StateUnknown.String()) {
		return fmt.Errorf("invalid lamp state %q", value)
	}
	*s = parsed
	return nil
}

type Snapshot struct {
	State            LampState `json:"state"`
	HTTPReachable    bool      `json:"http_reachable"`
	WebSocketOnline  bool      `json:"websocket_online"`
	DeviceURL        string    `json:"device_url"`
	LastEvent        string    `json:"last_event,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	PendingCommand   string    `json:"pending_command,omitempty"`
	LastSource       string    `json:"last_source,omitempty"`
	LastChanged      time.Time `json:"last_changed,omitempty"`
	LastConnected    time.Time `json:"last_connected,omitempty"`
	ReconnectAttempt int       `json:"reconnect_attempt"`
}

type EventKind uint8

const (
	EventSnapshot EventKind = iota
	EventMessage
	EventBeep
	EventRaw
)

type Event struct {
	Kind          EventKind
	Snapshot      Snapshot
	PreviousState LampState
	InitialSync   bool
	Title         string
	Message       string
	Icon          int
	BeepCount     int
	Raw           string
	FromRemote    bool
}
