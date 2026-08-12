package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

const (
	programmingSnapshotFormat = "pccontroller.board-settings-snapshot"
	programmingMarkerFormat   = "pccontroller.programming-recovery"

	// SettingsStore defers EEPROM.update() calls for 1500 ms. The host waits
	// beyond that window before deliberately resetting into the bootloader.
	ProgrammingSettingsPersistenceDelay = 1700 * time.Millisecond
	programmingRampSteps                = 8
	programmingRampStepDelay            = 25 * time.Millisecond
)

// ProgrammingLifecycleOptions keeps MCU EEPROM recovery artifacts under the
// host data directory; these are board backups, never PC application config.
type ProgrammingLifecycleOptions struct {
	DataPaths        programmer.HostDataPaths
	PersistenceDelay time.Duration
	Wait             func(context.Context, time.Duration) error
	Outputs          *OutputScheduler
	HostConfig       func() appconfig.Config
	// ReinitializeEEPROM is an explicit development exception for a
	// connected board whose settings payload cannot be decoded by this host.
	// It never teaches the firmware or protocol about an obsolete layout.
	ReinitializeEEPROM bool
}

type ProgrammingDeviceIdentity struct {
	Port         string `json:"port"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	FriendlyName string `json:"friendly_name,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
}

// BoardSettingsSnapshot is the queryable MCU EEPROM configuration captured
// immediately before programming. The full raw EEPROM remains in the guarded
// AVRDUDE backup; this semantic copy makes audible-state recovery deterministic.
type BoardSettingsSnapshot struct {
	Format               string                    `json:"format"`
	CapturedAt           time.Time                 `json:"captured_at"`
	Device               ProgrammingDeviceIdentity `json:"device"`
	BoardKind            byte                      `json:"board_kind"`
	ApplicationBuildHash uint32                    `json:"application_build_hash"`
	TargetFirmwareSHA256 string                    `json:"target_firmware_sha256"`
	Settings             *native.Settings          `json:"mcu_eeprom_settings,omitempty"`
	SettingsPayloadHex   string                    `json:"settings_payload_hex,omitempty"`
	SettingsQueryError   string                    `json:"settings_query_error,omitempty"`
	LiveState            *ProgrammingLiveState     `json:"live_state,omitempty"`
	LiveStateQueryError  string                    `json:"live_state_query_error,omitempty"`
}

// ProgrammingLiveState is the queryable output/application state captured
// before guarded programming changes anything on the MCU or host output lanes.
type ProgrammingLiveState struct {
	RelayMask          byte                 `json:"relay_mask"`
	PWM                *native.PWMValues    `json:"pwm,omitempty"`
	Macro              *native.MacroStatus  `json:"macro,omitempty"`
	MacroQueryError    string               `json:"macro_query_error,omitempty"`
	FrontPanel         *native.FrontPanel   `json:"front_panel,omitempty"`
	ProgramState       ProgramStateSnapshot `json:"program_state"`
	HostOutputs        OutputStreamState    `json:"host_outputs"`
	NonRestorableState []string             `json:"non_restorable_state,omitempty"`
}

// ProgrammingSession points at one durable board-settings snapshot and one
// recovery marker. The marker is removed only after reconnect, restore, the
// 1.5-second EEPROM coalescing window, and a read-back comparison all succeed.
type ProgrammingSession struct {
	Format               string                    `json:"format"`
	PreparedAt           time.Time                 `json:"prepared_at"`
	Device               ProgrammingDeviceIdentity `json:"device"`
	TargetFirmwareSHA256 string                    `json:"target_firmware_sha256"`
	SettingsSnapshotPath string                    `json:"settings_snapshot_path"`
	RecoveryMarkerPath   string                    `json:"-"`
	OriginalSettings     *native.Settings          `json:"original_mcu_eeprom_settings,omitempty"`
	OriginalLiveState    *ProgrammingLiveState     `json:"original_live_state,omitempty"`
	// ReinitializeEEPROM deliberately discards incompatible semantic settings
	// after the mandatory raw EEPROM backup. This is tooling-only development
	// state and is not a firmware migration or compatibility mechanism.
	ReinitializeEEPROM bool             `json:"reinitialize_eeprom,omitempty"`
	SettingsQueryError string           `json:"settings_query_error,omitempty"`
	PostFlashSettings  *native.Settings `json:"post_flash_mcu_eeprom_settings,omitempty"`
	DisplayPrepared    bool             `json:"display_prepared"`
	TemporarySilent    bool             `json:"temporary_silent"`
	SafeStateApplied   bool             `json:"safe_state_applied"`
	Phase              string           `json:"phase"`
	HostResult         string           `json:"host_result,omitempty"`
	Warnings           []string         `json:"warnings,omitempty"`
}

// ProgrammingRecoveryDiagnostic is a path-free, privacy-safe projection of a
// pending lifecycle marker for graceful host snapshots and recovery tooling.
// HostResult is advisory: only the normal lifecycle restore/readback may remove
// the marker or prove that the board returned to its requested state.
type ProgrammingRecoveryDiagnostic struct {
	MarkerSHA256           string
	TargetFirmwareSHA256   string
	SettingsSnapshotSHA256 string
	DeviceFingerprint      string
	PreparedAt             time.Time
	Phase                  string
	HostResult             string
	DiagnosticState        string
	WarningCount           int
	RestorationPending     bool
}

type programmingDevice interface {
	Snapshot() Snapshot
	QuerySettings(context.Context) (native.Settings, error)
	CaptureLiveState(context.Context) (ProgrammingLiveState, error)
	StoreSettings(context.Context, native.Settings) error
	CancelMacro(context.Context) error
	ReleaseAllRelays(context.Context) error
	RampPWMToZero(context.Context, ProgrammingLiveState) error
	SetProgrammingCue(context.Context) error
	PlayProgrammingMelody(context.Context, appconfig.Melody) error
	RestoreLiveState(context.Context, ProgrammingLiveState) ([]string, error)
	PublishProgrammingPhase(string)
	EnterSafeProgrammingState(context.Context) error
	ShowProgrammingPanel(context.Context) error
	ReleaseProgrammingPanel(context.Context) error
	RenderProgrammingLCD(context.Context, string, string) error
}

type runtimeProgrammingDevice struct {
	runtime *Runtime
	options ProgrammingLifecycleOptions
}

func (device runtimeProgrammingDevice) Snapshot() Snapshot { return device.runtime.Snapshot() }

func (device runtimeProgrammingDevice) QuerySettings(ctx context.Context) (native.Settings, error) {
	return querySettings(ctx, device.runtime)
}

func (device runtimeProgrammingDevice) CaptureLiveState(
	ctx context.Context,
) (ProgrammingLiveState, error) {
	live := device.Snapshot()
	state := ProgrammingLiveState{
		RelayMask:    live.Status.ActiveRelays,
		ProgramState: live.ProgramState,
	}
	if device.options.Outputs != nil {
		state.HostOutputs = device.options.Outputs.State()
	}
	pwmFrame, err := request(ctx, device.runtime, native.OpPWMGet, nil, native.OpPWMValues)
	if err != nil {
		return state, fmt.Errorf("capture all PWM channels before programming: %w", err)
	}
	pwm, err := native.ParsePWMValues(pwmFrame.Payload)
	if err != nil {
		return state, fmt.Errorf("decode all PWM channels before programming: %w", err)
	}
	state.PWM = &pwm

	if live.Hello.Capabilities&native.CapabilityTimedMacroQueue != 0 {
		macroFrame, queryErr := request(
			ctx, device.runtime, native.OpMacroStep,
			native.MacroQueueQueryPayload(), native.OpMacroStatus,
		)
		if queryErr != nil {
			state.MacroQueryError = queryErr.Error()
		} else if macroStatus, parseErr := native.ParseMacroStatus(macroFrame.Payload); parseErr != nil {
			state.MacroQueryError = parseErr.Error()
		} else {
			state.Macro = &macroStatus
			if macroStatus.Active() {
				state.NonRestorableState = append(
					state.NonRestorableState,
					"active macro playback position is canceled and cannot be resumed exactly",
				)
			}
		}
	}
	if live.HaveFrontPanel {
		panel := live.FrontPanel
		state.FrontPanel = &panel
	} else if live.Hello.Capabilities&native.CapabilityFrontPanelSnapshot != 0 {
		panelFrame, queryErr := request(
			ctx, device.runtime, native.OpFrontPanelGet, nil, native.OpFrontPanel,
		)
		if queryErr == nil {
			if panel, parseErr := native.ParseFrontPanel(panelFrame.Payload); parseErr == nil {
				state.FrontPanel = &panel
			}
		}
	}
	if state.HostOutputs.MelodyName != "" {
		state.NonRestorableState = append(
			state.NonRestorableState,
			"an active PC-streamed melody is canceled and is not resumed mid-note",
		)
	}
	if state.HostOutputs.EffectName != "" {
		state.NonRestorableState = append(
			state.NonRestorableState,
			"an active PC RGB effect restarts from its beginning after programming",
		)
	}
	return state, nil
}

func (device runtimeProgrammingDevice) StoreSettings(ctx context.Context, settings native.Settings) error {
	return storeSettings(ctx, device.runtime, settings)
}

func (device runtimeProgrammingDevice) CancelMacro(ctx context.Context) error {
	return command(ctx, device.runtime, native.OpMacroCancel, nil)
}

func (device runtimeProgrammingDevice) ReleaseAllRelays(ctx context.Context) error {
	return command(ctx, device.runtime, native.OpRelayAllOff, nil)
}

func (device runtimeProgrammingDevice) RampPWMToZero(
	ctx context.Context,
	state ProgrammingLiveState,
) error {
	if state.PWM == nil {
		return errors.New("captured PWM state is unavailable")
	}
	// PCA9685 is optional. A valid PWM_VALUES response with Available=false
	// means there is no hardware output to fade, not that the safe-programming
	// transaction failed. Relay/macro safe-state operations are still handled
	// by their own required steps.
	if !state.PWM.Available {
		return nil
	}
	if device.options.Outputs != nil {
		device.options.Outputs.StopMelody()
		device.options.Outputs.OverrideStatusEffect()
	}
	for step := programmingRampSteps - 1; step >= 0; step-- {
		for channel, original := range state.PWM.Values {
			if original == 0 {
				continue
			}
			value := uint16(uint32(original) * uint32(step) / programmingRampSteps)
			payload, err := native.PWMSetPayload(byte(channel), value)
			if err != nil {
				return err
			}
			if err := command(ctx, device.runtime, native.OpPWMSet, payload); err != nil {
				return fmt.Errorf("fade PWM channel %d to zero: %w", channel, err)
			}
		}
		if step != 0 {
			if err := waitProgrammingPersistence(ctx, programmingRampStepDelay); err != nil {
				return err
			}
		}
	}
	return command(ctx, device.runtime, native.OpPWMAllOff, nil)
}

func (device runtimeProgrammingDevice) SetProgrammingCue(ctx context.Context) error {
	if !device.Snapshot().Status.PWMAvailable {
		return nil
	}
	// Warm amber is distinct from normal idle/running/BT/RF ownership colors.
	return command(
		ctx, device.runtime, native.OpStatusRGB,
		native.StatusRGBPayload(255, 72, 0, 176),
	)
}

func (device runtimeProgrammingDevice) PlayProgrammingMelody(
	ctx context.Context,
	melody appconfig.Melody,
) error {
	if device.options.Outputs != nil {
		operation, err := device.options.Outputs.StartMelody(ctx, melody, 1)
		if err != nil {
			return err
		}
		select {
		case err := <-operation.Done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, note := range melody.Notes {
		if err := command(
			ctx, device.runtime, native.OpBuzzer,
			native.BuzzerPayload(note.FrequencyHz, note.DurationMS),
		); err != nil {
			return err
		}
		if err := waitProgrammingPersistence(
			ctx, time.Duration(note.DurationMS+note.GapMS)*time.Millisecond,
		); err != nil {
			return err
		}
	}
	return nil
}

func (device runtimeProgrammingDevice) RestoreLiveState(
	ctx context.Context,
	state ProgrammingLiveState,
) ([]string, error) {
	var warnings []string
	if state.PWM != nil {
		for channel, value := range state.PWM.Values {
			payload, err := native.PWMSetPayload(byte(channel), value)
			if err != nil {
				return warnings, err
			}
			if err := command(ctx, device.runtime, native.OpPWMSet, payload); err != nil {
				return warnings, fmt.Errorf("restore PWM channel %d: %w", channel, err)
			}
		}
	}
	if err := restoreProgrammingRelays(ctx, device.runtime, state.RelayMask); err != nil {
		return warnings, err
	}
	if state.FrontPanel != nil && state.FrontPanel.HostCaptured {
		payload, err := native.HostPanelPayload(
			"----", state.FrontPanel.LCDLine1, state.FrontPanel.LCDLine2,
			state.FrontPanel.HostState, state.FrontPanel.HostEditableValue,
		)
		if err != nil {
			return warnings, fmt.Errorf("rebuild captured host panel: %w", err)
		}
		if err := command(ctx, device.runtime, native.OpDisplayText, payload); err != nil {
			return warnings, fmt.Errorf("restore captured host panel: %w", err)
		}
		warnings = append(warnings,
			"host panel LCD/state restored; exact pre-program raw seven-segment glyphs were unavailable as ASCII")
	}
	if device.options.Outputs != nil && state.HostOutputs.HaveStatusBase {
		base := state.HostOutputs.StatusBase
		if err := device.options.Outputs.SetStatusBase(
			ctx, base[0], base[1], base[2], base[3],
		); err != nil {
			return warnings, fmt.Errorf("restore host RGB owner/base: %w", err)
		}
	}
	if device.options.Outputs != nil && state.HostOutputs.EffectName != "" &&
		device.options.HostConfig != nil {
		if effect, ok := findStatusEffect(
			appconfig.EffectiveStatusLEDEffects(device.options.HostConfig()),
			state.HostOutputs.EffectName,
		); ok {
			if _, err := device.options.Outputs.StartStatusEffect(ctx, effect); err != nil {
				return warnings, fmt.Errorf("restart host RGB effect: %w", err)
			}
		} else {
			warnings = append(warnings,
				"captured host RGB effect is no longer present in watched HOST configuration")
		}
	}
	if state.Macro != nil && state.Macro.Active() {
		warnings = append(warnings,
			"macro remained canceled; its exact buffered playback position is intentionally not restorable")
	}
	if state.HostOutputs.MelodyName != "" {
		warnings = append(warnings,
			"pre-program PC melody remained canceled instead of resuming mid-note")
	}
	return warnings, nil
}

func (device runtimeProgrammingDevice) PublishProgrammingPhase(phase string) {
	device.runtime.PublishHostEvent(
		"programming.lifecycle", "programming phase: "+phase,
	)
}

func (device runtimeProgrammingDevice) EnterSafeProgrammingState(ctx context.Context) error {
	errorsToJoin := []error{
		command(ctx, device.runtime, native.OpMacroCancel, nil),
		command(ctx, device.runtime, native.OpRelayAllOff, nil),
	}
	if device.Snapshot().Status.PWMAvailable {
		errorsToJoin = append(errorsToJoin,
			command(ctx, device.runtime, native.OpPWMAllOff, nil))
	}
	return errors.Join(errorsToJoin...)
}

func (device runtimeProgrammingDevice) ShowProgrammingPanel(ctx context.Context) error {
	payload, err := native.HostPanelPayload(
		"Prog", "Programming...", "Do not disconnect", 0, 0,
	)
	if err != nil {
		return err
	}
	return command(ctx, device.runtime, native.OpDisplayText, payload)
}

func (device runtimeProgrammingDevice) ReleaseProgrammingPanel(ctx context.Context) error {
	return command(ctx, device.runtime, native.OpDisplayText, native.HostPanelReleasePayload())
}

func (device runtimeProgrammingDevice) RenderProgrammingLCD(
	ctx context.Context,
	line1, line2 string,
) error {
	presenter := device.runtime.LCDPresenter()
	if presenter == nil {
		return errors.New("physical LCD presenter is unavailable")
	}
	return presenter.RenderPhysical(ctx, line1, line2)
}

// PrepareProgrammingSession snapshots MCU settings before any temporary
// mutation, asks supported front-panel devices to show programming state, and
// mutes an audible board long enough for the EEPROM write to become durable.
// Display, LCD, and temporary-mute failures are warnings so reduced peers can
// still use the guarded raw flash+EEPROM backup path.
func PrepareProgrammingSession(
	ctx context.Context,
	runtime *Runtime,
	firmwarePath string,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) (*ProgrammingSession, error) {
	if runtime == nil {
		return nil, errors.New("programming preparation requires an application runtime")
	}
	return prepareProgrammingSession(
		ctx, runtimeProgrammingDevice{runtime: runtime, options: options}, firmwarePath, options, output,
	)
}

func prepareProgrammingSession(
	ctx context.Context,
	device programmingDevice,
	firmwarePath string,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) (*ProgrammingSession, error) {
	options, err := normalizeProgrammingLifecycleOptions(options)
	if err != nil {
		return nil, err
	}
	document, err := programmer.LoadIntelHex(firmwarePath)
	if err != nil {
		return nil, fmt.Errorf("inspect programming target: %w", err)
	}
	live := device.Snapshot()
	if !live.Connected {
		return nil, errors.New("programming preparation requires an authenticated application connection")
	}
	identity := programmingIdentity(live.Port)
	snapshot := BoardSettingsSnapshot{
		Format: programmingSnapshotFormat, CapturedAt: time.Now().UTC(),
		Device: identity, BoardKind: live.Hello.BoardKind,
		ApplicationBuildHash: live.Hello.BuildHash,
		TargetFirmwareSHA256: document.SourceSHA256,
	}
	settings, settingsErr := device.QuerySettings(ctx)
	if settingsErr != nil {
		snapshot.SettingsQueryError = settingsErr.Error()
		if !options.ReinitializeEEPROM {
			return nil, fmt.Errorf(
				"capture exact MCU EEPROM settings before programming: %w", settingsErr,
			)
		}
	} else if settings.Flags&native.SettingsProgrammingMode != 0 {
		return nil, errors.New(
			"board programming safety latch is already active; recover the pending programming session first",
		)
	} else {
		snapshot.Settings = &settings
		payload, payloadErr := settings.Payload()
		if payloadErr != nil {
			return nil, fmt.Errorf("encode MCU settings snapshot: %w", payloadErr)
		}
		snapshot.SettingsPayloadHex = strings.ToUpper(hex.EncodeToString(payload))
	}
	liveState, liveStateErr := device.CaptureLiveState(ctx)
	if liveStateErr != nil {
		snapshot.LiveStateQueryError = liveStateErr.Error()
		if !options.ReinitializeEEPROM {
			return nil, fmt.Errorf("capture live board outputs before programming: %w", liveStateErr)
		}
	}
	snapshot.LiveState = &liveState
	snapshotPath, err := persistBoardSettingsSnapshot(options.DataPaths, snapshot)
	if err != nil {
		return nil, err
	}
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: document.SourceSHA256,
		SettingsSnapshotPath: snapshotPath, OriginalSettings: snapshot.Settings,
		OriginalLiveState: snapshot.LiveState, Phase: "captured",
		ReinitializeEEPROM: options.ReinitializeEEPROM,
		SettingsQueryError: snapshot.SettingsQueryError,
	}
	if options.ReinitializeEEPROM {
		session.Warnings = append(session.Warnings,
			"DEVELOPMENT EEPROM REINITIALIZATION: the pre-flash raw EEPROM backup is retained, but incompatible board settings and live outputs will not be restored")
		if snapshot.SettingsQueryError != "" {
			session.Warnings = append(session.Warnings,
				"current MCU settings could not be decoded and were captured as an error: "+snapshot.SettingsQueryError)
		}
		if snapshot.LiveStateQueryError != "" {
			session.Warnings = append(session.Warnings,
				"live-state capture was partial; the board will be forced directly off: "+snapshot.LiveStateQueryError)
		}
	}
	markerPath, err := persistProgrammingMarker(options.DataPaths, session)
	if err != nil {
		return nil, err
	}
	session.RecoveryMarkerPath = markerPath
	device.PublishProgrammingPhase(session.Phase)
	if err := device.CancelMacro(ctx); err != nil {
		return session, fmt.Errorf("cancel active macro before programming (recovery marker retained): %w", err)
	}
	if err := advanceProgrammingSessionPhase(device, session, "macro-cancelled"); err != nil {
		return session, err
	}
	if err := device.ReleaseAllRelays(ctx); err != nil {
		return session, fmt.Errorf("release all relays before programming (recovery marker retained): %w", err)
	}
	if err := advanceProgrammingSessionPhase(device, session, "relays-released"); err != nil {
		return session, err
	}
	if err := device.RampPWMToZero(ctx, liveState); err != nil {
		if !options.ReinitializeEEPROM {
			return session, fmt.Errorf("gracefully fade PWM outputs before programming (recovery marker retained): %w", err)
		}
		session.Warnings = append(session.Warnings,
			"PWM fade was unavailable; applying an immediate all-output safe state: "+err.Error())
		if safeErr := device.EnterSafeProgrammingState(ctx); safeErr != nil {
			return session, fmt.Errorf(
				"force relays/PWM/macro off for EEPROM reinitialization (recovery marker retained): %w",
				safeErr,
			)
		}
	}
	if err := advanceProgrammingSessionPhase(device, session, "outputs-ramped"); err != nil {
		return session, err
	}
	if err := device.SetProgrammingCue(ctx); err != nil {
		return session, fmt.Errorf("set programming RGB cue (recovery marker retained): %w", err)
	}
	if err := advanceProgrammingSessionPhase(device, session, "programming-cue"); err != nil {
		return session, err
	}

	capabilities := live.Hello.Capabilities
	if capabilities&native.CapabilityHostFrontPanel != 0 {
		if panelErr := device.ShowProgrammingPanel(ctx); panelErr != nil {
			session.Warnings = append(session.Warnings,
				"front-panel programming message unavailable: "+panelErr.Error())
		} else {
			session.DisplayPrepared = true
		}
	}
	if capabilities&native.CapabilityI2CTransfer != 0 {
		if lcdErr := device.RenderProgrammingLCD(
			ctx, "Programming...", "Do not disconnect",
		); lcdErr != nil {
			session.Warnings = append(session.Warnings,
				"physical LCD programming message unavailable: "+lcdErr.Error())
		}
	}
	if err := advanceProgrammingSessionPhase(device, session, "display-ready"); err != nil {
		return session, err
	}
	melody := programmingPowerDownMelody(options.HostConfig)
	if melodyErr := device.PlayProgrammingMelody(ctx, melody); melodyErr != nil {
		return session, fmt.Errorf(
			"play PC power-down melody before programming mute (recovery marker retained): %w",
			melodyErr,
		)
	}
	if err := advanceProgrammingSessionPhase(device, session, "melody-complete"); err != nil {
		return session, err
	}
	if snapshot.Settings != nil && !options.ReinitializeEEPROM {
		temporary := programmingSafeSettings(*snapshot.Settings)
		if err := storeAndVerifyProgrammingSettings(
			ctx, device, temporary, options,
			"persist silent programming latch and safe MCU settings",
		); err != nil {
			return session, err
		}
		session.TemporarySilent = snapshot.Settings.Flags&native.SettingsSilent == 0
	} else if snapshot.Settings == nil {
		session.Warnings = append(session.Warnings,
			"pre-flash EEPROM mute/latch was unavailable because the old settings payload is intentionally unsupported; outputs are live-safe and the completed power-down melody will not be followed by the host buzzer")
	} else {
		session.Warnings = append(session.Warnings,
			"pre-flash EEPROM was deliberately left byte-for-byte untouched so the mandatory raw backup preserves the original development settings; live outputs are safe and the host buzzer has stopped")
	}
	session.SafeStateApplied = true
	finalPreparationPhase := "latched-safe"
	if options.ReinitializeEEPROM {
		finalPreparationPhase = "development-reinitialize-safe"
	}
	if err := advanceProgrammingSessionPhase(device, session, finalPreparationPhase); err != nil {
		return session, err
	}
	writeProgrammingWarnings(output, session)
	if output != nil {
		fmt.Fprintln(output, "board settings snapshot:", session.SettingsSnapshotPath)
		fmt.Fprintln(output, "programming recovery marker:", session.RecoveryMarkerPath)
	}
	return session, nil
}

// ArmProgrammingSessionAfterBackup persists the durable MCU programming latch
// at the first safe boundary after an untouched raw EEPROM backup. Ordinary
// updates already arm the latch during preparation; development
// reinitialization defers this write so its mandatory backup remains the exact
// pre-operation image.
func ArmProgrammingSessionAfterBackup(
	ctx context.Context,
	runtime *Runtime,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) error {
	if runtime == nil {
		return errors.New("post-backup programming latch requires an application runtime")
	}
	return armProgrammingSessionAfterBackup(
		ctx, runtimeProgrammingDevice{runtime: runtime, options: options},
		session, options, output,
	)
}

func armProgrammingSessionAfterBackup(
	ctx context.Context,
	device programmingDevice,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) error {
	if session == nil || !session.ReinitializeEEPROM {
		return nil
	}
	options, err := normalizeProgrammingLifecycleOptions(options)
	if err != nil {
		return err
	}
	if session.OriginalSettings == nil {
		warning := "the pre-update firmware settings schema is unsupported, so its EEPROM latch cannot be safely rewritten; the newly programmed factory EEPROM will arm Prog before further verification resets"
		session.Warnings = append(session.Warnings, warning)
		if err := rewriteProgrammingMarker(session); err != nil {
			return fmt.Errorf("persist unsupported pre-update latch warning: %w", err)
		}
		if output != nil {
			fmt.Fprintln(output, "WARNING:", warning)
		}
		return nil
	}
	if err := device.EnterSafeProgrammingState(ctx); err != nil {
		return fmt.Errorf("reassert safe outputs after raw backup: %w", err)
	}
	if err := device.SetProgrammingCue(ctx); err != nil {
		return fmt.Errorf("reassert programming RGB cue after raw backup: %w", err)
	}
	latched := programmingSafeSettings(*session.OriginalSettings)
	if err := storeAndVerifyProgrammingSettings(
		ctx, device, latched, options,
		"arm durable programming latch after untouched raw EEPROM backup",
	); err != nil {
		return err
	}
	session.TemporarySilent = session.OriginalSettings.Flags&native.SettingsSilent == 0
	session.SafeStateApplied = true
	live := device.Snapshot()
	if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
		if err := device.ShowProgrammingPanel(ctx); err != nil {
			session.Warnings = append(session.Warnings,
				"front-panel programming message unavailable after backup: "+err.Error())
		} else {
			session.DisplayPrepared = true
		}
	}
	if live.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if err := device.RenderProgrammingLCD(
			ctx, "Programming...", "Do not disconnect",
		); err != nil {
			session.Warnings = append(session.Warnings,
				"physical LCD programming message unavailable after backup: "+err.Error())
		}
	}
	if err := advanceProgrammingSessionPhase(device, session, "latched-safe"); err != nil {
		return err
	}
	if output != nil {
		fmt.Fprintln(output,
			"raw EEPROM backup complete; durable Silent/Prog latch armed and verified before firmware write")
	}
	return nil
}

// RestoreProgrammingSession restores exactly the MCU settings captured before
// programming. It waits out SettingsStore's deferred write and verifies the
// semantic settings response before deleting the crash-recovery marker.
func RestoreProgrammingSession(
	ctx context.Context,
	runtime *Runtime,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) error {
	if runtime == nil {
		return errors.New("programming restore requires an application runtime")
	}
	return restoreProgrammingSession(
		ctx, runtimeProgrammingDevice{runtime: runtime, options: options}, session, options, output,
	)
}

func restoreProgrammingSession(
	ctx context.Context,
	device programmingDevice,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) error {
	if session == nil {
		return nil
	}
	if session.HostResult != "succeeded" &&
		session.HostResult != "aborted-before-write" {
		return fmt.Errorf(
			"programming host result is %q, not verified success; safety latch and recovery marker retained",
			session.HostResult,
		)
	}
	warningStart := len(session.Warnings)
	options, err := normalizeProgrammingLifecycleOptions(options)
	if err != nil {
		return err
	}
	live := device.Snapshot()
	if !live.Connected {
		return errors.New("application did not reconnect; programming recovery marker retained")
	}
	if !sameProgrammingDevice(session.Device, programmingIdentity(live.Port)) {
		return fmt.Errorf(
			"reconnected device %s does not match programming session %s; recovery marker retained",
			live.Port.Name, session.Device.Port,
		)
	}
	if session.ReinitializeEEPROM && session.HostResult == "succeeded" {
		if err := advanceProgrammingSessionPhase(device, session, "application-authenticated"); err != nil {
			return err
		}
		return finalizeDevelopmentEEPROMReinitialization(
			ctx, device, session, options, output, warningStart, live,
		)
	}
	if session.OriginalSettings == nil || session.OriginalLiveState == nil {
		return errors.New(
			"programming recovery marker lacks exact settings/live-state capture; safety latch retained",
		)
	}
	if err := advanceProgrammingSessionPhase(device, session, "application-authenticated"); err != nil {
		return err
	}
	original := *session.OriginalSettings
	// Bit 1 is transactional state, not user configuration. Restore every
	// captured field behind the latch first, then clear it as the final write.
	original.Flags &^= native.SettingsProgrammingMode
	latched := original
	latched.Flags |= native.SettingsProgrammingMode
	if err := storeAndVerifyProgrammingSettings(
		ctx, device, latched, options,
		"stage restored MCU EEPROM settings behind programming latch",
	); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	if err := advanceProgrammingSessionPhase(device, session, "settings-staged"); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	if err := storeAndVerifyProgrammingSettings(
		ctx, device, original, options,
		"clear programming latch after restored MCU EEPROM verification",
	); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	if err := advanceProgrammingSessionPhase(device, session, "latch-cleared"); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	warnings, liveRestoreErr := device.RestoreLiveState(ctx, *session.OriginalLiveState)
	if liveRestoreErr != nil {
		return retainProgrammingSafeState(ctx, device, session, options, fmt.Errorf(
			"restore exact live board state: %w", liveRestoreErr,
		))
	}
	session.Warnings = append(session.Warnings, warnings...)
	if err := advanceProgrammingSessionPhase(device, session, "live-restored"); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
		if err := device.ReleaseProgrammingPanel(ctx); err != nil {
			session.Warnings = append(session.Warnings,
				"front-panel release after programming failed: "+err.Error())
		}
	}
	if live.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if err := device.RenderProgrammingLCD(ctx, "Programming done", "Device ready"); err != nil {
			session.Warnings = append(session.Warnings,
				"physical LCD completion message unavailable: "+err.Error())
		}
	}
	if original.Flags&native.SettingsSilent == 0 {
		if err := device.PlayProgrammingMelody(
			ctx, programmingReadyMelody(options.HostConfig),
		); err != nil {
			session.Warnings = append(session.Warnings,
				"programming-ready melody unavailable: "+err.Error())
		}
	}
	if err := os.Remove(session.RecoveryMarkerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove completed programming recovery marker: %w", err)
	}
	writeProgrammingWarningsFrom(output, session, warningStart)
	if output != nil {
		fmt.Fprintln(output, "MCU EEPROM settings restored and verified; programming recovery marker cleared")
	}
	return nil
}

// finalizeDevelopmentEEPROMReinitialization validates the newly flashed
// firmware's current settings schema, then commits the host-owned canonical
// defaults. It never imports rich defaults from AVR flash and intentionally
// does not decode or restore superseded semantic settings from the raw backup.
func finalizeDevelopmentEEPROMReinitialization(
	ctx context.Context,
	device programmingDevice,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	output io.Writer,
	warningStart int,
	live Snapshot,
) error {
	queried, err := device.QuerySettings(ctx)
	if err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, fmt.Errorf(
			"query current firmware defaults after development EEPROM reinitialization: %w", err,
		))
	}
	if queried.Flags&native.SettingsProgrammingMode != 0 {
		session.Warnings = append(session.Warnings,
			"current-schema defaults retained the interrupted programming latch; the verified development reinitialization will clear it as its final settings write")
	}
	session.PostFlashSettings = &queried
	if err := advanceProgrammingSessionPhase(device, session, "development-defaults-queried"); err != nil {
		return err
	}

	// The Go host is the canonical factory-default owner. The AVR value queried
	// above proves only that the new schema is operational; it is not used as a
	// source of descriptive defaults.
	finalSettings := native.DefaultSettings()
	finalSettings.Persisted = true
	finalSettings.Flags &^= native.SettingsSilent | native.SettingsProgrammingMode
	finalSettings.LightMode = 0
	finalSettings.OutputPersistence = 0
	finalSettings.RelayRestoreMask = 0
	if err := device.EnterSafeProgrammingState(ctx); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, fmt.Errorf(
			"de-energize new firmware before committing reinitialized settings: %w", err,
		))
	}
	if err := storeAndVerifyProgrammingSettings(
		ctx, device, finalSettings, options,
		"commit host-owned current-schema defaults with Silent off and outputs disabled",
	); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, err)
	}
	if err := device.EnterSafeProgrammingState(ctx); err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, fmt.Errorf(
			"verify relays/PWM/macro remain off after EEPROM reinitialization: %w", err,
		))
	}
	confirmed, err := device.QuerySettings(ctx)
	if err != nil {
		return retainProgrammingSafeState(ctx, device, session, options, fmt.Errorf(
			"read back reinitialized MCU settings: %w", err,
		))
	}
	if confirmed != finalSettings || confirmed.Flags&native.SettingsSilent != 0 ||
		confirmed.Flags&native.SettingsProgrammingMode != 0 ||
		confirmed.LightMode != 0 || confirmed.OutputPersistence != 0 ||
		confirmed.RelayRestoreMask != 0 {
		return retainProgrammingSafeState(ctx, device, session, options, errors.New(
			"reinitialized settings failed audible/off/read-back invariants",
		))
	}
	session.PostFlashSettings = &confirmed
	if err := advanceProgrammingSessionPhase(device, session, "development-reinitialized"); err != nil {
		return err
	}
	if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
		if err := device.ReleaseProgrammingPanel(ctx); err != nil {
			session.Warnings = append(session.Warnings,
				"front-panel release after EEPROM reinitialization failed: "+err.Error())
		}
	}
	if live.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if err := device.RenderProgrammingLCD(ctx, "Programming done", "Device ready"); err != nil {
			session.Warnings = append(session.Warnings,
				"physical LCD completion message unavailable: "+err.Error())
		}
	}
	if err := device.PlayProgrammingMelody(
		ctx, programmingReadyMelody(options.HostConfig),
	); err != nil {
		session.Warnings = append(session.Warnings,
			"programming-ready melody unavailable: "+err.Error())
	}
	if err := os.Remove(session.RecoveryMarkerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove completed EEPROM reinitialization recovery marker: %w", err)
	}
	writeProgrammingWarningsFrom(output, session, warningStart)
	if output != nil {
		fmt.Fprintln(output,
			"DEVELOPMENT EEPROM REINITIALIZED: old semantic settings were not restored; raw pre-flash EEPROM remains in the verified backup")
		fmt.Fprintln(output,
			"current-schema settings verified with Silent off and relay/PWM/macro outputs off; recovery marker cleared")
	}
	return nil
}

// MarkProgrammingSessionComplete records that the host-side programmer has
// returned a final verified outcome. Only this transition permits restoration.
func MarkProgrammingSessionComplete(session *ProgrammingSession, succeeded bool) error {
	if session == nil {
		return nil
	}
	previousPhase, previousResult := session.Phase, session.HostResult
	if succeeded {
		session.Phase = "host-complete"
		session.HostResult = "succeeded"
	} else {
		session.Phase = "host-failed"
		session.HostResult = "failed"
	}
	if err := rewriteProgrammingMarker(session); err != nil {
		session.Phase, session.HostResult = previousPhase, previousResult
		return err
	}
	return nil
}

// AbortProgrammingSessionBeforeWrite is the only recovery transition allowed
// when the mandatory raw backup failed before the programmer was invoked. The
// application image is therefore still the authenticated pre-flash image, so
// it is safe to restore the captured settings/live state instead of stranding
// the board in Prog. Any failure after a complete backup remains fail-closed.
func AbortProgrammingSessionBeforeWrite(session *ProgrammingSession) error {
	if session == nil {
		return nil
	}
	previousPhase, previousResult := session.Phase, session.HostResult
	session.Phase = "aborted-before-write"
	session.HostResult = "aborted-before-write"
	if err := rewriteProgrammingMarker(session); err != nil {
		session.Phase, session.HostResult = previousPhase, previousResult
		return err
	}
	return nil
}

// RecoverPendingProgrammingSessions is used after a later authenticated
// reconnect (including a restarted host) to finish any interrupted restore.
func RecoverPendingProgrammingSessions(
	ctx context.Context,
	runtime *Runtime,
	options ProgrammingLifecycleOptions,
	output io.Writer,
) error {
	if runtime == nil {
		return errors.New("programming recovery requires an application runtime")
	}
	runtime.programmingMu.Lock()
	defer runtime.programmingMu.Unlock()
	options, err := normalizeProgrammingLifecycleOptions(options)
	if err != nil {
		return err
	}
	markers, err := filepath.Glob(filepath.Join(options.DataPaths.StateDir, "programming-recovery-*.json"))
	if err != nil {
		return err
	}
	var failures []error
	for _, markerPath := range markers {
		session, loadErr := loadProgrammingMarker(markerPath)
		if loadErr != nil {
			failures = append(failures, loadErr)
			continue
		}
		if !sameProgrammingDevice(session.Device, programmingIdentity(runtime.Snapshot().Port)) {
			continue
		}
		if session.HostResult != "succeeded" &&
			session.HostResult != "aborted-before-write" {
			if safeErr := reassertProgrammingSession(ctx, runtimeProgrammingDevice{runtime: runtime, options: options}, session, options); safeErr != nil {
				failures = append(failures, safeErr)
			} else if output != nil {
				fmt.Fprintln(output, "pending programming job reasserted Prog and safe outputs; awaiting verified host completion")
			}
			continue
		}
		if restoreErr := RestoreProgrammingSession(ctx, runtime, session, options, output); restoreErr != nil {
			failures = append(failures, restoreErr)
		}
	}
	return errors.Join(failures...)
}

func reassertProgrammingSession(
	ctx context.Context,
	device programmingDevice,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
) error {
	if err := device.EnterSafeProgrammingState(ctx); err != nil {
		return fmt.Errorf("reassert safe relays/PWM/macro state: %w", err)
	}
	if session.OriginalSettings != nil && !session.ReinitializeEEPROM {
		safe := programmingSafeSettings(*session.OriginalSettings)
		if err := storeAndVerifyProgrammingSettings(
			ctx, device, safe, options, "reassert safe EEPROM settings",
		); err != nil {
			return err
		}
	}
	live := device.Snapshot()
	if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
		if err := device.ShowProgrammingPanel(ctx); err != nil {
			session.Warnings = append(session.Warnings,
				"front-panel programming message unavailable during recovery: "+err.Error())
		}
	}
	if live.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if err := device.RenderProgrammingLCD(ctx, "Programming...", "Do not disconnect"); err != nil {
			session.Warnings = append(session.Warnings,
				"physical LCD programming message unavailable during recovery: "+err.Error())
		}
	}
	session.Phase = "latched-safe"
	if session.ReinitializeEEPROM {
		session.Phase = "development-reinitialize-safe"
	}
	session.SafeStateApplied = true
	if err := rewriteProgrammingMarker(session); err != nil {
		return err
	}
	device.PublishProgrammingPhase(session.Phase)
	return nil
}

// findRetryableProgrammingSession returns the one failed transaction that
// exactly matches this physical device, target image, and EEPROM policy.
func findRetryableProgrammingSession(
	paths programmer.HostDataPaths,
	device ProgrammingDeviceIdentity,
	targetSHA256 string,
	reinitializeEEPROM bool,
) (*ProgrammingSession, error) {
	session, err := findFailedProgrammingSession(paths, device, targetSHA256)
	if err != nil || session == nil {
		return session, err
	}
	if session.ReinitializeEEPROM != reinitializeEEPROM {
		return nil, errors.New(
			"pending programming session uses a different EEPROM policy; recover it before starting a new write",
		)
	}
	return session, nil
}

func findFailedProgrammingSession(
	paths programmer.HostDataPaths,
	device ProgrammingDeviceIdentity,
	targetSHA256 string,
) (*ProgrammingSession, error) {
	markers, err := filepath.Glob(filepath.Join(paths.StateDir, "programming-recovery-*.json"))
	if err != nil {
		return nil, err
	}
	targetSHA256 = strings.ToLower(strings.TrimSpace(targetSHA256))
	var candidate *ProgrammingSession
	for _, markerPath := range markers {
		session, loadErr := loadProgrammingMarker(markerPath)
		if loadErr != nil {
			return nil, loadErr
		}
		if !sameProgrammingDevice(session.Device, device) {
			continue
		}
		matching := session.HostResult == "failed" &&
			strings.EqualFold(session.TargetFirmwareSHA256, targetSHA256) &&
			session.SafeStateApplied
		if !matching {
			return nil, fmt.Errorf(
				"device has a pending programming session for another target or lifecycle phase (%s); recover it before starting a new write",
				session.Phase,
			)
		}
		if candidate != nil {
			return nil, errors.New("multiple retryable programming sessions match this device")
		}
		candidate = session
	}
	return candidate, nil
}

// retainProgrammingSafeState is the last-resort recovery edge for any failure
// after a verified programmer result. It immediately de-energizes outputs,
// re-persists the programming latch, and leaves the durable marker retryable.
func retainProgrammingSafeState(
	ctx context.Context,
	device programmingDevice,
	session *ProgrammingSession,
	options ProgrammingLifecycleOptions,
	cause error,
) error {
	var recoveryErrors []error
	if err := device.EnterSafeProgrammingState(ctx); err != nil {
		recoveryErrors = append(recoveryErrors, fmt.Errorf(
			"reassert safe live outputs after restore failure: %w", err,
		))
	}
	if session.OriginalSettings != nil && !session.ReinitializeEEPROM {
		if err := storeAndVerifyProgrammingSettings(
			ctx, device, programmingSafeSettings(*session.OriginalSettings), options,
			"re-persist programming latch after restore failure",
		); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}
	previousPhase := session.Phase
	session.Phase = "restore-failed-safe"
	if err := rewriteProgrammingMarker(session); err != nil {
		session.Phase = previousPhase
		recoveryErrors = append(recoveryErrors, fmt.Errorf(
			"persist restore-failed recovery phase: %w", err,
		))
	} else {
		device.PublishProgrammingPhase(session.Phase)
	}
	return errors.Join(
		fmt.Errorf("%w; programming safety latch and recovery marker retained", cause),
		errors.Join(recoveryErrors...),
	)
}

func programmingSafeSettings(original native.Settings) native.Settings {
	value := original
	value.Flags |= native.SettingsSilent | native.SettingsProgrammingMode
	value.LightMode = 0
	value.OnBrightness = 0
	value.OffBrightness = 0
	value.StatusBrightness = 0
	value.OutputPersistence = 0
	value.RelayRestoreMask = 0
	// The programming overlay must remain visible even with the enclosure shut.
	value.DisplayClosedBrightness = value.DisplayBrightness
	return value
}

func programmingPowerDownMelody(provider func() appconfig.Config) appconfig.Melody {
	fallback := appconfig.DefaultPowerDownMelody()
	if provider == nil {
		return fallback
	}
	configured, ok := findMelody(
		appconfig.EffectiveMelodies(provider()), appconfig.PowerDownMelodyName,
	)
	if !ok || appconfig.ValidateMelody(configured) != nil {
		return fallback
	}
	return configured
}

func programmingReadyMelody(provider func() appconfig.Config) appconfig.Melody {
	fallback := appconfig.DefaultProgrammingReadyMelody()
	if provider == nil {
		return fallback
	}
	configured, ok := findMelody(
		appconfig.EffectiveMelodies(provider()), appconfig.ProgrammingReadyMelodyName,
	)
	if !ok || appconfig.ValidateMelody(configured) != nil {
		return fallback
	}
	return configured
}

func advanceProgrammingSessionPhase(
	device programmingDevice,
	session *ProgrammingSession,
	phase string,
) error {
	previous := session.Phase
	session.Phase = phase
	if err := rewriteProgrammingMarker(session); err != nil {
		session.Phase = previous
		return err
	}
	device.PublishProgrammingPhase(phase)
	return nil
}

func restoreProgrammingRelays(
	ctx context.Context,
	runtime *Runtime,
	mask byte,
) error {
	if err := command(ctx, runtime, native.OpRelayAllOff, nil); err != nil {
		return fmt.Errorf("release relays before semantic restore: %w", err)
	}
	for side := byte(0); side < 2; side++ {
		directionBit := side * 2
		enableBit := directionBit + 1
		reverse := mask&(1<<directionBit) != 0
		enabled := mask&(1<<enableBit) != 0
		if enabled {
			motion := byte(1)
			if reverse {
				motion = 2
			}
			payload, err := native.RelaySidePayload(side, motion)
			if err != nil {
				return err
			}
			if err := command(ctx, runtime, native.OpRelaySide, payload); err != nil {
				return fmt.Errorf("restore motion side %d through interlock: %w", side, err)
			}
		} else if reverse {
			// The enable is known off after ALL_OFF. Restoring only the requested
			// direction through RelayController is safe and cannot energize motion.
			payload, err := native.RelayPayload(directionBit, true)
			if err != nil {
				return err
			}
			if err := command(ctx, runtime, native.OpRelaySet, payload); err != nil {
				return fmt.Errorf("restore idle direction for side %d: %w", side, err)
			}
		}
	}
	for relay := byte(4); relay < 8; relay++ {
		if mask&(1<<relay) == 0 {
			continue
		}
		payload, err := native.RelayPayload(relay, true)
		if err != nil {
			return err
		}
		if err := command(ctx, runtime, native.OpRelaySet, payload); err != nil {
			return fmt.Errorf("restore user relay R%d: %w", relay+1, err)
		}
	}
	if err := waitProgrammingPersistence(ctx, 200*time.Millisecond); err != nil {
		return err
	}
	status, err := refresh(ctx, runtime)
	if err != nil {
		return fmt.Errorf("verify restored relay state: %w", err)
	}
	if status.ActiveRelays != mask {
		return fmt.Errorf(
			"restored relay mask 0x%02X differs from captured 0x%02X",
			status.ActiveRelays, mask,
		)
	}
	return nil
}

func storeAndVerifyProgrammingSettings(
	ctx context.Context,
	device programmingDevice,
	expected native.Settings,
	options ProgrammingLifecycleOptions,
	operation string,
) error {
	if err := device.StoreSettings(ctx, expected); err != nil {
		return fmt.Errorf("%s (recovery marker retained): %w", operation, err)
	}
	if err := options.Wait(ctx, options.PersistenceDelay); err != nil {
		return fmt.Errorf("wait to %s (recovery marker retained): %w", operation, err)
	}
	confirmed, err := device.QuerySettings(ctx)
	if err != nil {
		return fmt.Errorf("verify %s (recovery marker retained): %w", operation, err)
	}
	if confirmed != expected {
		return fmt.Errorf("%s differs after read-back; recovery marker retained", operation)
	}
	return nil
}

func normalizeProgrammingLifecycleOptions(
	options ProgrammingLifecycleOptions,
) (ProgrammingLifecycleOptions, error) {
	if strings.TrimSpace(options.DataPaths.DataDir) == "" {
		paths, err := programmer.DefaultHostDataPaths()
		if err != nil {
			return options, err
		}
		options.DataPaths = paths
	}
	if err := programmer.EnsureHostDataPaths(options.DataPaths); err != nil {
		return options, err
	}
	if options.PersistenceDelay <= 0 {
		options.PersistenceDelay = ProgrammingSettingsPersistenceDelay
	}
	if options.Wait == nil {
		options.Wait = waitProgrammingPersistence
	}
	return options, nil
}

func waitProgrammingPersistence(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func programmingIdentity(info ports.Info) ProgrammingDeviceIdentity {
	return ProgrammingDeviceIdentity{
		Port: info.Name, VID: info.VID, PID: info.PID,
		SerialNumber: info.SerialNumber, FriendlyName: info.FriendlyName,
		InstanceID: info.InstanceID,
	}
}

func sameProgrammingDevice(left, right ProgrammingDeviceIdentity) bool {
	if left.SerialNumber != "" && right.SerialNumber != "" {
		return strings.EqualFold(left.SerialNumber, right.SerialNumber) &&
			strings.EqualFold(left.VID, right.VID) && strings.EqualFold(left.PID, right.PID)
	}
	if left.InstanceID != "" && right.InstanceID != "" {
		return strings.EqualFold(left.InstanceID, right.InstanceID)
	}
	return strings.EqualFold(left.Port, right.Port) && left.Port != ""
}

func persistBoardSettingsSnapshot(
	paths programmer.HostDataPaths,
	snapshot BoardSettingsSnapshot,
) (string, error) {
	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode board settings snapshot: %w", err)
	}
	hash := sha256.Sum256(content)
	hashText := hex.EncodeToString(hash[:])
	directory := filepath.Join(paths.BoardSettingsDir, hashText[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "board-settings-"+hashText+".json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil || string(existing) != string(content) {
			return "", errors.New("content-addressed board settings snapshot collision")
		}
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("create board settings snapshot: %w", err)
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

func persistProgrammingMarker(
	paths programmer.HostDataPaths,
	session *ProgrammingSession,
) (string, error) {
	file, err := os.CreateTemp(paths.StateDir, "programming-recovery-*.json")
	if err != nil {
		return "", fmt.Errorf("create programming recovery marker: %w", err)
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(session)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	return path, nil
}

func rewriteProgrammingMarker(session *ProgrammingSession) error {
	if session == nil || strings.TrimSpace(session.RecoveryMarkerPath) == "" {
		return errors.New("programming recovery marker path is unavailable")
	}
	directory := filepath.Dir(session.RecoveryMarkerPath)
	temporary, err := os.CreateTemp(directory, ".programming-recovery-*.tmp")
	if err != nil {
		return fmt.Errorf("stage programming recovery marker: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(session)
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(temporaryPath, session.RecoveryMarkerPath); err != nil {
		_ = os.Remove(session.RecoveryMarkerPath)
		if retryErr := os.Rename(temporaryPath, session.RecoveryMarkerPath); retryErr != nil {
			return fmt.Errorf("publish programming recovery marker: %w", retryErr)
		}
	}
	return nil
}

func loadProgrammingMarker(path string) (*ProgrammingSession, error) {
	name := filepath.Base(path)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read programming recovery marker %s: %w", name, err)
	}
	var session ProgrammingSession
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&session); err != nil {
		return nil, fmt.Errorf("decode programming recovery marker %s: %w", name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("programming recovery marker %s contains trailing JSON", name)
		}
		return nil, fmt.Errorf("decode programming recovery marker %s trailing data: %w", name, err)
	}
	if session.Format != programmingMarkerFormat {
		return nil, fmt.Errorf("unrecognized programming recovery marker %s", name)
	}
	if session.Phase == "" {
		return nil, fmt.Errorf("programming recovery marker %s has invalid phase %q", name, session.Phase)
	}
	switch session.Phase {
	case "captured", "macro-cancelled", "relays-released", "outputs-ramped",
		"programming-cue", "display-ready", "melody-complete", "latched-safe",
		"development-reinitialize-safe", "development-defaults-queried",
		"development-reinitialized",
		"host-complete", "host-failed", "application-authenticated",
		"settings-staged", "latch-cleared", "live-restored", "restore-failed-safe":
	default:
		return nil, fmt.Errorf("programming recovery marker %s has invalid phase %q", name, session.Phase)
	}
	session.RecoveryMarkerPath = path
	return &session, nil
}

// InspectProgrammingRecoveryMarkers consumes current durable marker/session
// metadata without contacting the board or changing lifecycle state.
func InspectProgrammingRecoveryMarkers(
	paths programmer.HostDataPaths,
) ([]ProgrammingRecoveryDiagnostic, error) {
	stateDir := filepath.Clean(strings.TrimSpace(paths.StateDir))
	if stateDir == "" || stateDir == "." || !filepath.IsAbs(stateDir) {
		return nil, errors.New("programming recovery state directory must be absolute")
	}
	if info, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
		return []ProgrammingRecoveryDiagnostic{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect programming recovery state directory: %w", err)
	} else if !info.IsDir() {
		return nil, errors.New("programming recovery state path is not a directory")
	}
	markers, err := filepath.Glob(filepath.Join(stateDir, "programming-recovery-*.json"))
	if err != nil {
		return nil, err
	}
	result := make([]ProgrammingRecoveryDiagnostic, 0, len(markers))
	var failures []error
	for _, markerPath := range markers {
		info, statErr := os.Lstat(markerPath)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > 256*1024 {
			failures = append(failures, fmt.Errorf("programming recovery marker is not a bounded regular file: %s", filepath.Base(markerPath)))
			continue
		}
		content, readErr := os.ReadFile(markerPath)
		if readErr != nil {
			failures = append(failures, fmt.Errorf("read programming recovery marker %s: %w", filepath.Base(markerPath), readErr))
			continue
		}
		session, loadErr := loadProgrammingMarker(markerPath)
		if loadErr != nil {
			failures = append(failures, loadErr)
			continue
		}
		targetHash, hashErr := normalizedProgrammingDiagnosticHash(session.TargetFirmwareSHA256)
		if hashErr != nil {
			failures = append(failures, fmt.Errorf("programming recovery marker %s target hash: %w", filepath.Base(markerPath), hashErr))
			continue
		}
		settingsHash := ""
		settingsPath := filepath.Clean(strings.TrimSpace(session.SettingsSnapshotPath))
		if settingsPath != "" && settingsPath != "." {
			if !programmingDiagnosticPathWithin(paths.BoardSettingsDir, settingsPath) {
				failures = append(failures, fmt.Errorf("programming recovery marker %s settings snapshot escapes the board-settings directory", filepath.Base(markerPath)))
				continue
			}
			settingsInfo, settingsErr := os.Lstat(settingsPath)
			if settingsErr != nil || !settingsInfo.Mode().IsRegular() || settingsInfo.Size() > 256*1024 {
				failures = append(failures, fmt.Errorf("programming recovery marker %s settings snapshot is unavailable", filepath.Base(markerPath)))
				continue
			}
			settingsContent, settingsErr := os.ReadFile(settingsPath)
			if settingsErr != nil {
				failures = append(failures, settingsErr)
				continue
			}
			settingsDigest := sha256.Sum256(settingsContent)
			settingsHash = hex.EncodeToString(settingsDigest[:])
		}
		markerDigest := sha256.Sum256(content)
		deviceBytes, _ := json.Marshal(session.Device)
		deviceDigest := sha256.Sum256(deviceBytes)
		state := "programming-incomplete"
		switch session.HostResult {
		case "succeeded":
			state = "restore-pending"
		case "failed":
			state = "programming-failed-safe"
		}
		result = append(result, ProgrammingRecoveryDiagnostic{
			MarkerSHA256:         hex.EncodeToString(markerDigest[:]),
			TargetFirmwareSHA256: targetHash, SettingsSnapshotSHA256: settingsHash,
			DeviceFingerprint: hex.EncodeToString(deviceDigest[:]),
			PreparedAt:        session.PreparedAt, Phase: session.Phase,
			HostResult: session.HostResult, DiagnosticState: state,
			WarningCount: len(session.Warnings), RestorationPending: true,
		})
	}
	return result, errors.Join(failures...)
}

func normalizedProgrammingDiagnosticHash(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return "", errors.New("SHA-256 must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("SHA-256 must be hexadecimal")
	}
	return value, nil
}

func programmingDiagnosticPathWithin(root, candidate string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "" || root == "." || candidate == "" || candidate == "." ||
		!filepath.IsAbs(root) || !filepath.IsAbs(candidate) {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func writeProgrammingWarnings(output io.Writer, session *ProgrammingSession) {
	writeProgrammingWarningsFrom(output, session, 0)
}

func writeProgrammingWarningsFrom(output io.Writer, session *ProgrammingSession, start int) {
	if output == nil || session == nil {
		return
	}
	if start < 0 || start > len(session.Warnings) {
		start = 0
	}
	for _, warning := range session.Warnings[start:] {
		fmt.Fprintln(output, "WARNING:", warning)
	}
}
