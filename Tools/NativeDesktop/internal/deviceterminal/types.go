// Package deviceterminal owns the one optional Telnet-style connection to the
// configured Rayan Lamp. It deliberately keeps terminal traffic separate from
// lamp events: terminal output is ordered, potentially sensitive, and must
// never be routed through lossy notification/automation fan-out.
package deviceterminal

import (
	"context"
	"errors"
	"net"
	"time"
)

const (
	DefaultPort            = 23
	DefaultScrollbackBytes = 256 << 10
	DefaultReadBytes       = 32 << 10
	MaxReadBytes           = 64 << 10
	MaxInputBytes          = 4 << 10
	MaxLongPoll            = 8 * time.Second
)

var (
	ErrClosed        = errors.New("device terminal is closed")
	ErrBusy          = errors.New("device terminal is connecting")
	ErrNotConnected  = errors.New("device terminal is not connected")
	ErrReadOnly      = errors.New("device terminal is read-only because the firmware shell is on Serial")
	ErrInputTooLarge = errors.New("device terminal input exceeds 4096 bytes")
)

type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
)

// Snapshot contains connection metadata only. It never contains transcript
// bytes, credentials, or commands.
type Snapshot struct {
	State          State      `json:"state"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Session        uint64     `json:"session"`
	Sequence       uint64     `json:"sequence"`
	OldestSequence uint64     `json:"oldest_sequence,omitempty"`
	ReadOnly       bool       `json:"read_only,omitempty"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	LastActivity   *time.Time `json:"last_activity,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

// Chunk is one ordered terminal cursor item. DataBase64 preserves raw terminal
// bytes across JSON/IPC without assuming a character encoding. Reset is an
// empty, sequence-bearing marker: consumers must clear their visible
// transcript when ReadResult.Reset is true and still advance through every
// returned chunk, including that marker.
type Chunk struct {
	Sequence   uint64    `json:"sequence"`
	DataBase64 string    `json:"data_base64"`
	At         time.Time `json:"at"`
	Reset      bool      `json:"reset,omitempty"`
}

// ReadResult is cursor based. Reset is true when a reader's cursor belongs to
// an earlier session, fell behind bounded scrollback, or is ahead of this
// manager. The returned chunks establish the replacement transcript and
// include a zero-byte reset marker when a live purge created the reset.
type ReadResult struct {
	Snapshot Snapshot `json:"snapshot"`
	Chunks   []Chunk  `json:"chunks"`
	Reset    bool     `json:"reset,omitempty"`
}

// DialFunc is supplied by appcore so the terminal reuses the lamp client's
// cached .local resolution rather than creating a second resolver policy.
type DialFunc func(context.Context) (net.Conn, error)
