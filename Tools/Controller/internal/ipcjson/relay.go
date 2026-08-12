package ipcjson

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	// DefaultRelayHopLimit is deliberately small enough to expose accidental
	// topologies quickly while allowing useful host -> repeater -> edge paths.
	DefaultRelayHopLimit uint8 = 8
	// MaxRelayHopLimit is a protocol safety ceiling, not a caller-controlled
	// capability. Peers may choose a lower limit but never a higher one.
	MaxRelayHopLimit uint8 = 16
	relayTraceBytes        = 16
)

// RelayTrace is transport-owned state carried by cascaded bridge calls and
// relayed event envelopes. It provides bounded forwarding and cycle detection;
// it never conveys authentication or authorization.
type RelayTrace struct {
	ID    string `json:"id"`
	Hops  uint8  `json:"hops"`
	Limit uint8  `json:"limit"`
}

// ValidateRelayTrace rejects malformed, unbounded, or already-over-budget
// trace state before a host bridge uses it.
func ValidateRelayTrace(trace RelayTrace) error {
	trace.ID = strings.TrimSpace(trace.ID)
	if len(trace.ID) != relayTraceBytes*2 {
		return errors.New("relay trace id must be 32 hexadecimal characters")
	}
	decoded, err := hex.DecodeString(trace.ID)
	if err != nil || len(decoded) != relayTraceBytes {
		return errors.New("relay trace id must be 32 hexadecimal characters")
	}
	if trace.Limit == 0 || trace.Limit > MaxRelayHopLimit {
		return fmt.Errorf("relay hop limit must be 1..%d", MaxRelayHopLimit)
	}
	if trace.Hops > trace.Limit {
		return errors.New("relay trace is already over its hop limit")
	}
	return nil
}

func inheritRelayTrace(parent *RelayTrace, child *Request) error {
	if parent == nil {
		return nil
	}
	if err := ValidateRelayTrace(*parent); err != nil {
		return err
	}
	if child == nil {
		return errors.New("nested relay request is unavailable")
	}
	if child.Relay != nil {
		return errors.New("nested requests cannot override inherited relay metadata")
	}
	copy := *parent
	child.Relay = &copy
	return nil
}
