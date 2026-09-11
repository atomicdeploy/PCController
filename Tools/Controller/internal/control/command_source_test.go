package control

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func TestCommandEvidencePreservesBackgroundSourceAndTimestamp(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx := WithBackgroundCommand(parent)
	deadline, _ := ctx.Deadline()
	wantDeadline, _ := parent.Deadline()
	if deadline != wantDeadline || CommandSourceFromContext(parent) != "" {
		t.Fatal("source annotation changed parent context or deadline")
	}
	payload := []byte{1, 2, 3, 4}
	frame := native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpStatusRGB, 0, 0x78, 0x56, 0x34, 0x12}}
	evidence := acknowledgedCommandEvidence(ctx, native.OpStatusRGB, payload, frame)
	payload[0] = 99
	if evidence.Source != CommandSourceBackground || !evidence.Timed || evidence.DeviceMicros != 0x12345678 || evidence.Payload[0] != 1 || evidence.ObservedAt.IsZero() {
		t.Fatalf("acknowledged evidence lost source, clock or payload ownership: %#v", evidence)
	}
	if explicit := acknowledgedCommandEvidence(parent, native.OpStatusRGB, payload, frame); explicit.Source != "" {
		t.Fatalf("explicit RGB was labelled background: %#v", explicit)
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("source annotation hid cancellation")
	}
}

func TestMCURecorderIgnoresBackgroundRGBButPreservesExplicitCommands(t *testing.T) {
	for _, explicitRGB := range []bool{false, true} {
		name := "two displays"
		if explicitRGB {
			name = "explicit RGB between displays"
		}
		t.Run(name, func(t *testing.T) {
			config := appconfig.Defaults()
			runner := macroTestRunner(&config, nil)
			if _, err := runner.StartMCURecording("clean-mcu", "tests", "green"); err != nil {
				t.Fatal(err)
			}
			publish := func(ctx context.Context, opcode byte, payload []byte, at uint32) {
				ack := native.Frame{Opcode: native.OpACK, Payload: []byte{opcode, 0, 0, 0, 0, 0}}
				binary.LittleEndian.PutUint32(ack.Payload[2:], at)
				runner.runtime.publishCommandEvidence(acknowledgedCommandEvidence(ctx, opcode, payload, ack))
			}
			background := WithBackgroundCommand(context.Background())
			rgb := native.StatusRGBPayload(10, 20, 30, 40)
			base := uint32(0xFFFFFF00)
			// Incidental frames before and between intentional commands must not
			// select the recording epoch or grow the take, even across clock wrap.
			for index := uint32(0); index < 250; index++ {
				publish(background, native.OpStatusRGB, rgb, base-250+index)
			}
			first, _ := native.DisplayTextPayload(native.DisplaySegments, 400, "M001")
			publish(context.Background(), native.OpDisplayText, first, base)
			for index := uint32(1); index < 500; index++ {
				publish(background, native.OpStatusRGB, rgb, base+index)
				if explicitRGB && index == 250 {
					publish(context.Background(), native.OpStatusRGB, rgb, base+index)
				}
			}
			second, _ := native.DisplayTextPayload(native.DisplaySegments, 400, "M002")
			publish(context.Background(), native.OpDisplayText, second, base+500)
			macro, err := runner.StopRecording(true)
			if err != nil {
				t.Fatal(err)
			}
			wantSteps := 2
			if explicitRGB {
				wantSteps = 3
				if len(macro.Steps) != wantSteps || macro.Steps[1].Kind != "rgb" || macro.Steps[1].AtUS != 250 {
					t.Fatalf("explicit user RGB was filtered or mistimed: %#v", macro.Steps)
				}
			}
			if len(macro.Steps) != wantSteps || macro.Mode != macroModeMCU || macro.TimingToleranceUS != 2500 || macro.Steps[0].AtUS != 0 || macro.Steps[wantSteps-1].AtUS != 500 {
				t.Fatalf("background traffic contaminated MCU take or shifted deltas: %#v", macro)
			}
			if _, err := compileMacro(macro); err != nil {
				t.Fatalf("saved MCU take cannot compile: %v", err)
			}
		})
	}
}
