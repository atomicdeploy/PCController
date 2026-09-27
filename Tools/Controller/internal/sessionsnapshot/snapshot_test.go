package sessionsnapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controller "pccontroller.local/controller"
)

type fakeSource struct {
	snapshot controller.Snapshot
	timeline []controller.TimelineEntry
	output   controller.OutputStreamState
}

func (source *fakeSource) Snapshot() controller.Snapshot { return source.snapshot }

func (source *fakeSource) Timeline(_ time.Time, limit int) []controller.TimelineEntry {
	values := append([]controller.TimelineEntry(nil), source.timeline...)
	if limit > 0 && len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}

func (source *fakeSource) OutputState() controller.OutputStreamState { return source.output }

func TestBuildCapturesUsefulStateWithoutSecretsOrReplayableRF(t *testing.T) {
	now := time.Date(2026, 8, 2, 13, 14, 16, 0, time.UTC)
	secret := "do-not-persist-this-password"
	source := &fakeSource{
		snapshot: controller.Snapshot{
			Connected: true,
			Port: controller.PortInfo{
				Name: "COM18", VID: "1A86", PID: "7523",
				SerialNumber: "BOARD-01", FriendlyName: "USB Serial Device",
				InstanceID: "USB\\VID_1A86&PID_7523\\BOARD-01",
			},
			Hello: controller.Hello{
				BoardKind: 1, Name: "PCController", IdentitySchema: 4,
				Capabilities: 0x12345678, BuildHash: 0xAABBCCDD,
				BuildTimestamp: 0x3456789A, BuildStamp: "260802131416",
			},
			Status: controller.Status{
				UptimeMS: 123456, SupplyMV: 12270, BusMV: 12262,
				CurrentMA: 73, PowerMW: 895, TLEDCenti: 2989,
				TBTCenti: 2599, Hot: false, DoorOpen: true,
				ActiveRelays: 0x50, PWMAvailable: true,
				PWMChannel: 11, PWMValue: 2048, PWMErrors: 2,
			},
			HaveStatus: true, StatusUpdated: now.Add(-time.Second),
			Settings: controller.Settings{
				Flags: 1, LightMode: 2, OnBrightness: 200,
				DisplayBrightness: 5, DefaultPage: 0,
			},
			HaveSettings: true,
			FrontPanel: controller.FrontPanel{
				Schema: 2, RawSegments: [4]byte{0x5E, 0x5C, 0x50, 0x50},
				Brightness: 5, SegmentsActive: true,
				LCDAvailable: true, LCDAddress: 0x27,
				LCDLine1: "password=" + secret, LCDLine2: "token=also-secret",
				PressedKeys: 3, MenuPage: 4, HostCaptured: true,
			},
			HaveFrontPanel: true, FrontPanelUpdated: now.Add(-500 * time.Millisecond),
			ConnectionState: "connected", ConnectionReason: secret,
			ConnectionUpdated: now.Add(-time.Minute),
			RFLearning: controller.RFLearnState{
				Active: true, Mode: controller.RFLearnIndefinite,
				StartedAt: now.Add(-time.Minute), Learned: 2,
			},
		},
		timeline: []controller.TimelineEntry{
			{
				ID: 8, Time: now.Add(-3 * time.Second), Kind: "door",
				State: "open", Text: secret, Reason: secret,
				Metadata: map[string]string{
					"page": "door", "auth_token": secret, "command": secret,
					"state": "password=" + secret,
				},
			},
			{
				ID: 9, Time: now.Add(-2 * time.Second), Kind: "rf.received",
				Source: "rf", RFCode: 0xDEADBEEF, RFBits: 24, RFProtocol: 1, RFPulseUS: 350,
				Metadata: map[string]string{"relay": "R5", "remote_code": secret},
			},
			// A duplicate event ID must not create duplicate diagnostic records.
			{ID: 9, Time: now.Add(-time.Second), Kind: "rf.repeat", RFCode: 0xDEADBEEF, RFBits: 24, RFProtocol: 1, RFPulseUS: 350},
			{ID: 10, Time: now, Kind: "relay", Action: "on", Metadata: map[string]string{"relay": "R5"}},
		},
		output: controller.OutputStreamState{
			EffectID: 7, EffectName: secret, StatusBase: [4]byte{0, 32, 0, 8}, HaveStatusBase: true,
		},
	}

	document := Build(source, HostIdentity{
		Title: "Control Center", Role: "primary-host", Version: "1.2.3",
		SourceHash: "0123456789abcdef", BuildTime: "2026-08-02T13:00:00Z",
	}, now)
	if !document.Complete || !document.Completeness.Hello ||
		!document.Completeness.Status || !document.Completeness.Settings ||
		!document.Completeness.FrontPanel || !document.Completeness.PWM ||
		!document.Completeness.Temperatures || !document.Completeness.RF {
		t.Fatalf("unexpected completeness: %#v errors=%#v", document.Completeness, document.Errors)
	}
	if document.ContainsHostConfig || document.ContainsMCUEEPROMImage ||
		document.SettingsSource != "board-reported-live-cache" {
		t.Fatalf("storage boundaries drifted: %#v", document)
	}
	if document.Hello == nil || document.Hello.BuildHash != 0xAABBCCDD ||
		document.Hello.BuildTimestamp != 0x3456789A {
		t.Fatalf("HELLO identity missing: %#v", document.Hello)
	}
	if document.PWM == nil || document.PWM.SelectedChannel != 11 ||
		document.PWM.SelectedValue != 2048 || document.Temperatures == nil ||
		document.Temperatures.IlluminationCentiC != 2989 {
		t.Fatalf("live summaries missing: pwm=%#v temperatures=%#v", document.PWM, document.Temperatures)
	}
	if document.FrontPanel == nil || !document.FrontPanel.LCDTextPresent ||
		!document.FrontPanel.LCDTextOmitted {
		t.Fatalf("front-panel privacy marker missing: %#v", document.FrontPanel)
	}
	if document.LastImportantEventID != 10 || len(document.RecentImportantEvents) != 3 {
		t.Fatalf("event deduplication failed: last=%d events=%#v", document.LastImportantEventID, document.RecentImportantEvents)
	}
	if len(document.RF.Observations) != 1 || document.RF.Observations[0].CodeFingerprint == "" ||
		document.RF.Observations[0].PulseUS != 350 {
		t.Fatalf("RF summary was not fingerprinted and deduplicated: %#v", document.RF)
	}
	content, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(content)
	for _, forbidden := range []string{secret, "3735928559", "DEADBEEF", "LCDLine1", "auth_token"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("snapshot retained forbidden content %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, `"page":"door"`) || !strings.Contains(encoded, `"relay":"R5"`) {
		t.Fatalf("safe event metadata was not retained: %s", encoded)
	}
}

func TestRecorderAtomicallyReplacesRollingFileAndSavesOnce(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state", "last-session.json")
	source := &fakeSource{
		snapshot: controller.Snapshot{
			Hello:      controller.Hello{BoardKind: 1, Name: "Board", BuildHash: 1},
			HaveStatus: true, Status: controller.Status{SupplyMV: 12000},
			HaveSettings: true, Settings: controller.Settings{DefaultPage: 1},
			ConnectionState: "disconnected",
		},
	}
	identityCalls := 0
	recorder, err := NewRecorder(path, source, func() HostIdentity {
		identityCalls++
		return HostIdentity{Title: "Controller", Role: "primary", SourceHash: "abc"}
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time {
		return time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)
	}
	first, err := recorder.Save()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := recorder.Save()
	if err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if identityCalls != 1 || first != second || string(content) != string(again) {
		t.Fatalf("idempotent save drifted: calls=%d first=%#v second=%#v", identityCalls, first, second)
	}
	if first.Path != path || first.Bytes != int64(len(content)) || len(first.SHA256) != 64 || !first.Complete {
		t.Fatalf("unexpected save result: %#v", first)
	}
	stored, err := recorder.Stored()
	if err != nil || !stored.Exists || stored.Snapshot == nil || stored.Snapshot.Status.SupplyMV != 12000 {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	// A new graceful session replaces the same durable file, including on Windows.
	source.snapshot.Status.SupplyMV = 12123
	replacement, err := NewRecorder(path, source, func() HostIdentity {
		return HostIdentity{Title: "Controller", Role: "primary", SourceHash: "def"}
	})
	if err != nil {
		t.Fatal(err)
	}
	replacement.now = func() time.Time {
		return time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	}
	if _, err := replacement.Save(); err != nil {
		t.Fatal(err)
	}
	stored, err = replacement.Stored()
	if err != nil || stored.Snapshot == nil || stored.Snapshot.Status.SupplyMV != 12123 ||
		stored.Snapshot.Host.SourceHash != "def" {
		t.Fatalf("atomic replacement was not visible: stored=%#v err=%v", stored, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".session-snapshot-") {
			t.Fatalf("temporary file survived atomic replacement: %s", entry.Name())
		}
	}
}

func TestPartialSnapshotReportsCompletenessAndStrictRead(t *testing.T) {
	document := Build(&fakeSource{}, HostIdentity{Title: "Controller"}, time.Now())
	if document.Complete || len(document.Errors) != 3 || document.Completeness.Status {
		t.Fatalf("partial snapshot did not report explicit errors: %#v", document)
	}
	if _, err := NewRecorder("relative.json", &fakeSource{}, func() HostIdentity { return HostIdentity{} }); err == nil {
		t.Fatal("relative snapshot destination was accepted")
	}
	path := filepath.Join(t.TempDir(), "last-session.json")
	if err := os.WriteFile(path, []byte(`{"format":"pccontroller.host-diagnostic-snapshot","schema":1,"captured_at":"2026-08-02T00:00:00Z","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("snapshot with an unknown field was accepted")
	}
	unsafe := Build(&fakeSource{}, HostIdentity{Title: "Controller"}, time.Now())
	unsafe.InterruptedWriteProven = true
	content, err := encode(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil || !strings.Contains(err.Error(), "recovery safety") {
		t.Fatalf("unsafe interrupted-write claim was accepted: %v", err)
	}
}

func TestRecorderCapturesOperationalHashesAndProvidesSafeRecoveryInput(t *testing.T) {
	now := time.Date(2026, 8, 2, 16, 30, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "last-session.json")
	source := &fakeSource{snapshot: controller.Snapshot{
		Hello:      controller.Hello{BoardKind: 1, Name: "Board", BuildHash: 0xAABBCCDD, BuildTimestamp: 0x11223344},
		HaveStatus: true, Status: controller.Status{SupplyMV: 12000},
		HaveSettings: true, Settings: controller.Settings{DefaultPage: 1},
		ConnectionState: "connected",
	}}
	recorder, err := NewRecorder(path, source, func() HostIdentity {
		return HostIdentity{Title: "Controller", Role: "primary", SourceHash: "host-source"}
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.now = func() time.Time { return now }
	firmwareHash := strings.Repeat("a", 64)
	eepromHash := strings.Repeat("b", 64)
	markerHash := strings.Repeat("c", 64)
	settingsHash := strings.Repeat("d", 64)
	deviceHash := strings.Repeat("e", 64)
	providerCalls := 0
	if err := recorder.SetOperationalContextProvider(func() (OperationalContext, error) {
		providerCalls++
		return OperationalContext{
			Programming: &ProgrammingOperation{
				ID: "op-fixture", Kind: "firmware", State: "programming", Active: true,
				ProgressPercent: 45, ArtifactSHA256: firmwareHash,
				ProgrammingMethod: "urclock", StartedAt: now.Add(-time.Minute), UpdatedAt: now,
			},
			Artifacts: ArtifactHashes{
				CurrentFirmwareSHA256: firmwareHash, CurrentEEPROMSHA256: eepromHash,
			},
			RecoveryMarkers: []RecoveryMarker{{
				MarkerSHA256: markerHash, TargetFirmwareSHA256: firmwareHash,
				SettingsSnapshotSHA256: settingsHash, DeviceFingerprint: deviceHash,
				PreparedAt: now.Add(-2 * time.Minute), Phase: "latched-safe",
				DiagnosticState: "programming-incomplete", RestorationPending: true,
			}},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Save(); err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 {
		t.Fatalf("operational provider calls = %d", providerCalls)
	}
	stored, err := recorder.Stored()
	if err != nil || stored.Snapshot == nil {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	document := stored.Snapshot
	if document.Schema != Schema || !document.Completeness.Programming ||
		!document.Completeness.Artifacts || !document.Completeness.Recovery ||
		document.Programming == nil || !document.Programming.Active ||
		document.Artifacts.CurrentFirmwareSHA256 != firmwareHash ||
		len(document.RecoveryMarkers) != 1 || document.RecoveryMarkers[0].WriteCompletionProven ||
		!document.RecoveryDiagnosticOnly || document.InterruptedWriteProven {
		t.Fatalf("operational snapshot drifted: %#v", document)
	}
	input, err := ConsumeRecoveryDiagnosticSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if input.WriteCompletionProven || input.BoardBuildHash != 0xAABBCCDD ||
		input.Artifacts.CurrentEEPROMSHA256 != eepromHash ||
		input.Programming == nil || len(input.RecoveryMarkers) != 1 {
		t.Fatalf("unsafe or incomplete recovery input: %#v", input)
	}
}
