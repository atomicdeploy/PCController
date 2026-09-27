// Package sessionsnapshot persists one privacy-bounded diagnostic view of the
// most recently closed primary host session.
package sessionsnapshot

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/native"
)

const (
	Format           = "pccontroller.host-diagnostic-snapshot"
	Schema           = 2
	RecentEventLimit = 16
	maxSnapshotBytes = 1024 * 1024
)

// Source is the passive portion of the controller API needed at shutdown. It
// never asks the board for fresh data or mutates either host or MCU settings.
type Source interface {
	Snapshot() controller.Snapshot
	Timeline(time.Time, int) []controller.TimelineEntry
	OutputState() controller.OutputStreamState
}

// HostIdentity identifies the executable that produced a diagnostic snapshot.
// It deliberately excludes command-line arguments, environment, and config.
type HostIdentity struct {
	Title      string `json:"title"`
	Role       string `json:"role"`
	Version    string `json:"version"`
	SourceHash string `json:"source_hash"`
	BuildTime  string `json:"build_time"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	ProcessID  int    `json:"process_id"`
}

// Connection captures the last known transport and device identity without
// retaining selectors, authentication data, or the host configuration.
type Connection struct {
	Connected bool                `json:"connected"`
	Paused    bool                `json:"paused"`
	State     string              `json:"state"`
	UpdatedAt time.Time           `json:"updated_at,omitempty"`
	Device    controller.PortInfo `json:"device"`
}

// FrontPanel is a privacy-bounded live panel summary. LCD text can mirror host
// prompts, so the snapshot records its presence but never the line contents.
type FrontPanel struct {
	Schema            byte    `json:"schema"`
	RawSegments       [4]byte `json:"raw_segments"`
	Brightness        byte    `json:"brightness"`
	Blink             bool    `json:"blink"`
	SegmentsActive    bool    `json:"segments_active"`
	CategorySelector  bool    `json:"category_selector"`
	LCDAddress        byte    `json:"lcd_address"`
	LCDAvailable      bool    `json:"lcd_available"`
	LCDBacklight      bool    `json:"lcd_backlight"`
	LCDTextPresent    bool    `json:"lcd_text_present"`
	LCDTextOmitted    bool    `json:"lcd_text_omitted"`
	PressedKeys       byte    `json:"pressed_keys"`
	MenuPage          byte    `json:"menu_page"`
	ProgramMode       byte    `json:"program_mode"`
	HostCaptured      bool    `json:"host_captured"`
	HostState         byte    `json:"host_state"`
	HostEditableValue uint16  `json:"host_editable_value"`
}

// PWMSummary records the status stream's selected channel. Full 16-channel
// values are intentionally not queried while the primary host is shutting down.
type PWMSummary struct {
	Available       bool   `json:"available"`
	SelectedChannel byte   `json:"selected_channel"`
	SelectedValue   uint16 `json:"selected_value"`
	ErrorCount      byte   `json:"error_count"`
}

// TemperatureSummary preserves the two named board measurements in their
// exact wire unit so consumers do not lose precision through float conversion.
type TemperatureSummary struct {
	IlluminationCentiC    int16 `json:"illumination_centi_c"`
	IlluminationAvailable bool  `json:"illumination_available"`
	BTAudioCentiC         int16 `json:"bt_audio_centi_c"`
	BTAudioAvailable      bool  `json:"bt_audio_available"`
	Hot                   bool  `json:"hot"`
}

// BoardSettings is a decoded live-cache projection, not an EEPROM image. A
// dedicated type keeps the diagnostic schema symmetric and independently
// readable even when the protocol type adds computed JSON fields.
type BoardSettings struct {
	Flags                   byte   `json:"flags"`
	LightMode               byte   `json:"light_mode"`
	OnBrightness            byte   `json:"on_brightness"`
	OffBrightness           byte   `json:"off_brightness"`
	DisplayBrightness       byte   `json:"display_brightness"`
	StatusBrightness        byte   `json:"status_brightness"`
	OutputPersistence       byte   `json:"output_persistence"`
	StreamPeriodMS          uint16 `json:"stream_period_ms"`
	DefaultPage             byte   `json:"default_page"`
	ExtendedFlags           byte   `json:"extended_flags"`
	DisplayClosedBrightness byte   `json:"display_closed_brightness"`
	MotionExitHoldSeconds   byte   `json:"motion_exit_hold_seconds"`
	RelayRestoreMask        byte   `json:"relay_restore_mask"`
	MotionBreakMS           uint16 `json:"motion_break_ms"`
}

// RFObservation fingerprints a received code rather than storing a replayable
// value. Protocol diagnostics remain useful without turning this cache into a
// collection of remote-control credentials.
type RFObservation struct {
	EventID         uint64 `json:"event_id"`
	CodeFingerprint string `json:"code_fingerprint"`
	Bits            byte   `json:"bits"`
	Protocol        byte   `json:"protocol"`
	PulseUS         uint16 `json:"pulse_us,omitempty"`
}

// RFSummary combines the host learning state with recent unique observations.
type RFSummary struct {
	Learning     controller.RFLearnState `json:"learning"`
	Observations []RFObservation         `json:"observations,omitempty"`
}

// OutputStreams omits user-assigned melody/effect names while preserving the
// operational IDs and status-LED base needed for diagnostics.
type OutputStreams struct {
	MelodyID       uint64  `json:"melody_id,omitempty"`
	EffectID       uint64  `json:"effect_id,omitempty"`
	StatusBase     [4]byte `json:"status_base"`
	HaveStatusBase bool    `json:"have_status_base"`
}

// ArtifactHashes records only immutable content identities. Paths, remote
// origins, credentials, and host configuration remain outside this snapshot.
type ArtifactHashes struct {
	CurrentFirmwareSHA256 string `json:"current_firmware_sha256,omitempty"`
	CurrentEEPROMSHA256   string `json:"current_eeprom_sha256,omitempty"`
	CurrentFlashSHA256    string `json:"current_flash_readback_sha256,omitempty"`
	CurrentHostSHA256     string `json:"current_host_sha256,omitempty"`
	DefaultFirmwareSHA256 string `json:"default_firmware_sha256,omitempty"`
	DefaultEEPROMSHA256   string `json:"default_eeprom_sha256,omitempty"`
}

// ProgrammingOperation is the latest host-owned artifact operation. Active is
// explicit so a completed journal entry is never mistaken for an in-flight
// device write after a crash.
type ProgrammingOperation struct {
	ID                   string    `json:"id"`
	Kind                 string    `json:"kind"`
	State                string    `json:"state"`
	Active               bool      `json:"active"`
	ProgressPercent      int       `json:"progress_percent"`
	ArtifactSHA256       string    `json:"artifact_sha256,omitempty"`
	ProgrammingMethod    string    `json:"programming_method,omitempty"`
	BootloaderOutcome    string    `json:"bootloader_outcome,omitempty"`
	ISPFallbackSuggested bool      `json:"isp_fallback_suggested,omitempty"`
	ErrorCode            string    `json:"error_code,omitempty"`
	StartedAt            time.Time `json:"started_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

// RecoveryMarker is a privacy-safe projection of a durable programming
// session marker and its settings-snapshot hash. It is diagnostic evidence,
// never proof that an interrupted programmer write completed.
type RecoveryMarker struct {
	MarkerSHA256           string    `json:"marker_sha256"`
	TargetFirmwareSHA256   string    `json:"target_firmware_sha256,omitempty"`
	SettingsSnapshotSHA256 string    `json:"settings_snapshot_sha256,omitempty"`
	DeviceFingerprint      string    `json:"device_fingerprint"`
	PreparedAt             time.Time `json:"prepared_at"`
	Phase                  string    `json:"phase"`
	HostResult             string    `json:"host_result,omitempty"`
	DiagnosticState        string    `json:"diagnostic_state"`
	WarningCount           int       `json:"warning_count,omitempty"`
	RestorationPending     bool      `json:"restoration_pending"`
	WriteCompletionProven  bool      `json:"write_completion_proven"`
}

// OperationalContext is supplied by the host's artifact service and recovery
// marker inventory without contacting the board during shutdown.
type OperationalContext struct {
	Programming     *ProgrammingOperation `json:"programming,omitempty"`
	Artifacts       ArtifactHashes        `json:"artifacts"`
	RecoveryMarkers []RecoveryMarker      `json:"recovery_markers"`
}

// OperationalContextProvider supplies cached programming/artifact diagnostics.
type OperationalContextProvider func() (OperationalContext, error)

// EventSummary contains bounded diagnostic metadata only. Raw frame payloads,
// display strings, command text, and arbitrary event text are never persisted.
type EventSummary struct {
	ID         uint64            `json:"id"`
	Time       time.Time         `json:"time"`
	Kind       string            `json:"kind"`
	Lifecycle  string            `json:"lifecycle,omitempty"`
	State      string            `json:"state,omitempty"`
	Source     string            `json:"source,omitempty"`
	Target     string            `json:"target,omitempty"`
	Action     string            `json:"action,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ResetCause byte              `json:"reset_cause,omitempty"`
	ResetCount uint32            `json:"reset_count,omitempty"`
	RFObserved bool              `json:"rf_observed,omitempty"`
	RFBits     byte              `json:"rf_bits,omitempty"`
	RFProtocol byte              `json:"rf_protocol,omitempty"`
	RFPulseUS  uint16            `json:"rf_pulse_us,omitempty"`
}

// Completeness distinguishes unavailable cached sections from persistence
// failures and makes partial offline snapshots unambiguous to API consumers.
type Completeness struct {
	Connection     bool `json:"connection"`
	DeviceIdentity bool `json:"device_identity"`
	Hello          bool `json:"hello"`
	Status         bool `json:"status"`
	Settings       bool `json:"settings"`
	FrontPanel     bool `json:"front_panel"`
	PWM            bool `json:"pwm"`
	Temperatures   bool `json:"temperatures"`
	RF             bool `json:"rf"`
	Events         bool `json:"events"`
	Programming    bool `json:"programming"`
	Artifacts      bool `json:"artifacts"`
	Recovery       bool `json:"recovery"`
}

// Issue describes one missing or failed section without embedding raw data.
type Issue struct {
	Section string `json:"section"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Document is deliberately separate from the host config and the board's
// EEPROM image. Settings is only the last board-reported decoded live value.
type Document struct {
	Format                 string                `json:"format"`
	Schema                 int                   `json:"schema"`
	CapturedAt             time.Time             `json:"captured_at"`
	StorageClass           string                `json:"storage_class"`
	ContainsHostConfig     bool                  `json:"contains_host_config"`
	ContainsMCUEEPROMImage bool                  `json:"contains_mcu_eeprom_image"`
	Complete               bool                  `json:"complete"`
	Completeness           Completeness          `json:"completeness"`
	Errors                 []Issue               `json:"errors"`
	Warnings               []Issue               `json:"warnings"`
	Host                   HostIdentity          `json:"host"`
	Connection             Connection            `json:"connection"`
	Hello                  *controller.Hello     `json:"hello,omitempty"`
	Status                 *controller.Status    `json:"status,omitempty"`
	Settings               *BoardSettings        `json:"settings,omitempty"`
	SettingsSource         string                `json:"settings_source,omitempty"`
	FrontPanel             *FrontPanel           `json:"front_panel,omitempty"`
	FrontPanelUpdatedAt    time.Time             `json:"front_panel_updated_at,omitempty"`
	PWM                    *PWMSummary           `json:"pwm,omitempty"`
	Temperatures           *TemperatureSummary   `json:"temperatures,omitempty"`
	RF                     RFSummary             `json:"rf"`
	OutputStreams          OutputStreams         `json:"output_streams"`
	Programming            *ProgrammingOperation `json:"programming,omitempty"`
	Artifacts              ArtifactHashes        `json:"artifacts"`
	RecoveryMarkers        []RecoveryMarker      `json:"recovery_markers"`
	RecoveryDiagnosticOnly bool                  `json:"recovery_diagnostic_only"`
	InterruptedWriteProven bool                  `json:"interrupted_write_completion_proven"`
	LastImportantEventID   uint64                `json:"last_important_event_id"`
	RecentImportantEvents  []EventSummary        `json:"recent_important_events"`
}

// SaveResult is the compact value suitable for logs, events, and RPC replies.
type SaveResult struct {
	Path       string    `json:"path"`
	CapturedAt time.Time `json:"captured_at"`
	Complete   bool      `json:"complete"`
	ErrorCount int       `json:"error_count"`
	Bytes      int64     `json:"bytes"`
	SHA256     string    `json:"sha256"`
}

// Stored is returned by the read-only RPC surface while a session is running.
type Stored struct {
	Path     string    `json:"path"`
	Exists   bool      `json:"exists"`
	Snapshot *Document `json:"snapshot,omitempty"`
}

// Recorder saves at most once, so duplicate close paths cannot produce drift
// or rewrite the same graceful-exit evidence.
type Recorder struct {
	path         string
	source       Source
	hostIdentity func() HostIdentity
	now          func() time.Time
	once         sync.Once
	providerMu   sync.RWMutex
	operational  OperationalContextProvider
	result       SaveResult
	err          error
}

// SetOperationalContextProvider attaches the artifact/recovery cache used at
// graceful exit. It does not query hardware and may be set after construction.
func (recorder *Recorder) SetOperationalContextProvider(provider OperationalContextProvider) error {
	if recorder == nil {
		return errors.New("session snapshot recorder is unavailable")
	}
	if provider == nil {
		return errors.New("session snapshot operational context provider is required")
	}
	recorder.providerMu.Lock()
	recorder.operational = provider
	recorder.providerMu.Unlock()
	return nil
}

// NewRecorder validates the stable destination without touching the file.
func NewRecorder(path string, source Source, hostIdentity func() HostIdentity) (*Recorder, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || !filepath.IsAbs(path) {
		return nil, errors.New("session snapshot path must be absolute")
	}
	if source == nil {
		return nil, errors.New("session snapshot source is required")
	}
	if hostIdentity == nil {
		return nil, errors.New("session snapshot host identity provider is required")
	}
	return &Recorder{
		path: path, source: source, hostIdentity: hostIdentity, now: time.Now,
	}, nil
}

// Save captures only cached state and atomically replaces the rolling file.
func (recorder *Recorder) Save() (SaveResult, error) {
	if recorder == nil {
		return SaveResult{}, errors.New("session snapshot recorder is unavailable")
	}
	recorder.once.Do(func() {
		recorder.providerMu.RLock()
		provider := recorder.operational
		recorder.providerMu.RUnlock()
		var operational *OperationalContext
		var operationalErr error
		if provider != nil {
			value, err := provider()
			operational, operationalErr = &value, err
		}
		document := BuildWithOperationalContext(
			recorder.source, recorder.hostIdentity(), recorder.now().UTC(),
			operational, operationalErr,
		)
		content, err := encode(document)
		if err != nil {
			recorder.err = err
			return
		}
		recorder.result = SaveResult{
			Path: recorder.path, CapturedAt: document.CapturedAt,
			Complete: document.Complete, ErrorCount: len(document.Errors),
			Bytes: int64(len(content)), SHA256: sha256Hex(content),
		}
		if err := writeFileAtomicReplace(recorder.path, content, 0o600); err != nil {
			recorder.err = fmt.Errorf("persist graceful-exit diagnostic snapshot: %w", err)
		}
	})
	return recorder.result, recorder.err
}

// Stored returns the previous graceful session without contacting the board.
func (recorder *Recorder) Stored() (Stored, error) {
	if recorder == nil {
		return Stored{}, errors.New("session snapshot recorder is unavailable")
	}
	return Read(recorder.path)
}

// Build creates a deterministic, privacy-bounded document from cached state.
func Build(source Source, host HostIdentity, capturedAt time.Time) Document {
	return BuildWithOperationalContext(source, host, capturedAt, nil, nil)
}

// BuildWithOperationalContext adds passive artifact and recovery evidence to
// the existing cached board snapshot. Provider failures are explicit and do
// not erase otherwise useful shutdown state.
func BuildWithOperationalContext(
	source Source,
	host HostIdentity,
	capturedAt time.Time,
	operational *OperationalContext,
	operationalErr error,
) Document {
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	} else {
		capturedAt = capturedAt.UTC()
	}
	host = normalizedHostIdentity(host)
	document := Document{
		Format: Format, Schema: Schema, CapturedAt: capturedAt,
		StorageClass:       "host-diagnostic-cache",
		ContainsHostConfig: false, ContainsMCUEEPROMImage: false,
		Host: host, Errors: []Issue{}, Warnings: []Issue{},
		RecentImportantEvents: []EventSummary{}, RecoveryMarkers: []RecoveryMarker{},
		RecoveryDiagnosticOnly: true, InterruptedWriteProven: false,
	}
	if operational != nil {
		normalized, issues := normalizeOperationalContext(*operational)
		document.Programming = normalized.Programming
		document.Artifacts = normalized.Artifacts
		document.RecoveryMarkers = normalized.RecoveryMarkers
		document.Completeness.Programming = true
		document.Completeness.Artifacts = true
		document.Completeness.Recovery = true
		document.Warnings = append(document.Warnings, issues...)
	}
	if operationalErr != nil {
		document.Errors = append(document.Errors, Issue{
			Section: "operational_context", Code: "provider_failed",
			Message: "cached artifact or recovery context could not be fully collected",
		})
		document.Completeness.Programming = false
		document.Completeness.Artifacts = false
		document.Completeness.Recovery = false
	}
	if source == nil {
		document.Errors = append(document.Errors, Issue{
			Section: "capture", Code: "source_unavailable",
			Message: "cached controller state source is unavailable",
		})
		return document
	}
	live := source.Snapshot()
	document.Connection = Connection{
		Connected: live.Connected, Paused: live.Paused,
		State: bounded(live.ConnectionState, 64), UpdatedAt: live.ConnectionUpdated,
		Device: live.Port,
	}
	document.Completeness.Connection = strings.TrimSpace(live.ConnectionState) != "" || live.Connected
	document.Completeness.DeviceIdentity = haveDeviceIdentity(live.Port)
	if haveHello(live.Hello) {
		hello := live.Hello
		document.Hello = &hello
		document.Completeness.Hello = true
	} else {
		document.Errors = append(document.Errors, unavailable("hello"))
	}
	if live.HaveStatus {
		status := live.Status
		document.Status = &status
		document.Completeness.Status = true
		document.PWM = &PWMSummary{
			Available: status.PWMAvailable, SelectedChannel: status.PWMChannel,
			SelectedValue: status.PWMValue, ErrorCount: status.PWMErrors,
		}
		document.Temperatures = &TemperatureSummary{
			IlluminationCentiC:    status.TLEDCenti,
			IlluminationAvailable: native.TemperatureAvailable(status.Flags, status.TLEDCenti, native.StatusTemperatureLED),
			BTAudioCentiC:         status.TBTCenti,
			BTAudioAvailable:      native.TemperatureAvailable(status.Flags, status.TBTCenti, native.StatusTemperatureBT),
			Hot:                   status.Hot,
		}
		document.Completeness.PWM = true
		document.Completeness.Temperatures = true
	} else {
		document.Errors = append(document.Errors, unavailable("status"))
		document.Warnings = append(document.Warnings,
			unavailable("pwm"), unavailable("temperatures"))
	}
	if live.HaveSettings {
		settings := live.Settings
		document.Settings = &BoardSettings{
			Flags: settings.Flags, LightMode: settings.LightMode,
			OnBrightness: settings.OnBrightness, OffBrightness: settings.OffBrightness,
			DisplayBrightness: settings.DisplayBrightness,
			StatusBrightness:  settings.StatusBrightness,
			OutputPersistence: settings.OutputPersistence,
			StreamPeriodMS:    settings.StreamPeriodMS, DefaultPage: settings.DefaultPage,
			ExtendedFlags:           settings.ExtendedFlags,
			DisplayClosedBrightness: settings.DisplayClosedBrightness,
			MotionExitHoldSeconds:   settings.MotionExitHoldSeconds,
			RelayRestoreMask:        settings.RelayRestoreMask,
			MotionBreakMS:           settings.MotionBreakMS(),
		}
		document.SettingsSource = "board-reported-live-cache"
		document.Completeness.Settings = true
	} else {
		document.Errors = append(document.Errors, unavailable("settings"))
	}
	if live.HaveFrontPanel {
		panel := live.FrontPanel
		document.FrontPanel = &FrontPanel{
			Schema: panel.Schema, RawSegments: panel.RawSegments,
			Brightness: panel.Brightness, Blink: panel.Blink,
			SegmentsActive:   panel.SegmentsActive,
			CategorySelector: panel.CategorySelector,
			LCDAddress:       panel.LCDAddress, LCDAvailable: panel.LCDAvailable,
			LCDBacklight:   panel.LCDBacklight,
			LCDTextPresent: panel.LCDLine1 != "" || panel.LCDLine2 != "",
			LCDTextOmitted: true, PressedKeys: panel.PressedKeys,
			MenuPage: panel.MenuPage, ProgramMode: panel.ProgramMode,
			HostCaptured: panel.HostCaptured, HostState: panel.HostState,
			HostEditableValue: panel.HostEditableValue,
		}
		document.FrontPanelUpdatedAt = live.FrontPanelUpdated
		document.Completeness.FrontPanel = true
	} else {
		document.Warnings = append(document.Warnings, unavailable("front_panel"))
	}
	document.RF.Learning = live.RFLearning
	outputState := source.OutputState()
	document.OutputStreams = OutputStreams{
		MelodyID: outputState.MelodyID, EffectID: outputState.EffectID,
		StatusBase: outputState.StatusBase, HaveStatusBase: outputState.HaveStatusBase,
	}
	events := summarizeEvents(source.Timeline(time.Time{}, RecentEventLimit*2), RecentEventLimit)
	document.RecentImportantEvents = events
	document.Completeness.Events = true
	if len(events) != 0 {
		document.LastImportantEventID = events[len(events)-1].ID
	}
	document.RF.Observations = summarizeRF(source.Timeline(time.Time{}, RecentEventLimit*4), 8)
	document.Completeness.RF = live.RFLearning.Mode != "" || len(document.RF.Observations) != 0
	if !document.Completeness.RF {
		document.Warnings = append(document.Warnings, unavailable("rf"))
	}
	document.Complete = document.Completeness.Hello &&
		document.Completeness.Status && document.Completeness.Settings && operationalErr == nil
	return document
}

// RecoveryDiagnosticInput is the explicitly non-authoritative subset accepted
// by migration/recovery diagnostics. Consuming it never advances a lifecycle
// marker or reports an interrupted write as successful.
type RecoveryDiagnosticInput struct {
	SnapshotCapturedAt    time.Time             `json:"snapshot_captured_at"`
	HostSourceHash        string                `json:"host_source_hash,omitempty"`
	BoardBuildHash        uint32                `json:"board_build_hash,omitempty"`
	BoardBuildTimestamp   uint32                `json:"board_build_timestamp,omitempty"`
	Programming           *ProgrammingOperation `json:"programming,omitempty"`
	Artifacts             ArtifactHashes        `json:"artifacts"`
	RecoveryMarkers       []RecoveryMarker      `json:"recovery_markers"`
	WriteCompletionProven bool                  `json:"write_completion_proven"`
}

// ConsumeRecoveryDiagnosticSnapshot validates the stored schema and returns a
// safe advisory input. The rolling snapshot remains intact for later audit.
func ConsumeRecoveryDiagnosticSnapshot(path string) (RecoveryDiagnosticInput, error) {
	stored, err := Read(path)
	if err != nil {
		return RecoveryDiagnosticInput{}, err
	}
	if !stored.Exists || stored.Snapshot == nil {
		return RecoveryDiagnosticInput{}, errors.New("session snapshot does not exist")
	}
	document := stored.Snapshot
	input := RecoveryDiagnosticInput{
		SnapshotCapturedAt:    document.CapturedAt,
		HostSourceHash:        document.Host.SourceHash,
		Programming:           cloneProgrammingOperation(document.Programming),
		Artifacts:             document.Artifacts,
		RecoveryMarkers:       append([]RecoveryMarker(nil), document.RecoveryMarkers...),
		WriteCompletionProven: false,
	}
	if document.Hello != nil {
		input.BoardBuildHash = document.Hello.BuildHash
		input.BoardBuildTimestamp = document.Hello.BuildTimestamp
	}
	return input, nil
}

// Read validates and decodes the rolling snapshot without using live hardware.
func Read(path string) (Stored, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || !filepath.IsAbs(path) {
		return Stored{}, errors.New("session snapshot path must be absolute")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Stored{Path: path, Exists: false}, nil
	}
	if err != nil {
		return Stored{}, fmt.Errorf("inspect session snapshot: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxSnapshotBytes {
		return Stored{}, errors.New("session snapshot must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Stored{}, fmt.Errorf("open session snapshot: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(file, maxSnapshotBytes+1)))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Stored{}, fmt.Errorf("decode session snapshot: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Stored{}, errors.New("session snapshot contains trailing JSON")
		}
		return Stored{}, fmt.Errorf("decode session snapshot trailing data: %w", err)
	}
	if document.Format != Format || document.Schema != Schema || document.CapturedAt.IsZero() {
		return Stored{}, errors.New("session snapshot identity or timestamp is invalid")
	}
	if document.StorageClass != "host-diagnostic-cache" ||
		document.ContainsHostConfig || document.ContainsMCUEEPROMImage ||
		!document.RecoveryDiagnosticOnly || document.InterruptedWriteProven {
		return Stored{}, errors.New("session snapshot violates diagnostic storage or recovery safety boundaries")
	}
	for _, marker := range document.RecoveryMarkers {
		if marker.WriteCompletionProven {
			return Stored{}, errors.New("session snapshot improperly claims interrupted-write completion")
		}
	}
	normalized, issues := normalizeOperationalContext(OperationalContext{
		Programming: document.Programming, Artifacts: document.Artifacts,
		RecoveryMarkers: document.RecoveryMarkers,
	})
	if len(issues) != 0 {
		return Stored{}, errors.New("session snapshot contains invalid operational hashes")
	}
	document.Programming = normalized.Programming
	document.Artifacts = normalized.Artifacts
	document.RecoveryMarkers = normalized.RecoveryMarkers
	return Stored{Path: path, Exists: true, Snapshot: &document}, nil
}

func encode(document Document) ([]byte, error) {
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode graceful-exit diagnostic snapshot: %w", err)
	}
	content = append(content, '\n')
	if len(content) > maxSnapshotBytes {
		return nil, errors.New("graceful-exit diagnostic snapshot exceeds size limit")
	}
	return content, nil
}

func normalizedHostIdentity(value HostIdentity) HostIdentity {
	value.Title = bounded(strings.TrimSpace(value.Title), 128)
	value.Role = bounded(strings.TrimSpace(value.Role), 64)
	value.Version = bounded(strings.TrimSpace(value.Version), 64)
	value.SourceHash = bounded(strings.TrimSpace(value.SourceHash), 128)
	value.BuildTime = bounded(strings.TrimSpace(value.BuildTime), 64)
	value.GOOS = runtime.GOOS
	value.GOARCH = runtime.GOARCH
	value.ProcessID = os.Getpid()
	return value
}

func unavailable(section string) Issue {
	return Issue{
		Section: section, Code: "cached_value_unavailable",
		Message: "no cached " + strings.ReplaceAll(section, "_", " ") + " value was available at graceful exit",
	}
}

func normalizeOperationalContext(value OperationalContext) (OperationalContext, []Issue) {
	result := OperationalContext{Artifacts: value.Artifacts, RecoveryMarkers: []RecoveryMarker{}}
	issues := make([]Issue, 0)
	if value.Programming != nil {
		operation := *value.Programming
		operation.ID = bounded(strings.TrimSpace(operation.ID), 96)
		operation.Kind = bounded(strings.TrimSpace(operation.Kind), 64)
		operation.State = bounded(strings.TrimSpace(operation.State), 64)
		operation.ProgrammingMethod = bounded(strings.TrimSpace(operation.ProgrammingMethod), 32)
		operation.BootloaderOutcome = bounded(strings.TrimSpace(operation.BootloaderOutcome), 32)
		operation.ErrorCode = bounded(strings.TrimSpace(operation.ErrorCode), 64)
		if operation.ProgressPercent < 0 {
			operation.ProgressPercent = 0
		}
		if operation.ProgressPercent > 100 {
			operation.ProgressPercent = 100
		}
		operation.ArtifactSHA256 = normalizeSnapshotSHA256(
			"programming", "artifact_sha256", operation.ArtifactSHA256, &issues,
		)
		result.Programming = &operation
	}
	result.Artifacts.CurrentFirmwareSHA256 = normalizeSnapshotSHA256(
		"artifacts", "current_firmware_sha256", result.Artifacts.CurrentFirmwareSHA256, &issues,
	)
	result.Artifacts.CurrentEEPROMSHA256 = normalizeSnapshotSHA256(
		"artifacts", "current_eeprom_sha256", result.Artifacts.CurrentEEPROMSHA256, &issues,
	)
	result.Artifacts.CurrentFlashSHA256 = normalizeSnapshotSHA256(
		"artifacts", "current_flash_readback_sha256", result.Artifacts.CurrentFlashSHA256, &issues,
	)
	result.Artifacts.CurrentHostSHA256 = normalizeSnapshotSHA256(
		"artifacts", "current_host_sha256", result.Artifacts.CurrentHostSHA256, &issues,
	)
	result.Artifacts.DefaultFirmwareSHA256 = normalizeSnapshotSHA256(
		"artifacts", "default_firmware_sha256", result.Artifacts.DefaultFirmwareSHA256, &issues,
	)
	result.Artifacts.DefaultEEPROMSHA256 = normalizeSnapshotSHA256(
		"artifacts", "default_eeprom_sha256", result.Artifacts.DefaultEEPROMSHA256, &issues,
	)
	seen := make(map[string]bool, len(value.RecoveryMarkers))
	for _, marker := range value.RecoveryMarkers {
		marker.MarkerSHA256 = normalizeSnapshotSHA256(
			"recovery", "marker_sha256", marker.MarkerSHA256, &issues,
		)
		marker.TargetFirmwareSHA256 = normalizeSnapshotSHA256(
			"recovery", "target_firmware_sha256", marker.TargetFirmwareSHA256, &issues,
		)
		marker.SettingsSnapshotSHA256 = normalizeSnapshotSHA256(
			"recovery", "settings_snapshot_sha256", marker.SettingsSnapshotSHA256, &issues,
		)
		marker.DeviceFingerprint = normalizeSnapshotSHA256(
			"recovery", "device_fingerprint", marker.DeviceFingerprint, &issues,
		)
		marker.Phase = bounded(strings.TrimSpace(marker.Phase), 64)
		marker.HostResult = bounded(strings.TrimSpace(marker.HostResult), 32)
		marker.DiagnosticState = bounded(strings.TrimSpace(marker.DiagnosticState), 64)
		marker.WriteCompletionProven = false
		if marker.WarningCount < 0 {
			marker.WarningCount = 0
		}
		if marker.MarkerSHA256 == "" || seen[marker.MarkerSHA256] {
			continue
		}
		seen[marker.MarkerSHA256] = true
		result.RecoveryMarkers = append(result.RecoveryMarkers, marker)
	}
	sort.Slice(result.RecoveryMarkers, func(left, right int) bool {
		if result.RecoveryMarkers[left].PreparedAt.Equal(result.RecoveryMarkers[right].PreparedAt) {
			return result.RecoveryMarkers[left].MarkerSHA256 < result.RecoveryMarkers[right].MarkerSHA256
		}
		return result.RecoveryMarkers[left].PreparedAt.Before(result.RecoveryMarkers[right].PreparedAt)
	})
	return result, issues
}

func normalizeSnapshotSHA256(section, field, value string, issues *[]Issue) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if len(value) == sha256.Size*2 {
		if _, err := hex.DecodeString(value); err == nil {
			return value
		}
	}
	*issues = append(*issues, Issue{
		Section: section, Code: "invalid_hash",
		Message: "cached " + field + " was omitted because it was not a SHA-256 value",
	})
	return ""
}

func cloneProgrammingOperation(value *ProgrammingOperation) *ProgrammingOperation {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func haveHello(value controller.Hello) bool {
	return value.BoardKind != 0 || value.Name != "" || value.Capabilities != 0 ||
		value.BuildHash != 0 || value.BuildTimestamp != 0
}

func haveDeviceIdentity(value controller.PortInfo) bool {
	return value.Name != "" || value.VID != "" || value.PID != "" ||
		value.SerialNumber != "" || value.InstanceID != "" || value.FriendlyName != ""
}

func summarizeEvents(entries []controller.TimelineEntry, limit int) []EventSummary {
	if limit <= 0 {
		return []EventSummary{}
	}
	seen := make(map[uint64]struct{}, len(entries))
	reversed := make([]EventSummary, 0, min(limit, len(entries)))
	for index := len(entries) - 1; index >= 0 && len(reversed) < limit; index-- {
		entry := entries[index]
		if _, exists := seen[entry.ID]; exists {
			continue
		}
		seen[entry.ID] = struct{}{}
		reversed = append(reversed, EventSummary{
			ID: entry.ID, Time: entry.Time, Kind: bounded(entry.Kind, 64),
			Lifecycle: bounded(entry.Lifecycle, 64), State: bounded(entry.State, 64),
			Source: bounded(entry.Source, 64), Target: bounded(entry.Target, 64),
			Action: bounded(entry.Action, 64), Metadata: safeMetadata(entry.Metadata),
			ResetCause: entry.ResetCause, ResetCount: entry.ResetCount,
			RFObserved: entry.RFCode != 0, RFBits: entry.RFBits,
			RFProtocol: entry.RFProtocol, RFPulseUS: entry.RFPulseUS,
		})
	}
	result := make([]EventSummary, len(reversed))
	for index := range reversed {
		result[len(reversed)-1-index] = reversed[index]
	}
	return result
}

func summarizeRF(entries []controller.TimelineEntry, limit int) []RFObservation {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]struct{})
	reversed := make([]RFObservation, 0, min(limit, len(entries)))
	for index := len(entries) - 1; index >= 0 && len(reversed) < limit; index-- {
		entry := entries[index]
		if entry.RFCode == 0 {
			continue
		}
		fingerprint := rfFingerprint(entry.RFCode, entry.RFBits, entry.RFProtocol)
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		reversed = append(reversed, RFObservation{
			EventID: entry.ID, CodeFingerprint: fingerprint,
			Bits: entry.RFBits, Protocol: entry.RFProtocol,
			PulseUS: entry.RFPulseUS,
		})
	}
	result := make([]RFObservation, len(reversed))
	for index := range reversed {
		result[len(reversed)-1-index] = reversed[index]
	}
	return result
}

func rfFingerprint(code uint32, bits, protocol byte) string {
	var encoded [6]byte
	binary.BigEndian.PutUint32(encoded[:4], code)
	encoded[4] = bits
	encoded[5] = protocol
	sum := sha256.Sum256(encoded[:])
	return hex.EncodeToString(sum[:8])
}

func safeMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string)
	for _, key := range keys {
		if len(result) == 16 || sensitiveMetadataKey(key) || sensitiveMetadataValue(values[key]) {
			continue
		}
		cleanKey := bounded(strings.TrimSpace(key), 64)
		if cleanKey == "" {
			continue
		}
		result[cleanKey] = bounded(values[key], 256)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sensitiveMetadataKey(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, marker := range []string{
		"secret", "token", "password", "passwd", "auth", "cookie",
		"credential", "api_key", "apikey", "private", "bearer",
		"payload", "message", "content", "body", "command", "code",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func sensitiveMetadataValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, marker := range []string{
		"password=", "passwd=", "secret=", "token=", "api_key=",
		"apikey=", "authorization:", "bearer ", "private key",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func bounded(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
