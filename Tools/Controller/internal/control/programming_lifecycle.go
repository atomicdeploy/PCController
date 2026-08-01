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
)

// ProgrammingLifecycleOptions keeps MCU EEPROM recovery artifacts under the
// host data directory; these are board backups, never PC application config.
type ProgrammingLifecycleOptions struct {
	DataPaths        programmer.HostDataPaths
	PersistenceDelay time.Duration
	Wait             func(context.Context, time.Duration) error
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
	DisplayPrepared      bool                      `json:"display_prepared"`
	TemporarySilent      bool                      `json:"temporary_silent"`
	Warnings             []string                  `json:"warnings,omitempty"`
}

type programmingDevice interface {
	Snapshot() Snapshot
	QuerySettings(context.Context) (native.Settings, error)
	StoreSettings(context.Context, native.Settings) error
	ShowProgrammingPanel(context.Context) error
	ReleaseProgrammingPanel(context.Context) error
	RenderProgrammingLCD(context.Context, string, string) error
}

type runtimeProgrammingDevice struct{ runtime *Runtime }

func (device runtimeProgrammingDevice) Snapshot() Snapshot { return device.runtime.Snapshot() }

func (device runtimeProgrammingDevice) QuerySettings(ctx context.Context) (native.Settings, error) {
	return querySettings(ctx, device.runtime)
}

func (device runtimeProgrammingDevice) StoreSettings(ctx context.Context, settings native.Settings) error {
	return storeSettings(ctx, device.runtime, settings)
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
		ctx, runtimeProgrammingDevice{runtime: runtime}, firmwarePath, options, output,
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
	} else {
		snapshot.Settings = &settings
		payload, payloadErr := settings.Payload()
		if payloadErr != nil {
			return nil, fmt.Errorf("encode MCU settings snapshot: %w", payloadErr)
		}
		snapshot.SettingsPayloadHex = strings.ToUpper(hex.EncodeToString(payload))
	}
	snapshotPath, err := persistBoardSettingsSnapshot(options.DataPaths, snapshot)
	if err != nil {
		return nil, err
	}
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: document.SourceSHA256,
		SettingsSnapshotPath: snapshotPath, OriginalSettings: snapshot.Settings,
	}
	if settingsErr != nil {
		session.Warnings = append(session.Warnings,
			"MCU settings query unavailable; full raw EEPROM backup remains mandatory: "+settingsErr.Error())
	}
	markerPath, err := persistProgrammingMarker(options.DataPaths, session)
	if err != nil {
		return nil, err
	}
	session.RecoveryMarkerPath = markerPath

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
	if snapshot.Settings != nil && snapshot.Settings.Flags&native.SettingsSilent == 0 {
		temporary := *snapshot.Settings
		temporary.Flags |= native.SettingsSilent
		if silentErr := device.StoreSettings(ctx, temporary); silentErr != nil {
			session.Warnings = append(session.Warnings,
				"temporary MCU mute unavailable; original audible state is still backed up: "+silentErr.Error())
		} else {
			session.TemporarySilent = true
			if waitErr := options.Wait(ctx, options.PersistenceDelay); waitErr != nil {
				return session, fmt.Errorf(
					"wait for temporary MCU mute EEPROM persistence (recovery marker retained): %w",
					waitErr,
				)
			}
		}
	}
	writeProgrammingWarnings(output, session)
	if output != nil {
		fmt.Fprintln(output, "board settings snapshot:", session.SettingsSnapshotPath)
		fmt.Fprintln(output, "programming recovery marker:", session.RecoveryMarkerPath)
	}
	return session, nil
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
		ctx, runtimeProgrammingDevice{runtime: runtime}, session, options, output,
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
	if session.OriginalSettings != nil {
		if err := device.StoreSettings(ctx, *session.OriginalSettings); err != nil {
			return fmt.Errorf("restore MCU EEPROM settings (recovery marker retained): %w", err)
		}
		if err := options.Wait(ctx, options.PersistenceDelay); err != nil {
			return fmt.Errorf("wait for restored MCU EEPROM settings (recovery marker retained): %w", err)
		}
		confirmed, err := device.QuerySettings(ctx)
		if err != nil {
			return fmt.Errorf("verify restored MCU EEPROM settings (recovery marker retained): %w", err)
		}
		if confirmed != *session.OriginalSettings {
			return errors.New("restored MCU EEPROM settings differ from snapshot; recovery marker retained")
		}
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
	if err := os.Remove(session.RecoveryMarkerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove completed programming recovery marker: %w", err)
	}
	writeProgrammingWarningsFrom(output, session, warningStart)
	if output != nil {
		fmt.Fprintln(output, "MCU EEPROM settings restored and verified; programming recovery marker cleared")
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
		if restoreErr := RestoreProgrammingSession(ctx, runtime, session, options, output); restoreErr != nil {
			failures = append(failures, restoreErr)
		}
	}
	return errors.Join(failures...)
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

func loadProgrammingMarker(path string) (*ProgrammingSession, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var session ProgrammingSession
	if err := json.Unmarshal(content, &session); err != nil {
		return nil, fmt.Errorf("decode programming recovery marker %s: %w", path, err)
	}
	if session.Format != programmingMarkerFormat {
		return nil, fmt.Errorf("unrecognized programming recovery marker %s", path)
	}
	session.RecoveryMarkerPath = path
	return &session, nil
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
