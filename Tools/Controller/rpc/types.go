// Package rpc defines PCController's transport-independent JSON-RPC client
// contract. In-process, native-local, and network adapters all use these exact
// envelopes; transport selection never changes method semantics.
package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	Version         = "2.0"
	MaxMessageBytes = 1024 * 1024

	TransportInProcess = "inproc"
	TransportNamedPipe = "named_pipe"
	TransportUnix      = "unix"
	TransportTCP       = "tcp"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Auth    string          `json:"auth,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (rpcError *RPCError) Error() string {
	if rpcError == nil {
		return ""
	}
	return rpcError.Message
}

// Endpoint is a live transport address. Scope and Priority are descriptive
// capability-negotiation fields; they never grant access by themselves.
type Endpoint struct {
	Transport string `json:"transport"`
	Address   string `json:"address"`
	Scope     string `json:"scope,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

func (endpoint Endpoint) Validate() error {
	transport := strings.ToLower(strings.TrimSpace(endpoint.Transport))
	if endpoint.Transport != transport {
		return errors.New("RPC endpoint transport must be canonical lower-case text")
	}
	if endpoint.Address != strings.TrimSpace(endpoint.Address) || endpoint.Address == "" {
		return errors.New("RPC endpoint address is required")
	}
	switch transport {
	case TransportNamedPipe, TransportUnix, TransportTCP:
		return nil
	case TransportInProcess:
		return errors.New("in-process RPC does not have a dialable endpoint")
	default:
		return fmt.Errorf("unsupported RPC endpoint transport %q", endpoint.Transport)
	}
}
