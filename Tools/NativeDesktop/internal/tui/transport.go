package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/model"
)

// TransportMode selects how the terminal UI talks to RayanLamp.
type TransportMode string

const (
	ModeAuto       TransportMode = "auto"
	ModeIPC        TransportMode = "ipc"
	ModeAPI        TransportMode = "api"
	ModeStandalone TransportMode = "standalone"
)

// Action is a state-changing operation supported by every transport.
type Action string

const (
	ActionToggle    Action = "toggle"
	ActionOn        Action = "on"
	ActionOff       Action = "off"
	ActionReconnect Action = "reconnect"
	ActionRefresh   Action = "refresh"
)

// ConfirmationError means the command endpoint acknowledged an operation, but
// the transport could not fetch the resulting snapshot. Callers must not retry
// the operation—especially Toggle—because it may already have taken effect.
type ConfirmationError struct {
	Action Action
	Cause  error
}

func (e *ConfirmationError) Error() string {
	if e == nil || e.Cause == nil {
		return "command was accepted but its resulting state is not confirmed"
	}
	return fmt.Sprintf(
		"%s was accepted but its resulting state is not confirmed: %v",
		actionTitle(e.Action),
		e.Cause,
	)
}

func (e *ConfirmationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Transport is the deliberately small boundary between the Bubble Tea model
// and the GUI/headless application. Implementations must not retry ambiguous
// state-changing commands: in particular, retrying Toggle could undo a command
// that reached the lamp before the response was lost.
type Transport interface {
	Mode() TransportMode
	Label() string
	Status(context.Context) (model.Snapshot, error)
	Execute(context.Context, Action) (model.Snapshot, error)
	Events() <-chan model.Event
	Close() error
}

// ConfigTransport is the complete, typed configuration capability shared by
// the named-pipe, authenticated loopback API, and standalone transports.
// PatchConfig accepts an object-shaped partial configuration and returns the
// normalized, persisted configuration applied by the primary dispatcher.
type ConfigTransport interface {
	GetConfig(context.Context) (config.Config, error)
	PatchConfig(context.Context, any) (config.Config, error)
}

// CommandTransport exposes the canonical application dispatcher without
// coupling the TUI to one concrete transport. Result is the command's raw JSON
// value, not an IPC or HTTP envelope, so callers can decode the same contract
// in attached and standalone modes.
type CommandTransport interface {
	ExecuteCommand(
		context.Context,
		string,
		any,
	) (json.RawMessage, error)
}

// ServingStatus is the observed loopback listener state. Enabled is durable
// intent; Running and URL describe the listener that actually exists.
type ServingStatus struct {
	Enabled bool
	Running bool
	URL     string
	Error   string
}

// ServingStatusTransport is implemented by a transport that owns the local
// listener directly (currently standalone mode). Attached GUI/API transports
// may omit it rather than infer runtime state from configuration.
type ServingStatusTransport interface {
	ServingStatus(context.Context) (ServingStatus, error)
}

// DiagnosticResult is the canonical read-only response returned by the
// application's diagnostics.fetch command. The TUI never talks to an
// arbitrary device URL directly; each transport reaches this result through
// the running application's authenticated API/IPC boundary or its dispatcher.
type DiagnosticResult struct {
	Endpoint string `json:"endpoint"`
	Content  string `json:"content"`
	Bytes    int    `json:"bytes"`
}

// DiagnosticsTransport is an optional read-only extension implemented by all
// built-in transports. Keeping it separate from Transport makes the capability
// explicit for embedders and preserves a useful error if a custom transport
// intentionally omits diagnostics.
type DiagnosticsTransport interface {
	FetchDiagnostics(context.Context, string) (DiagnosticResult, error)
}

// TerminalTransport is the optional raw device-terminal capability. The
// application owns the single connection; TUI clients only attach to its
// ordered cursor stream, so IPC/API and standalone mode have identical
// semantics.
type TerminalTransport interface {
	TerminalStatus(context.Context) (deviceterminal.Snapshot, error)
	TerminalConnect(context.Context) (deviceterminal.Snapshot, error)
	TerminalDisconnect(context.Context) (deviceterminal.Snapshot, error)
	TerminalRead(
		context.Context,
		uint64,
		int,
		time.Duration,
	) (deviceterminal.ReadResult, error)
	TerminalWrite(context.Context, []byte) (deviceterminal.Snapshot, error)
}

// QuitEventTransport is implemented by a primary transport that can receive a
// remote quit command. Run watches this channel so RayanLamp.exe cli --quit and
// installers shut down a standalone TUI just like the GUI primary.
type QuitEventTransport interface {
	QuitEvents() <-chan struct{}
}

// TransportOptions configures transport discovery. HTTPClient is primarily
// useful to callers embedding the TUI and to focused transport tests.
type TransportOptions struct {
	Mode           TransportMode
	ConfigPath     string
	APIURL         string
	APIToken       string
	RequestTimeout time.Duration
}

func ParseTransportMode(value string) (TransportMode, error) {
	mode := TransportMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case ModeAuto, ModeIPC, ModeAPI, ModeStandalone:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid mode %q (want auto, ipc, api, or standalone)",
			value,
		)
	}
}

func validateAction(action Action) error {
	switch action {
	case ActionToggle, ActionOn, ActionOff, ActionReconnect, ActionRefresh:
		return nil
	default:
		return fmt.Errorf("unsupported TUI action %q", action)
	}
}

func validateTerminalRead(limit int, wait time.Duration) error {
	if limit < 0 || limit > deviceterminal.MaxReadBytes {
		return fmt.Errorf(
			"terminal read limit must be between 0 and %d bytes",
			deviceterminal.MaxReadBytes,
		)
	}
	if wait < 0 || wait > deviceterminal.MaxLongPoll {
		return fmt.Errorf(
			"terminal wait must be between 0 and %s",
			deviceterminal.MaxLongPoll,
		)
	}
	return nil
}

func validateTerminalInput(data []byte) error {
	if len(data) == 0 {
		return errors.New("terminal input is empty")
	}
	if len(data) > deviceterminal.MaxInputBytes {
		return fmt.Errorf(
			"terminal input exceeds %d bytes",
			deviceterminal.MaxInputBytes,
		)
	}
	return nil
}

func decodeDirectTerminalResult(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode device terminal result: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode device terminal result: %w", err)
	}
	return nil
}

func decodeConfigCommandResult(
	source string,
	data json.RawMessage,
) (config.Config, error) {
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, fmt.Errorf("decode %s configuration: %w", source, err)
	}
	return cfg, nil
}

// validateDiagnosticEndpoint performs syntax validation only. The active
// application's dispatcher remains responsible for allow-list and expert-mode
// authorization.
func validateDiagnosticEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("enter an absolute endpoint such as /info")
	}
	if strings.TrimSpace(endpoint) != endpoint {
		return errors.New("endpoint must not have surrounding whitespace")
	}
	if strings.ContainsAny(endpoint, "\\\r\n\t ") {
		return errors.New("endpoint must be a clean URL path")
	}
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") {
		return errors.New("endpoint must be an absolute path beginning with one /")
	}
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return errors.New("endpoint must be a valid absolute path")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.ContainsAny(endpoint, "?#") {
		return errors.New("endpoint must not contain a query or fragment")
	}
	if parsed.Path != endpoint || path.Clean(parsed.Path) != parsed.Path {
		return errors.New("endpoint path must already be clean")
	}
	return nil
}

func decodeDiagnosticResult(
	requested string,
	data []byte,
) (DiagnosticResult, error) {
	var result DiagnosticResult
	if err := json.Unmarshal(data, &result); err != nil {
		return DiagnosticResult{}, fmt.Errorf("decode diagnostic result: %w", err)
	}
	return normalizeDiagnosticResult(requested, result)
}

func decodeDirectDiagnosticResult(
	requested string,
	value any,
) (DiagnosticResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return DiagnosticResult{}, fmt.Errorf("encode diagnostic result: %w", err)
	}
	return decodeDiagnosticResult(requested, data)
}

func normalizeDiagnosticResult(
	requested string,
	result DiagnosticResult,
) (DiagnosticResult, error) {
	if result.Endpoint == "" {
		result.Endpoint = requested
	}
	if result.Endpoint != requested {
		return DiagnosticResult{}, fmt.Errorf(
			"diagnostic response endpoint mismatch: got %q",
			result.Endpoint,
		)
	}
	if result.Bytes < 0 {
		return DiagnosticResult{}, errors.New("diagnostic response has an invalid byte count")
	}
	if result.Bytes == 0 && result.Content != "" {
		result.Bytes = len([]byte(result.Content))
	}
	return result, nil
}

func normalizeTransportOptions(options TransportOptions) (TransportOptions, error) {
	if options.Mode == "" {
		options.Mode = ModeAuto
	}
	mode, err := ParseTransportMode(string(options.Mode))
	if err != nil {
		return TransportOptions{}, err
	}
	options.Mode = mode
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 5 * time.Second
	}
	if options.RequestTimeout < 100*time.Millisecond || options.RequestTimeout > time.Minute {
		return TransportOptions{}, errors.New(
			"transport timeout must be between 100ms and 1m",
		)
	}
	options.ConfigPath = strings.TrimSpace(options.ConfigPath)
	options.APIURL = strings.TrimSpace(options.APIURL)
	options.APIToken = strings.TrimSpace(options.APIToken)
	return options, nil
}
