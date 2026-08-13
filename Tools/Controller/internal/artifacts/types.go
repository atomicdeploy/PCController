// Package artifacts owns immutable host, firmware, EEPROM, and device-readback
// artifacts plus the explicit update operations that consume them.
package artifacts

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind identifies the validation and execution rules for an artifact.
type Kind string

const (
	KindFirmware       Kind = "firmware"
	KindEEPROM         Kind = "eeprom"
	KindFlashBackup    Kind = "flash-backup"
	KindHostExecutable Kind = "host-executable"
)

var supportedKinds = []Kind{
	KindFirmware, KindEEPROM, KindFlashBackup, KindHostExecutable,
}

// Descriptor is the stable, content-addressed representation exposed to all
// local, IPC, HTTP, WebSocket, and bridge clients.
type Descriptor struct {
	Kind             Kind              `json:"kind"`
	Name             string            `json:"name"`
	SHA256           string            `json:"sha256"`
	Bytes            int64             `json:"bytes"`
	CreatedAt        time.Time         `json:"created_at"`
	Source           string            `json:"source"`
	MediaType        string            `json:"media_type"`
	DownloadURL      string            `json:"download_url,omitempty"`
	BuildHash        string            `json:"build_hash,omitempty"`
	BuildTimestamp   string            `json:"build_timestamp,omitempty"`
	PackedTimestamp  uint32            `json:"packed_timestamp,omitempty"`
	Platform         string            `json:"platform,omitempty"`
	Embedded         bool              `json:"embedded,omitempty"`
	Current          bool              `json:"current,omitempty"`
	VerifiedReadback bool              `json:"verified_readback,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	LocalPath        string            `json:"-"`
}

// BoardIdentity supports hash/date comparison without introducing a firmware
// version number or claiming that a stored image is already running.
type BoardIdentity struct {
	Connected       bool   `json:"connected"`
	BuildHash       string `json:"build_hash,omitempty"`
	BuildTimestamp  string `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32 `json:"packed_timestamp,omitempty"`
}

// CurrentArtifacts separates a running/programmed image from a verified flash
// readback. Only FlashReadback may be served as "current device flash".
type CurrentArtifacts struct {
	Firmware      *Descriptor `json:"firmware,omitempty"`
	EEPROM        *Descriptor `json:"eeprom,omitempty"`
	FlashReadback *Descriptor `json:"flash_readback,omitempty"`
	Host          *Descriptor `json:"host,omitempty"`
}

// DefaultArtifacts identifies the bundled firmware and EEPROM candidates used
// for explicit first-install or recovery workflows.
type DefaultArtifacts struct {
	Firmware *Descriptor `json:"firmware,omitempty"`
	EEPROM   *Descriptor `json:"eeprom,omitempty"`
}

// Policy reports the authorization and remote-programming rules enforced by
// the primary artifact service.
type Policy struct {
	ExplicitAuthorizationRequired bool `json:"explicit_authorization_required"`
	RemoteProgrammingEnabled      bool `json:"remote_programming_enabled"`
}

// Comparison summarizes the running board and candidate build identities
// without treating timestamps as firmware versions.
type Comparison struct {
	DefaultFirmware    string `json:"default_firmware"`
	BoardBuildHash     string `json:"board_build_hash,omitempty"`
	CandidateHash      string `json:"candidate_hash,omitempty"`
	BoardTimestamp     string `json:"board_timestamp,omitempty"`
	CandidateTimestamp string `json:"candidate_timestamp,omitempty"`
}

// Manifest is the one-call discovery contract used by native and browser UIs.
type Manifest struct {
	Enabled         bool             `json:"enabled"`
	DefaultsEnabled bool             `json:"defaults_enabled"`
	Defaults        DefaultArtifacts `json:"defaults"`
	Current         CurrentArtifacts `json:"current"`
	Board           BoardIdentity    `json:"board"`
	Comparison      Comparison       `json:"comparison"`
	Policy          Policy           `json:"policy"`
	Update          *UpdateStatus    `json:"update,omitempty"`
}

// List is the transport-neutral collection returned by artifact inventory APIs.
type List struct {
	Artifacts []Descriptor `json:"artifacts"`
}

// PutOptions supplies identity and provenance metadata when importing content
// into the immutable artifact store.
type PutOptions struct {
	Kind             Kind
	Name             string
	Source           string
	ExpectedSHA256   string
	ExpectedBytes    int64
	BuildHash        string
	BuildTimestamp   string
	PackedTimestamp  uint32
	Platform         string
	Embedded         bool
	Current          bool
	VerifiedReadback bool
	Metadata         map[string]string
}

// FetchRequest describes a bounded remote artifact download and its optional
// integrity expectations.
type FetchRequest struct {
	URL             string `json:"url"`
	Kind            Kind   `json:"kind"`
	Name            string `json:"name,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	Bytes           int64  `json:"bytes,omitempty"`
	BearerToken     string `json:"bearer_token,omitempty"`
	BuildHash       string `json:"build_hash,omitempty"`
	BuildTimestamp  string `json:"build_timestamp,omitempty"`
	PackedTimestamp uint32 `json:"packed_timestamp,omitempty"`
	Platform        string `json:"platform,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

// UploadRequest carries a bounded artifact from a secondary local process to
// the primary owner over authenticated IPC. JSON encodes Data as base64.
type UploadRequest struct {
	Kind           Kind   `json:"kind"`
	Name           string `json:"name,omitempty"`
	Data           []byte `json:"data"`
	SHA256         string `json:"sha256,omitempty"`
	Bytes          int64  `json:"bytes,omitempty"`
	BuildHash      string `json:"build_hash,omitempty"`
	BuildTimestamp string `json:"build_timestamp,omitempty"`
	Platform       string `json:"platform,omitempty"`
}

// CaptureRequest authorizes a primary-host readback of selected device memory
// components through the requested programming transport.
type CaptureRequest struct {
	Components     []string `json:"components"`
	Authorized     bool     `json:"authorized"`
	Method         string   `json:"method,omitempty"`
	Port           string   `json:"port,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// UpdateRequest authorizes one immutable artifact application transaction.
type UpdateRequest struct {
	ArtifactSHA256 string `json:"artifact_sha256"`
	Authorized     bool   `json:"authorized"`
	Method         string `json:"method,omitempty"`
	Port           string `json:"port,omitempty"`
	// ReinitializeEEPROM is an explicit development data-loss exception.
	// The primary retains the raw pre-flash EEPROM backup but does not restore
	// incompatible semantic settings after the new firmware authenticates.
	ReinitializeEEPROM bool   `json:"reinitialize_eeprom,omitempty"`
	IdempotencyKey     string `json:"idempotency_key,omitempty"`
}

// OperationResult returns the queued or completed operation and any imported
// artifact produced by it.
type OperationResult struct {
	Operation UpdateStatus `json:"operation"`
	Artifact  *Descriptor  `json:"artifact,omitempty"`
	Reused    bool         `json:"reused,omitempty"`
}

// ProgrammingMethod is explicit telemetry for the transport that owned the
// update transaction; it is never inferred by API consumers from error text.
type ProgrammingMethod string

const (
	ProgrammingMethodNone    ProgrammingMethod = "none"
	ProgrammingMethodUrclock ProgrammingMethod = "urclock"
	ProgrammingMethodUSBasp  ProgrammingMethod = "usbasp"
)

// BootloaderOutcome describes only the UART bootloader attempt. ISP and host
// updates report not_attempted rather than pretending a bootloader was probed.
type BootloaderOutcome string

const (
	BootloaderNotAttempted BootloaderOutcome = "not_attempted"
	BootloaderSucceeded    BootloaderOutcome = "succeeded"
	BootloaderFailed       BootloaderOutcome = "failed"
	BootloaderTimedOut     BootloaderOutcome = "timed_out"
	BootloaderUnavailable  BootloaderOutcome = "unavailable"
)

// UpdateStatus is the durable progress snapshot exposed over local and remote
// controller transports.
type UpdateStatus struct {
	ID                   string            `json:"id"`
	Kind                 string            `json:"kind"`
	State                string            `json:"state"`
	ProgressPercent      int               `json:"progress_percent"`
	BytesDone            int64             `json:"bytes_done,omitempty"`
	BytesTotal           int64             `json:"bytes_total,omitempty"`
	StartedAt            time.Time         `json:"started_at,omitempty"`
	UpdatedAt            time.Time         `json:"updated_at,omitempty"`
	ArtifactSHA256       string            `json:"artifact_sha256,omitempty"`
	Detail               string            `json:"detail,omitempty"`
	ErrorCode            string            `json:"error_code,omitempty"`
	IdempotencyKey       string            `json:"idempotency_key,omitempty"`
	ProgrammingMethod    ProgrammingMethod `json:"programming_method,omitempty"`
	BootloaderOutcome    BootloaderOutcome `json:"bootloader_outcome,omitempty"`
	ISPFallbackSuggested bool              `json:"isp_fallback_suggested,omitempty"`
}

// ExecutionFailure is the typed boundary between a programmer adapter and the
// transport-neutral operation service.
type ExecutionFailure struct {
	Code                 string
	Method               ProgrammingMethod
	BootloaderOutcome    BootloaderOutcome
	ISPFallbackSuggested bool
	Cause                error
}

// Error implements error using the underlying programmer failure.
func (failure *ExecutionFailure) Error() string {
	if failure == nil || failure.Cause == nil {
		return "programming operation failed"
	}
	return failure.Cause.Error()
}

// Unwrap exposes the underlying programmer failure for errors.Is/errors.As.
func (failure *ExecutionFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

// NewExecutionFailure wraps a programmer error with structured transport and
// recovery telemetry; a nil cause produces a nil error.
func NewExecutionFailure(
	method ProgrammingMethod,
	outcome BootloaderOutcome,
	code string,
	suggestISP bool,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	return &ExecutionFailure{
		Code: strings.TrimSpace(code), Method: method, BootloaderOutcome: outcome,
		ISPFallbackSuggested: suggestISP, Cause: cause,
	}
}

// CapturedFile is returned by an injected hardware owner after a verified
// readback. Service imports it before publishing it to clients.
type CapturedFile struct {
	Kind            Kind
	Name            string
	Path            string
	BuildHash       string
	BuildTimestamp  string
	PackedTimestamp uint32
}

// ProgressFunc publishes operation state, percentage, and human-readable detail.
type ProgressFunc func(state string, percent int, detail string)

// Executor is implemented by the primary host. Tests use a fake, and secondary
// processes reach it through JSON-RPC rather than opening the serial port.
type Executor interface {
	Capture(ctx Context, request CaptureRequest, progress ProgressFunc) ([]CapturedFile, error)
	ProgramFirmware(ctx Context, artifact Descriptor, request UpdateRequest, progress ProgressFunc) error
	RestoreFlash(ctx Context, artifact Descriptor, request UpdateRequest, progress ProgressFunc) error
	ProgramEEPROM(ctx Context, artifact Descriptor, request UpdateRequest, progress ProgressFunc) error
	StageHostUpdate(ctx Context, artifact Descriptor, request UpdateRequest, progress ProgressFunc) error
}

// Context is the subset needed from context.Context. The alias keeps executor
// fakes small while retaining cancellation and deadlines in production.
type Context interface {
	Done() <-chan struct{}
	Err() error
	Deadline() (time.Time, bool)
	Value(key any) any
}

// EventSink publishes progress into the controller's existing event stream.
type EventSink func(kind, text string, metadata map[string]string)

// ValidKind reports whether value is a supported immutable artifact kind.
func ValidKind(value Kind) bool {
	for _, candidate := range supportedKinds {
		if value == candidate {
			return true
		}
	}
	return false
}

// ParseKind normalizes and validates a transport-provided artifact kind.
func ParseKind(value string) (Kind, error) {
	kind := Kind(strings.ToLower(strings.TrimSpace(value)))
	if !ValidKind(kind) {
		return "", fmt.Errorf("unsupported artifact kind %q", value)
	}
	return kind, nil
}

func normalizeSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 {
		return "", errors.New("SHA-256 must contain exactly 64 hexadecimal characters")
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", errors.New("SHA-256 contains a non-hexadecimal character")
		}
	}
	return value, nil
}
