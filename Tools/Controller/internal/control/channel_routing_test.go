package control

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestRoutedOutputUsesExactPhysicalTargetAndGeneration(t *testing.T) {
	runtime, port := runtimeForRelayConfirmationTest(t, true)
	if err := runtime.SetRoutedOutput(context.Background(), 1, "relay.6", 1); err != nil {
		t.Fatal(err)
	}
	if port.active != 1<<5 {
		t.Fatalf("wrong physical relay: %02x", port.active)
	}
	if err := runtime.SetRoutedOutput(context.Background(), 2, "relay.5", 1); err == nil {
		t.Fatal("replacement generation accepted")
	}
	if port.active != 1<<5 {
		t.Fatal("stale generation actuated output")
	}
	if err := runtime.SetRoutedOutput(context.Background(), 1, "relay.1", 1); err == nil {
		t.Fatal("interlocked relay admitted")
	}
	if err := runtime.SetRoutedOutput(context.Background(), 1, "pwm.12", 100); err == nil {
		t.Fatal("autonomous PWM admitted")
	}
	if err := runtime.SetRoutedOutput(context.Background(), 1, "relay.6", 0); err != nil {
		t.Fatal(err)
	}
	if port.active != 0 {
		t.Fatal("off did not apply")
	}
}

func TestRoutedOutputPWMFramedConfirmationAndSafety(t *testing.T) {
	for _, mode := range []string{"confirmed", "mismatch", "replacement", "emergency", "canceled", "stale"} {
		t.Run(mode, func(t *testing.T) {
			runtime := New(Options{})
			var mu sync.Mutex
			writes, reads := 0, 0
			var applied uint16
			port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
			port.handle = func(frame native.Frame) (native.Frame, error) {
				mu.Lock()
				defer mu.Unlock()
				switch frame.Opcode {
				case native.OpPWMSet:
					if len(frame.Payload) != 3 || frame.Payload[0] != 3 {
						return native.Frame{}, fmt.Errorf("wrong channel: %v", frame.Payload)
					}
					writes++
					if mode != "mismatch" {
						applied = binary.LittleEndian.Uint16(frame.Payload[1:])
					}
					return native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}}, nil
				case native.OpPWMGet:
					reads++
					if mode == "replacement" {
						runtime.mu.Lock()
						runtime.generation++
						runtime.mu.Unlock()
					}
					payload := make([]byte, 34)
					payload[0] = 1
					binary.LittleEndian.PutUint16(payload[8:], applied)
					return native.Frame{Opcode: native.OpPWMValues, Payload: payload}, nil
				}
				return native.Frame{}, fmt.Errorf("unexpected opcode %d", frame.Opcode)
			}
			session := link.NewForPort("fixture", port)
			runtime.session, runtime.generation = session, 7
			t.Cleanup(func() { _ = session.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			generation := uint64(7)
			if mode == "emergency" {
				runtime.emergencyStop.Store(true)
			}
			if mode == "canceled" {
				cancel()
			}
			if mode == "stale" {
				generation = 6
			}
			err := runtime.SetRoutedOutput(ctx, generation, "pwm.3", 1234)
			mu.Lock()
			observedWrites, observedReads := writes, reads
			mu.Unlock()
			if mode == "confirmed" {
				if err != nil || observedWrites != 1 || observedReads != 1 {
					t.Fatalf("confirmation writes%d reads%d %v", observedWrites, observedReads, err)
				}
				if err := runtime.SetRoutedOutput(ctx, generation, "pwm.3", 0); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe success")
			}
			if (mode == "emergency" || mode == "canceled" || mode == "stale") && observedWrites != 0 {
				t.Fatal("blocked operation wrote hardware")
			}
		})
	}
}

func TestRoutedOutputBusyGateDoesNotExpandCatalog(t *testing.T) {
	runtime := New(Options{})
	for _, mask := range []uint32{activeUseMacroPlayback, activeUseMacroRecording} {
		runtime.activeUseMask.Store(mask)
		if !runtime.ChannelRoutingBusy() {
			t.Fatal("active recorder/player omitted")
		}
	}
	runtime.activeUseMask.Store(0)
	if runtime.ChannelRoutingBusy() {
		t.Fatal("idle reported busy")
	}
}

func TestRoutedOutputDoesNotTreatACKAsConfirmedValue(t *testing.T) {
	runtime, _ := runtimeForRelayConfirmationTest(t, false)
	if err := runtime.SetRoutedOutput(context.Background(), 1, "relay.5", 1); err == nil {
		t.Fatal("incorrect relay readback accepted")
	}
}
