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
}

func (device *fakeProgrammingDevice) Snapshot() Snapshot { return device.snapshot }

func (device *fakeProgrammingDevice) QuerySettings(context.Context) (native.Settings, error) {
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
	return nil
}

func (device *fakeProgrammingDevice) ShowProgrammingPanel(context.Context) error {
	device.panelShown++
	return device.panelErr
}

func (device *fakeProgrammingDevice) ReleaseProgrammingPanel(context.Context) error {
	device.panelReleased++
	return device.panelErr
}

func (device *fakeProgrammingDevice) RenderProgrammingLCD(
	_ context.Context,
	line1, line2 string,
) error {
	device.lcdLines = append(device.lcdLines, [2]string{line1, line2})
	return device.lcdErr
}

func TestProgrammingLifecycleSnapshotsMutesWaitsRestoresAndVerifies(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{
		LightMode: 1, OnBrightness: 200, DisplayBrightness: 5,
		StatusBrightness: 128, PWMBootMode: 2, StreamPeriodMS: 250,
		DefaultPage: 4,
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
		!session.TemporarySilent || device.panelShown != 1 || len(device.lcdLines) != 1 {
		t.Fatalf("preparation session=%+v device=%+v", session, device)
	}
	if len(waits) != 1 || waits[0] != ProgrammingSettingsPersistenceDelay {
		t.Fatalf("preparation waits=%v", waits)
	}
	if _, err := os.Stat(session.SettingsSnapshotPath); err != nil {
		t.Fatalf("settings snapshot missing: %v", err)
	}
	if _, err := os.Stat(session.RecoveryMarkerPath); err != nil {
		t.Fatalf("recovery marker missing: %v", err)
	}
	if err := restoreProgrammingSession(
		context.Background(), device, session, options, io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if device.settings != original || len(device.stores) != 2 || device.panelReleased != 1 ||
		len(waits) != 2 || len(device.lcdLines) != 2 {
		t.Fatalf("restore device=%+v waits=%v", device, waits)
	}
	if _, err := os.Stat(session.RecoveryMarkerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed recovery marker still exists: %v", err)
	}
}

func TestProgrammingLifecycleOptionalFeaturesAreNonfatal(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{Flags: native.SettingsSilent, LightMode: 1, DisplayBrightness: 5}
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
		len(device.stores) != 0 {
		t.Fatalf("unsupported optional features were invoked: session=%+v device=%+v", session, device)
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
		settings: native.Settings{LightMode: 1, DisplayBrightness: 5},
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
		loaded.SettingsSnapshotPath == "" {
		t.Fatalf("failure marker cannot restore original board settings: %+v", loaded)
	}
}

func TestProgrammingLifecycleReconnectRecoversPendingMarker(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	original := native.Settings{LightMode: 2, OffBrightness: 3, DisplayBrightness: 6}
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
		settings: native.Settings{Flags: native.SettingsSilent, DisplayBrightness: 1},
	}
	loaded, err := loadProgrammingMarker(session.RecoveryMarkerPath)
	if err != nil {
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

func TestProgrammingLifecycleSettingsQueryFailureKeepsRawBackupPathUsable(t *testing.T) {
	paths, firmware := programmingLifecycleFixture(t)
	device := &fakeProgrammingDevice{
		snapshot: connectedProgrammingSnapshot(0),
		queryErr: errors.New("unsupported opcode"),
	}
	session, err := prepareProgrammingSession(
		context.Background(), device, firmware,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if session.OriginalSettings != nil || len(session.Warnings) != 1 ||
		!strings.Contains(session.Warnings[0], "raw EEPROM backup") {
		t.Fatalf("unexpected reduced-peer session: %+v", session)
	}
	device.queryErr = nil
	if err := restoreProgrammingSession(
		context.Background(), device, session,
		ProgrammingLifecycleOptions{DataPaths: paths, Wait: noProgrammingWait}, io.Discard,
	); err != nil {
		t.Fatal(err)
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
