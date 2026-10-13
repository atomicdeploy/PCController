package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestPWMWriteTraceIsBoundedExplicitAndDebugOnly(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	payload := []byte{1, 0xEF, 0}
	ack := native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0, 0x78, 0x56, 0x34, 0x12}}
	if runtime.pwmTraceAdmission(native.OpPWMSet, payload) != 0 {
		t.Fatal("trace enabled by default")
	}
	if _, err := runtime.StartPWMWriteTrace(1, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ctx           context.Context
		source, actor string
	}{
		{WithPWMTraceOrigin(context.Background(), "controller.pwm.set", "fixture-writer", "http"), "rpc", "fixture-writer"},
		{WithBackgroundCommand(context.Background()), "background", ""},
		{context.Background(), "unknown", ""},
	} {
		epoch := runtime.pwmTraceAdmission(native.OpPWMSet, payload)
		runtime.observePWMWriteAck(test.ctx, epoch, 7, native.OpPWMSet, payload, ack)
		snapshot := runtime.PWMWriteTrace()
		record := snapshot.Records[len(snapshot.Records)-1]
		if record.Source != test.source || record.ActorID != test.actor || record.RawValue != 239 || record.ConnectionGeneration != 7 || !record.Timed || record.DeviceMicros != 0x12345678 {
			t.Fatalf("lost provenance %+v", record)
		}
	}
	for index := 0; index < 70; index++ {
		epoch := runtime.pwmTraceAdmission(native.OpPWMSet, payload)
		runtime.observePWMWriteAck(context.Background(), epoch, 7, native.OpPWMSet, payload, ack)
	}
	snapshot := runtime.PWMWriteTrace()
	if snapshot.Total != 73 || snapshot.Dropped != 9 || len(snapshot.Records) != 64 || snapshot.Records[0].Sequence != 10 {
		t.Fatalf("unbounded or reordered trace %+v", snapshot)
	}
	snapshot.Records[0].RawValue = 999
	if runtime.PWMWriteTrace().Records[0].RawValue != 239 {
		t.Fatal("snapshot aliases ring")
	}
	if EventStreamForKind("pwm.write.ack") != EventStreamDebug {
		t.Fatal("trace leaked into normal activity")
	}
	seenDebug := false
	for len(runtime.Events()) > 0 {
		event := <-runtime.Events()
		if event.Kind == "pwm.write.ack" {
			seenDebug = true
			if event.Stream != EventStreamDebug || event.Frame.Opcode != native.OpPWMSet || event.Metadata["capture_id"] == "" {
				t.Fatalf("trace not opcode-filterable DEBUG: %+v", event)
			}
		}
	}
	if !seenDebug {
		t.Fatal("no diagnostic event")
	}
	runtime.StopPWMWriteTrace()
	if runtime.PWMWriteTrace().Active || runtime.pwmTraceAdmission(native.OpPWMSet, payload) != 0 {
		t.Fatal("stop failed")
	}
}

func TestPWMWriteTraceFramedTransportOnlyRecordsAcceptedCurrentWrites(t *testing.T) {
	for _, mode := range []string{"accepted", "write failure", "canceled", "replaced owner"} {
		t.Run(mode, func(t *testing.T) {
			runtime := New(Options{})
			port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
			port.handle = func(frame native.Frame) (native.Frame, error) {
				if mode == "write failure" {
					return native.Frame{}, errors.New("fixture write failed")
				}
				if mode == "replaced owner" {
					runtime.mu.Lock()
					runtime.generation++
					runtime.mu.Unlock()
				}
				return native.Frame{Opcode: native.OpACK, Payload: []byte{frame.Opcode, 0}}, nil
			}
			session := link.NewForPort("fixture", port)
			runtime.session, runtime.generation, runtime.connectionState = session, 7, "connected"
			t.Cleanup(func() { _ = runtime.Close() })
			if _, err := runtime.StartPWMWriteTrace(1, time.Second); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(WithPWMTraceOrigin(context.Background(), "controller.pwm.set", "fixture-writer", "http"), time.Second)
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			_, err := runtime.Request(ctx, native.OpPWMSet, []byte{1, 0xFF, 0x0F}, native.OpACK)
			trace := runtime.PWMWriteTrace()
			if mode == "accepted" {
				if err != nil || trace.Total != 1 || trace.Records[0].ActorID != "fixture-writer" || trace.Records[0].RawValue != 4095 || trace.Records[0].ConnectionGeneration != 7 {
					t.Fatalf("accepted trace %+v %v", trace, err)
				}
			} else if err == nil || trace.Total != 0 {
				t.Fatalf("failed write claimed accepted %+v %v", trace, err)
			}
		})
	}
}

func TestPWMWriteTraceRejectsUnconfirmedAndReplacedCapture(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	for _, duration := range []time.Duration{0, time.Nanosecond, time.Minute + time.Millisecond} {
		if _, err := runtime.StartPWMWriteTrace(1, duration); err == nil {
			t.Fatal("invalid duration accepted")
		}
	}
	if _, err := runtime.StartPWMWriteTrace(16, time.Second); err == nil {
		t.Fatal("invalid channel accepted")
	}
	runtime.StartPWMWriteTrace(1, time.Second)
	payload := []byte{1, 1, 0}
	epoch := runtime.pwmTraceAdmission(native.OpPWMSet, payload)
	for _, ack := range []native.Frame{{}, {Opcode: native.OpACK}, {Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 1}}, {Opcode: native.OpACK, Payload: []byte{native.OpPWMGet, 0}}} {
		runtime.observePWMWriteAck(context.Background(), epoch, 1, native.OpPWMSet, payload, ack)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Request(ctx, native.OpPWMSet, payload, native.OpACK); err == nil {
		t.Fatal("disconnected canceled request succeeded")
	}
	if runtime.PWMWriteTrace().Total != 0 {
		t.Fatal("failed request claimed acknowledged")
	}
	ack := native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}}
	runtime.StartPWMWriteTrace(1, time.Second)
	runtime.observePWMWriteAck(context.Background(), epoch, 1, native.OpPWMSet, payload, ack)
	if runtime.PWMWriteTrace().Total != 0 {
		t.Fatal("old request entered replaced trace")
	}
	runtime.pwmTrace.mu.Lock()
	runtime.pwmTrace.expiresAt = time.Now().Add(-time.Second)
	runtime.pwmTrace.mu.Unlock()
	if runtime.PWMWriteTrace().Active || runtime.pwmTraceAdmission(native.OpPWMSet, payload) != 0 {
		t.Fatal("expired trace captured")
	}
	for _, args := range [][]string{{"trace", "start", "1", "15000"}, {"trace", "get"}, {"trace", "stop"}} {
		if _, err := pwmCommand(context.Background(), runtime, CommandOptions{}, args); err != nil {
			t.Fatal(err)
		}
	}
}
