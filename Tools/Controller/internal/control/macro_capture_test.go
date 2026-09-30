package control

import (
	"context"
	"encoding/binary"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"strings"
	"testing"
	"time"
)

func TestHostRecordingGuardsStripClockAndRawUART(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("protected", "test", "green"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.runtime.Request(context.Background(), native.OpAddressableLED, []byte{0}); err == nil || !strings.Contains(err.Error(), "WS2811") {
		t.Fatalf("strip guard: %v", err)
	}
	if err := runner.runtime.WriteRaw([]byte{0}); err == nil || !strings.Contains(err.Error(), "raw UART") {
		t.Fatalf("raw guard: %v", err)
	}
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.runtime.Request(context.Background(), native.OpAddressableLED, []byte{0}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("guard not released: %v", err)
	}
}

func TestHostCaptureUsesAppliedRelayClockNotUSBArrival(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("RF and PC", "test", "green"); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	runner.captureCommand(CommandEvidence{Opcode: native.OpRelaySet, Payload: []byte{4, 1}, ObservedAt: base})
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 16, Timed: true, DeviceMicros: 0xffffff00, ObservedAt: base.Add(200 * time.Millisecond)})
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 0, Timed: true, DeviceMicros: 0x000002e8, ObservedAt: base.Add(500 * time.Millisecond)})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(macro.Steps) != 2 || macro.Steps[1].AtUS != 1000 || macro.Steps[1].Kind != "relay-mask" || macro.Steps[1].Value != 0 {
		t.Fatalf("edge timing/duplicate: %#v", macro)
	}
}

func TestDecodeRelayCaptureBaselineOffBeforeOnAndExactDelta(t *testing.T) {
	raw := make([]byte, 15)
	raw[4] = 16
	binary.LittleEndian.PutUint32(raw[5:9], 12345)
	raw[9] = 32
	binary.LittleEndian.PutUint32(raw[10:14], 23456)
	steps, err := decodeRelayCapture(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 || steps[0].Value != 16 || steps[1].Value != 32 || steps[1].AtUS != 12345 || steps[2].Value != 0 || steps[2].AtUS != 23456 {
		t.Fatalf("steps=%#v", steps)
	}
	binary.LittleEndian.PutUint32(raw[10:14], 1)
	if _, err := decodeRelayCapture(raw); err == nil {
		t.Fatal("unordered capture accepted")
	}
}

func TestDecodeRelayCaptureRejectsMalformedChunks(t *testing.T) {
	for _, raw := range [][]byte{nil, {0}, {0, 0, 0, 128, 0}} {
		if _, err := decodeRelayCapture(raw); err == nil {
			t.Fatalf("accepted %v", raw)
		}
	}
}

func TestDecodeOverwrittenTailStartsAtZero(t *testing.T) {
	raw := []byte{0x10, 0x27, 0, 0, 16, 0x04, 0x29, 0, 0, 0}
	steps, err := decodeRelayCapture(raw)
	if err != nil || len(steps) != 2 || steps[0].AtUS != 0 || steps[1].AtUS != 500 {
		t.Fatalf("steps=%#v err=%v", steps, err)
	}
}

func TestRelayMaskSemanticEvidencePreservesExactMask(t *testing.T) {
	cinema := relayMaskSteps(1200, 0, 0b00101111, false, appconfig.BoardModeCinemaSeatMotion)
	if len(cinema) != 1 || cinema[0].Value != 0b00101111 || cinema[0].AtUS != 1200 {
		t.Fatalf("cinema step=%+v", cinema)
	}
	wantCinema := []string{"seat.a.down", "seat.b.down", "relay.6.on"}
	if strings.Join(cinema[0].ActionIDs, ",") != strings.Join(wantCinema, ",") {
		t.Fatalf("cinema actions=%v, want %v", cinema[0].ActionIDs, wantCinema)
	}
	ordinary := relayMaskSteps(10, 0b00000001, 0b10000000, false, appconfig.BoardModeOrdinaryRelays)
	wantOrdinary := []string{"relay.1.off", "relay.8.on"}
	if len(ordinary) != 1 || ordinary[0].Value != 0b10000000 || strings.Join(ordinary[0].ActionIDs, ",") != strings.Join(wantOrdinary, ",") {
		t.Fatalf("ordinary step=%+v", ordinary)
	}
	unconfigured := relayMaskSteps(10, 0, 1, false, appconfig.BoardModeUnconfigured)
	if len(unconfigured) != 1 || len(unconfigured[0].ActionIDs) != 0 {
		t.Fatalf("unconfigured step=%+v", unconfigured)
	}
}

func TestDecodedBoardCaptureCarriesConfiguredSemanticEvidence(t *testing.T) {
	raw := make([]byte, 10)
	raw[4] = 0
	binary.LittleEndian.PutUint32(raw[5:9], 500)
	raw[9] = 0b00000010
	steps, err := decodeRelayCapture(raw, appconfig.BoardModeCinemaSeatMotion)
	if err != nil || len(steps) != 2 || steps[1].AtUS != 500 || strings.Join(steps[1].ActionIDs, ",") != "seat.a.up" {
		t.Fatalf("steps=%+v err=%v", steps, err)
	}
}

func TestPeripheralCatalogChangesUseStateStream(t *testing.T) {
	if got := EventStreamForKind("peripherals.changed"); got != EventStreamState {
		t.Fatalf("stream=%q, want %q", got, EventStreamState)
	}
}

func TestRecordingBindsProfileAndAnnotatesAppliedRelayEvidence(t *testing.T) {
	config := appconfig.Defaults()
	config.Connection.LastDevice = &appconfig.DeviceIdentity{Port: "COM18", SerialNumber: "BOARD-42"}
	config.BoardProfiles = map[string]appconfig.BoardProfile{
		"serial:board-42": {Key: "cafe-cinema", Mode: appconfig.BoardModeCinemaSeatMotion},
	}
	runner := macroTestRunner(&config, nil)
	state, err := runner.StartRecording("seats", "cinema", "violet")
	if err != nil {
		t.Fatal(err)
	}
	if state.BoardProfileKey != "cafe-cinema" || state.BoardProfileMode != appconfig.BoardModeCinemaSeatMotion {
		t.Fatalf("recording profile=%+v", state)
	}
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 0b00000010, Timed: true, DeviceMicros: 100, ObservedAt: time.Now()})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.BoardProfileKey != "cafe-cinema" || macro.BoardProfileMode != appconfig.BoardModeCinemaSeatMotion ||
		len(macro.Steps) != 1 || strings.Join(macro.Steps[0].ActionIDs, ",") != "seat.a.up,seat.b.stop,relay.5.off,relay.6.off,relay.7.off,relay.8.off" {
		t.Fatalf("recorded macro=%+v", macro)
	}
}
