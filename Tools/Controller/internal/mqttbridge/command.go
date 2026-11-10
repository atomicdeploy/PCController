package mqttbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
)

const maxCommandBytes = 16 * 1024
const maxReplayEntries = 2048

// Pakar-compatible addressing and receipts, with typed PCController methods.
type Command struct {
	Command   string          `json:"command"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	BoardID   string          `json:"board_id,omitempty"`
	BootID    string          `json:"boot_id,omitempty"`
	ExpiresAt int64           `json:"expires_at,omitempty"`
}

type Receipt struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Accepted  bool   `json:"accepted"`
	RequestID string `json:"request_id,omitempty"`
	BoardID   string `json:"board_id"`
	BootID    string `json:"boot_id"`
	Message   string `json:"message,omitempty"`
	Result    any    `json:"result,omitempty"`
}

type Identity struct{ BoardID, BootID string }

type Hooks struct {
	Identity func() Identity
	Observe  func() (Identity, any)
	Execute  func(context.Context, Command) Receipt
	Events   <-chan any
	State    <-chan struct{}
	Status   func(Status)
}

func admit(payload []byte, retained, duplicate bool, config Config, identity Identity, seen map[string]int64, now time.Time) (Command, Receipt) {
	r := Receipt{Type: "command-result", Code: "invalid_json", BoardID: identity.BoardID, BootID: identity.BootID}
	var command Command
	if len(payload) == 0 || len(payload) > maxCommandBytes {
		return command, r
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(new(any)) != io.EOF {
		return command, r
	}
	if len(command.RequestID) > 0 && len(command.RequestID) <= 40 && identifier(command.RequestID) {
		r.RequestID = command.RequestID
	} else {
		r.Code = "invalid_request_id"
		return command, r
	}
	switch {
	case retained || duplicate:
		r.Code = "replayed_delivery"
	case (command.Command == "status" || command.Command == "telemetry") && command.Method == "" && len(command.Params) == 0:
		r.Accepted, r.Code = true, "accepted"
	case command.Command != "rpc" || command.Method == "":
		r.Code = "unsupported_command"
	case !config.AllowCommands:
		r.Code = "read_only"
	case command.BoardID == "" || command.BootID == "":
		r.Code = "invalid_envelope"
	case identity.BoardID == "" || command.BoardID != identity.BoardID || command.BootID != identity.BootID:
		r.Code = "stale_owner"
	case command.ExpiresAt <= now.Unix() || command.ExpiresAt > now.Unix()+60:
		r.Code = "invalid_deadline"
	default:
		for id, expiry := range seen {
			if expiry <= now.Unix() {
				delete(seen, id)
			}
		}
		if _, found := seen[command.RequestID]; found {
			r.Code = "duplicate_request"
		} else if len(seen) >= maxReplayEntries {
			r.Code = "replay_cache_full"
		} else {
			seen[command.RequestID] = command.ExpiresAt
			r.Accepted, r.Code = true, "accepted"
		}
	}
	return command, r
}
