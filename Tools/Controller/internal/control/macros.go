package control

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

const (
	defaultMacroTimingToleranceUS uint32 = 2500
	defaultHostMacroToleranceUS   uint32 = 100000
	macroStatusPollInterval              = 100 * time.Millisecond
	macroRequestTimeout                  = 2 * time.Second
<<<<<<< HEAD
	macroModeHost                        = "host"
	macroModeMCU                         = "mcu"
=======
	macroMaximumRecordedSteps            = 65535
	macroMaximumRecordedBytes            = 65535
>>>>>>> origin/agent/webui-defects
)

// MacroState is the host-authoritative view shared by TUI, CLI, API, IPC, and
// bridge clients. Mode states which clock and executor own playback.
type MacroState struct {
	Running              bool               `json:"running"`
	Generation           uint64             `json:"connection_generation,omitempty"`
	ID                   byte               `json:"id"`
	Name                 string             `json:"name"`
	Mode                 string             `json:"mode"`
	Category             string             `json:"category,omitempty"`
	Color                string             `json:"color,omitempty"`
	Step                 int                `json:"step"`
	StepCount            int                `json:"step_count"`
	DurationUS           uint32             `json:"duration_us"`
	StartedAt            time.Time          `json:"started_at,omitempty"`
	FinishedAt           time.Time          `json:"finished_at,omitempty"`
	DeviceStartedAtUS    uint32             `json:"device_started_at_us,omitempty"`
	AcceptedBytes        uint16             `json:"accepted_bytes"`
	BufferFill           byte               `json:"buffer_fill"`
	Underruns            byte               `json:"underruns"`
	DispatchErrors       byte               `json:"dispatch_errors"`
	DroppedSteps         uint16             `json:"dropped_steps"`
	EvidenceSteps        int                `json:"evidence_steps"`
	TimingViolations     int                `json:"timing_violations"`
	LastTimingDeltaUS    int32              `json:"last_timing_delta_us"`
	MaximumTimingErrorUS uint32             `json:"maximum_timing_error_us"`
	StartupDelayUS       uint32             `json:"startup_delay_us"`
	TimingToleranceUS    uint32             `json:"timing_tolerance_us"`
	Faithful             bool               `json:"faithful"`
	Lifecycle            string             `json:"lifecycle,omitempty"`
	LastError            string             `json:"last_error,omitempty"`
	Device               native.MacroStatus `json:"device"`
}

// MacroSnapshot is shared by local and remote clients; the host remains the
// sole owner of the persisted library and recording/playback clocks.
type MacroSnapshot struct {
	Library   []appconfig.Macro   `json:"library"`
	Playback  MacroState          `json:"playback"`
	Recording MacroRecordingState `json:"recording"`
}

type compiledMacroStep struct {
	dueUS        uint32
	opcode       byte
	payload      []byte
	recordLength int
	streamEnd    int
}

type compiledMacro struct {
	definition appconfig.Macro
	stream     []byte
	steps      []compiledMacroStep
	durationUS uint32
}

// macroStreamLease is a volatile, connection-bound promise to restore the
// board's periodic STATUS cadence after MCU-timed playback. SET_STREAM is
// deliberately used instead of rewriting the complete settings record. The
// board identity check is independent of the generation check so a future
// transport implementation cannot accidentally restore onto a replacement
// controller while reusing a session token.
type macroStreamLease struct {
	Generation uint64
	Board      string
	PeriodMS   uint16
	restored   bool
}

// boardCaptureToken binds an ephemeral MCU capture ID to one authenticated
// connection and one board-clock epoch. IDs are intentionally reusable after
// reboot; the full token is not.
type boardCaptureToken struct {
	Generation  uint64
	ID          byte
	StartedAtUS uint32
	Board       string
}

type MacroRunner struct {
	runtime          *Runtime
	library          func() []appconfig.Macro
	hostConfig       func() appconfig.Config
	updateHostConfig func(func(*appconfig.Config) error) error

	operationMu sync.Mutex
	mu          sync.RWMutex
	state       MacroState
	cancel      context.CancelFunc
	cancelKeep  bool
	done        chan struct{}
	presentOnce sync.Once
	present     chan MacroState

<<<<<<< HEAD
	recordMu      sync.RWMutex
	recording     MacroRecordingState
	recordMacro   appconfig.Macro
	recordBaseUS  uint32
	recordBaseAt  time.Time
	recordHasBase bool
	recordRelease func()
=======
	recordMu               sync.RWMutex
	recording              MacroRecordingState
	recordMacro            appconfig.Macro
	recordBaseUS           uint32
	recordHasBase          bool
	recordBytes            uint32
	recordRelease          func()
	recordConnectionCancel context.CancelFunc
	recordSealed           bool
	recordCapture          boardCaptureToken
	recordConnection       boardCaptureToken
	recordConnectionPinned bool
	boardCaptureMu         sync.Mutex
	boardCaptureGeneration uint64
	boardCaptureFinalizing map[boardCaptureToken]struct{}
	boardCaptureFinished   map[boardCaptureToken]struct{}
	fetchCapture           func(boardCaptureToken) ([]byte, error)
	queryCaptureStatus     func(uint64) (native.MacroStatus, error)
	ackCapture             func(boardCaptureToken) error
	// requestGeneration is a focused test seam. Production always falls back
	// to Runtime.requestAtGeneration, preserving the authenticated-session
	// pin and its request/response observation checks.
	requestGeneration func(context.Context, uint64, byte, []byte, byte) (native.Frame, error)
>>>>>>> origin/agent/webui-defects
}

// MacroRecordingState describes a HOST-owned recording session. Mode states
// whether offsets come from host monotonic observations or MCU ACK timestamps.
type MacroRecordingState struct {
<<<<<<< HEAD
	Active    bool      `json:"active"`
	ID        byte      `json:"id"`
	Name      string    `json:"name"`
	Mode      string    `json:"mode"`
	Category  string    `json:"category,omitempty"`
	Color     string    `json:"color,omitempty"`
	Steps     int       `json:"steps"`
	StartedAt time.Time `json:"started_at,omitempty"`
	LastError string    `json:"last_error,omitempty"`
=======
	Active       bool      `json:"active"`
	ID           byte      `json:"id"`
	Name         string    `json:"name"`
	Category     string    `json:"category,omitempty"`
	Color        string    `json:"color,omitempty"`
	Steps        int       `json:"steps"`
	HostSteps    int       `json:"host_steps"`
	PanelSteps   int       `json:"panel_steps"`
	RFSteps      int       `json:"rf_steps"`
	LastAtUS     uint32    `json:"last_at_us"`
	LastDeltaUS  uint32    `json:"last_delta_us"`
	LastOpcode   byte      `json:"last_opcode"`
	LastSource   byte      `json:"last_source"`
	BoardOwned   bool      `json:"board_owned,omitempty"`
	BoardID      byte      `json:"board_id,omitempty"`
	DroppedSteps uint16    `json:"dropped_steps,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
>>>>>>> origin/agent/webui-defects
}

func NewMacroRunner(
	runtime *Runtime,
	library func() []appconfig.Macro,
	hostConfig func() appconfig.Config,
	updateHostConfig ...func(func(*appconfig.Config) error) error,
) *MacroRunner {
	if library == nil {
		library = func() []appconfig.Macro { return nil }
	}
	var updater func(func(*appconfig.Config) error) error
	if len(updateHostConfig) != 0 {
		updater = updateHostConfig[0]
	}
	runner := &MacroRunner{
		runtime: runtime, library: library, hostConfig: hostConfig,
		updateHostConfig: updater,
	}
	runner.fetchCapture = runner.fetchBoardCapture
	runner.queryCaptureStatus = runner.queryBoardMacroStatus
	runner.ackCapture = runner.ackBoardCaptureExport
	if runtime != nil {
		runtime.ObserveMacroStatuses(runner.handleBoardMacroStatus)
		runtime.ObserveConnectionReady(runner.recoverBoardCaptureOnConnect)
	}
	return runner
}

func (runner *MacroRunner) List() []appconfig.Macro {
	source := runner.library()
	result := make([]appconfig.Macro, len(source))
	for index, macro := range source {
		result[index] = macro
		result[index].Steps = append([]appconfig.MacroStep(nil), macro.Steps...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (runner *MacroRunner) State() MacroState {
	runner.mu.RLock()
	defer runner.mu.RUnlock()
	return runner.state
}

func (runner *MacroRunner) RecordingState() MacroRecordingState {
	runner.recordMu.RLock()
	defer runner.recordMu.RUnlock()
	return runner.recording
}

func (runner *MacroRunner) Snapshot() MacroSnapshot {
	return MacroSnapshot{Library: runner.List(), Playback: runner.State(), Recording: runner.RecordingState()}
}

func (runner *MacroRunner) UpdateMetadata(reference, field, value string) (appconfig.Macro, error) {
	if runner.updateHostConfig == nil {
		return appconfig.Macro{}, errors.New("macro persistence is unavailable")
	}
	macro, err := runner.find(reference)
	if err != nil {
		return macro, err
	}
	if state := runner.State(); state.Running && state.ID == macro.ID {
		return macro, errors.New("cancel playback before editing the playing macro")
	}
	value = strings.TrimSpace(value)
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		for index := range config.Macros {
			if config.Macros[index].ID != macro.ID {
				continue
			}
			updated := config.Macros[index]
			switch field {
			case "name":
				updated.Name = value
			case "category":
				updated.Category = value
			default:
				return errors.New("unsupported macro metadata field")
			}
			candidate := *config
			candidate.Macros = append([]appconfig.Macro(nil), config.Macros...)
			candidate.Macros[index] = updated
			if err := candidate.Validate(); err != nil {
				return err
			}
			config.Macros[index] = updated
			macro = updated
			return nil
		}
		return errors.New("macro disappeared before update")
	})
	if err == nil {
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "updated", Text: fmt.Sprintf("macro %d/%s updated", macro.ID, macro.Name)})
	}
	return macro, err
}

// CreateDraft persists an empty, editable macro definition. Empty drafts are
// listable but intentionally cannot be played until they contain a step.
func (runner *MacroRunner) CreateDraft(id byte, name, category, color string) (appconfig.Macro, error) {
	if runner.updateHostConfig == nil {
		return appconfig.Macro{}, errors.New("macro persistence is unavailable")
	}
	macro := appconfig.Macro{
		ID: id, Name: strings.TrimSpace(name), Category: strings.TrimSpace(category),
		Mode: macroModeHost, Color: normalizedMacroColor(color), TimingToleranceUS: defaultHostMacroToleranceUS,
	}
	err := runner.updateHostConfig(func(config *appconfig.Config) error {
		for _, existing := range config.Macros {
			if existing.ID == id || strings.EqualFold(existing.Name, macro.Name) {
				return fmt.Errorf("macro ID %d or name %q already exists", id, macro.Name)
			}
		}
		config.Macros = append(config.Macros, macro)
		return nil
	})
	if err == nil {
<<<<<<< HEAD
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "created", Text: fmt.Sprintf("macro %d/%s created", macro.ID, macro.Name)})
=======
		runner.publishLibraryChange("created", macro)
	}
	return macro, err
}

// UpdateMetadata keeps names/categories/colors host-owned, including for a
// provisional `Board capture N` recovered from the MCU ring.
func (runner *MacroRunner) UpdateMetadata(
	reference, name string,
	category, color *string,
) (appconfig.Macro, error) {
	if runner.updateHostConfig == nil {
		return appconfig.Macro{}, errors.New("macro persistence is unavailable")
	}
	macro, err := runner.find(reference)
	if err != nil {
		return appconfig.Macro{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return appconfig.Macro{}, errors.New("macro name is required")
	}
	var nextCategory, nextColor *string
	if category != nil {
		value := strings.TrimSpace(*category)
		if value == "-" {
			value = ""
		}
		nextCategory = &value
	}
	if color != nil {
		value := strings.TrimSpace(*color)
		if value == "-" {
			value = ""
		}
		value = normalizedMacroColor(value)
		if !validMacroColor(value) {
			return appconfig.Macro{}, fmt.Errorf("macro color %q is not red, blue, violet, green, white, or empty", value)
		}
		nextColor = &value
	}
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		for index, existing := range config.Macros {
			if existing.ID != macro.ID && strings.EqualFold(existing.Name, name) {
				return fmt.Errorf("macro name %q already exists", name)
			}
			if existing.ID == macro.ID {
				config.Macros[index].Name = name
				if nextCategory != nil {
					config.Macros[index].Category = *nextCategory
				}
				if nextColor != nil {
					config.Macros[index].Color = *nextColor
				}
				macro = config.Macros[index]
				return nil
			}
		}
		return fmt.Errorf("macro %q disappeared before it could be updated", reference)
	})
	if err == nil {
		runner.publishLibraryChange("updated", macro)
>>>>>>> origin/agent/webui-defects
	}
	return macro, err
}

func (runner *MacroRunner) Delete(reference string) error {
	if runner.updateHostConfig == nil {
		return errors.New("macro persistence is unavailable")
	}
	macro, err := runner.find(reference)
	if err != nil {
		return err
	}
	state := runner.State()
	if state.Running && state.ID == macro.ID {
		return errors.New("cannot delete the macro currently playing")
	}
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		for index, existing := range config.Macros {
			if existing.ID == macro.ID {
				config.Macros = append(config.Macros[:index], config.Macros[index+1:]...)
				return nil
			}
		}
		return fmt.Errorf("macro %q disappeared before it could be deleted", reference)
	})
	if err == nil {
<<<<<<< HEAD
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "deleted", Text: fmt.Sprintf("macro %d/%s deleted", macro.ID, macro.Name)})
	}
	return err
}

func (runner *MacroRunner) StartRecording(name, category, color string) (MacroRecordingState, error) {
	return runner.startRecording(name, category, color, macroModeHost)
}

// StartMCURecording retains the exact acknowledgement-timestamp recorder for
// users who explicitly want the stricter firmware-oriented workflow.
func (runner *MacroRunner) StartMCURecording(name, category, color string) (MacroRecordingState, error) {
	return runner.startRecording(name, category, color, macroModeMCU)
}

func (runner *MacroRunner) startRecording(name, category, color, mode string) (MacroRecordingState, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	if runner.State().Running {
		return MacroRecordingState{}, errors.New("cancel playback before starting a recording")
	}
=======
		runner.publishLibraryChange("deleted", macro)
	}
	return err
}

// Rename updates host-owned macro metadata without introducing another macro
// store. A playing macro keeps a stable identity through its final lifecycle
// event, so its definition cannot be renamed mid-run.
func (runner *MacroRunner) Rename(reference, name string) (appconfig.Macro, error) {
	if runner.updateHostConfig == nil {
		return appconfig.Macro{}, errors.New("macro persistence is unavailable")
	}
	macro, err := runner.find(reference)
	if err != nil {
		return appconfig.Macro{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return appconfig.Macro{}, errors.New("macro name is required")
	}
	if state := runner.State(); state.Running && state.ID == macro.ID {
		return appconfig.Macro{}, errors.New("cannot rename the macro currently playing")
	}
	updated := macro
	updated.Name = name
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		for index := range config.Macros {
			if config.Macros[index].ID != macro.ID && strings.EqualFold(config.Macros[index].Name, name) {
				return fmt.Errorf("macro name %q already exists", name)
			}
		}
		for index := range config.Macros {
			if config.Macros[index].ID == macro.ID {
				config.Macros[index].Name = name
				return nil
			}
		}
		return fmt.Errorf("macro %q disappeared before it could be renamed", reference)
	})
	if err == nil {
		runner.publishLibraryChange("renamed", updated)
	}
	return updated, err
}

// SetCategory updates the durable host-library grouping used by CLI, TUI, and
// hosted menus. It intentionally shares the same persistence transaction as
// every other macro metadata mutation.
func (runner *MacroRunner) SetCategory(reference, category string) (appconfig.Macro, error) {
	if runner.updateHostConfig == nil {
		return appconfig.Macro{}, errors.New("macro persistence is unavailable")
	}
	macro, err := runner.find(reference)
	if err != nil {
		return appconfig.Macro{}, err
	}
	category = strings.TrimSpace(category)
	if state := runner.State(); state.Running && state.ID == macro.ID {
		return appconfig.Macro{}, errors.New("cannot change the category of the macro currently playing")
	}
	updated := macro
	updated.Category = category
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		for index := range config.Macros {
			if config.Macros[index].ID == macro.ID {
				config.Macros[index].Category = category
				return nil
			}
		}
		return fmt.Errorf("macro %q disappeared before its category could be updated", reference)
	})
	if err == nil {
		runner.publishLibraryChange("categorized", updated)
	}
	return updated, err
}

func (runner *MacroRunner) StartRecording(name, category, color string) (MacroRecordingState, error) {
	return runner.startRecording(nil, name, category, color)
}

// StartBoardCapture arms the same retained MCU ring used by front-panel
// recording. The resulting lifecycle is consumed by every host surface through
// the ordinary macro status/event path.
func (runner *MacroRunner) StartBoardCapture(ctx context.Context, id byte) (MacroRecordingState, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return MacroRecordingState{}, errors.New("device is not connected")
	}
	if snapshot.Hello.Capabilities&native.CapabilityTimedMacroQueue == 0 ||
		snapshot.Hello.BuildFeatures&native.BuildFeatureLocalMacroCapture == 0 {
		return MacroRecordingState{}, errors.New("connected firmware does not advertise retained macro capture")
	}
	if state := runner.RecordingState(); state.Active {
		return state, fmt.Errorf("macro recording %d/%s is already active", state.ID, state.Name)
	}
	status, err := runner.queryBoard(ctx, snapshot.Generation)
	if err != nil {
		return MacroRecordingState{}, fmt.Errorf("query macro queue before capture: %w", err)
	}
	if status.State == native.MacroCaptured || status.State == native.MacroExported ||
		status.State == native.MacroRecording {
		runner.handleBoardMacroStatusAtGeneration(
			status, snapshot.Generation,
			captureBoardIdentity(snapshot.Port, snapshot.Hello),
		)
		return runner.RecordingState(), fmt.Errorf(
			"board retains macro capture %d in state %d; recover and explicitly clear it before starting another capture",
			status.ID, status.State,
		)
	}
	if status.Active() {
		return MacroRecordingState{}, fmt.Errorf("board macro queue is busy in state %d", status.State)
	}
	if _, err = runner.runtime.requestAtGeneration(
		ctx, snapshot.Generation, native.OpMacroStart,
		native.MacroCaptureStartPayload(id), native.OpACK,
	); err != nil {
		return MacroRecordingState{}, err
	}
	status, err = runner.queryBoard(ctx, snapshot.Generation)
	if err != nil {
		return MacroRecordingState{}, fmt.Errorf("verify board capture start: %w", err)
	}
	if status.State != native.MacroRecording || status.ID != id {
		return MacroRecordingState{}, fmt.Errorf(
			"board capture start returned state=%d id=%d, want recording id=%d",
			status.State, status.ID, id,
		)
	}
	runner.handleBoardMacroStatusAtGeneration(
		status, snapshot.Generation,
		captureBoardIdentity(snapshot.Port, snapshot.Hello),
	)
	return runner.RecordingState(), nil
}

// StopBoardCapture seals, fetches, and asynchronously persists the retained
// stream. It never aliases destructive clear.
func (runner *MacroRunner) StopBoardCapture(ctx context.Context) (MacroRecordingState, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	state := runner.RecordingState()
	if !state.Active || !state.BoardOwned {
		return state, errors.New("no board-owned macro capture is active")
	}
	runner.recordMu.RLock()
	token := runner.recordCapture
	runner.recordMu.RUnlock()
	if !recordingConnectionMatches(runner.runtime.Snapshot(), token) {
		return state, errors.New("board capture connection changed before it could be stopped")
	}
	status, err := runner.queryCaptureStatus(token.Generation)
	if err != nil {
		return state, fmt.Errorf("query board capture before stop: %w", err)
	}
	if status.State == native.MacroRecording {
		if _, err = runner.runtime.requestAtGeneration(
			ctx, token.Generation, native.OpMacroStep,
			native.MacroCaptureStopPayload(), native.OpACK,
		); err != nil {
			return state, err
		}
		status, err = runner.queryBoard(ctx, token.Generation)
		if err != nil {
			return state, fmt.Errorf("verify board capture stop: %w", err)
		}
	}
	if status.ID != token.ID ||
		(status.State != native.MacroCaptured && status.State != native.MacroExported) {
		return state, fmt.Errorf(
			"board capture stop returned state=%d id=%d, want captured id=%d",
			status.State, status.ID, token.ID,
		)
	}
	// Explicit user stop is the seal boundary even when the retained ring filled
	// earlier and connected Action streaming continued beyond it.
	runner.beginBoardCaptureFinalization(token, status)
	return runner.RecordingState(), nil
}

// ClearBoardCapture removes one retained board ring after generation/identity
// verification. Normal callers may clear only an exported capture; force is a
// deliberate data-loss override, but even force cannot race an active fetch.
func (runner *MacroRunner) ClearBoardCapture(ctx context.Context, force bool) (native.MacroStatus, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return native.MacroStatus{}, errors.New("device is not connected")
	}
	status, err := runner.queryBoard(ctx, snapshot.Generation)
	if err != nil {
		return native.MacroStatus{}, fmt.Errorf("query retained macro capture: %w", err)
	}
	if status.State != native.MacroCaptured && status.State != native.MacroExported {
		return status, fmt.Errorf("board has no retained macro capture to clear (state %d)", status.State)
	}
	if status.State != native.MacroExported && !force {
		return status, errors.New("retained macro capture is not export-acknowledged; save it first or explicitly force clear")
	}
	token := boardCaptureToken{
		Generation: snapshot.Generation, ID: status.ID,
		StartedAtUS: status.StartedAtUS,
		Board:       captureBoardIdentity(snapshot.Port, snapshot.Hello),
	}
	runner.boardCaptureMu.Lock()
	_, finalizing := runner.boardCaptureFinalizing[token]
	runner.boardCaptureMu.Unlock()
	if finalizing {
		return status, errors.New("retained macro capture export is still being finalized")
	}
	if _, err = runner.runtime.requestAtGeneration(
		ctx, snapshot.Generation, native.OpMacroStep,
		native.MacroCaptureClearPayload(status.ID, status.StartedAtUS), native.OpACK,
	); err != nil {
		return status, err
	}
	cleared, err := runner.queryBoard(ctx, snapshot.Generation)
	if err != nil {
		return cleared, fmt.Errorf("verify retained macro capture clear: %w", err)
	}
	if cleared.State != native.MacroIdle || cleared.Fill != 0 {
		return cleared, fmt.Errorf("retained macro capture clear returned state=%d fill=%d", cleared.State, cleared.Fill)
	}
	runner.boardCaptureMu.Lock()
	delete(runner.boardCaptureFinished, token)
	runner.boardCaptureMu.Unlock()
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "cleared", State: "idle",
		Text: fmt.Sprintf("retained board macro capture %d cleared", status.ID),
	})
	return cleared, nil
}

func (runner *MacroRunner) startRecording(board *boardCaptureToken, name, category, color string) (MacroRecordingState, error) {
>>>>>>> origin/agent/webui-defects
	if runner.updateHostConfig == nil {
		return MacroRecordingState{}, errors.New("macro persistence is unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return MacroRecordingState{}, errors.New("macro recording name is required")
	}
	color = normalizedMacroColor(color)
	if !validMacroColor(color) {
		return MacroRecordingState{}, fmt.Errorf("macro color %q is not red, blue, violet, green, or white", color)
	}
	var connection boardCaptureToken
	var connectionCursor uint64
	if board != nil {
		connection = *board
	} else {
		// Capture the event cursor before the snapshot. A detach either appears
		// in that snapshot or remains visible to the watcher from this cursor.
		connectionCursor = runner.runtime.LatestEventID()
		snapshot := runner.runtime.Snapshot()
		if !snapshot.Connected {
			return MacroRecordingState{}, errors.New("device is not connected")
		}
		connection = boardCaptureToken{
			Generation: snapshot.Generation,
			Board:      captureBoardIdentity(snapshot.Port, snapshot.Hello),
		}
	}
	used := make(map[byte]bool)
	for _, macro := range runner.List() {
		if strings.EqualFold(macro.Name, name) {
			return MacroRecordingState{}, fmt.Errorf("macro %q already exists", name)
		}
		used[macro.ID] = true
	}
	id := byte(0)
	if board != nil && !used[board.ID] {
		id = board.ID
	} else {
		for used[id] && id != 0xFF {
			id++
		}
	}
	if used[id] {
		return MacroRecordingState{}, errors.New("all macro IDs are in use")
	}

	runner.recordMu.Lock()
	if runner.recording.Active {
		state := runner.recording
		runner.recordMu.Unlock()
		return state, fmt.Errorf("macro recording %q is already active", state.Name)
	}
	runner.recordMacro = appconfig.Macro{
		ID: id, Name: name, Category: strings.TrimSpace(category), Color: color,
		Mode: mode, TimingToleranceUS: modeTimingTolerance(mode),
	}
	runner.recordBaseUS = 0
	runner.recordBaseAt = time.Time{}
	runner.recordHasBase = false
	runner.recordBytes = 0
	runner.recordSealed = false
	runner.recordCapture = boardCaptureToken{}
	runner.recordConnection = connection
	runner.recordConnectionPinned = true
	runner.recording = MacroRecordingState{
		Active: true, ID: id, Name: name, Mode: mode, Category: strings.TrimSpace(category),
		Color: color, StartedAt: time.Now(),
	}
	if board != nil {
		runner.recording.BoardOwned = true
		runner.recording.BoardID = board.ID
		runner.recordCapture = *board
		runner.recordBaseUS = board.StartedAtUS
		runner.recordHasBase = true
	}
	runner.recordRelease = runner.runtime.ObserveActions(runner.captureAction)
	if board == nil {
		watchContext, cancel := context.WithCancel(context.Background())
		runner.recordConnectionCancel = cancel
		go runner.watchRecordingConnection(watchContext, connectionCursor, connection)
	}
	state := runner.recording
	runner.recordMu.Unlock()
	if board == nil && !recordingConnectionMatches(runner.runtime.Snapshot(), connection) {
		runner.abortRecordingConnection(connection, "board connection changed while macro recording started")
		return runner.RecordingState(), errors.New("board connection changed while macro recording started")
	}
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "started", State: "recording",
		Text: fmt.Sprintf("macro recording %d/%s started; MCU action deltas are authoritative", id, name),
	})
	return state, nil
}

func recordingConnectionMatches(snapshot Snapshot, token boardCaptureToken) bool {
	return snapshot.Connected && snapshot.Generation == token.Generation &&
		captureBoardIdentity(snapshot.Port, snapshot.Hello) == token.Board
}

func (runner *MacroRunner) watchRecordingConnection(
	ctx context.Context,
	afterID uint64,
	token boardCaptureToken,
) {
	for {
		event, err := runner.runtime.WaitEvent(ctx, afterID, "connection")
		if err != nil {
			return
		}
		afterID = event.ID
		if !recordingConnectionMatches(runner.runtime.Snapshot(), token) {
			reason := strings.TrimSpace(event.Reason)
			if reason == "" {
				reason = "board disconnected or was replaced"
			}
			runner.abortRecordingConnection(token, reason)
			return
		}
	}
}

func (runner *MacroRunner) abortRecordingConnection(token boardCaptureToken, reason string) {
	runner.recordMu.Lock()
	if !runner.recording.Active || runner.recording.BoardOwned ||
		!runner.recordConnectionPinned || runner.recordConnection != token {
		runner.recordMu.Unlock()
		return
	}
	if runner.recordRelease != nil {
		runner.recordRelease()
		runner.recordRelease = nil
	}
	if runner.recordConnectionCancel != nil {
		runner.recordConnectionCancel()
		runner.recordConnectionCancel = nil
	}
	runner.recordSealed = true
	runner.recording.Active = false
	runner.recording.LastError = "recording stopped because its pinned board connection changed: " + reason
	state := runner.recording
	runner.recordMu.Unlock()
	runner.runtime.PublishStructuredEvent(Event{
<<<<<<< HEAD
		Kind: "macro.recording", Lifecycle: "started", State: "recording",
		Text:     fmt.Sprintf("macro recording %d/%s started in %s mode", id, name, mode),
		Metadata: map[string]string{"macro_mode": mode},
=======
		Kind: "macro.recording", Lifecycle: "failed", State: "error",
		Reason: state.LastError, Text: state.LastError,
>>>>>>> origin/agent/webui-defects
	})
}

func (runner *MacroRunner) StopRecording(save bool) (appconfig.Macro, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	runner.recordMu.Lock()
	if !runner.recording.Active {
		runner.recordMu.Unlock()
		return appconfig.Macro{}, errors.New("no macro recording is active")
	}
	if !runner.recording.BoardOwned &&
		!recordingConnectionMatches(runner.runtime.Snapshot(), runner.recordConnection) {
		token := runner.recordConnection
		runner.recordMu.Unlock()
		runner.abortRecordingConnection(token, "board disconnected or was replaced before recording stopped")
		return appconfig.Macro{}, errors.New("recording stopped because its pinned board connection changed")
	}
	if save && runner.recording.LastError != "" {
		macro := runner.recordMacro
		err := runner.recording.LastError
		runner.recordMu.Unlock()
		return macro, fmt.Errorf(
			"recording cannot be saved: %s; discard it or start a new recording",
			err,
		)
	}
	if runner.recordRelease != nil {
		runner.recordRelease()
		runner.recordRelease = nil
	}
	if runner.recordConnectionCancel != nil {
		runner.recordConnectionCancel()
		runner.recordConnectionCancel = nil
	}
	macro := runner.recordMacro
	if save && len(macro.Steps) == 0 {
		// Keep an empty recording active: Save must never destroy the take.
		runner.recordRelease = runner.runtime.ObserveCommands(runner.captureCommand)
		runner.recordMu.Unlock()
		return macro, errors.New("recording is empty; run at least one board command or discard it")
	}
	runner.recording.Active = false
	runner.recordSealed = false
	runner.recordConnectionPinned = false
	runner.recording.Steps = len(macro.Steps)
	runner.recordMu.Unlock()

	if save {
		if err := runner.updateHostConfig(func(config *appconfig.Config) error {
			for _, existing := range config.Macros {
				if existing.ID == macro.ID || strings.EqualFold(existing.Name, macro.Name) {
					return fmt.Errorf("macro ID %d or name %q already exists", macro.ID, macro.Name)
				}
			}
			config.Macros = append(config.Macros, macro)
			return nil
		}); err != nil {
			runner.recordMu.Lock()
			runner.recording.Active = true
			runner.recording.LastError = "save failed; recording retained: " + err.Error()
			runner.recordRelease = runner.runtime.ObserveCommands(runner.captureCommand)
			runner.recordMu.Unlock()
			return macro, err
		}
		runner.publishLibraryChange("saved", macro)
	}
	runner.recordMu.Lock()
	runner.recording.LastError = ""
	runner.recordMu.Unlock()
	lifecycle := map[bool]string{true: "saved", false: "discarded"}[save]
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: lifecycle, State: lifecycle,
		Text:     fmt.Sprintf("macro recording %d/%s %s with %d %s-timed steps", macro.ID, macro.Name, lifecycle, len(macro.Steps), normalizedMacroMode(macro.Mode)),
		Metadata: map[string]string{"macro_mode": normalizedMacroMode(macro.Mode)},
	})
	return macro, nil
}

<<<<<<< HEAD
func (runner *MacroRunner) captureCommand(evidence CommandEvidence) {
	runner.recordMu.Lock()
	defer runner.recordMu.Unlock()
	if !runner.recording.Active || evidence.Source == CommandSourceBackground {
		return
	}
	if len(runner.recordMacro.Steps) >= 65535 {
		runner.recording.LastError = "recording reached the 65535 step limit; save it before continuing"
		return
	}
	mode := normalizedMacroMode(runner.recordMacro.Mode)
	if mode == macroModeHost {
		if !hostRecordableOpcode(evidence.Opcode) {
			return
		}
	} else if !evidence.Timed || !macroQueueableOpcode(evidence.Opcode) {
=======
func (runner *MacroRunner) publishLibraryChange(lifecycle string, macro appconfig.Macro) {
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.library", Lifecycle: lifecycle, State: lifecycle,
		Text: fmt.Sprintf("macro library %s %d/%s", lifecycle, macro.ID, macro.Name),
		Metadata: map[string]string{
			"macro_id": strconv.Itoa(int(macro.ID)), "macro_name": macro.Name,
			"category": macro.Category, "color": normalizedMacroColor(macro.Color),
			"steps": strconv.Itoa(len(macro.Steps)),
		},
	})
}

func (runner *MacroRunner) captureAction(evidence ActionEvidence) {
	// Successful host commands are echoed as board Action events for remote
	// observers. Their synchronous ACK already supplied the authoritative MCU
	// timestamp, so the echo must not become a duplicate recorded step.
	if (evidence.BoardOrigin && evidence.Source == native.InputSourceHost) ||
		!evidence.Timed || !native.MacroPlaybackAllowed(evidence.Opcode) {
		return
	}
	runner.recordMu.RLock()
	pinned := runner.recordConnectionPinned
	connection := runner.recordConnection
	boardOwned := runner.recording.BoardOwned
	active := runner.recording.Active && !runner.recordSealed
	runner.recordMu.RUnlock()
	if !active || !pinned {
		return
	}
	if evidence.Generation != connection.Generation {
		if !boardOwned {
			runner.abortRecordingConnection(connection, "action arrived from a different connection generation")
		}
		return
	}
	if !boardOwned && !recordingConnectionMatches(runner.runtime.Snapshot(), connection) {
		runner.abortRecordingConnection(connection, "board disconnected or was replaced")
>>>>>>> origin/agent/webui-defects
		return
	}
	step, ok := recordedMacroStep(evidence)
	if !ok {
		return
	}
<<<<<<< HEAD
	if mode == macroModeHost {
		observedAt := evidence.ObservedAt
		if observedAt.IsZero() {
			observedAt = time.Now()
		}
		if runner.recordBaseAt.IsZero() {
			runner.recordBaseAt = observedAt
		}
		delta := observedAt.Sub(runner.recordBaseAt)
		if delta < 0 || delta > time.Duration(0x7FFFFFFF)*time.Microsecond {
			runner.recording.LastError = "recording exceeded the host signed timing window"
			return
		}
		step.AtUS = uint32(delta / time.Microsecond)
		runner.recordMacro.Steps = append(runner.recordMacro.Steps, step)
		runner.recording.Steps = len(runner.recordMacro.Steps)
		runner.publishRecordedStep(step)
=======
	runner.recordMu.Lock()
	if !runner.recording.Active || runner.recordSealed {
		runner.recordMu.Unlock()
		return
	}
	if runner.recording.LastError != "" {
		runner.recordMu.Unlock()
>>>>>>> origin/agent/webui-defects
		return
	}
	if !runner.recordHasBase {
		runner.recordBaseUS = evidence.DeviceMicros
		runner.recordHasBase = true
	} else if !runner.recording.BoardOwned &&
		deviceMicrosBefore(evidence.DeviceMicros, runner.recordBaseUS) {
		// ACK callbacks and unsolicited Action events originate on different
		// goroutines. If an earlier MCU timestamp arrives second, move the host
		// epoch backwards and rebase the already collected offsets instead of
		// underflowing the recording into a multi-hour delay.
		shift := runner.recordBaseUS - evidence.DeviceMicros
		if shift > 0x7FFFFFFF {
			runner.recording.LastError = "recording evidence exceeded the MCU signed ordering window"
			runner.recordMu.Unlock()
			return
		}
		for index := range runner.recordMacro.Steps {
			if runner.recordMacro.Steps[index].AtUS > 0x7FFFFFFF-shift {
				runner.recording.LastError = "recording exceeded the MCU signed timing window while ordering evidence"
				runner.recordMu.Unlock()
				return
			}
		}
		for index := range runner.recordMacro.Steps {
			runner.recordMacro.Steps[index].AtUS += shift
		}
		runner.recordBaseUS = evidence.DeviceMicros
	}
	delta := evidence.DeviceMicros - runner.recordBaseUS
	if delta > 0x7FFFFFFF {
		runner.recording.LastError = "recording exceeded the MCU signed timing window"
		runner.recordMu.Unlock()
		return
	}
	step.AtUS = delta
	opcode, payload, err := compileMacroCommand(step)
	if err != nil {
		runner.recording.LastError = fmt.Sprintf("record action: %v", err)
		runner.recordMu.Unlock()
		return
	}
	record, err := native.EncodeMacroRecord(delta, opcode, payload)
	if err != nil {
		runner.recording.LastError = fmt.Sprintf("record action: %v", err)
		runner.recordMu.Unlock()
		return
	}
	if len(runner.recordMacro.Steps) >= macroMaximumRecordedSteps ||
		int(runner.recordBytes)+len(record) > macroMaximumRecordedBytes {
		runner.recording.LastError = fmt.Sprintf(
			"recording reached the bounded %d-step/%d-byte stream limit",
			macroMaximumRecordedSteps, macroMaximumRecordedBytes,
		)
		runner.recordMu.Unlock()
		return
	}
	insertAt := sort.Search(len(runner.recordMacro.Steps), func(index int) bool {
		return runner.recordMacro.Steps[index].AtUS > step.AtUS
	})
	wasNewest := insertAt == len(runner.recordMacro.Steps)
	runner.recordMacro.Steps = append(runner.recordMacro.Steps, appconfig.MacroStep{})
	copy(runner.recordMacro.Steps[insertAt+1:], runner.recordMacro.Steps[insertAt:])
	runner.recordMacro.Steps[insertAt] = step
	runner.recordBytes += uint32(len(record))
	runner.recording.Steps = len(runner.recordMacro.Steps)
<<<<<<< HEAD
	runner.publishRecordedStep(step)
=======
	last := len(runner.recordMacro.Steps) - 1
	runner.recording.LastAtUS = runner.recordMacro.Steps[last].AtUS
	runner.recording.LastDeltaUS = runner.recording.LastAtUS
	if last != 0 {
		runner.recording.LastDeltaUS -= runner.recordMacro.Steps[last-1].AtUS
	}
	if wasNewest {
		runner.recording.LastOpcode = evidence.Opcode
		runner.recording.LastSource = evidence.Source
	}
	switch evidence.Source {
	case native.InputSourcePhysical:
		runner.recording.PanelSteps++
	case native.InputSourceRF:
		runner.recording.RFSteps++
	default:
		runner.recording.HostSteps++
	}
	state := runner.recording
	runner.recordMu.Unlock()
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording.step", Lifecycle: "captured", State: "recording",
		Text: fmt.Sprintf("macro recording %d/%s captured step %d at %dus", state.ID, state.Name, state.Steps, state.LastAtUS),
		Metadata: map[string]string{
			"macro_id": strconv.Itoa(int(state.ID)), "macro_name": state.Name,
			"step": strconv.Itoa(state.Steps), "at_us": strconv.FormatUint(uint64(state.LastAtUS), 10),
			"delta_us": strconv.FormatUint(uint64(state.LastDeltaUS), 10),
			"opcode":   fmt.Sprintf("0x%02X", state.LastOpcode),
			"source":   strconv.Itoa(int(state.LastSource)),
		},
	})
}

func deviceMicrosBefore(candidate, current uint32) bool {
	return int32(candidate-current) < 0
}

func (runner *MacroRunner) handleBoardMacroStatus(
	status native.MacroStatus,
	generation uint64,
) {
	snapshot := runner.runtime.Snapshot()
	if snapshot.Generation != generation {
		return
	}
	runner.handleBoardMacroStatusAtGeneration(
		status, generation, captureBoardIdentity(snapshot.Port, snapshot.Hello),
	)
}

func (runner *MacroRunner) handleBoardMacroStatusAtGeneration(
	status native.MacroStatus,
	generation uint64,
	board string,
) {
	token := boardCaptureToken{
		Generation: generation,
		ID:         status.ID, StartedAtUS: status.StartedAtUS, Board: board,
	}
	runner.observeLocalPlaybackStatus(status, generation, board)
	switch status.State {
	case native.MacroRecording:
		runner.boardCaptureMu.Lock()
		defer runner.boardCaptureMu.Unlock()
		runner.prepareBoardCaptureGenerationLocked(token.Generation)
		if _, finished := runner.boardCaptureFinished[token]; finished {
			return
		}
		state := runner.RecordingState()
		if state.Active {
			runner.recordMu.RLock()
			sameCapture := state.BoardOwned && runner.recordCapture == token
			runner.recordMu.RUnlock()
			if sameCapture {
				return
			}
			if state.BoardOwned {
				// A board reboot/reconnect may legally reuse its uint8 ID. Never
				// append the new generation to an older provisional recording.
				if _, err := runner.StopRecording(false); err != nil {
					runner.publishBoardCaptureFailure(token, err)
					return
				}
			} else {
				runner.runtime.PublishStructuredEvent(Event{
					Kind: "macro.recording", Lifecycle: "conflict", State: "blocked",
					Reason: "another macro recording is already active",
					Text:   fmt.Sprintf("board capture %d could not start while %d/%s is recording", status.ID, state.ID, state.Name),
				})
				return
			}
		}
		name := runner.uniqueBoardCaptureName(status.ID)
		if _, err := runner.startRecording(&token, name, "board", "green"); err != nil {
			runner.runtime.PublishStructuredEvent(Event{
				Kind: "macro.recording", Lifecycle: "failed", State: "error",
				Reason: err.Error(), Text: fmt.Sprintf("start board capture %d: %v", status.ID, err),
			})
		}
	case native.MacroCaptured, native.MacroExported:
		state := runner.RecordingState()
		if state.Active && state.BoardOwned && status.DroppedSteps != 0 {
			// The MCU retained ring is a reconnect-safe prefix. While this exact
			// connection remains present, timestamped Action events keep extending
			// the host recording beyond that prefix until the user explicitly
			// stops. Do not let the bounded/offline lifecycle seal it early.
			runner.recordMu.RLock()
			sameCapture := runner.recordCapture == token
			runner.recordMu.RUnlock()
			if sameCapture && recordingConnectionMatches(runner.runtime.Snapshot(), token) {
				runner.recordMu.Lock()
				runner.recording.DroppedSteps = status.DroppedSteps
				runner.recordMu.Unlock()
				runner.runtime.PublishStructuredEvent(Event{
					Kind: "macro.recording", Lifecycle: "streaming-host", State: "recording",
					Text: fmt.Sprintf(
						"board capture %d retained ring is full; connected MCU-timestamped action streaming continues until explicit stop",
						status.ID,
					),
				})
				return
			}
		}
		runner.beginBoardCaptureFinalization(token, status)
	case native.MacroIdle, native.MacroCancelled, native.MacroFailed:
		runner.finishBoardRecordingLifecycle(token, status.State)
	}
}

func (runner *MacroRunner) observeLocalPlaybackStatus(
	status native.MacroStatus,
	generation uint64,
	board string,
) {
	runner.mu.Lock()
	hostPlayback := runner.state.Running &&
		runner.state.Generation == generation &&
		!strings.HasPrefix(runner.state.Lifecycle, "local-")
	if hostPlayback {
		runner.mu.Unlock()
		return
	}
	localPlayback := strings.HasPrefix(runner.state.Lifecycle, "local-") &&
		runner.state.Generation == generation && runner.state.ID == status.ID
	switch status.State {
	case native.MacroPlaying:
		macro := runner.localCaptureMacro(status.ID, board)
		name := macro.Name
		if name == "" {
			name = fmt.Sprintf("Board capture %d", status.ID)
		}
		tolerance := macro.TimingToleranceUS
		if tolerance == 0 {
			tolerance = defaultMacroTimingToleranceUS
		}
		runner.state = MacroState{
			Running: true, Generation: generation, ID: status.ID,
			Name: name, Category: macro.Category, Color: macro.Color,
			Step: int(status.ExecutedSteps), StepCount: int(status.TotalSteps),
			StartedAt: time.Now(), DeviceStartedAtUS: status.StartedAtUS,
			AcceptedBytes: status.AcceptedBytes, BufferFill: status.Fill,
			Underruns: status.Underruns, DispatchErrors: status.DispatchErrors,
			DroppedSteps: status.DroppedSteps, TimingToleranceUS: tolerance,
			Lifecycle: "local-playing", Device: status,
		}
		localPlayback = true
	case native.MacroCompleted, native.MacroCancelled, native.MacroFailed:
		if !localPlayback {
			runner.mu.Unlock()
			return
		}
		runner.state.Running = false
		runner.state.Step = int(status.ExecutedSteps)
		runner.state.FinishedAt = time.Now()
		runner.state.Device = status
		runner.state.DeviceStartedAtUS = status.StartedAtUS
		runner.state.BufferFill = status.Fill
		runner.state.Underruns = status.Underruns
		runner.state.DispatchErrors = status.DispatchErrors
		runner.state.DroppedSteps = status.DroppedSteps
		runner.state.Faithful = status.State == native.MacroCompleted &&
			status.ExecutedSteps == status.TotalSteps && status.Underruns == 0 &&
			status.DispatchErrors == 0 && status.DroppedSteps == 0
		switch status.State {
		case native.MacroCompleted:
			runner.state.Lifecycle = "local-completed"
		case native.MacroCancelled:
			runner.state.Lifecycle = "local-cancelled"
		case native.MacroFailed:
			runner.state.Lifecycle = "local-failed"
			runner.state.LastError = "board-local macro replay failed"
		}
	default:
		if !localPlayback || status.State == native.MacroCaptured ||
			status.State == native.MacroExported {
			runner.mu.Unlock()
			return
		}
		runner.state.Step = int(status.ExecutedSteps)
		runner.state.Device = status
	}
	state := runner.state
	runner.mu.Unlock()
	runner.publishLocalPlaybackStatus(state)
}

func (runner *MacroRunner) localCaptureMacro(id byte, board string) appconfig.Macro {
	for _, macro := range runner.List() {
		if macro.CaptureID == id && macro.CaptureBoard == board {
			return macro
		}
	}
	return appconfig.Macro{ID: id, Name: fmt.Sprintf("Board capture %d", id)}
}

func (runner *MacroRunner) publishLocalPlaybackStatus(state MacroState) {
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.playback", Lifecycle: state.Lifecycle,
		State: strings.TrimPrefix(state.Lifecycle, "local-"),
		Text: fmt.Sprintf(
			"board-local macro %d/%s %s step %d/%d MCU epoch=%d",
			state.ID, state.Name, strings.TrimPrefix(state.Lifecycle, "local-"),
			state.Step, state.StepCount, state.DeviceStartedAtUS,
		),
		Metadata: map[string]string{
			"macro_id": strconv.Itoa(int(state.ID)), "macro_name": state.Name,
			"step": strconv.Itoa(state.Step), "steps": strconv.Itoa(state.StepCount),
			"device_started_at_us": strconv.FormatUint(uint64(state.DeviceStartedAtUS), 10),
		},
	})
}

func (runner *MacroRunner) recoverBoardCaptureOnConnect(
	generation uint64,
	port ports.Info,
	hello native.Hello,
) {
	if hello.Capabilities&native.CapabilityTimedMacroQueue == 0 {
		return
	}
	status, err := runner.queryCaptureStatus(generation)
	if err != nil {
		runner.runtime.PublishStructuredEvent(Event{
			Kind: "macro.recovery", Lifecycle: "unavailable", State: "idle",
			Reason: err.Error(), Text: "board macro status could not be queried after connect: " + err.Error(),
		})
		return
	}
	if status.State == native.MacroRecording || status.State == native.MacroCaptured ||
		status.State == native.MacroExported {
		runner.handleBoardMacroStatusAtGeneration(
			status, generation, captureBoardIdentity(port, hello),
		)
	}
}

func (runner *MacroRunner) queryBoardMacroStatus(generation uint64) (native.MacroStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	frame, err := runner.runtime.requestAtGeneration(
		ctx, generation, native.OpMacroStep, native.MacroQueueQueryPayload(),
		native.OpMacroStatus,
	)
	if err != nil {
		return native.MacroStatus{}, err
	}
	return native.ParseMacroStatus(frame.Payload)
}

func (runner *MacroRunner) prepareBoardCaptureGenerationLocked(generation uint64) {
	if runner.boardCaptureGeneration == generation &&
		runner.boardCaptureFinalizing != nil && runner.boardCaptureFinished != nil {
		return
	}
	runner.boardCaptureGeneration = generation
	runner.boardCaptureFinalizing = make(map[boardCaptureToken]struct{})
	runner.boardCaptureFinished = make(map[boardCaptureToken]struct{})
}

func captureBoardIdentity(port ports.Info, hello native.Hello) string {
	transport := strings.TrimSpace(port.SerialNumber)
	if transport == "" {
		transport = strings.TrimSpace(port.InstanceID)
	}
	if transport == "" {
		transport = strings.TrimSpace(port.Name)
	}
	if transport == "" {
		transport = "unknown"
	}
	if len(transport) > 160 || !printableCaptureIdentity(transport) {
		digest := sha256.Sum256([]byte(transport))
		transport = "sha256:" + hex.EncodeToString(digest[:])
	}
	return fmt.Sprintf(
		"transport=%s;vid=%s;pid=%s;kind=%d;build=%08X",
		transport, strings.ToUpper(port.VID), strings.ToUpper(port.PID),
		hello.BoardKind, hello.BuildHash,
	)
}

func printableCaptureIdentity(value string) bool {
	for _, character := range []byte(value) {
		if character < 0x20 || character > 0x7E {
			return false
		}
	}
	return true
}

func (runner *MacroRunner) finishBoardRecordingLifecycle(
	token boardCaptureToken,
	state byte,
) {
	runner.boardCaptureMu.Lock()
	defer runner.boardCaptureMu.Unlock()
	recording := runner.RecordingState()
	if !recording.Active || !recording.BoardOwned {
		return
	}
	runner.recordMu.RLock()
	active := runner.recordCapture
	runner.recordMu.RUnlock()
	if active.Generation != token.Generation || active.ID != token.ID ||
		(token.StartedAtUS != 0 && active.StartedAtUS != token.StartedAtUS) {
		return
	}
	macro, err := runner.StopRecording(false)
	if err != nil {
		return
	}
	lifecycle := "discarded"
	if state == native.MacroFailed {
		lifecycle = "failed"
	}
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: lifecycle, State: lifecycle,
		Text: fmt.Sprintf(
			"board capture %d/%s ended in state %d and was %s",
			macro.ID, macro.Name, state, lifecycle,
		),
	})
}

// beginBoardCaptureFinalization freezes the Action subscription synchronously
// in UART order. The final action that overflowed the strict prefix ring is
// therefore retained when firmware emits Action-before-Captured, while later
// traffic and duplicate lifecycle reports cannot change the sealed recording.
func (runner *MacroRunner) beginBoardCaptureFinalization(
	token boardCaptureToken,
	status native.MacroStatus,
) {
	runner.boardCaptureMu.Lock()
	runner.prepareBoardCaptureGenerationLocked(token.Generation)
	if _, busy := runner.boardCaptureFinalizing[token]; busy {
		runner.boardCaptureMu.Unlock()
		return
	}
	if _, finished := runner.boardCaptureFinished[token]; finished {
		runner.boardCaptureMu.Unlock()
		return
	}

	state := runner.RecordingState()
	if !state.Active {
		name := runner.uniqueBoardCaptureName(status.ID)
		if _, err := runner.startRecording(&token, name, "board", "green"); err != nil {
			runner.boardCaptureMu.Unlock()
			runner.publishBoardCaptureFailure(token, err)
			return
		}
	} else {
		runner.recordMu.RLock()
		matches := state.BoardOwned && runner.recordCapture == token
		runner.recordMu.RUnlock()
		if !matches {
			runner.boardCaptureMu.Unlock()
			runner.publishBoardCaptureFailure(
				token, errors.New("captured lifecycle does not match the active recording"),
			)
			return
		}
	}

	runner.recordMu.Lock()
	if runner.recordRelease != nil {
		runner.recordRelease()
		runner.recordRelease = nil
	}
	runner.recordSealed = true
	runner.recordMu.Unlock()
	runner.boardCaptureFinalizing[token] = struct{}{}
	runner.boardCaptureMu.Unlock()

	// Recovery uses request/response I/O and must never block the runtime's
	// unsolicited event pump.
	go runner.finalizeBoardCapture(token, status)
}

func (runner *MacroRunner) uniqueBoardCaptureName(id byte) string {
	base := fmt.Sprintf("Board capture %d", id)
	used := make(map[string]bool)
	for _, macro := range runner.List() {
		used[strings.ToLower(macro.Name)] = true
	}
	if !used[strings.ToLower(base)] {
		return base
	}
	for suffix := 2; ; suffix++ {
		name := fmt.Sprintf("%s (%d)", base, suffix)
		if !used[strings.ToLower(name)] {
			return name
		}
	}
}

func (runner *MacroRunner) finalizeBoardCapture(
	token boardCaptureToken,
	status native.MacroStatus,
) {
	stream, err := runner.fetchCapture(token)
	if err != nil {
		runner.finishBoardCapture(token, false)
		runner.publishBoardCaptureFailure(token, err)
		return
	}
	recovered, err := decodeMacroCaptureStream(stream)
	if err != nil {
		runner.finishBoardCapture(token, false)
		runner.publishBoardCaptureFailure(token, err)
		return
	}
	importKey := boardCaptureImportKey(token, stream)

	runner.boardCaptureMu.Lock()
	defer runner.boardCaptureMu.Unlock()
	if runner.runtime.Snapshot().Generation != token.Generation {
		runner.recordMu.RLock()
		stale := runner.recording.Active && runner.recording.BoardOwned &&
			runner.recordCapture == token
		runner.recordMu.RUnlock()
		if stale {
			_, _ = runner.StopRecording(false)
		}
		runner.finishBoardCaptureLocked(token, false)
		return
	}
	if existing, imported := runner.importedBoardCapture(importKey); imported {
		_, _ = runner.StopRecording(false)
		if err := runner.ackCapture(token); err != nil {
			runner.finishBoardCaptureLocked(token, false)
			runner.runtime.PublishStructuredEvent(Event{
				Kind: "macro.recovery", Lifecycle: "ack-pending", State: "saved",
				Reason: err.Error(), Text: fmt.Sprintf(
					"board capture %d is already saved as %d/%s but export acknowledgement failed: %v",
					token.ID, existing.ID, existing.Name, err,
				),
			})
			return
		}
		runner.finishBoardCaptureLocked(token, true)
		runner.runtime.PublishStructuredEvent(Event{
			Kind: "macro.recovery", Lifecycle: "deduplicated", State: "saved",
			Text: fmt.Sprintf(
				"board capture %d was already saved as %d/%s; retained export acknowledged",
				token.ID, existing.ID, existing.Name,
			),
		})
		return
	}
	runner.recordMu.Lock()
	if !runner.recording.Active || !runner.recording.BoardOwned ||
		runner.recordCapture != token || !runner.recordSealed {
		runner.recordMu.Unlock()
		runner.finishBoardCaptureLocked(token, false)
		return
	}
	runner.recordMacro.Steps = mergeRecordedMacroSteps(runner.recordMacro.Steps, recovered)
	runner.recordMacro.RecordingSource = "board"
	runner.recordMacro.CaptureDroppedSteps = status.DroppedSteps
	runner.recordMacro.CaptureImportKey = importKey
	runner.recordMacro.CaptureBoard = token.Board
	runner.recordMacro.CaptureID = token.ID
	runner.recordMacro.CaptureStartedAtUS = token.StartedAtUS
	// accepted_steps is the exact retained prefix. dropped_steps counts the
	// first action that did not fit and stopped local capture; a continuously
	// connected host still receives that Action event, while recovery-only
	// clients truthfully report it missing.
	expected := int(status.AcceptedSteps) + int(status.DroppedSteps)
	missing := 0
	if expected > len(runner.recordMacro.Steps) {
		missing = expected - len(runner.recordMacro.Steps)
	}
	if missing > 65535 {
		missing = 65535
	}
	runner.recordMacro.CaptureMissingSteps = uint16(missing)
	runner.recording.Steps = len(runner.recordMacro.Steps)
	runner.recording.DroppedSteps = status.DroppedSteps
	runner.recording.LastError = ""
	runner.recordMu.Unlock()

	macro, err := runner.StopRecording(true)
	if err != nil {
		runner.finishBoardCaptureLocked(token, false)
		runner.publishBoardCaptureFailure(token, err)
		return
	}
	acknowledged := true
	if err := runner.ackCapture(token); err != nil {
		acknowledged = false
		runner.runtime.PublishStructuredEvent(Event{
			Kind: "macro.recovery", Lifecycle: "ack-pending", State: "saved",
			Reason: err.Error(), Text: fmt.Sprintf(
				"board capture %d was saved as %d/%s but export acknowledgement failed: %v",
				status.ID, macro.ID, macro.Name, err,
			),
		})
	}
	runner.finishBoardCaptureLocked(token, acknowledged)
	lifecycle := "saved"
	if macro.CaptureMissingSteps != 0 {
		lifecycle = "saved-truncated"
	}
	if !acknowledged {
		lifecycle += "-ack-pending"
	}
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: lifecycle, State: lifecycle,
		Text: fmt.Sprintf(
			"board capture %d saved as %d/%s with %d steps; dropped=%d missing=%d",
			status.ID, macro.ID, macro.Name, len(macro.Steps),
			macro.CaptureDroppedSteps, macro.CaptureMissingSteps,
		),
	})
}

func boardCaptureImportKey(token boardCaptureToken, stream []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("pccontroller-board-capture-v1\x00"))
	_, _ = hash.Write([]byte(token.Board))
	_, _ = hash.Write([]byte{0, token.ID})
	var identity [4]byte
	binary.LittleEndian.PutUint32(identity[:], token.StartedAtUS)
	_, _ = hash.Write(identity[:])
	_, _ = hash.Write(stream)
	return hex.EncodeToString(hash.Sum(nil))
}

func (runner *MacroRunner) importedBoardCapture(importKey string) (appconfig.Macro, bool) {
	for _, macro := range runner.List() {
		if macro.CaptureImportKey == importKey {
			return macro, true
		}
	}
	return appconfig.Macro{}, false
}

func (runner *MacroRunner) ackBoardCaptureExport(token boardCaptureToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	_, err := runner.runtime.requestAtGeneration(
		ctx, token.Generation, native.OpMacroStep,
		native.MacroCaptureAcknowledgePayload(token.ID, token.StartedAtUS),
		native.OpACK,
	)
	return err
}

func (runner *MacroRunner) finishBoardCapture(token boardCaptureToken, completed bool) {
	runner.boardCaptureMu.Lock()
	runner.finishBoardCaptureLocked(token, completed)
	runner.boardCaptureMu.Unlock()
}

func (runner *MacroRunner) finishBoardCaptureLocked(token boardCaptureToken, completed bool) {
	if token.Generation != runner.boardCaptureGeneration {
		return
	}
	delete(runner.boardCaptureFinalizing, token)
	if completed {
		runner.boardCaptureFinished[token] = struct{}{}
	}
}

func (runner *MacroRunner) publishBoardCaptureFailure(token boardCaptureToken, err error) {
	runner.recordMu.Lock()
	if runner.recording.Active && runner.recording.BoardOwned &&
		runner.recordCapture == token {
		runner.recording.LastError = err.Error()
	}
	runner.recordMu.Unlock()
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "failed", State: "error",
		Reason: err.Error(), Text: fmt.Sprintf("recover board capture %d: %v", token.ID, err),
	})
}

func (runner *MacroRunner) fetchBoardCapture(token boardCaptureToken) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stream []byte
	var total uint16
	for first := true; first || len(stream) < int(total); first = false {
		frame, err := runner.runtime.requestAtGeneration(
			ctx, token.Generation, native.OpMacroStep,
			native.MacroCaptureQueryPayload(token.ID, uint16(len(stream))),
			native.OpMacroStatus,
		)
		if err != nil {
			return nil, fmt.Errorf("query capture at byte %d: %w", len(stream), err)
		}
		chunk, err := native.ParseMacroCaptureChunk(frame.Payload)
		if err != nil {
			return nil, err
		}
		if chunk.ID != token.ID || int(chunk.Offset) != len(stream) ||
			(!first && chunk.TotalBytes != total) {
			return nil, fmt.Errorf(
				"capture identity/range changed: want id=%d offset=%d total=%d, got id=%d offset=%d total=%d",
				token.ID, len(stream), total, chunk.ID, chunk.Offset, chunk.TotalBytes,
			)
		}
		total = chunk.TotalBytes
		stream = append(stream, chunk.Data...)
	}
	return stream, nil
}

func decodeMacroCaptureStream(stream []byte) ([]appconfig.MacroStep, error) {
	steps := make([]appconfig.MacroStep, 0)
	for offset := 0; offset < len(stream); {
		if len(stream)-offset < native.MacroRecordHeaderSize {
			return nil, fmt.Errorf("capture ends in a %d-byte record header", len(stream)-offset)
		}
		due := binary.LittleEndian.Uint32(stream[offset : offset+4])
		opcode := stream[offset+4]
		length := int(stream[offset+5])
		end := offset + native.MacroRecordHeaderSize + length
		required, recordable := native.MacroBoardActionPayloadLength(opcode)
		if !recordable || length != int(required) || end > len(stream) {
			return nil, fmt.Errorf("invalid captured record at byte %d", offset)
		}
		step, ok := recordedMacroStep(ActionEvidence{
			Opcode: opcode, Payload: append([]byte(nil), stream[offset+6:end]...),
			Source: native.InputSourcePhysical, BoardOrigin: true,
			DeviceMicros: due, Timed: true,
		})
		if !ok {
			return nil, fmt.Errorf("captured opcode 0x%02X payload is invalid", opcode)
		}
		step.AtUS = due
		if len(steps) != 0 && step.AtUS < steps[len(steps)-1].AtUS {
			return nil, errors.New("captured step timing is not ordered")
		}
		steps = append(steps, step)
		offset = end
	}
	return steps, nil
}

func mergeRecordedMacroSteps(current, recovered []appconfig.MacroStep) []appconfig.MacroStep {
	result := append([]appconfig.MacroStep(nil), current...)
	available := make(map[string]int)
	for _, step := range current {
		available[macroStepIdentity(step)]++
	}
	seen := make(map[string]int)
	for _, step := range recovered {
		key := macroStepIdentity(step)
		seen[key]++
		if seen[key] > available[key] {
			result = append(result, step)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].AtUS < result[j].AtUS })
	return result
}

func macroStepIdentity(step appconfig.MacroStep) string {
	opcode, payload, err := compileMacroCommand(step)
	if err != nil {
		return fmt.Sprintf("invalid:%#v", step)
	}
	record, err := native.EncodeMacroRecord(step.AtUS, opcode, payload)
	if err != nil {
		return fmt.Sprintf("invalid:%#v", step)
	}
	return string(record)
>>>>>>> origin/agent/webui-defects
}

func (runner *MacroRunner) publishRecordedStep(step appconfig.MacroStep) {
	// PublishStructuredEvent queues delivery without invoking snapshot readers;
	// the recorder lock preserves order between concurrent acknowledged commands.
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "captured", State: "recording",
		Text:     fmt.Sprintf("macro %d/%s recorded step %d (%s)", runner.recording.ID, runner.recording.Name, runner.recording.Steps, step.Kind),
		Metadata: map[string]string{"macro_mode": runner.recording.Mode, "steps": strconv.Itoa(runner.recording.Steps)},
	})
}

// Start validates a macro and selects its persisted playback engine. Legacy
// macros with no mode retain MCU playback; newly recorded alpha macros use the
// host monotonic scheduler by default.
func (runner *MacroRunner) Start(ctx context.Context, reference string) (MacroState, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	if recording := runner.RecordingState(); recording.Active {
		return MacroState{}, fmt.Errorf("macro recording %d/%s is active; save or discard it before playback", recording.ID, recording.Name)
	}

	macro, err := runner.find(reference)
	if err != nil {
		return MacroState{}, err
	}
	compiled, err := compileMacro(macro)
	if err != nil {
		return MacroState{}, err
	}
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return MacroState{}, errors.New("device is not connected")
	}
	mode := normalizedMacroMode(macro.Mode)
	if mode == macroModeMCU && snapshot.Hello.Capabilities&native.CapabilityTimedMacroQueue == 0 {
		return MacroState{}, errors.New("connected firmware does not advertise the MCU-timed macro queue")
	}
	deviceStatus, err := runner.queryBoard(ctx, snapshot.Generation)
	if err != nil {
		return MacroState{}, fmt.Errorf("query macro queue before playback: %w", err)
	}
	runner.applyDeviceStatus(deviceStatus)
	switch deviceStatus.State {
	case native.MacroRecording, native.MacroCaptured, native.MacroExported:
		// Start recovery immediately, but never clear or overwrite the retained
		// board-owned ring. Explicit capture clear remains a distinct user act.
		runner.handleBoardMacroStatusAtGeneration(
			deviceStatus, snapshot.Generation,
			captureBoardIdentity(snapshot.Port, snapshot.Hello),
		)
		return MacroState{}, fmt.Errorf(
			"board retains macro capture %d in state %d; save/recover it and explicitly clear it before host playback",
			deviceStatus.ID, deviceStatus.State,
		)
	case native.MacroBuffering, native.MacroPlaying:
		return MacroState{}, fmt.Errorf("board macro queue is busy in state %d", deviceStatus.State)
	}
	if macroNeedsMotionPermission(macro) {
		if err := requireMotionAllowed(ctx, runner.runtime, runner.hostConfig); err != nil {
			return MacroState{}, err
		}
	}

	runner.mu.Lock()
	if runner.state.Running {
		state := runner.state
		runner.mu.Unlock()
		return state, fmt.Errorf("macro %d/%s is already running; cancel it first", state.ID, state.Name)
	}
	tolerance := macro.TimingToleranceUS
	if tolerance == 0 {
		tolerance = modeTimingTolerance(mode)
	}
	runner.state = MacroState{
<<<<<<< HEAD
		Running: true, ID: macro.ID, Name: macro.Name,
		Mode:     mode,
=======
		Running: true, Generation: snapshot.Generation,
		ID: macro.ID, Name: macro.Name,
>>>>>>> origin/agent/webui-defects
		Category: macro.Category, Color: normalizedMacroColor(macro.Color),
		StepCount: len(compiled.steps), DurationUS: compiled.durationUS,
		StartedAt: time.Now(), TimingToleranceUS: tolerance,
		Lifecycle: "buffering",
	}
	runner.mu.Unlock()

	lease, _, err := runner.runtime.AcquireProgramState(
		fmt.Sprintf("macro:%d", macro.ID),
		fmt.Sprintf("playing macro %s", macro.Name),
	)
	if err != nil {
		runner.failStart(macro, err)
		return runner.State(), err
	}
	begun := false
	var streamLease *macroStreamLease
	fail := func(cause error) (MacroState, error) {
		if begun {
			if cleanupErr := runner.cancelBoardAtGeneration(snapshot.Generation, false); cleanupErr != nil {
				cause = errors.Join(cause, fmt.Errorf("macro cleanup: %w", cleanupErr))
			}
		}
		if restoreErr := runner.restoreMacroStream(streamLease); restoreErr != nil {
			cause = errors.Join(cause, fmt.Errorf("restore telemetry stream: %w", restoreErr))
		}
		lease.Release()
		runner.failStart(macro, cause)
		return runner.State(), cause
	}
	if mode == macroModeHost {
		command := boundHostMacroCommand(runner.runtime)
		if err := runner.showMacroIdentity(ctx, compiled); err != nil {
			runner.runtime.PublishHostEvent("macro.display", "macro identity display unavailable: "+err.Error())
		}
		playContext, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		runner.mu.Lock()
		runner.cancel = cancel
		runner.cancelKeep = false
		runner.done = done
		runner.state.Lifecycle = "playing"
		state := runner.state
		runner.mu.Unlock()
		runner.publishLifecycle("started", state, nil)
		go runner.playHost(playContext, done, compiled, lease, command)
		return state, nil
	}

	startPayload, err := native.MacroQueueStartPayload(
		macro.ID,
		uint16(len(compiled.steps)),
		macro.KeepOutputsOnCancel,
	)
	if err != nil {
		return fail(err)
	}
	if _, err = runner.request(ctx, snapshot.Generation, native.OpMacroStart, startPayload, native.OpACK); err != nil {
		return fail(err)
	}
	begun = true
	afterID := runner.runtime.LatestEventID()
	sent, err := runner.appendBytes(ctx, snapshot.Generation, compiled, 0, native.MacroQueueCapacity)
	if err != nil {
		return fail(err)
	}
	if err := runner.showMacroIdentity(ctx, snapshot.Generation, compiled); err != nil {
		runner.runtime.PublishHostEvent("macro.display", "macro identity display unavailable: "+err.Error())
	}
	streamLease, err = runner.pauseMacroStream(ctx, snapshot)
	if err != nil {
		return fail(fmt.Errorf("pause periodic telemetry for exact macro timing: %w", err))
	}
	if _, err = runner.request(ctx, snapshot.Generation, native.OpMacroStep, native.MacroQueueRunPayload(), native.OpACK); err != nil {
		return fail(err)
	}

	playContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runner.mu.Lock()
	runner.cancel = cancel
	runner.cancelKeep = false
	runner.done = done
	runner.state.Lifecycle = "playing"
	state := runner.state
	runner.mu.Unlock()
	runner.publishLifecycle("started", state, nil)
	go runner.play(playContext, done, snapshot.Generation, compiled, sent, afterID, lease, streamLease)
	return state, nil
}

// Cancel defaults to a safe stop: every relay and user PWM output is switched
// off. CancelWithPolicy(true) is the explicit opt-in that preserves outputs.
func (runner *MacroRunner) Cancel(ctx context.Context) error {
	return runner.CancelWithPolicy(ctx, false)
}

func (runner *MacroRunner) CancelWithPolicy(ctx context.Context, keepOutputs bool) error {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	runner.mu.Lock()
	cancel := runner.cancel
	done := runner.done
	runner.cancelKeep = keepOutputs
	runner.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		state := runner.State()
		if state.LastError != "" {
			return errors.New(state.LastError)
		}
		return nil
	}
	if runner.State().Mode == macroModeHost {
		if keepOutputs {
			return nil
		}
		return runner.safeStopHost()
	}
	return runner.cancelBoard(keepOutputs)
}

func (runner *MacroRunner) find(reference string) (appconfig.Macro, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return appconfig.Macro{}, fmt.Errorf("macro name or ID is required")
	}
	numericID, numericErr := strconv.ParseUint(reference, 0, 8)
	for _, macro := range runner.List() {
		if strings.EqualFold(macro.Name, reference) ||
			(numericErr == nil && uint64(macro.ID) == numericID) {
			return macro, nil
		}
	}
	return appconfig.Macro{}, fmt.Errorf("macro %q is not configured", reference)
}

func (runner *MacroRunner) play(
	ctx context.Context,
	done chan struct{},
	generation uint64,
	compiled compiledMacro,
	sent int,
	afterID uint64,
	lease *ProgramStateLease,
	streamLease *macroStreamLease,
) {
	defer close(done)
	defer lease.Release()

	status, err := runner.queryBoard(ctx, generation)
	if err == nil {
		runner.applyDeviceStatus(status)
	}
	lastQuery := time.Now()
	observed := 0
	watchdog := time.Duration(compiled.durationUS)*time.Microsecond + 10*time.Second
	if watchdog < 15*time.Second {
		watchdog = 15 * time.Second
	}
	completion := macroCompletionWindow{watchdog: watchdog, watchdogDeadline: time.Now().Add(watchdog)}
	cancelled := false
	consume := func(event Event) {
		afterID = event.ID
		if event.Frame.Seq == native.MacroExecutionSequence {
			if observed < len(compiled.steps) {
				actualUS, timed := native.ResponseDeviceMicros(event.Frame)
				if timed {
					step := compiled.steps[observed]
					delta := int32(actualUS - (status.StartedAtUS + step.dueUS))
					runner.recordEvidence(observed, delta, event.Frame.Opcode == native.OpACK)
					observed++
					status = acknowledgeMacroDeviceStatus(status, observed, step.recordLength)
					runner.applyDeviceStatus(status)
				}
			}
			return
		}
		if event.Frame.Opcode == native.OpEvent {
			deviceEvent, parseErr := native.ParseDeviceEvent(event.Frame.Payload)
			if parseErr == nil && deviceEvent.Macro != nil && deviceEvent.Macro.ID == compiled.definition.ID && deviceEvent.Macro.State != native.MacroBuffering {
				status = mergeMacroDeviceStatus(status, *deviceEvent.Macro)
				runner.applyDeviceStatus(status)
			}
		}
	}
	for err == nil {
		playSnapshot := runner.runtime.Snapshot()
		if !playSnapshot.Connected || playSnapshot.Generation != generation {
			err = fmt.Errorf("connection generation %d became unavailable during macro playback", generation)
			break
		}
		if ctx.Err() != nil {
			cancelled = true
			runner.mu.RLock()
			keep := runner.cancelKeep
			runner.mu.RUnlock()
			err = runner.cancelBoardAtGeneration(generation, keep)
			if err == nil {
				status.State = native.MacroCancelled
			}
			break
		}
		if event, pending := runner.runtime.pendingMacroEvent(afterID, compiled.definition.ID); pending {
			consume(event)
			continue
		}
		if terminal, terminalErr := completion.terminal(status, observed, len(compiled.steps), time.Now()); terminal {
			err = terminalErr
			break
		}

		if status.Active() && sent < len(compiled.stream) && status.Free() != 0 {
			before := sent
			sent, err = runner.appendBytes(ctx, generation, compiled, sent, int(status.Free()))
			if err != nil {
				break
			}
			status.Fill += byte(sent - before)
			status.AcceptedBytes = uint16(sent)
			status.AcceptedSteps = uint16(compiled.completeSteps(sent))
			runner.applyDeviceStatus(status)
		}

		wait := macroStatusPollInterval - time.Since(lastQuery)
		if status.State == native.MacroCompleted {
			// Execution is finished. Wait only for evidence; another blocking
			// query cannot supply missing per-step timestamps.
			wait = min(macroStatusPollInterval, time.Until(completion.evidenceDeadline))
		}
		if wait <= 0 {
<<<<<<< HEAD
			if status.State == native.MacroCompleted {
				continue
			}
			var reported native.MacroStatus
			reported, err = runner.queryBoard(ctx)
=======
			status, err = runner.queryBoard(ctx, generation)
>>>>>>> origin/agent/webui-defects
			lastQuery = time.Now()
			if err == nil {
				status = mergeMacroDeviceStatus(status, reported)
				runner.applyDeviceStatus(status)
			}
			continue
		}
		waitContext, cancelWait := context.WithTimeout(ctx, wait)
		event, waitErr := runner.runtime.WaitEvent(waitContext, afterID, "")
		cancelWait()
		if waitErr != nil {
			if errors.Is(waitErr, context.DeadlineExceeded) {
				continue
			}
			if ctx.Err() != nil {
				continue
			}
			err = waitErr
			break
		}
<<<<<<< HEAD
		consume(event)
=======
		playSnapshot = runner.runtime.Snapshot()
		if !playSnapshot.Connected || playSnapshot.Generation != generation {
			err = fmt.Errorf("connection generation %d became unavailable while waiting for macro evidence", generation)
			break
		}
		afterID = event.ID
		if event.Frame.Seq == native.MacroExecutionSequence {
			if observed < len(compiled.steps) {
				actualUS, timed := native.ResponseDeviceMicros(event.Frame)
				if timed {
					step := compiled.steps[observed]
					delta := int32(actualUS - (status.StartedAtUS + step.dueUS))
					runner.recordEvidence(observed, delta, event.Frame.Opcode == native.OpACK)
					if int(status.Fill) >= step.recordLength {
						status.Fill -= byte(step.recordLength)
					} else {
						status.Fill = 0
					}
					observed++
				}
				status.ExecutedSteps = observedExecutionCount(observed)
				runner.applyDeviceStatus(status)
			}
			continue
		}
		if event.Frame.Opcode == native.OpEvent {
			deviceEvent, parseErr := native.ParseDeviceEvent(event.Frame.Payload)
			if parseErr == nil && deviceEvent.Macro != nil &&
				deviceEvent.Macro.ID == compiled.definition.ID &&
				deviceEvent.Macro.State != native.MacroBuffering {
				status = *deviceEvent.Macro
				runner.applyDeviceStatus(status)
			}
		}
>>>>>>> origin/agent/webui-defects
	}
	if ctx.Err() != nil && !cancelled {
		cancelled = true
		runner.mu.RLock()
		keep := runner.cancelKeep
		runner.mu.RUnlock()
		cancelErr := runner.cancelBoardAtGeneration(generation, keep)
		if cancelErr == nil {
			status.State = native.MacroCancelled
			err = nil
		} else {
			err = errors.Join(ctx.Err(), cancelErr)
		}
	}

<<<<<<< HEAD
	var evidenceError *macroTimingEvidenceError
	// A completed queue has already executed its intended final output state.
	// Missing host timestamps are a proof failure, not unfinished execution;
	// do not issue another cancellation/output action merely to repair evidence.
	if err != nil && !cancelled && !errors.As(err, &evidenceError) {
		if cleanupErr := runner.cancelBoard(false); cleanupErr != nil {
=======
	if err != nil && !cancelled {
		if cleanupErr := runner.cancelBoardAtGeneration(generation, false); cleanupErr != nil {
>>>>>>> origin/agent/webui-defects
			err = errors.Join(err, fmt.Errorf("macro safe-stop cleanup: %w", cleanupErr))
		}
	}
	if restoreErr := runner.restoreMacroStream(streamLease); restoreErr != nil {
		err = errors.Join(err, fmt.Errorf("restore telemetry stream: %w", restoreErr))
	}
	runner.finishPlayback(done, compiled.definition, status, observed, cancelled, err)
}

<<<<<<< HEAD
func (runner *MacroRunner) playHost(
	ctx context.Context,
	done chan struct{},
	compiled compiledMacro,
	lease *ProgramStateLease,
	command hostMacroCommand,
) {
	defer close(done)
	defer lease.Release()

	observed, err := runHostMacro(ctx, compiled, command, runner.recordEvidence)
	cancelled := ctx.Err() != nil
	if cancelled && errors.Is(err, context.Canceled) {
		err = nil
	}
	if cancelled || err != nil {
		runner.mu.RLock()
		keep := runner.cancelKeep
		runner.mu.RUnlock()
		if !keep {
			if cleanupErr := safeStopHostWithCommand(command); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("macro safe-stop cleanup: %w", cleanupErr))
			}
		}
	}
	runner.finishHostPlayback(done, compiled.definition, observed, cancelled, err)
}

type hostMacroCommand func(context.Context, byte, []byte) error

// Bind both playback and cleanup to one serial session. An unplug/reconnect
// must not silently redirect a scheduled output command to a replacement board.
func boundHostMacroCommand(runtime *Runtime) hostMacroCommand {
	session := runtime.currentSession()
	return func(ctx context.Context, opcode byte, payload []byte) error {
		if session == nil || runtime.currentSession() != session {
			return errors.New("macro board session disconnected or replaced; playback stopped")
		}
		frame, err := session.Request(ctx, opcode, payload, native.OpACK)
		if err != nil {
			return err
		}
		if runtime.currentSession() != session {
			return errors.New("macro board session changed while acknowledging a step")
		}
		runtime.observe(frame)
		return nil
	}
}

func runHostMacro(
	ctx context.Context,
	compiled compiledMacro,
	command hostMacroCommand,
	observe func(int, int32, bool),
) (int, error) {
	return runHostMacroWithClock(ctx, compiled, command, observe, time.Now, waitHostMacroDeadline)
}

// The recorder stores offsets relative to its first acknowledged command.
// Anchor playback to that same boundary once, without clearing startup evidence
// or hiding later overruns by resetting the clock after every command.
func runHostMacroWithClock(
	ctx context.Context,
	compiled compiledMacro,
	command hostMacroCommand,
	observe func(int, int32, bool),
	now func() time.Time,
	waitUntil func(context.Context, time.Time) error,
) (int, error) {
	epoch := now()
	for index, step := range compiled.steps {
		if err := ctx.Err(); err != nil {
			return index, err
		}
		due := epoch.Add(time.Duration(step.dueUS) * time.Microsecond)
		if err := waitUntil(ctx, due); err != nil {
			return index, err
		}
		if err := ctx.Err(); err != nil {
			return index, err
		}
		err := command(ctx, step.opcode, step.payload)
		acknowledgedAt := now()
		delta := hostTimingDeltaAt(epoch, step.dueUS, acknowledgedAt)
		if err != nil {
			observe(index, delta, false)
			return index, err
		}
		if index == 0 {
			// Preserve any explicit leading wait, and measure the first command
			// against the original deadline before choosing the relative epoch.
			epoch = acknowledgedAt.Add(-time.Duration(step.dueUS) * time.Microsecond)
		}
		observe(index, delta, true)
	}
	return len(compiled.steps), nil
}

func waitHostMacroDeadline(ctx context.Context, due time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wait := time.Until(due)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func hostTimingDeltaAt(epoch time.Time, dueUS uint32, acknowledgedAt time.Time) int32 {
	delta := acknowledgedAt.Sub(epoch)/time.Microsecond - time.Duration(dueUS)
	if delta > time.Duration(int64(^uint32(0)>>1)) {
		return int32(^uint32(0) >> 1)
	}
	if delta < -time.Duration(int64(^uint32(0)>>1))-1 {
		return -int32(^uint32(0)>>1) - 1
	}
	return int32(delta)
}

func (runner *MacroRunner) safeStopHost() error {
	return safeStopHostWithCommand(runner.runtime.Command)
}

func safeStopHostWithCommand(command hostMacroCommand) error {
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	relayErr := command(ctx, native.OpRelayAllOff, nil)
	pwmErr := command(ctx, native.OpPWMAllOff, nil)
	return errors.Join(relayErr, pwmErr)
=======
func observedExecutionCount(observed int) uint16 {
	if observed <= 0 {
		return 0
	}
	if observed > 65535 {
		return 65535
	}
	return uint16(observed)
>>>>>>> origin/agent/webui-defects
}

func (runner *MacroRunner) appendBytes(
	ctx context.Context,
	generation uint64,
	compiled compiledMacro,
	offset int,
	available int,
) (int, error) {
	for offset < len(compiled.stream) && available > 0 {
		length := len(compiled.stream) - offset
		if length > native.MacroMaximumFragment {
			length = native.MacroMaximumFragment
		}
		if length > available {
			length = available
		}
		payload, err := native.MacroQueueAppendPayload(
			uint16(offset),
			uint16(compiled.completeSteps(offset+length)),
			compiled.stream[offset:offset+length],
		)
		if err != nil {
			return offset, err
		}
		if _, err := runner.request(ctx, generation, native.OpMacroStep, payload, native.OpACK); err != nil {
			return offset, err
		}
		offset += length
		available -= length
	}
	return offset, nil
}

func (runner *MacroRunner) queryBoard(ctx context.Context, generation uint64) (native.MacroStatus, error) {
	frame, err := runner.request(
		ctx, generation,
		native.OpMacroStep,
		native.MacroQueueQueryPayload(),
		native.OpMacroStatus,
	)
	if err != nil {
		return native.MacroStatus{}, err
	}
	return native.ParseMacroStatus(frame.Payload)
}

// pauseMacroStream drains and disables only the periodic STATUS producer
// before RUN is acknowledged. Macro execution ACKs, macro status events, and
// all other pushed state continue over the ordinary event path while this
// lease is active.
func (runner *MacroRunner) pauseMacroStream(
	ctx context.Context,
	snapshot Snapshot,
) (*macroStreamLease, error) {
	frame, err := runner.request(
		ctx, snapshot.Generation,
		native.OpGetSettings, nil, native.OpSettings,
	)
	if err != nil {
		return nil, fmt.Errorf("read current stream period: %w", err)
	}
	settings, err := native.ParseSettings(frame.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse current stream period: %w", err)
	}
	lease := &macroStreamLease{
		Generation: snapshot.Generation,
		Board:      captureBoardIdentity(snapshot.Port, snapshot.Hello),
		PeriodMS:   settings.StreamPeriodMS,
	}
	if !runner.macroStreamLeaseCurrent(lease) {
		return nil, fmt.Errorf("connection generation %d changed before telemetry could be paused", snapshot.Generation)
	}
	payload, err := native.StreamPeriodPayload(0)
	if err != nil {
		return nil, err
	}
	// Return the lease even when the request reports an error: the MCU may
	// have accepted SET_STREAM before the response transport failed. A
	// same-generation restoration attempt is harmless in that uncertainty.
	if _, err = runner.request(
		ctx, snapshot.Generation,
		native.OpSetStream, payload, native.OpACK,
	); err != nil {
		return lease, fmt.Errorf("disable periodic telemetry: %w", err)
	}
	return lease, nil
}

// restoreMacroStream is idempotent and intentionally uses a fresh bounded
// context so caller cancellation cannot strand a still-connected board at a
// zero telemetry period. A disconnected or replacement session is skipped;
// restoration must never target a board that did not grant this lease.
func (runner *MacroRunner) restoreMacroStream(lease *macroStreamLease) error {
	if lease == nil || lease.restored {
		return nil
	}
	lease.restored = true
	if !runner.macroStreamLeaseCurrent(lease) {
		return nil
	}
	payload, err := native.StreamPeriodPayload(lease.PeriodMS)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	_, err = runner.request(
		ctx, lease.Generation,
		native.OpSetStream, payload, native.OpACK,
	)
	return err
}

func (runner *MacroRunner) macroStreamLeaseCurrent(lease *macroStreamLease) bool {
	if lease == nil || runner.runtime == nil {
		return false
	}
	snapshot := runner.runtime.Snapshot()
	return snapshot.Connected && snapshot.Generation == lease.Generation &&
		captureBoardIdentity(snapshot.Port, snapshot.Hello) == lease.Board
}

func (runner *MacroRunner) request(
	ctx context.Context,
	generation uint64,
	opcode byte,
	payload []byte,
	expected byte,
) (native.Frame, error) {
	requestContext, cancel := context.WithTimeout(ctx, macroRequestTimeout)
	defer cancel()
	if runner.requestGeneration != nil {
		return runner.requestGeneration(
			requestContext, generation, opcode, payload, expected,
		)
	}
	return runner.runtime.requestAtGeneration(
		requestContext, generation, opcode, payload, expected,
	)
}

func (runner *MacroRunner) cancelBoard(keepOutputs bool) error {
	state := runner.State()
	generation := runner.runtime.Snapshot().Generation
	if state.Running && state.Generation != 0 {
		generation = state.Generation
	}
	return runner.cancelBoardAtGeneration(generation, keepOutputs)
}

func (runner *MacroRunner) cancelBoardAtGeneration(generation uint64, keepOutputs bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	_, err := runner.request(
		ctx, generation, native.OpMacroCancel,
		native.MacroQueueCancelPayload(keepOutputs), native.OpACK,
	)
	return err
}

func (runner *MacroRunner) showMacroIdentity(
	ctx context.Context,
	generation uint64,
	compiled compiledMacro,
) error {
	macro := compiled.definition
	label := strings.TrimSpace(macro.Label)
	if label == "" {
		label = macro.Name
	}
	if len(label) > 4 {
		label = label[:4]
	}
	durationMS := uint32(compiled.durationUS/1000) + 1500
	if durationMS > 65535 {
		durationMS = 65535
	}
	if label != "" {
		payload, err := native.DisplayTextPayload(native.DisplaySegments, uint16(durationMS), label)
		if err != nil {
			return err
		}
		if _, err := runner.request(ctx, generation, native.OpDisplayText, payload, native.OpACK); err != nil {
			return err
		}
	}
	if macro.LCDMessage != "" {
		payload, err := native.DisplayTextPayload(native.DisplayLCD, uint16(durationMS), macro.LCDMessage)
		if err != nil {
			return err
		}
		if _, err := runner.request(ctx, generation, native.OpDisplayText, payload, native.OpACK); err != nil {
			return err
		}
	}
	return nil
}

func (runner *MacroRunner) applyDeviceStatus(status native.MacroStatus) {
	runner.mu.Lock()
	runner.state.Step = max(runner.state.Step, int(status.ExecutedSteps))
	runner.state.Device = status
	runner.state.DeviceStartedAtUS = status.StartedAtUS
	runner.state.AcceptedBytes = status.AcceptedBytes
	runner.state.BufferFill = status.Fill
	runner.state.Underruns = status.Underruns
	runner.state.DispatchErrors = status.DispatchErrors
	runner.state.DroppedSteps = status.DroppedSteps
	runner.mu.Unlock()
}

func (runner *MacroRunner) recordEvidence(index int, delta int32, succeeded bool) {
	runner.mu.Lock()
	if !succeeded && runner.state.DispatchErrors < 255 {
		runner.state.DispatchErrors++
	}
	runner.state.Step = max(runner.state.Step, index+1)
	runner.state.EvidenceSteps = index + 1
	runner.state.LastTimingDeltaUS = delta
	absolute := uint32(delta)
	if delta < 0 {
		absolute = uint32(-int64(delta))
	}
	if index == 0 && runner.state.Mode == macroModeHost {
		runner.state.StartupDelayUS = absolute
	}
	if absolute > runner.state.MaximumTimingErrorUS {
		runner.state.MaximumTimingErrorUS = absolute
	}
	if absolute > runner.state.TimingToleranceUS {
		runner.state.TimingViolations++
	}
	state := runner.state
	runner.mu.Unlock()
	metadata := map[string]string{
		"macro_id": strconv.Itoa(int(state.ID)), "macro_name": state.Name,
		"macro_mode": state.Mode,
		"step":       strconv.Itoa(index + 1), "steps": strconv.Itoa(state.StepCount),
		"timing_delta_us": strconv.FormatInt(int64(delta), 10),
	}
	if state.Mode == macroModeMCU {
		metadata["mcu_delta_us"] = strconv.FormatInt(int64(delta), 10)
	} else {
		metadata["host_delta_us"] = strconv.FormatInt(int64(delta), 10)
		metadata["startup_delay_us"] = strconv.FormatUint(uint64(state.StartupDelayUS), 10)
	}
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.step", Lifecycle: "executed", State: map[bool]string{true: "acknowledged", false: "rejected"}[succeeded],
		Text:     fmt.Sprintf("macro %d/%s step %d/%d delta=%dus", state.ID, state.Name, index+1, state.StepCount, delta),
		Metadata: metadata,
	})
	runner.queueMacroPresentation(state)
}

func (runner *MacroRunner) finishPlayback(
	done chan struct{},
	macro appconfig.Macro,
	status native.MacroStatus,
	observed int,
	cancelled bool,
	err error,
) {
	runner.mu.Lock()
	if runner.done != done {
		runner.mu.Unlock()
		return
	}
	runner.state.Running = false
	runner.state.FinishedAt = time.Now()
	runner.state.Device = status
	runner.state.Underruns = status.Underruns
	runner.state.DispatchErrors = status.DispatchErrors
	runner.state.DroppedSteps = status.DroppedSteps
	runner.state.BufferFill = status.Fill
	runner.state.AcceptedBytes = status.AcceptedBytes
	runner.state.Step = max(runner.state.Step, int(status.ExecutedSteps))
	runner.state.Faithful = err == nil && !cancelled &&
		status.State == native.MacroCompleted &&
		status.Underruns == 0 && status.DispatchErrors == 0 && status.DroppedSteps == 0 &&
		observed == len(macro.Steps) && runner.state.TimingViolations == 0
	var evidenceError *macroTimingEvidenceError
	switch {
	case err != nil && errors.As(err, &evidenceError) && status.State == native.MacroCompleted:
		runner.state.Lifecycle = "completed"
		runner.state.LastError = err.Error()
	case err != nil:
		runner.state.Lifecycle = "failed"
		runner.state.LastError = err.Error()
	case cancelled || status.State == native.MacroCancelled:
		runner.state.Lifecycle = "cancelled"
		runner.state.LastError = ""
	case status.State != native.MacroCompleted:
		err = fmt.Errorf("macro ended in device state %d", status.State)
		runner.state.Lifecycle = "failed"
		runner.state.LastError = err.Error()
	default:
		runner.state.Lifecycle = "completed"
		runner.state.LastError = ""
	}
	runner.cancel = nil
	runner.done = nil
	state := runner.state
	runner.mu.Unlock()
	runner.publishLifecycle(state.Lifecycle, state, err)
}

func (runner *MacroRunner) finishHostPlayback(
	done chan struct{},
	macro appconfig.Macro,
	observed int,
	cancelled bool,
	err error,
) {
	runner.mu.Lock()
	if runner.done != done {
		runner.mu.Unlock()
		return
	}
	runner.state.Running = false
	runner.state.FinishedAt = time.Now()
	runner.state.Faithful = err == nil && !cancelled &&
		observed == len(macro.Steps) && runner.state.TimingViolations == 0
	switch {
	case cancelled && err == nil:
		runner.state.Lifecycle = "cancelled"
		runner.state.LastError = ""
		err = nil
	case err != nil:
		runner.state.Lifecycle = "failed"
		runner.state.LastError = err.Error()
	default:
		runner.state.Lifecycle = "completed"
		runner.state.LastError = ""
	}
	runner.cancel = nil
	runner.done = nil
	state := runner.state
	runner.mu.Unlock()
	runner.publishLifecycle(state.Lifecycle, state, err)
}

func (runner *MacroRunner) failStart(macro appconfig.Macro, err error) {
	runner.mu.Lock()
	runner.state.Running = false
	runner.state.FinishedAt = time.Now()
	runner.state.Lifecycle = "failed"
	runner.state.LastError = err.Error()
	state := runner.state
	runner.mu.Unlock()
	runner.publishLifecycle("failed", state, err)
}

func (runner *MacroRunner) publishLifecycle(lifecycle string, state MacroState, err error) {
	text := fmt.Sprintf("macro %d/%s %s", state.ID, state.Name, lifecycle)
	if err != nil {
		text += ": " + err.Error()
	}
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro", Lifecycle: lifecycle, State: lifecycle, Text: text,
		Metadata: map[string]string{
			"macro_id": strconv.Itoa(int(state.ID)), "macro_name": state.Name,
			"macro_mode": state.Mode,
			"category":   state.Category, "color": state.Color,
			"step": strconv.Itoa(state.Step), "steps": strconv.Itoa(state.StepCount),
			"faithful":         strconv.FormatBool(state.Faithful),
			"timing_error_us":  strconv.FormatUint(uint64(state.MaximumTimingErrorUS), 10),
			"startup_delay_us": strconv.FormatUint(uint64(state.StartupDelayUS), 10),
			"underruns":        strconv.Itoa(int(state.Underruns)),
			"dispatch_errors":  strconv.Itoa(int(state.DispatchErrors)),
		},
	})
	runner.queueMacroPresentation(state)
}

func compileMacro(macro appconfig.Macro) (compiledMacro, error) {
	if len(macro.Steps) == 0 || len(macro.Steps) > 65535 {
		return compiledMacro{}, fmt.Errorf("macro %d/%s must contain 1..65535 steps", macro.ID, macro.Name)
	}
	result := compiledMacro{definition: macro, steps: make([]compiledMacroStep, 0, len(macro.Steps))}
	var previous uint32
	for index, step := range macro.Steps {
		dueUS, err := macroStepDueUS(step)
		if err != nil {
			return compiledMacro{}, fmt.Errorf("macro %d/%s step %d: %w", macro.ID, macro.Name, index+1, err)
		}
		if index != 0 && dueUS < previous {
			return compiledMacro{}, fmt.Errorf("macro %d/%s step %d timing is not ordered", macro.ID, macro.Name, index+1)
		}
		opcode, payload, err := compileMacroCommand(step)
		if err != nil {
			return compiledMacro{}, fmt.Errorf("macro %d/%s step %d: %w", macro.ID, macro.Name, index+1, err)
		}
		record, err := native.EncodeMacroRecord(dueUS, opcode, payload)
		if err != nil {
			return compiledMacro{}, fmt.Errorf("macro %d/%s step %d: %w", macro.ID, macro.Name, index+1, err)
		}
		if normalizedMacroMode(macro.Mode) == macroModeMCU && len(result.stream)+len(record) > 65535 {
			return compiledMacro{}, fmt.Errorf("macro %d/%s encoded stream exceeds 65535 bytes", macro.ID, macro.Name)
		}
		result.stream = append(result.stream, record...)
		result.steps = append(result.steps, compiledMacroStep{
			dueUS: dueUS, opcode: opcode, payload: append([]byte(nil), payload...),
			recordLength: len(record), streamEnd: len(result.stream),
		})
		previous = dueUS
	}
	result.durationUS = previous
	return result, nil
}

func (compiled compiledMacro) completeSteps(offset int) int {
	return sort.Search(len(compiled.steps), func(index int) bool {
		return compiled.steps[index].streamEnd > offset
	})
}

func macroStepDueUS(step appconfig.MacroStep) (uint32, error) {
	due := step.AtUS
	if due > 0x7FFFFFFF {
		return 0, errors.New("timing exceeds 2147483647 us")
	}
	return due, nil
}

func compileMacroCommand(step appconfig.MacroStep) (byte, []byte, error) {
	switch strings.ToLower(strings.TrimSpace(step.Kind)) {
	case "relay":
		payload, err := native.RelayPayload(step.Target, step.Value != 0)
		if step.Value > 1 {
			err = fmt.Errorf("relay value %d is outside 0..1", step.Value)
		}
		return native.OpRelaySet, payload, err
	case "motion", "side":
		if step.Value > 2 {
			return 0, nil, fmt.Errorf("motion value %d is outside 0..2", step.Value)
		}
		payload, err := native.RelaySidePayload(step.Target, byte(step.Value))
		return native.OpRelaySide, payload, err
	case "pwm", "mosfet":
		payload, err := native.PWMSetPayload(step.Target, step.Value)
		return native.OpPWMSet, payload, err
	case "relays-off":
		return native.OpRelayAllOff, nil, nil
	case "pwm-off":
		return native.OpPWMAllOff, nil, nil
	case "beep", "buzzer", "tone":
		frequency := step.FrequencyHz
		if frequency == 0 {
			frequency = step.Value
		}
		if (frequency == 0 && step.DurationMS != 0) ||
			(frequency != 0 && (step.DurationMS == 0 || frequency < 20 || frequency > 20000)) {
			return 0, nil, errors.New("buzzer requires frequency/duration 0/0 to stop, or frequency 20..20000 Hz with nonzero duration_ms")
		}
		return native.OpBuzzer, native.BuzzerPayload(frequency, step.DurationMS), nil
	case "display", "message":
		target := native.DisplayBoth
		switch strings.ToLower(strings.TrimSpace(step.Destination)) {
		case "segments", "segment":
			target = native.DisplaySegments
		case "lcd":
			target = native.DisplayLCD
		case "", "both":
		default:
			return 0, nil, fmt.Errorf("display destination %q is unknown", step.Destination)
		}
		payload, err := native.DisplayTextPayload(target, step.DurationMS, step.Text)
		return native.OpDisplayText, payload, err
	case "rf", "radio":
		payload, err := native.RFTxPayload(step.Code, step.Bits, step.Protocol, step.PulseUS)
		return native.OpRFTx, payload, err
	case "rgb", "status-led":
		return native.OpStatusRGB, native.StatusRGBPayload(step.Red, step.Green, step.Blue, step.Brightness), nil
	case "addressable", "ws2812":
		payload, err := native.AddressableLEDPayload(step.Target, step.Red, step.Green, step.Blue, step.Brightness)
		return native.OpAddressableLED, payload, err
	case "menu":
		return native.OpMenuSetPage, []byte{step.Target}, nil
	case "menu-action":
		if step.Target > native.MenuIncrease {
			return 0, nil, fmt.Errorf("menu action %d is outside 0..3", step.Target)
		}
		return native.OpMenuAction, []byte{step.Target}, nil
	case "raw", "opcode":
		payload, err := decodeMacroHex(step.PayloadHex)
		if err != nil {
			return 0, nil, err
		}
		if !native.MacroPlaybackPayloadSemanticallyValid(step.Opcode, payload) {
			return 0, nil, fmt.Errorf("opcode 0x%02X payload violates the ordinary macro action contract", step.Opcode)
		}
		return step.Opcode, payload, nil
	default:
		return 0, nil, fmt.Errorf("unknown macro step kind %q", step.Kind)
	}
}

<<<<<<< HEAD
func macroQueueableOpcode(opcode byte) bool {
	switch opcode {
	case native.OpSetStream, native.OpSetSettings,
		native.OpBuzzer, native.OpPWMSet, native.OpPWMAllOff,
		native.OpStatusRGB, native.OpStatusEffect, native.OpStatusProfileSet,
		native.OpAddressableLED, native.OpRFTx,
		native.OpRFLearnStart, native.OpRFLearnCancel, native.OpRFLearnClear,
		native.OpRFLearnRemove, native.OpRFLearnReplace,
		native.OpMenuAction, native.OpRelaySet, native.OpRelaySide,
		native.OpRelayAllOff, native.OpRelayTest, native.OpMenuSetPage,
		native.OpDisplayText, native.OpRemoteKeyGesture:
		return true
	default:
		return false
	}
}

func hostRecordableOpcode(opcode byte) bool {
	switch opcode {
	case native.OpRelaySet, native.OpRelaySide, native.OpRelayAllOff,
		native.OpPWMSet, native.OpPWMAllOff, native.OpBuzzer,
		native.OpDisplayText, native.OpRFTx, native.OpAddressableLED:
		return true
	default:
		return false
	}
}

func normalizedMacroMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), macroModeHost) {
		return macroModeHost
	}
	return macroModeMCU
}

func modeTimingTolerance(mode string) uint32 {
	if normalizedMacroMode(mode) == macroModeHost {
		return defaultHostMacroToleranceUS
	}
	return defaultMacroTimingToleranceUS
}

=======
>>>>>>> origin/agent/webui-defects
func decodeMacroHex(value string) ([]byte, error) {
	value = strings.NewReplacer(" ", "", "\t", "", ":", "", "-", "").Replace(strings.TrimSpace(value))
	payload, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode payload_hex: %w", err)
	}
	if len(payload) > native.MaxPayload {
		return nil, native.ErrPayloadTooLong
	}
	return payload, nil
}

func recordedMacroStep(evidence ActionEvidence) (appconfig.MacroStep, bool) {
	payload := evidence.Payload
	step := appconfig.MacroStep{}
	if evidence.BoardOrigin {
		required, recordable := native.MacroBoardActionPayloadLength(evidence.Opcode)
		if !recordable || len(payload) != int(required) {
			return step, false
		}
	} else if !native.MacroPlaybackPayloadSemanticallyValid(evidence.Opcode, payload) {
		return step, false
	}
	switch evidence.Opcode {
	case native.OpRelaySet:
		if len(payload) != 2 {
			return step, false
		}
		step.Kind, step.Target, step.Value = "relay", payload[0], uint16(payload[1])
	case native.OpRelaySide:
		if len(payload) != 2 {
			return step, false
		}
		step.Kind, step.Target, step.Value = "motion", payload[0], uint16(payload[1])
	case native.OpRelayAllOff:
		step.Kind = "relays-off"
	case native.OpPWMSet:
		if len(payload) != 3 {
			return step, false
		}
		step.Kind, step.Target = "pwm", payload[0]
		step.Value = binary.LittleEndian.Uint16(payload[1:3])
	case native.OpPWMAllOff:
		step.Kind = "pwm-off"
	case native.OpBuzzer:
		if len(payload) != 4 {
			return step, false
		}
		step.Kind = "beep"
		step.FrequencyHz = binary.LittleEndian.Uint16(payload[0:2])
		step.DurationMS = binary.LittleEndian.Uint16(payload[2:4])
	case native.OpDisplayText:
<<<<<<< HEAD
		if len(payload) >= 8 && payload[0] == native.DisplayScheduledSegments && int(payload[3])+8 == len(payload) {
			// Preserve the complete scheduling contract (scroll/repeat/hold), not
			// a lossy conversion to the legacy four-byte display header.
			step.Kind, step.Opcode = "opcode", native.OpDisplayText
			step.PayloadHex = strings.ToUpper(hex.EncodeToString(payload))
			step.Text = string(payload[8:])
			return step, true
		}
		if len(payload) < 4 || int(payload[3])+4 != len(payload) || payload[0] > native.DisplayBoth {
			return step, false
=======
		if payload[0] > native.DisplayBoth {
			step.Kind = "raw"
			step.Opcode = evidence.Opcode
			step.PayloadHex = strings.ToUpper(hex.EncodeToString(payload))
			break
>>>>>>> origin/agent/webui-defects
		}
		step.Kind = "display"
		step.DurationMS = binary.LittleEndian.Uint16(payload[1:3])
		step.Text = string(payload[4:])
		step.Destination = map[byte]string{
			native.DisplaySegments: "segments", native.DisplayLCD: "lcd", native.DisplayBoth: "both",
		}[payload[0]]
	case native.OpStatusEffect:
		// MacroStep intentionally stays schema-light. Preserve the already ACKed
		// ordinary descriptor losslessly instead of duplicating every effect
		// field in a second macro-only model.
		step.Kind = "raw"
		step.Opcode = evidence.Opcode
		step.PayloadHex = strings.ToUpper(hex.EncodeToString(payload))
	case native.OpRFTx:
		if len(payload) != 8 {
			return step, false
		}
		step.Kind = "rf"
		step.Code = binary.LittleEndian.Uint32(payload[0:4])
		step.Bits, step.Protocol = payload[4], payload[5]
		step.PulseUS = binary.LittleEndian.Uint16(payload[6:8])
	case native.OpStatusRGB:
		if len(payload) != 4 {
			return step, false
		}
		step.Kind = "status-led"
		step.Red, step.Green, step.Blue, step.Brightness = payload[0], payload[1], payload[2], payload[3]
	case native.OpAddressableLED:
		if len(payload) != 5 {
			return step, false
		}
		step.Kind, step.Target = "addressable", payload[0]
		step.Red, step.Green, step.Blue, step.Brightness = payload[1], payload[2], payload[3], payload[4]
	case native.OpMenuSetPage:
		if len(payload) != 1 {
			return step, false
		}
		step.Kind, step.Target = "menu", payload[0]
	case native.OpMenuAction:
		if len(payload) != 1 {
			return step, false
		}
		step.Kind, step.Target = "menu-action", payload[0]
	default:
		step.Kind = "raw"
		step.Opcode = evidence.Opcode
		step.PayloadHex = strings.ToUpper(hex.EncodeToString(payload))
	}
	return step, true
}

func macroNeedsMotionPermission(macro appconfig.Macro) bool {
	for _, step := range macro.Steps {
		opcode, payload, err := compileMacroCommand(step)
		if err != nil {
			continue
		} // compilation reports validation failures first
		if (opcode == native.OpRelaySet && len(payload) == 2 && payload[0] < 4 && payload[1] != 0) ||
			(opcode == native.OpRelaySide && len(payload) == 2 && payload[1] != 0) ||
			opcode == native.OpRelayTest || opcode == native.OpRemoteKeyGesture {
			return true
		}
	}
	return false
}

func normalizedMacroColor(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "purple" {
		return "violet"
	}
	return value
}

func validMacroColor(value string) bool {
	switch normalizedMacroColor(value) {
	case "", "red", "blue", "violet", "green", "white":
		return true
	default:
		return false
	}
}

func macroTerminal(state byte) bool {
	return state == native.MacroCancelled || state == native.MacroCompleted || state == native.MacroFailed
}
