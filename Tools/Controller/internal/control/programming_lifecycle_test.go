package control

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
)

type fakeProgrammingDevice struct {
	snapshot      Snapshot
	settings      native.Settings
	queryErr      error
	storeErr      error
	panelErr      error
	lcdErr        error
	stores        []native.Settings
	panelShown    int
	panelReleased int
	lcdLines      [][2]string
	safeCalls     int
	liveState     ProgrammingLiveState
	liveErr       error
	rampErr       error
	restoreErr    error
	restoredLive  *ProgrammingLiveState
	calls         []string
	phases        []string
	melodies      []string
}

func (device *fakeProgrammingDevice) EnterSafeProgrammingState(context.Context) error {
	device.safeCalls++
	device.calls = append(device.calls, "safe-live")
	return nil
}

func (device *fakeProgrammingDevice) Snapshot() Snapshot { return device.snapshot }

func (device *fakeProgrammingDevice) QuerySettings(context.Context) (native.Settings, error) {
	device.calls = append(device.calls, "settings-query")
	if device.queryErr != nil {
		return native.Settings{}, device.queryErr
	}
	return device.settings, nil
}

func (device *fakeProgrammingDevice) StoreSettings(
	_ context.Context,
	settings native.Settings,
) error {
	if device.storeErr != nil {
		return device.storeErr
	}
	device.settings = settings
	device.stores = append(device.stores, settings)
	device.calls = append(device.calls, "settings-store")
	return nil
}

func (device *fakeProgrammingDevice) CaptureLiveState(context.Context) (ProgrammingLiveState, error) {
	device.calls = append(device.calls, "capture-live")
	if device.liveErr != nil {
		return ProgrammingLiveState{}, device.liveErr
	}
	state := device.liveState
	state.RelayMask = device.snapshot.Status.ActiveRelays
	if state.PWM == nil {
		state.PWM = &native.PWMValues{Available: true}
	}
	return state, nil
}

func (device *fakeProgrammingDevice) CancelMacro(context.Context) error {
	device.calls = append(device.calls, "macro-cancel")
	return nil
}

func (device *fakeProgrammingDevice) ReleaseAllRelays(context.Context) error {
	device.calls = append(device.calls, "relays-off")
	return nil
}

func (device *fakeProgrammingDevice) RampPWMToZero(context.Context, ProgrammingLiveState) error {
	device.calls = append(device.calls, "pwm-ramp")
	return device.rampErr
}

func (device *fakeProgrammingDevice) SetProgrammingCue(context.Context) error {
	device.calls = append(device.calls, "programming-cue")
	return nil
}

func (device *fakeProgrammingDevice) PlayProgrammingMelody(_ context.Context, melody appconfig.Melody) error {
	device.melodies = append(device.melodies, melody.Name)
	device.calls = append(device.calls, melody.Name+"-melody")
	return nil
}

func (device *fakeProgrammingDevice) RestoreLiveState(
	_ context.Context,
	state ProgrammingLiveState,
) ([]string, error) {
	device.calls = append(device.calls, "restore-live")
	device.restoredLive = &state
	return nil, device.restoreErr
}

func (device *fakeProgrammingDevice) PublishProgrammingPhase(phase string) {
	device.phases = append(device.phases, phase)
}

func (device *fakeProgrammingDevice) ShowProgrammingPanel(context.Context) error {
	device.panelShown++
	device.calls = append(device.calls, "panel-show")
	return device.panelErr
}

func (device *fakeProgrammingDevice) ReleaseProgrammingPanel(context.Context) error {
	device.panelReleased++
	device.calls = append(device.calls, "panel-release")
	return device.panelErr
}

func (device *fakeProgrammingDevice) RenderProgrammingLCD(
	_ context.Context,
	line1, line2 string,
) error {
	device.lcdLines = append(device.lcdLines, [2]string{line1, line2})
	device.calls = append(device.calls, "lcd")
	return device.lcdErr
}

func TestProgrammingLifecycleSnapshotsMutesWaitsRestoresAndVerifies(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{
		MotionBreakMSValue: 1,
		LightMode:          1, OnBrightness: 200, DisplayBrightness: 5,
		StatusBrightness: 128, OutputPersistence: native.OutputPersistUserPWM,
		StreamPeriodMS: 250,
		DefaultPage:    4,
	}
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(
			native.CapabilityHostFrontPanel | native.CapabilityI2CTransfer,
		),
		settings: original,
	}
	var waits []time.Duration
	options := ProgrammingLifecycleOptions{
		DataPaths: paths,
		Wait: func(_ context.Context, duration time.Duration) error {
			waits = append(waits, duration)
			return nil
		},
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware, options, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(device.stores) != 1 || device.stores[0].Flags&native.SettingsSilent == 0 ||
		device.stores[0].Flags&native.SettingsProgrammingMode == 0 ||
		device.stores[0].LightMode != 0 || device.stores[0].OutputPersistence != 0 ||
		device.stores[0].OnBrightness != 0 || device.stores[0].StatusBrightness != 0 ||
		device.stores[0].DisplayClosedBrightness != original.DisplayBrightness ||
		!session.TemporarySilent || !session.SafeStateApplied || session.Phase != "latched-safe" ||
		device.safeCalls != 0 || device.panelShown != 1 || len(device.lcdLines) != 1 {
		t.Fatalf("preparation session=%+v device=%+v", session, device)
	}
	if len(waits) != 1 || waits[0] != ProgrammingSettingsPersistenceDelay {
		t.Fatalf("preparation waits=%v", waits)
	}
	wantOrder := []string{
		"settings-query", "capture-live", "macro-cancel", "relays-off",
		"pwm-ramp", "programming-cue", "panel-show", "lcd",
		"power-down-melody", "settings-store", "settings-query",
	}
	if strings.Join(device.calls, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("unsafe programming preparation order:\n got %v\nwant %v", device.calls, wantOrder)
	}
	if _, err := os.Stat(session.SettingsSnapshotPath); err != nil {
		t.Fatalf("settings snapshot missing: %v", err)
	}
	if _, err := os.Stat(session.RecoveryMarkerPath); err != nil {
		t.Fatalf("recovery marker missing: %v", err)
	}
	if err := MarkProgrammingSessionComplete(session, true); err != nil {
		t.Fatal(err)
	}
	if err := restoreProgrammingSession(
		context.Background(), device, session, options, io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if device.settings != original || len(device.stores) != 3 || device.panelReleased != 1 ||
		len(waits) != 3 || len(device.lcdLines) != 2 ||
		device.restoredLive == nil ||
		strings.Join(device.melodies, ",") != "power-down,programming-ready" ||
		device.stores[1].Flags&native.SettingsProgrammingMode == 0 ||
		device.stores[2].Flags&native.SettingsProgrammingMode != 0 {
		t.Fatalf("restore device=%+v waits=%v", device, waits)
	}
	if _, err := os.Stat(session.RecoveryMarkerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed recovery marker still exists: %v", err)
	}
}

func TestProgrammingLifecycleOptionalFeaturesAreNonfatal(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{Flags: native.SettingsSilent, LightMode: 1, DisplayBrightness: 5, MotionBreakMSValue: 1}
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(0), settings: original,
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if session.TemporarySilent || device.panelShown != 0 || len(device.lcdLines) != 0 ||
		len(device.stores) != 1 || device.safeCalls != 0 || !session.SafeStateApplied {
		t.Fatalf("unsupported optional features were invoked: session=%+v device=%+v", session, device)
	}
	if err := MarkProgrammingSessionComplete(session, true); err != nil {
		t.Fatal(err)
	}
	if err := restoreProgrammingSession(
		context.Background(), device, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait},
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if device.settings.Flags&native.SettingsSilent == 0 {
		t.Fatal("original silent state was not preserved")
	}
}

func TestProgrammingLifecycleFlashFailureLeavesRecoverableMarker(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: native.Settings{LightMode: 1, DisplayBrightness: 5, MotionBreakMSValue: 1},
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadProgrammingMarker(session.RecoveryMarkerPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OriginalSettings == nil || loaded.OriginalSettings.Flags&native.SettingsSilent != 0 ||
		loaded.SettingsSnapshotPath == "" || loaded.Phase != "latched-safe" || !loaded.SafeStateApplied {
		t.Fatalf("failure marker cannot restore original board settings: %+v", loaded)
	}
	diagnostics, err := InspectProgrammingRecoveryMarkers(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || len(diagnostics[0].MarkerSHA256) != 64 ||
		len(diagnostics[0].TargetFirmwareSHA256) != 64 ||
		len(diagnostics[0].SettingsSnapshotSHA256) != 64 ||
		len(diagnostics[0].DeviceFingerprint) != 64 ||
		diagnostics[0].Phase != "latched-safe" ||
		diagnostics[0].DiagnosticState != "programming-incomplete" ||
		!diagnostics[0].RestorationPending {
		t.Fatalf("recovery diagnostic=%#v", diagnostics)
	}
	if err := MarkProgrammingSessionComplete(loaded, true); err != nil {
		t.Fatal(err)
	}
	diagnostics, err = InspectProgrammingRecoveryMarkers(paths)
	if err != nil || len(diagnostics) != 1 ||
		diagnostics[0].DiagnosticState != "restore-pending" ||
		diagnostics[0].HostResult != "succeeded" {
		t.Fatalf("completed-host recovery diagnostic=%#v err=%v", diagnostics, err)
	}
}

func TestLoadProgrammingMarkerRejectsMissingPhaseWithoutDevelopmentBaggage(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "programming-recovery-phase-less.json")
	content := `{
  "format": "pccontroller.programming-recovery",
  "prepared_at": "2026-08-02T01:29:36Z",
  "device": {"port": "COM18"},
  "target_firmware_sha256": "` + strings.Repeat("a", 64) + `",
  "settings_snapshot_path": "` + filepath.ToSlash(filepath.Join(directory, "settings.json")) + `",
  "original_mcu_eeprom_settings": {"flags": 1, "motion_break_ms": 1}
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadProgrammingMarker(path)
	if err == nil || !strings.Contains(err.Error(), "has invalid phase") ||
		strings.Contains(err.Error(), filepath.Dir(path)) {
		t.Fatalf("phase-less development marker was accepted or leaked a path: %v", err)
	}
}

func TestLoadProgrammingMarkerRejectsUnrecoverableMissingPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "programming-recovery-invalid.json")
	if err := os.WriteFile(path, []byte(`{"format":"pccontroller.programming-recovery"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadProgrammingMarker(path)
	if err == nil || !strings.Contains(err.Error(), "has invalid phase") || strings.Contains(err.Error(), filepath.Dir(path)) {
		t.Fatalf("unsafe or path-leaking phase error=%v", err)
	}
}

func TestLoadProgrammingMarkerAcceptsLegacyMacroDroppedSteps(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "programming-recovery-legacy-macro.json")
	content := `{
  "format": "pccontroller.programming-recovery",
  "prepared_at": "2026-08-12T15:05:54Z",
  "device": {"port": "COM4"},
  "target_firmware_sha256": "` + strings.Repeat("a", 64) + `",
  "settings_snapshot_path": "` + filepath.ToSlash(filepath.Join(directory, "settings.json")) + `",
  "original_live_state": {
    "macro": {"schema": 3, "state": 0, "dropped_steps": 7},
    "program_state": {"mode": "Idle"},
    "host_outputs": {}
  },
  "safe_state_applied": true,
  "phase": "latched-safe"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadProgrammingMarker(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OriginalLiveState == nil || loaded.OriginalLiveState.Macro == nil ||
		loaded.OriginalLiveState.Macro.DroppedSteps != 7 {
		t.Fatalf("legacy macro status was not preserved: %#v", loaded.OriginalLiveState)
	}
}

func TestProgrammingPWMRampAcceptsExplicitlyUnavailablePeripheral(t *testing.T) {
	device := runtimeProgrammingDevice{}
	if err := device.RampPWMToZero(context.Background(), ProgrammingLiveState{
		PWM: &native.PWMValues{Available: false},
	}); err != nil {
		t.Fatalf("unavailable PWM peripheral should already be safe: %v", err)
	}
}

func TestProgrammingLifecycleDoesNotRestoreBeforeHostCompletion(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: native.Settings{LightMode: 1, OnBrightness: 200, DisplayBrightness: 5, MotionBreakMSValue: 1},
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreProgrammingSession(
		context.Background(), device, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	); err == nil || !strings.Contains(err.Error(), "not verified success") {
		t.Fatalf("premature restore err=%v", err)
	}
	if device.settings.LightMode != 0 || device.settings.Flags&native.SettingsSilent == 0 ||
		device.settings.Flags&native.SettingsProgrammingMode == 0 {
		t.Fatalf("safe state was lost: %+v", device.settings)
	}
}

func TestProgrammingLifecycleReassertsSafeStateAfterReset(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	before := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel | native.CapabilityI2CTransfer),
		settings: native.Settings{LightMode: 2, OnBrightness: 180, DisplayBrightness: 6, MotionBreakMSValue: 1},
	}
	session, err := prepareProgrammingSession(
		context.Background(), before, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	after := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel | native.CapabilityI2CTransfer),
		settings: native.Settings{LightMode: 2, OnBrightness: 180, DisplayBrightness: 6, MotionBreakMSValue: 1},
	}
	if err := reassertProgrammingSession(
		context.Background(), after, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait},
	); err != nil {
		t.Fatal(err)
	}
	if after.safeCalls != 1 || after.panelShown != 1 || len(after.lcdLines) != 1 ||
		after.settings.LightMode != 0 || after.settings.OutputPersistence != 0 ||
		after.settings.Flags&native.SettingsSilent == 0 ||
		after.settings.Flags&native.SettingsProgrammingMode == 0 ||
		after.settings.DisplayClosedBrightness != session.OriginalSettings.DisplayBrightness {
		t.Fatalf("reassert after=%+v session=%+v", after, session)
	}
}

func TestProgrammingLifecycleRejectsAnUnownedActiveSafetyLatch(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: native.Settings{
			Flags: native.SettingsProgrammingMode, DisplayBrightness: 5, MotionBreakMSValue: 1,
		},
	}
	_, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "safety latch is already active") {
		t.Fatalf("active programming latch was accepted: %v", err)
	}
	if len(device.stores) != 0 || device.safeCalls != 0 {
		t.Fatalf("rejected latch mutated the device: %+v", device)
	}
}

func TestProgrammingLifecycleReconnectRecoversPendingMarker(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{LightMode: 2, OffBrightness: 3, DisplayBrightness: 6, MotionBreakMSValue: 1}
	before := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: original,
	}
	session, err := prepareProgrammingSession(
		context.Background(), before, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	after := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: native.Settings{Flags: native.SettingsSilent, DisplayBrightness: 1, MotionBreakMSValue: 1},
	}
	loaded, err := loadProgrammingMarker(session.RecoveryMarkerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkProgrammingSessionComplete(loaded, true); err != nil {
		t.Fatal(err)
	}
	if err := restoreProgrammingSession(
		context.Background(), after, loaded,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if after.settings != original || after.panelReleased != 1 {
		t.Fatalf("reconnect did not restore exact MCU state: %+v", after)
	}
}

func TestProgrammingLifecycleSettingsQueryFailureStopsBeforeMutation(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(0),
		queryErr: errors.New("unsupported opcode"),
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "capture exact MCU EEPROM settings") {
		t.Fatalf("settings query failure was not rejected: session=%+v err=%v", session, err)
	}
	if session != nil || len(device.stores) != 0 || device.safeCalls != 0 {
		t.Fatalf("failed settings capture mutated the device: session=%+v device=%+v", session, device)
	}
}

func TestProgrammingLifecycleDevelopmentEEPROMReinitializationCapturesErrorAndKeepsOutputsOff(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	queryErr := errors.New("SETTINGS payload length 13, expected 15")
	liveErr := errors.New("PWM_VALUES unavailable on development firmware")
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(
			native.CapabilityHostFrontPanel | native.CapabilityI2CTransfer,
		),
		queryErr: queryErr,
		liveErr:  liveErr,
		rampErr:  liveErr,
		liveState: ProgrammingLiveState{
			PWM: &native.PWMValues{Available: true, Values: [16]uint16{120, 240}},
		},
	}
	options := ProgrammingLifecycleOptions{
		DataPaths: paths, Wait: noProgrammingWait, ReinitializeEEPROM: true,
	}
	var output strings.Builder
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware, options, &output,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !session.ReinitializeEEPROM || session.SettingsQueryError != queryErr.Error() ||
		session.OriginalSettings != nil || session.OriginalLiveState == nil ||
		session.Phase != "development-reinitialize-safe" || len(device.stores) != 0 {
		t.Fatalf("development preparation session=%+v stores=%v", session, device.stores)
	}
	content, err := os.ReadFile(session.SettingsSnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"settings_query_error": "SETTINGS payload length 13, expected 15"`) ||
		!strings.Contains(string(content), `"live_state_query_error": "PWM_VALUES unavailable on development firmware"`) ||
		!strings.Contains(output.String(), "DEVELOPMENT EEPROM REINITIALIZATION") {
		t.Fatalf("snapshot/output did not preserve the compatibility exception:\n%s\n%s", content, output.String())
	}

	if err := MarkProgrammingSessionComplete(session, true); err != nil {
		t.Fatal(err)
	}
	device.queryErr = nil
	device.settings = native.Settings{
		Flags: native.SettingsSilent | native.SettingsProgrammingMode, LightMode: 2,
		OnBrightness: 180, DisplayBrightness: 5,
		StatusBrightness: 128, OutputPersistence: native.OutputPersistenceMask,
		RelayRestoreMask: 0xFF, MotionBreakMSValue: 1,
	}
	if err := restoreProgrammingSession(
		context.Background(), device, session, options, &output,
	); err != nil {
		t.Fatal(err)
	}
	if len(device.stores) != 1 || device.settings.Flags&native.SettingsSilent != 0 ||
		device.settings.Flags&native.SettingsProgrammingMode != 0 ||
		device.settings.LightMode != 0 || device.settings.OutputPersistence != 0 ||
		device.settings.RelayRestoreMask != 0 || device.safeCalls != 3 ||
		device.settings.OnBrightness != native.DefaultSettings().OnBrightness ||
		device.settings.DisplayBrightness != native.DefaultSettings().DisplayBrightness ||
		device.settings.StatusBrightness != native.DefaultSettings().StatusBrightness ||
		strings.Join(device.melodies, ",") != "power-down,programming-ready" {
		t.Fatalf("post-flash development settings were not safe and audible: %+v", device)
	}
	if device.restoredLive != nil {
		t.Fatalf("incompatible live state was restored: %+v", device.restoredLive)
	}
	if _, err := os.Stat(session.RecoveryMarkerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verified development marker still exists: %v", err)
	}
	if !strings.Contains(output.String(), "old semantic settings were not restored") ||
		!strings.Contains(output.String(), "Silent off") {
		t.Fatalf("data-loss result was not explicit:\n%s", output.String())
	}
}

func TestDevelopmentReinitializationArmsDurableLatchOnlyAfterRawBackup(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{
		LightMode: 2, OnBrightness: 190, OffBrightness: 12,
		DisplayBrightness: 6, StatusBrightness: 128,
		OutputPersistence: native.OutputPersistenceMask,
		RelayRestoreMask:  0xFF, MotionBreakMSValue: 1,
	}
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(native.CapabilityHostFrontPanel),
		settings: original,
	}
	options := ProgrammingLifecycleOptions{
		DataPaths: paths, Wait: noProgrammingWait, ReinitializeEEPROM: true,
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware, options, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(device.stores) != 0 || session.Phase != "development-reinitialize-safe" {
		t.Fatalf("raw-backup preparation changed EEPROM: session=%+v stores=%v", session, device.stores)
	}
	if err := armProgrammingSessionAfterBackup(
		context.Background(), device, session, options, io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if len(device.stores) != 1 ||
		device.stores[0].Flags&(native.SettingsSilent|native.SettingsProgrammingMode) !=
			(native.SettingsSilent|native.SettingsProgrammingMode) ||
		device.stores[0].LightMode != 0 || device.stores[0].OnBrightness != 0 ||
		device.stores[0].StatusBrightness != 0 || device.stores[0].OutputPersistence != 0 ||
		device.stores[0].RelayRestoreMask != 0 ||
		device.stores[0].DisplayClosedBrightness != original.DisplayBrightness ||
		session.Phase != "latched-safe" || !session.SafeStateApplied ||
		!session.TemporarySilent || device.panelShown != 2 {
		t.Fatalf("post-backup latch was not durable/visible: session=%+v device=%+v", session, device)
	}
}

func TestDevelopmentReinitializationDefersUnsupportedLatchToNewFactoryImage(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(0),
		queryErr: errors.New("obsolete settings schema"),
	}
	options := ProgrammingLifecycleOptions{
		DataPaths: paths, Wait: noProgrammingWait, ReinitializeEEPROM: true,
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware, options, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := armProgrammingSessionAfterBackup(
		context.Background(), device, session, options, &output,
	); err != nil {
		t.Fatal(err)
	}
	if len(device.stores) != 0 || len(session.Warnings) == 0 ||
		!strings.Contains(output.String(), "factory EEPROM will arm Prog") {
		t.Fatalf("unsupported latch fallback was not explicit: session=%+v output=%q", session, output.String())
	}
}

func TestFindRetryableProgrammingSessionRequiresExactFailedTransaction(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: document.SourceSHA256,
		SettingsSnapshotPath: filepath.Join(paths.BoardSettingsDir, "captured.json"),
		ReinitializeEEPROM:   true, SafeStateApplied: true,
		Phase: "host-failed", HostResult: "failed",
	}
	markerPath, err := persistProgrammingMarker(paths, session)
	if err != nil {
		t.Fatal(err)
	}
	session.RecoveryMarkerPath = markerPath
	loaded, err := findRetryableProgrammingSession(
		paths, identity, document.SourceSHA256, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.RecoveryMarkerPath != markerPath {
		t.Fatalf("retryable session not recovered: %+v", loaded)
	}
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(
			native.CapabilityHostFrontPanel | native.CapabilityI2CTransfer,
		),
		panelErr: errors.New("panel unavailable"),
		lcdErr:   errors.New("LCD not detected"),
	}
	if err := reassertProgrammingSession(
		context.Background(), device, loaded,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait},
	); err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != "development-reinitialize-safe" || device.safeCalls != 1 ||
		len(loaded.Warnings) != 2 {
		t.Fatalf("retry did not preserve development-safe phase: session=%+v device=%+v", loaded, device)
	}
	if _, err := findRetryableProgrammingSession(
		paths, identity, strings.Repeat("0", 64), true,
	); err == nil || !strings.Contains(err.Error(), "another target") {
		t.Fatalf("mismatched target was accepted: %v", err)
	}
}

func TestFindRetryableProgrammingSessionResumesSafePrewriteMarker(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: document.SourceSHA256,
		SettingsSnapshotPath: filepath.Join(paths.BoardSettingsDir, "captured.json"),
		SafeStateApplied:     true, Phase: "latched-safe",
	}
	markerPath, err := persistProgrammingMarker(paths, session)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := findRetryableProgrammingSession(
		paths, identity, document.SourceSHA256, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.RecoveryMarkerPath != markerPath || loaded.HostResult != "" {
		t.Fatalf("safe pre-write session not recovered: %+v", loaded)
	}
}

func TestFindRetryableProgrammingSessionRejectsIncompletePreparation(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: document.SourceSHA256,
		SafeStateApplied: true, Phase: "display-ready",
	}
	if _, err := persistProgrammingMarker(paths, session); err != nil {
		t.Fatal(err)
	}
	if _, err := findRetryableProgrammingSession(
		paths, identity, document.SourceSHA256, false,
	); err == nil || !strings.Contains(err.Error(), "not safely retryable") {
		t.Fatalf("incomplete preparation was accepted: %v", err)
	}
}

func TestFindRetryableProgrammingSessionAllowsExplicitFactorySupersession(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	oldTarget := strings.Repeat("1", 64)
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: oldTarget,
		SafeStateApplied: true, Phase: "latched-safe",
	}
	markerPath, err := persistProgrammingMarker(paths, session)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := findRetryableProgrammingSession(
		paths, identity, document.SourceSHA256, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.TargetFirmwareSHA256 != document.SourceSHA256 ||
		!loaded.ReinitializeEEPROM || len(loaded.Warnings) != 1 ||
		!strings.Contains(loaded.Warnings[0], oldTarget) {
		t.Fatalf("factory reinitialization did not supersede safe pre-write target: %+v", loaded)
	}
	persisted, err := loadProgrammingMarker(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.TargetFirmwareSHA256 != document.SourceSHA256 || !persisted.ReinitializeEEPROM {
		t.Fatalf("superseded target was not durable: %+v", persisted)
	}
}

func TestFindRetryableProgrammingSessionRejectsOrdinaryTargetSupersession(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	session := &ProgrammingSession{
		Format: programmingMarkerFormat, PreparedAt: time.Now().UTC(),
		Device: identity, TargetFirmwareSHA256: strings.Repeat("2", 64),
		SafeStateApplied: true, Phase: "latched-safe",
	}
	if _, err := persistProgrammingMarker(paths, session); err != nil {
		t.Fatal(err)
	}
	if _, err := findRetryableProgrammingSession(
		paths, identity, document.SourceSHA256, false,
	); err == nil || !strings.Contains(err.Error(), "recover it before starting another target") {
		t.Fatalf("ordinary target supersession was accepted: %v", err)
	}
}

func TestFindRetryableProgrammingSessionUsesNewestMarkerBeforeOlderConflicts(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	content, err := os.ReadFile(firmware)
	if err != nil {
		t.Fatal(err)
	}
	newerFirmware := filepath.Join(t.TempDir(), "newer.hex")
	if err := os.WriteFile(newerFirmware, append(content, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	olderDocument, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	newerDocument, err := programmer.LoadIntelHex(newerFirmware)
	if err != nil {
		t.Fatal(err)
	}
	identity := programmingIdentity(connectedProgrammingSnapshot(0).Port)
	preparedAt := time.Now().UTC()
	for _, session := range []*ProgrammingSession{
		{
			Format: programmingMarkerFormat, PreparedAt: preparedAt.Add(-time.Minute),
			Device: identity, TargetFirmwareSHA256: olderDocument.SourceSHA256,
			SafeStateApplied: true, Phase: "host-failed", HostResult: "failed",
		},
		{
			Format: programmingMarkerFormat, PreparedAt: preparedAt,
			Device: identity, TargetFirmwareSHA256: newerDocument.SourceSHA256,
			SafeStateApplied: true, Phase: "latched-safe", HostResult: "failed",
		},
	} {
		markerPath, persistErr := persistProgrammingMarker(paths, session)
		if persistErr != nil {
			t.Fatal(persistErr)
		}
		session.RecoveryMarkerPath = markerPath
	}

	loaded, err := findRetryableProgrammingSession(
		paths, identity, newerDocument.SourceSHA256, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.TargetFirmwareSHA256 != newerDocument.SourceSHA256 {
		t.Fatalf("newest exact recovery marker not selected: %+v", loaded)
	}
	if _, err := findRetryableProgrammingSession(
		paths, identity, olderDocument.SourceSHA256, false,
	); err == nil || !strings.Contains(err.Error(), newerDocument.SourceSHA256) ||
		!strings.Contains(err.Error(), "newer pending") {
		t.Fatalf("older recovery did not identify the newer target: %v", err)
	}
}

func TestProgrammingLifecycleFailedProgrammerResultRetainsLatchAndMarker(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(0),
		settings: native.Settings{LightMode: 2, DisplayBrightness: 5, MotionBreakMSValue: 1},
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkProgrammingSessionComplete(session, false); err != nil {
		t.Fatal(err)
	}
	err = restoreProgrammingSession(
		context.Background(), device, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "not verified success") {
		t.Fatalf("failed programmer result was allowed to restore: %v", err)
	}
	if session.Phase != "host-failed" || session.HostResult != "failed" ||
		device.settings.Flags&native.SettingsProgrammingMode == 0 {
		t.Fatalf("failed programmer result lost safe state: session=%+v settings=%+v", session, device.settings)
	}
	if _, statErr := os.Stat(session.RecoveryMarkerPath); statErr != nil {
		t.Fatalf("failed programmer result removed recovery marker: %v", statErr)
	}
}

func TestProgrammingLifecycleLiveRestoreFailureRelatchesSafeOutputs(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{LightMode: 2, OnBrightness: 180, DisplayBrightness: 6, MotionBreakMSValue: 1}
	device := &fakeProgrammingDevice{
		snapshot:   connectedProgrammingSnapshot(0),
		settings:   original,
		restoreErr: errors.New("relay verification failed"),
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkProgrammingSessionComplete(session, true); err != nil {
		t.Fatal(err)
	}
	err = restoreProgrammingSession(
		context.Background(), device, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "safety latch and recovery marker retained") {
		t.Fatalf("live-restore failure did not report retained safety: %v", err)
	}
	if session.Phase != "restore-failed-safe" || device.safeCalls != 1 ||
		device.settings.Flags&native.SettingsProgrammingMode == 0 ||
		device.settings.Flags&native.SettingsSilent == 0 ||
		device.settings.LightMode != 0 {
		t.Fatalf("restore failure did not relatch safe state: session=%+v device=%+v", session, device)
	}
	if _, statErr := os.Stat(session.RecoveryMarkerPath); statErr != nil {
		t.Fatalf("restore failure removed recovery marker: %v", statErr)
	}
}

func programmingLifecycleFixture(t *testing.T) (programmer.HostDataPaths, string) {
	t.Helper()
	paths, err := programmer.HostDataPathsFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := programmer.EnsureHostDataPaths(paths); err != nil {
		t.Fatal(err)
	}
	firmware := filepath.Join(t.TempDir(), "firmware.hex")
	if err := os.WriteFile(
		firmware,
		[]byte(":020000000102FB\n:00000001FF\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return paths, firmware
}

func connectedProgrammingSnapshot(capabilities uint32) Snapshot {
	return Snapshot{
		Connected: true,
		Port: ports.Info{
			Name: "COM18", VID: "1A86", PID: "7523", SerialNumber: "BOARD-1",
		},
		Hello: native.Hello{
			BoardKind: native.BoardKindPCController,
			Name:      "PCController", Capabilities: capabilities, BuildHash: 0x12345678,
		},
	}
}

func noProgrammingWait(context.Context, time.Duration) error { return nil }
