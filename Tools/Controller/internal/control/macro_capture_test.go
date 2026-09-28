package control

import (
	"encoding/binary"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

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
	if len(macro.Steps) != 9 || macro.Steps[8].AtUS != 1000 || macro.Steps[8].Target != 4 || macro.Steps[8].Value != 0 {
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
	if len(steps) != 11 || steps[8].Target != 4 || steps[8].Value != 0 || steps[9].Target != 5 || steps[9].Value != 1 || steps[9].AtUS != 12345 || steps[10].AtUS != 23456 {
		t.Fatalf("steps=%#v", steps)
	}
	binary.LittleEndian.PutUint32(raw[10:14], 1)
	if _, err := decodeRelayCapture(raw); err == nil {
		t.Fatal("unordered capture accepted")
	}
}

func TestDecodeRelayCaptureRejectsMalformedChunks(t *testing.T) {
	for _, raw := range [][]byte{nil, {0}, {1, 0, 0, 0, 0}, {0, 0, 0, 128, 0}} {
		if _, err := decodeRelayCapture(raw); err == nil {
			t.Fatalf("accepted %v", raw)
		}
	}
}
