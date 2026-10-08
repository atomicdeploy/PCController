package control

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const (
	defaultMacroTimingToleranceUS uint32 = 2500
	defaultHostMacroToleranceUS   uint32 = 100000
	macroStatusPollInterval              = 100 * time.Millisecond
	macroRequestTimeout                  = 2 * time.Second
	macroModeHost                        = "host"
	macroModeMCU                         = "mcu"
	macroModeAuto                        = "auto"
)

// MacroState is the host-authoritative view shared by TUI, CLI, API, IPC, and
// bridge clients. Mode states which clock and executor own playback.
type MacroState struct {
	Running              bool               `json:"running"`
	ID                   byte               `json:"id"`
	Name                 string             `json:"name"`
	Mode                 string             `json:"mode"`
	Policy               string             `json:"policy"`
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

// macroStreamLease remembers the exact connected board and stream cadence
// that MCU-timed playback temporarily suspends. A replacement board must never
// receive restoration intended for the session that granted this lease.
type macroStreamLease struct {
	Generation uint64
	Board      string
	PeriodMS   uint16
	restored   bool
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

	recordMu            sync.RWMutex
	recording           MacroRecordingState
	recordMacro         appconfig.Macro
	recordBaseUS        uint32
	recordBaseAt        time.Time
	recordHasBase       bool
	recordRelease       func()
	recordRelayMask     byte
	recordRelaySeen     bool
	recordRelayOriginUS uint32
	recordRelayOriginAt uint32
	recordRelayClock    bool
	recordSeed          *appconfig.Macro
	recordOffsetUS      uint32
	recordPrefix        []appconfig.MacroStep

	// requestGeneration is a focused protocol-order test seam. Production uses
	// Runtime.requestAtGeneration so every macro request remains pinned to the
	// authenticated connection observed at playback start.
	requestGeneration func(context.Context, uint64, byte, []byte, byte) (native.Frame, error)
}

// MacroRecordingState describes one transient take that will be saved into the
// PCController effect catalog. Mode describes the capture clock; DeviceRetained
// means the bounded relay tail currently also resides in device RAM.
type MacroRecordingState struct {
	DeviceRetained   bool   `json:"device_retained"`
	LastAtUS         uint32 `json:"last_at_us"`
	LastDeltaUS      uint32 `json:"last_delta_us"`
	Overwritten      int    `json:"overwritten"`
	Active           bool   `json:"active"`
	ID               byte   `json:"id"`
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	Category         string `json:"category,omitempty"`
	Color            string `json:"color,omitempty"`
	BoardProfileKey  string `json:"board_profile_key,omitempty"`
	BoardProfileMode string `json:"board_profile_mode,omitempty"`
	Steps            int    `json:"steps"`
	// Preview is the live, host-observed sequence accumulated for the current
	// take. It lets Pealayer and other coordinator clients render captured
	// actions before Finish is pressed. Board-retained capture remains
	// authoritative on the MCU; its host mirror is replaced by the downloaded
	// ring when the take is saved.
	Preview   []appconfig.MacroStep `json:"preview,omitempty"`
	StartedAt time.Time             `json:"started_at,omitempty"`
	LastError string                `json:"last_error,omitempty"`
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
	return &MacroRunner{
		runtime: runtime, library: library, hostConfig: hostConfig,
		updateHostConfig: updater,
	}
}

func (runner *MacroRunner) activeBoardProfile() (string, string) {
	if runner.hostConfig == nil {
		return "", ""
	}
	config := runner.hostConfig()
	port := runner.runtime.Snapshot().Port
	serialNumber, instanceID, portName := port.SerialNumber, port.InstanceID, port.Name
	if strings.TrimSpace(serialNumber) == "" && strings.TrimSpace(instanceID) == "" && strings.TrimSpace(portName) == "" && config.Connection.LastDevice != nil {
		serialNumber = config.Connection.LastDevice.SerialNumber
		instanceID = config.Connection.LastDevice.InstanceID
		portName = config.Connection.LastDevice.Port
	}
	identity := appconfig.ResolveBoardIdentity(serialNumber, instanceID, portName)
	profile, exists := config.BoardProfiles[identity.Value]
	if !exists {
		return "", ""
	}
	mode := appconfig.NormalizeBoardMode(profile.Mode)
	if mode == appconfig.BoardModeUnconfigured {
		return "", ""
	}
	return profile.Key, mode
}

func (runner *MacroRunner) List() []appconfig.Macro {
	source := runner.library()
	result := make([]appconfig.Macro, len(source))
	for index, macro := range source {
		result[index] = macro
		result[index].Steps = append([]appconfig.MacroStep(nil), macro.Steps...)
		for stepIndex := range result[index].Steps {
			result[index].Steps[stepIndex].ActionIDs = append([]string(nil), macro.Steps[stepIndex].ActionIDs...)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func cloneMacroSteps(source []appconfig.MacroStep) []appconfig.MacroStep {
	result := append([]appconfig.MacroStep(nil), source...)
	for index := range result {
		result[index].ActionIDs = append([]string(nil), source[index].ActionIDs...)
	}
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
	state := runner.recording
	state.Preview = cloneMacroSteps(runner.recordMacro.Steps)
	return state
}

func (runner *MacroRunner) Snapshot() MacroSnapshot {
	return MacroSnapshot{Library: runner.List(), Playback: runner.State(), Recording: runner.RecordingState()}
}

// EffectCatalog returns the one PCController-owned library exposed to every
// interface. Recorded sequences and rendered strip streams retain their
// distinct engines, but share discovery, stable references, and commands.
func (runner *MacroRunner) EffectCatalog() []EffectDescriptor {
	config := appconfig.Defaults()
	if runner.hostConfig != nil {
		config = runner.hostConfig()
	}
	return EffectCatalogWithGroups(runner.List(), config.StripEffects, config.EffectGroups)
}

func (runner *MacroRunner) EffectGroups() []EffectGroupDescriptor {
	config := appconfig.Defaults()
	if runner.hostConfig != nil {
		config = runner.hostConfig()
	}
	config.Macros = runner.List()
	return EffectGroupCatalog(config)
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
		Mode: macroModeAuto, Color: normalizedMacroColor(color),
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
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "created", Text: fmt.Sprintf("macro %d/%s created", macro.ID, macro.Name)})
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
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "deleted", Text: fmt.Sprintf("macro %d/%s deleted", macro.ID, macro.Name)})
	}
	return err
}

func (runner *MacroRunner) StartRecording(name, category, color string) (MacroRecordingState, error) {
	return runner.startRecording(name, category, color, macroModeAuto)
}

// StartMCURecording retains the exact acknowledgement-timestamp recorder for
// users who explicitly want the stricter firmware-oriented workflow.
func (runner *MacroRunner) StartMCURecording(name, category, color string) (MacroRecordingState, error) {
	return runner.startRecording(name, category, color, macroModeMCU)
}

func (runner *MacroRunner) startRecording(name, category, color, mode string, seeds ...appconfig.Macro) (MacroRecordingState, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	if runner.runtime.emergencyStop.Load() {
		return MacroRecordingState{}, ErrEmergencyStopActive
	}
	if runner.State().Running {
		return MacroRecordingState{}, errors.New("cancel playback before starting a recording")
	}
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
	used := make(map[byte]bool)
	for _, macro := range runner.List() {
		if strings.EqualFold(macro.Name, name) && (len(seeds) == 0 || macro.ID != seeds[0].ID) {
			return MacroRecordingState{}, fmt.Errorf("macro %q already exists", name)
		}
		used[macro.ID] = true
	}
	id := byte(0)
	for used[id] && id != 0xFF {
		id++
	}
	if used[id] {
		if len(seeds) == 0 {
			return MacroRecordingState{}, errors.New("all macro IDs are in use")
		}
	}
	var offset uint32
	if len(seeds) > 0 {
		id = seeds[0].ID
		var err error
		offset, err = recordingSequenceEnd(seeds[0].Steps)
		if err != nil {
			return MacroRecordingState{}, err
		}
	}

	runner.recordMu.Lock()
	if runner.recording.Active {
		state := runner.recording
		runner.recordMu.Unlock()
		return state, fmt.Errorf("macro recording %q is already active", state.Name)
	}
	runner.recordMu.Unlock()
	runner.runtime.beginMacroTimingWindow(activeUseMacroRecording)
	profileKey, profileMode := runner.activeBoardProfile()
	runner.recordMu.Lock()
	runner.recordMacro = appconfig.Macro{
		ID: id, Name: name, Category: strings.TrimSpace(category), Color: color,
		Mode: mode, TimingToleranceUS: modeTimingTolerance(mode),
		BoardProfileKey: profileKey, BoardProfileMode: profileMode,
	}
	runner.recordSeed = nil
	runner.recordOffsetUS = offset
	runner.recordPrefix = nil
	if len(seeds) > 0 {
		seed := seeds[0]
		seed.Steps = cloneMacroSteps(seed.Steps)
		runner.recordSeed = &seed
		runner.recordMacro = seed
		runner.recordMacro.Steps = cloneMacroSteps(seed.Steps)
		runner.recordPrefix = cloneMacroSteps(seed.Steps)
	}
	runner.recordBaseUS = 0
	runner.recordBaseAt = time.Time{}
	runner.recordHasBase = false
	runner.recordRelayClock = false
	runner.recordRelaySeen = false
	runner.recording = MacroRecordingState{
		Active: true, ID: id, Name: name, Mode: mode, Category: strings.TrimSpace(category),
		Color: color, BoardProfileKey: profileKey, BoardProfileMode: profileMode, StartedAt: time.Now(),
	}
	runner.recording.Steps = len(runner.recordMacro.Steps)
	runner.recordRelease = runner.runtime.ObserveCommands(runner.captureCommand)
	state := runner.recording
	runner.recordMu.Unlock()
	runner.runtime.setActiveUseState(activeUseMacroRecording, true)
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "started", State: "recording",
		Text:     fmt.Sprintf("macro recording %d/%s started in %s mode", id, name, mode),
		Metadata: map[string]string{"macro_mode": mode},
	})
	return state, nil
}

// StartAppendingRecording keeps one catalog identity and its existing steps.
// The capture clock is independent of the effect's playback execution policy.
func (runner *MacroRunner) StartAppendingRecording(ctx context.Context, reference, captureMode string) (MacroRecordingState, error) {
	macro, err := runner.find(strings.TrimPrefix(reference, "effect:"))
	if err != nil {
		return MacroRecordingState{}, err
	}
	mode := macroModeAuto
	if captureMode == "device-clock" || captureMode == "board-retained" {
		mode = macroModeMCU
	} else if captureMode != "automatic" && captureMode != "" {
		return MacroRecordingState{}, errors.New("invalid capture mode")
	}
	if captureMode == "board-retained" && !runner.runtime.Snapshot().Connected {
		return MacroRecordingState{}, errors.New("device is not connected")
	}
	state, err := runner.startRecording(macro.Name, macro.Category, macro.Color, mode, macro)
	if err != nil || captureMode != "board-retained" {
		return state, err
	}
	runner.recordMu.Lock()
	runner.recording.DeviceRetained = true
	runner.recordMu.Unlock()
	_, err = runner.request(ctx, native.OpMacroStep, []byte{3, state.ID}, native.OpACK)
	if err != nil {
		runner.recordMu.Lock()
		runner.recording.DeviceRetained = false
		runner.recordMu.Unlock()
		_, _ = runner.StopRecording(false)
		return MacroRecordingState{}, err
	}
	return runner.RecordingState(), nil
}

func recordingSequenceEnd(steps []appconfig.MacroStep) (uint32, error) {
	expanded, err := expandMacroTimeline(steps)
	if err != nil {
		return 0, err
	}
	var end uint64
	for _, step := range expanded {
		at := uint64(step.AtUS) + uint64(step.DurationMS)*1000
		if at > end {
			end = at
		}
	}
	// A constant range/color cue can have a authored span without a release
	// command. Its span still determines where a following take begins.
	for _, step := range steps {
		repeats := max(uint64(step.RepeatCount), uint64(1))
		interval := uint64(step.RepeatIntervalMS)
		if repeats > 1 && interval == 0 {
			interval = max(uint64(step.DurationMS), uint64(1))
		}
		end = max(end, uint64(step.AtUS)+(repeats-1)*interval*1000+uint64(step.DurationMS)*1000)
	}
	if end > 0x7fffffff {
		return 0, errors.New("sequence end exceeds recording timing window")
	}
	return uint32(end), nil
}

func (runner *MacroRunner) StopRecording(save bool) (appconfig.Macro, error) {
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	if runner.RecordingState().DeviceRetained && runner.RecordingState().Active {
		if err := runner.collectBoardRecording(context.Background(), save); err != nil {
			return appconfig.Macro{}, err
		}
	}
	runner.recordMu.Lock()
	if !runner.recording.Active {
		runner.recordMu.Unlock()
		return appconfig.Macro{}, errors.New("no macro recording is active")
	}
	if runner.recordRelease != nil {
		runner.recordRelease()
		runner.recordRelease = nil
	}
	macro := runner.recordMacro
	macro.Steps = append([]appconfig.MacroStep(nil), macro.Steps...)
	for stepIndex := range macro.Steps {
		macro.Steps[stepIndex].ActionIDs = append([]string(nil), macro.Steps[stepIndex].ActionIDs...)
	}
	sort.SliceStable(macro.Steps, func(i, j int) bool { return macro.Steps[i].AtUS < macro.Steps[j].AtUS })
	if save && len(macro.Steps) == 0 {
		// Keep an empty recording active: Save must never destroy the take.
		runner.recordRelease = runner.runtime.ObserveCommands(runner.captureCommand)
		runner.recordMu.Unlock()
		return macro, errors.New("recording is empty; run at least one board command or discard it")
	}
	runner.recording.Active = false
	runner.recording.Steps = len(macro.Steps)
	runner.recordMu.Unlock()
	runner.runtime.setActiveUseState(activeUseMacroRecording, false)

	if save {
		if err := runner.updateHostConfig(func(config *appconfig.Config) error {
			for index, existing := range config.Macros {
				if runner.recordSeed != nil && existing.ID == macro.ID {
					before, _ := json.Marshal(runner.recordSeed)
					current, _ := json.Marshal(existing)
					if string(before) != string(current) {
						return errors.New("effect changed during capture; recording retained, concurrent edits were not overwritten")
					}
					config.Macros[index] = macro
					return nil
				}
				if existing.ID == macro.ID || strings.EqualFold(existing.Name, macro.Name) {
					return fmt.Errorf("macro ID %d or name %q already exists", macro.ID, macro.Name)
				}
			}
			if runner.recordSeed != nil {
				return errors.New("effect was removed during capture; recording retained")
			}
			config.Macros = append(config.Macros, macro)
			return nil
		}); err != nil {
			runner.recordMu.Lock()
			runner.recording.Active = true
			runner.recording.LastError = "save failed; recording retained: " + err.Error()
			runner.recordRelease = runner.runtime.ObserveCommands(runner.captureCommand)
			runner.recordMu.Unlock()
			runner.runtime.setActiveUseState(activeUseMacroRecording, true)
			return macro, err
		}
	}
	runner.recordMu.Lock()
	runner.recording.LastError = ""
	runner.recordMu.Unlock()
	lifecycle := map[bool]string{true: "saved", false: "discarded"}[save]
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: lifecycle, State: lifecycle,
		Text:     fmt.Sprintf("macro recording %d/%s %s with %d %s-timed steps", macro.ID, macro.Name, lifecycle, len(macro.Steps), macro.Mode),
		Metadata: map[string]string{"macro_mode": macro.Mode},
	})
	return macro, nil
}

func (runner *MacroRunner) captureCommand(evidence CommandEvidence) {
	runner.recordMu.Lock()
	defer runner.recordMu.Unlock()
	if !runner.recording.Active || evidence.Source == CommandSourceBackground {
		return
	}
	if evidence.RelayEdge {
		runner.captureRelayEdge(evidence)
		return
	}
	if runner.recording.DeviceRetained {
		return
	}
	// Relay commands are intentions, not output edges. Record the timestamped
	// applied mask instead so PC, RF and physical controls share one path.
	if evidence.Opcode == native.OpRelaySet || evidence.Opcode == native.OpRelaySide || evidence.Opcode == native.OpRelayAllOff {
		return
	}
	if len(runner.recordMacro.Steps) >= 65535 {
		runner.recording.LastError = "recording reached the 65535 step limit; save it before continuing"
		return
	}
	mode := runner.recording.Mode
	if mode != macroModeMCU {
		if !hostRecordableOpcode(evidence.Opcode) {
			return
		}
	} else if !evidence.Timed || !macroQueueableOpcode(evidence.Opcode) {
		return
	}
	step, ok := recordedMacroStep(evidence)
	if !ok {
		return
	}
	if mode != macroModeMCU {
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
		if uint64(delta/time.Microsecond)+uint64(runner.recordOffsetUS) > 0x7fffffff {
			runner.recording.LastError = "appended recording timing overflow"
			return
		}
		step.AtUS = uint32(delta/time.Microsecond) + runner.recordOffsetUS
		runner.recordMacro.Steps = append(runner.recordMacro.Steps, step)
		runner.recording.Steps = len(runner.recordMacro.Steps)
		runner.publishRecordedStep(step)
		return
	}
	if !runner.recordHasBase {
		runner.recordBaseUS = evidence.DeviceMicros
		runner.recordHasBase = true
	}
	delta := evidence.DeviceMicros - runner.recordBaseUS
	if delta > 0x7FFFFFFF {
		runner.recording.LastError = "recording exceeded the MCU signed timing window"
		return
	}
	if uint64(delta)+uint64(runner.recordOffsetUS) > 0x7fffffff {
		runner.recording.LastError = "appended recording timing overflow"
		return
	}
	step.AtUS = delta + runner.recordOffsetUS
	runner.recordMacro.Steps = append(runner.recordMacro.Steps, step)
	runner.recording.Steps = len(runner.recordMacro.Steps)
	runner.publishRecordedStep(step)
}

func (runner *MacroRunner) publishRecordedStep(step appconfig.MacroStep) {
	runner.recording.LastDeltaUS = step.AtUS - runner.recording.LastAtUS
	runner.recording.LastAtUS = step.AtUS
	// PublishStructuredEvent queues delivery without invoking snapshot readers;
	// the recorder lock preserves order between concurrent acknowledged commands.
	runner.runtime.PublishStructuredEvent(Event{
		Kind: "macro.recording", Lifecycle: "captured", State: "recording",
		Text:     fmt.Sprintf("macro %d/%s recorded step %d (%s)", runner.recording.ID, runner.recording.Name, runner.recording.Steps, step.Kind),
		Metadata: map[string]string{"macro_mode": runner.recording.Mode, "steps": strconv.Itoa(runner.recording.Steps)},
	})
}

// Start validates an effect sequence and resolves its persisted execution
// policy. The definition stays in the PCController library; only a volatile
// run plan is staged into host or board RAM.
func (runner *MacroRunner) Start(ctx context.Context, reference string) (MacroState, error) {
	return runner.StartMode(ctx, reference, "")
}

// StartMode plays the same saved definition with an optional execution-policy
// override. "auto" is capability driven and remains transparent to callers.
func (runner *MacroRunner) StartMode(ctx context.Context, reference, modeOverride string) (MacroState, error) {
	if runner.runtime.MediaTimeline().State == "playing" {
		return MacroState{}, errors.New("pause media-bound effects before starting standalone playback")
	}
	runner.operationMu.Lock()
	defer runner.operationMu.Unlock()
	if runner.runtime.emergencyStop.Load() {
		return MacroState{}, ErrEmergencyStopActive
	}
	if recording := runner.RecordingState(); recording.Active {
		return MacroState{}, fmt.Errorf("macro recording %d/%s is active; save or discard it before playback", recording.ID, recording.Name)
	}

	macro, err := runner.find(reference)
	if err != nil {
		return MacroState{}, err
	}
	if modeOverride != "" {
		if modeOverride != macroModeAuto && modeOverride != macroModeHost && modeOverride != macroModeMCU {
			return MacroState{}, errors.New("playback policy must be auto, host, or mcu")
		}
		macro.Mode = modeOverride
		macro.TimingToleranceUS = modeTimingTolerance(modeOverride)
	}
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return MacroState{}, errors.New("device is not connected")
	}
	policy := macro.Mode
	if policy == "" {
		policy = macroModeAuto
	}
	if policy == macroModeAuto {
		macro.Mode = resolveMacroMode(macro, snapshot.Hello.Capabilities)
	}
	compiled, err := compileMacro(macro)
	if err != nil {
		return MacroState{}, err
	}
	if macro.BoardProfileKey != "" {
		profileKey, profileMode := runner.activeBoardProfile()
		if profileKey != macro.BoardProfileKey || profileMode != macro.BoardProfileMode {
			return MacroState{}, fmt.Errorf(
				"macro %q is bound to board profile %s/%s; attached profile is %s/%s",
				macro.Name, macro.BoardProfileKey, macro.BoardProfileMode, profileKey, profileMode,
			)
		}
	}
	mode := macro.Mode
	if mode == macroModeMCU && snapshot.Hello.Capabilities&native.CapabilityTimedMacroQueue == 0 {
		return MacroState{}, errors.New("connected firmware does not advertise the MCU-timed macro queue")
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
		Running: true, ID: macro.ID, Name: macro.Name,
		Mode: mode, Policy: policy,
		Category: macro.Category, Color: normalizedMacroColor(macro.Color),
		StepCount: len(compiled.steps), DurationUS: compiled.durationUS,
		StartedAt: time.Now(), TimingToleranceUS: tolerance,
		Lifecycle: "buffering",
	}
	runner.mu.Unlock()
	runner.runtime.beginMacroTimingWindow(activeUseMacroPlayback)

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
			if cleanupErr := runner.cancelBoardAtGeneration(snapshot.ConnectionGeneration, false); cleanupErr != nil {
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
		if err := runner.showMacroIdentity(ctx, snapshot.ConnectionGeneration, compiled); err != nil {
			runner.runtime.PublishHostEvent("macro.display", "macro identity display unavailable: "+err.Error())
		}
		playContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
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
	if _, err = runner.requestAtGeneration(ctx, snapshot.ConnectionGeneration, native.OpMacroStart, startPayload, native.OpACK); err != nil {
		return fail(err)
	}
	begun = true
	afterID := runner.runtime.LatestEventID()
	sent, err := runner.appendBytes(ctx, snapshot.ConnectionGeneration, compiled, 0, native.MacroQueueCapacity)
	if err != nil {
		return fail(err)
	}
	if err := runner.showMacroIdentity(ctx, snapshot.ConnectionGeneration, compiled); err != nil {
		runner.runtime.PublishHostEvent("macro.display", "macro identity display unavailable: "+err.Error())
	}
	streamLease, err = runner.pauseMacroStream(ctx, snapshot)
	if err != nil {
		return fail(fmt.Errorf("pause periodic telemetry for exact macro timing: %w", err))
	}
	if _, err = runner.requestAtGeneration(ctx, snapshot.ConnectionGeneration, native.OpMacroStep, native.MacroQueueRunPayload(), native.OpACK); err != nil {
		return fail(err)
	}

	playContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	runner.mu.Lock()
	runner.cancel = cancel
	runner.cancelKeep = false
	runner.done = done
	runner.state.Lifecycle = "playing"
	state := runner.state
	runner.mu.Unlock()
	runner.publishLifecycle("started", state, nil)
	go runner.play(playContext, done, snapshot.ConnectionGeneration, compiled, sent, afterID, lease, streamLease)
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

	status, err := runner.queryBoardAtGeneration(ctx, generation)
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
			if status.State == native.MacroCompleted {
				continue
			}
			var reported native.MacroStatus
			reported, err = runner.queryBoardAtGeneration(ctx, generation)
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
		consume(event)
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

	var evidenceError *macroTimingEvidenceError
	// A completed queue has already executed its intended final output state.
	// Missing host timestamps are a proof failure, not unfinished execution;
	// do not issue another cancellation/output action merely to repair evidence.
	if err != nil && !cancelled && !errors.As(err, &evidenceError) {
		if cleanupErr := runner.cancelBoardAtGeneration(generation, false); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("macro safe-stop cleanup: %w", cleanupErr))
		}
	}
	if restoreErr := runner.restoreMacroStream(streamLease); restoreErr != nil {
		err = errors.Join(err, fmt.Errorf("restore telemetry stream: %w", restoreErr))
	}
	runner.finishPlayback(done, compiled.definition, status, observed, cancelled, err)
}

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
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), mediaTimelineContextKey{}, true), macroRequestTimeout)
	defer cancel()
	relayErr := command(ctx, native.OpRelayAllOff, nil)
	pwmErr := command(ctx, native.OpPWMAllOff, nil)
	return errors.Join(relayErr, pwmErr)
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
		if _, err := runner.requestAtGeneration(ctx, generation, native.OpMacroStep, payload, native.OpACK); err != nil {
			return offset, err
		}
		offset += length
		available -= length
	}
	return offset, nil
}

func (runner *MacroRunner) queryBoard(ctx context.Context) (native.MacroStatus, error) {
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return native.MacroStatus{}, errors.New("device is not connected")
	}
	return runner.queryBoardAtGeneration(ctx, snapshot.ConnectionGeneration)
}

func (runner *MacroRunner) queryBoardAtGeneration(ctx context.Context, generation uint64) (native.MacroStatus, error) {
	frame, err := runner.requestAtGeneration(
		ctx,
		generation,
		native.OpMacroStep,
		native.MacroQueueQueryPayload(),
		native.OpMacroStatus,
	)
	if err != nil {
		return native.MacroStatus{}, err
	}
	return native.ParseMacroStatus(frame.Payload)
}

// pauseMacroStream drains and disables only periodic STATUS production before
// RUN is acknowledged. Macro ACKs, completion status, and all other changed
// state continue over the ordinary event path while the lease is active.
func (runner *MacroRunner) pauseMacroStream(
	ctx context.Context,
	snapshot Snapshot,
) (*macroStreamLease, error) {
	frame, err := runner.requestAtGeneration(
		ctx, snapshot.ConnectionGeneration,
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
		Generation: snapshot.ConnectionGeneration,
		Board:      macroBoardIdentity(snapshot),
		PeriodMS:   settings.StreamPeriodMS,
	}
	if !runner.macroStreamLeaseCurrent(lease) {
		return nil, fmt.Errorf("connection generation %d changed before telemetry could be paused", snapshot.ConnectionGeneration)
	}
	payload, err := native.StreamPeriodPayload(0)
	if err != nil {
		return nil, err
	}
	// Return the lease even when the response fails: the MCU might have accepted
	// SET_STREAM before the transport failed. Same-session restoration is safe.
	if _, err = runner.requestAtGeneration(
		ctx, snapshot.ConnectionGeneration,
		native.OpSetStream, payload, native.OpACK,
	); err != nil {
		return lease, fmt.Errorf("disable periodic telemetry: %w", err)
	}
	return lease, nil
}

// restoreMacroStream is idempotent and uses a fresh bounded context so caller
// cancellation cannot strand the same connected board with telemetry disabled.
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
	_, err = runner.requestAtGeneration(
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
	return snapshot.Connected && snapshot.ConnectionGeneration == lease.Generation &&
		macroBoardIdentity(snapshot) == lease.Board
}

func macroBoardIdentity(snapshot Snapshot) string {
	device := strings.TrimSpace(snapshot.Port.SerialNumber)
	if device == "" {
		device = strings.TrimSpace(snapshot.Port.InstanceID)
	}
	if device == "" {
		device = strings.TrimSpace(snapshot.Port.Name)
	}
	return fmt.Sprintf(
		"%s|%s|%s|%s|%d|%08X|%08X|%08X",
		device, snapshot.Port.VID, snapshot.Port.PID, snapshot.Hello.Name,
		snapshot.Hello.BoardKind, snapshot.Hello.BuildHash,
		snapshot.Hello.BuildTimestamp, snapshot.Hello.Capabilities,
	)
}

func (runner *MacroRunner) request(
	ctx context.Context,
	opcode byte,
	payload []byte,
	expected byte,
) (native.Frame, error) {
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return native.Frame{}, errors.New("device is not connected")
	}
	return runner.requestAtGeneration(ctx, snapshot.ConnectionGeneration, opcode, payload, expected)
}

func (runner *MacroRunner) requestAtGeneration(
	ctx context.Context,
	generation uint64,
	opcode byte,
	payload []byte,
	expected byte,
) (native.Frame, error) {
	requestContext, cancel := context.WithTimeout(ctx, macroRequestTimeout)
	defer cancel()
	if runner.requestGeneration != nil {
		return runner.requestGeneration(requestContext, generation, opcode, payload, expected)
	}
	return runner.runtime.requestAtGeneration(requestContext, generation, opcode, payload, expected)
}

func (runner *MacroRunner) cancelBoard(keepOutputs bool) error {
	snapshot := runner.runtime.Snapshot()
	if !snapshot.Connected {
		return errors.New("device is not connected")
	}
	return runner.cancelBoardAtGeneration(snapshot.ConnectionGeneration, keepOutputs)
}

func (runner *MacroRunner) cancelBoardAtGeneration(generation uint64, keepOutputs bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), macroRequestTimeout)
	defer cancel()
	_, err := runner.requestAtGeneration(
		ctx, generation,
		native.OpMacroCancel,
		native.MacroQueueCancelPayload(keepOutputs),
		native.OpACK,
	)
	return err
}

func (runner *MacroRunner) showMacroIdentity(ctx context.Context, generation uint64, compiled compiledMacro) error {
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
		if _, err := runner.requestAtGeneration(ctx, generation, native.OpDisplayText, payload, native.OpACK); err != nil {
			return err
		}
	}
	if macro.LCDMessage != "" {
		payload, err := native.DisplayTextPayload(native.DisplayLCD, uint16(durationMS), macro.LCDMessage)
		if err != nil {
			return err
		}
		if _, err := runner.requestAtGeneration(ctx, generation, native.OpDisplayText, payload, native.OpACK); err != nil {
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
	runner.state.BufferFill = status.Fill
	runner.state.AcceptedBytes = status.AcceptedBytes
	runner.state.Step = max(runner.state.Step, int(status.ExecutedSteps))
	runner.state.Faithful = err == nil && !cancelled &&
		status.State == native.MacroCompleted &&
		status.Underruns == 0 && status.DispatchErrors == 0 &&
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
	runner.runtime.setActiveUseState(activeUseMacroPlayback, false)
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
	runner.runtime.setActiveUseState(activeUseMacroPlayback, false)
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
	runner.runtime.setActiveUseState(activeUseMacroPlayback, false)
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

func timelineEase(name string, position float64) float64 {
	position = math.Max(0, math.Min(1, position))
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ease-in":
		return position * position
	case "ease-out":
		remaining := 1 - position
		return 1 - remaining*remaining
	case "ease-in-out":
		if position < 0.5 {
			return 2 * position * position
		}
		return 1 - math.Pow(-2*position+2, 2)/2
	default:
		return position
	}
}

func timelineLerp(start, end uint16, position float64) uint16 {
	return uint16(math.Round(float64(start) + (float64(end)-float64(start))*timelineEase("linear", position)))
}

func timelineLerpWithEasing(start, end uint16, easing string, position float64) uint16 {
	return timelineLerp(start, end, timelineEase(easing, position))
}

func clearTimelineAuthoring(step *appconfig.MacroStep) {
	step.ToValue = nil
	step.ToRed, step.ToGreen, step.ToBlue, step.ToBrightness = nil, nil, nil, nil
	step.Easing = ""
	step.SampleRateHz = 0
	step.RepeatCount = 0
	step.RepeatIntervalMS = 0
}

func timelineStepAt(step appconfig.MacroStep, offsetUS uint64) (appconfig.MacroStep, error) {
	at := uint64(step.AtUS) + offsetUS
	if at > 0x7FFFFFFF {
		return appconfig.MacroStep{}, errors.New("expanded cue timing exceeds 2147483647 us")
	}
	step.AtUS = uint32(at)
	return step, nil
}

func timelineReleaseActions(actionIDs []string, release string) []string {
	result := make([]string, 0, len(actionIDs))
	for _, actionID := range actionIDs {
		trimmed := strings.TrimSpace(actionID)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, ".")
		if len(parts) > 1 {
			parts[len(parts)-1] = release
			trimmed = strings.Join(parts, ".")
		}
		result = append(result, trimmed)
	}
	return result
}

func expandTimelineCue(source appconfig.MacroStep) ([]appconfig.MacroStep, error) {
	kind := strings.ToLower(strings.TrimSpace(source.Kind))
	durationUS := uint64(source.DurationMS) * 1000
	base := source
	clearTimelineAuthoring(&base)

	switch kind {
	case "relay":
		base.DurationMS = 0
		result := []appconfig.MacroStep{base}
		if source.Value != 0 && durationUS != 0 {
			end, err := timelineStepAt(base, durationUS)
			if err != nil {
				return nil, err
			}
			end.Value = 0
			end.ActionIDs = timelineReleaseActions(source.ActionIDs, "off")
			result = append(result, end)
		}
		return result, nil
	case "motion", "side":
		base.DurationMS = 0
		result := []appconfig.MacroStep{base}
		if source.Value != 0 && durationUS != 0 {
			end, err := timelineStepAt(base, durationUS)
			if err != nil {
				return nil, err
			}
			end.Value = 0
			end.ActionIDs = timelineReleaseActions(source.ActionIDs, "stop")
			result = append(result, end)
		}
		return result, nil
	case "pwm", "mosfet":
		base.DurationMS = 0
		if source.ToValue == nil || durationUS == 0 {
			return []appconfig.MacroStep{base}, nil
		}
		rate := uint64(source.SampleRateHz)
		if rate == 0 {
			rate = 30
		}
		samples := max((uint64(source.DurationMS)*rate+999)/1000, uint64(1))
		result := make([]appconfig.MacroStep, 0, samples+1)
		for sample := uint64(0); sample <= samples; sample++ {
			position := float64(sample) / float64(samples)
			step, err := timelineStepAt(base, durationUS*sample/samples)
			if err != nil {
				return nil, err
			}
			step.Value = timelineLerpWithEasing(source.Value, *source.ToValue, source.Easing, position)
			result = append(result, step)
		}
		return result, nil
	case "rgb", "status-led", "addressable", "ws2812":
		base.DurationMS = 0
		transition := source.ToRed != nil || source.ToGreen != nil || source.ToBlue != nil || source.ToBrightness != nil
		if !transition || durationUS == 0 {
			return []appconfig.MacroStep{base}, nil
		}
		rate := uint64(source.SampleRateHz)
		if rate == 0 {
			rate = 30
		}
		samples := max((uint64(source.DurationMS)*rate+999)/1000, uint64(1))
		endRed, endGreen := source.Red, source.Green
		endBlue, endBrightness := source.Blue, source.Brightness
		if source.ToRed != nil {
			endRed = *source.ToRed
		}
		if source.ToGreen != nil {
			endGreen = *source.ToGreen
		}
		if source.ToBlue != nil {
			endBlue = *source.ToBlue
		}
		if source.ToBrightness != nil {
			endBrightness = *source.ToBrightness
		}
		result := make([]appconfig.MacroStep, 0, samples+1)
		for sample := uint64(0); sample <= samples; sample++ {
			position := float64(sample) / float64(samples)
			step, err := timelineStepAt(base, durationUS*sample/samples)
			if err != nil {
				return nil, err
			}
			step.Red = byte(timelineLerpWithEasing(uint16(source.Red), uint16(endRed), source.Easing, position))
			step.Green = byte(timelineLerpWithEasing(uint16(source.Green), uint16(endGreen), source.Easing, position))
			step.Blue = byte(timelineLerpWithEasing(uint16(source.Blue), uint16(endBlue), source.Easing, position))
			step.Brightness = byte(timelineLerpWithEasing(uint16(source.Brightness), uint16(endBrightness), source.Easing, position))
			result = append(result, step)
		}
		return result, nil
	default:
		return []appconfig.MacroStep{base}, nil
	}
}

// expandMacroTimeline compiles authoring-friendly blocks into the exact point
// commands consumed by the existing host/MCU schedulers. The saved catalog
// retains durations, curves, and repetition; only the volatile run plan is
// expanded.
func expandMacroTimeline(steps []appconfig.MacroStep) ([]appconfig.MacroStep, error) {
	result := make([]appconfig.MacroStep, 0, len(steps))
	for index, source := range steps {
		repeats := int(source.RepeatCount)
		if repeats < 1 {
			repeats = 1
		}
		intervalMS := uint64(source.RepeatIntervalMS)
		if repeats > 1 && intervalMS == 0 {
			intervalMS = max(uint64(source.DurationMS), uint64(1))
		}
		if repeats > 1 && intervalMS < uint64(source.DurationMS) {
			return nil, fmt.Errorf("step %d repeat interval must not be shorter than its duration", index+1)
		}
		for repeat := 0; repeat < repeats; repeat++ {
			copy := source
			at := uint64(source.AtUS) + uint64(repeat)*intervalMS*1000
			if at > 0x7FFFFFFF {
				return nil, fmt.Errorf("step %d repetition exceeds maximum effect time", index+1)
			}
			copy.AtUS = uint32(at)
			expanded, err := expandTimelineCue(copy)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", index+1, err)
			}
			result = append(result, expanded...)
			if len(result) > 65535 {
				return nil, errors.New("expanded effect exceeds 65535 runtime commands")
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].AtUS < result[right].AtUS })
	return result, nil
}

func compileMacro(macro appconfig.Macro) (compiledMacro, error) {
	if macro.Mode != macroModeHost && macro.Mode != macroModeMCU {
		return compiledMacro{}, fmt.Errorf("macro %d/%s mode must be host or mcu", macro.ID, macro.Name)
	}
	if len(macro.Steps) == 0 || len(macro.Steps) > 65535 {
		return compiledMacro{}, fmt.Errorf("macro %d/%s must contain 1..65535 steps", macro.ID, macro.Name)
	}
	expandedSteps, err := expandMacroTimeline(macro.Steps)
	if err != nil {
		return compiledMacro{}, fmt.Errorf("macro %d/%s timeline: %w", macro.ID, macro.Name, err)
	}
	result := compiledMacro{definition: macro, steps: make([]compiledMacroStep, 0, len(expandedSteps))}
	var previous uint32
	for index, step := range expandedSteps {
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
		if macro.Mode == macroModeMCU && opcode == native.OpAddressableLED {
			return compiledMacro{}, errors.New("strip commands cannot share the MCU macro clock/workspace; use host playback for strip-only profiles")
		}
		record, err := native.EncodeMacroRecord(dueUS, opcode, payload)
		if err != nil {
			return compiledMacro{}, fmt.Errorf("macro %d/%s step %d: %w", macro.ID, macro.Name, index+1, err)
		}
		if macro.Mode == macroModeMCU && len(result.stream)+len(record) > 65535 {
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

// resolveMacroMode selects the device clock only when the firmware advertises
// the queue and every step can coexist with its shared AVR workspace. The
// strip framebuffer is deliberately host streamed; a sequence containing an
// addressable-pixel command therefore uses the host clock without creating a
// second effect definition.
func resolveMacroMode(macro appconfig.Macro, capabilities uint32) string {
	if capabilities&native.CapabilityTimedMacroQueue == 0 {
		return macroModeHost
	}
	for _, step := range macro.Steps {
		switch strings.ToLower(strings.TrimSpace(step.Kind)) {
		case "addressable", "ws2812":
			return macroModeHost
		}
	}
	return macroModeMCU
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
	case "relay-mask":
		if step.Value > 255 || step.Target != 0 {
			return 0, nil, errors.New("relay-mask requires target zero and value 0..255")
		}
		return native.OpRelaySet, []byte{byte(step.Value)}, nil
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
		if step.DurationMS == 0 || (frequency != 0 && (frequency < 20 || frequency > 20000)) {
			return 0, nil, errors.New("buzzer requires duration_ms and frequency 0 or 20..20000 Hz")
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
		if !macroQueueableOpcode(step.Opcode) {
			return 0, nil, fmt.Errorf("opcode 0x%02X is not a queueable acknowledged command", step.Opcode)
		}
		payload, err := decodeMacroHex(step.PayloadHex)
		return step.Opcode, payload, err
	default:
		return 0, nil, fmt.Errorf("unknown macro step kind %q", step.Kind)
	}
}

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
		native.OpDisplayText, native.OpRFTx, native.OpAddressableLED,
		native.OpStatusRGB, native.OpMenuSetPage, native.OpMenuAction:
		return true
	default:
		return false
	}
}

func modeTimingTolerance(mode string) uint32 {
	switch mode {
	case macroModeHost:
		return defaultHostMacroToleranceUS
	case macroModeMCU:
		return defaultMacroTimingToleranceUS
	case macroModeAuto:
		return 0
	default:
		return 0
	}
}

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

func recordedMacroStep(evidence CommandEvidence) (appconfig.MacroStep, bool) {
	payload := evidence.Payload
	step := appconfig.MacroStep{}
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
		}
		step.Kind = "display"
		step.DurationMS = binary.LittleEndian.Uint16(payload[1:3])
		step.Text = string(payload[4:])
		step.Destination = map[byte]string{
			native.DisplaySegments: "segments", native.DisplayLCD: "lcd", native.DisplayBoth: "both",
		}[payload[0]]
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
		if (opcode == native.OpRelaySet && len(payload) == 1 && payload[0]&0x0f != 0) ||
			(opcode == native.OpRelaySet && len(payload) == 2 && payload[0] < 4 && payload[1] != 0) ||
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
