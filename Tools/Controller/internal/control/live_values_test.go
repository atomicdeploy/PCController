package control

import (
	"context"
	"encoding/binary"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

func TestPWMFramePublishesCompleteChangedOnlyOwnedCache(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	payload := make([]byte, 34)
	payload[0] = 1
	payload[1] = 2
	binary.LittleEndian.PutUint16(payload[6:8], 1234)
	runtime.observe(native.Frame{Opcode: native.OpPWMValues, Payload: payload})
	first := runtime.Snapshot()
	if first.PWMValues == nil || first.PWMValues.Values[2] != 1234 || first.PWMRevision != 1 {
		t.Fatalf("cache=%+v", first.PWMValues)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEventStreamFilter(ctx, 0, "pwm.changed", nil, EventStreamState)
	if err != nil || event.Metadata["pwm.2"] != "1234" || event.Metadata["revision"] != "1" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	runtime.observe(native.Frame{Opcode: native.OpPWMValues, Payload: payload})
	if runtime.Snapshot().PWMRevision != 1 {
		t.Fatal("unchanged frame republished")
	}
	runtime.mu.Lock()
	runtime.clearPeerStateLocked()
	runtime.generation++
	runtime.mu.Unlock()
	if runtime.Snapshot().PWMValues != nil {
		t.Fatal("old board cache survived detach")
	}
	runtime.observe(native.Frame{Opcode: native.OpPWMValues, Payload: payload})
	if runtime.Snapshot().PWMRevision != 2 {
		t.Fatal("same values on new board lost")
	}
	binary.LittleEndian.PutUint16(payload[6:8], 5000)
	runtime.observe(native.Frame{Opcode: native.OpPWMValues, Payload: payload})
	if runtime.Snapshot().PWMRevision != 2 {
		t.Fatal("invalid raw PWM admitted")
	}
}

func TestStatusLEDQuietTrailingFrameFlushesWithoutAnotherObservation(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	first := native.StatusLEDState{Red: 40, Brightness: 255}
	runtime.observe(native.Frame{Opcode: native.OpStatusLEDChanged, Payload: []byte{40, 0, 0, 255, 0, 0}})
	if !runtime.shouldPublishStatusLED(first, time.Now()) {
		t.Fatal("initial frame lost")
	}
	runtime.observe(native.Frame{Opcode: native.OpStatusLEDChanged, Payload: []byte{41, 0, 0, 255, 0, 0}})
	if runtime.shouldPublishStatusLED(native.StatusLEDState{Red: 41, Brightness: 255}, time.Now()) {
		t.Fatal("frame not bounded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEventStreamFilter(ctx, 0, "status_led.changed", nil, EventStreamState)
	if err != nil || event.Metadata["red"] != "41" || event.Metadata["revision"] != "2" {
		t.Fatalf("trailing event=%+v err=%v", event, err)
	}
}

func TestStatusLEDTrailingFrameIsRetiredOnOwnerChange(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	runtime.observe(native.Frame{Opcode: native.OpStatusLEDChanged, Payload: []byte{40, 0, 0, 255, 0, 0}})
	runtime.shouldPublishStatusLED(native.StatusLEDState{Red: 40, Brightness: 255}, time.Now())
	runtime.observe(native.Frame{Opcode: native.OpStatusLEDChanged, Payload: []byte{41, 0, 0, 255, 0, 0}})
	runtime.shouldPublishStatusLED(native.StatusLEDState{Red: 41, Brightness: 255}, time.Now())
	runtime.mu.Lock()
	runtime.clearPeerStateLocked()
	runtime.generation++
	runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if event, err := runtime.WaitEventStreamFilter(ctx, 0, "status_led.changed", nil, EventStreamState); err == nil {
		t.Fatalf("retired frame published: %+v", event)
	}
}
