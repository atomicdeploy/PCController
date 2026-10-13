package control

import (
	"encoding/binary"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

func timedRFFrameFixture(micros uint32) (native.Frame, native.DeviceEvent) {
	p := make([]byte, 14)
	p[0] = native.EventRFReceived | 0x80
	binary.LittleEndian.PutUint32(p[1:5], 0x123456)
	p[5], p[6], p[9] = 24, 1, 5
	binary.LittleEndian.PutUint16(p[7:9], 380)
	binary.LittleEndian.PutUint32(p[10:], micros)
	f := native.Frame{Opcode: native.OpEvent, Payload: p}
	e, _ := native.ParseDeviceEvent(p)
	return f, e
}

func TestRFWireClockHostDispatchDelayCannotGenerateFalseTap(t *testing.T) {
	runtime := New(Options{})
	p := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 32)}
	p.handle = func(f native.Frame) (native.Frame, error) {
		payload := make([]byte, 60)
		binary.LittleEndian.PutUint32(payload, 1900)
		return native.Frame{Opcode: native.OpStatus, Payload: payload}, nil
	}
	s := link.NewForPort("TEST", p)
	runtime.session, runtime.generation = s, 1
	defer runtime.Close()
	var input []native.DeviceEvent
	for i := 0; i < 10; i++ {
		f, e := timedRFFrameFixture(1000000 + uint32(i)*100000)
		data, _ := native.Encode(f)
		p.replies <- data
		input = append(input, e)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if v, ok := s.RFReceiveProgress(0x123456, 24, 1); ok && v.ReceiptMicros == 1900000 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if v, _ := s.RFReceiveProgress(0x123456, 24, 1); v.ReceiptMicros != 1900000 {
		t.Fatal("wire did not receive fixture")
	}
	after := runtime.LatestEventID()
	runtime.observeRFGesture(input[0])
	key := rfGestureKey{code: 0x123456, bits: 24, protocol: 1}
	down := waitTestEvent(t, runtime, after, "rf.gesture")
	// Simulate the presentation consumer being late while the wire has already
	// received the continuing physical hold. No host inactivity may emit Tap.
	runtime.finishRFGesture(key, expiredRFState(t, runtime, key))
	if runtime.LatestEventID() != down.ID {
		t.Fatal("presentation delay produced a release")
	}
	for _, e := range input[1:] {
		runtime.observeRFGesture(e)
	}
	hold := waitTestEvent(t, runtime, down.ID, "rf.gesture")
	if hold.Gesture != "hold" {
		t.Fatal("MCU elapsed hold was not recognized", hold.Gesture)
	}
	ack := native.Frame{Opcode: native.OpACK, Payload: make([]byte, 6)}
	ack.Payload[0] = native.OpPWMSet
	binary.LittleEndian.PutUint32(ack.Payload[2:], 2300000)
	encoded, _ := native.Encode(ack)
	p.replies <- encoded
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if v, _ := s.RFReceiveProgress(0x123456, 24, 1); v.QuietFor(rfReleaseAfter) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	runtime.finishRFGesture(key, expiredRFState(t, runtime, key))
	up := waitTestEvent(t, runtime, hold.ID, "rf.gesture")
	end := waitTestEvent(t, runtime, up.ID, "rf.gesture")
	if up.Gesture != "up" || end.Gesture != "hold-end" {
		t.Fatal("confirmed release classification", up.Gesture, end.Gesture)
	}
	for _, e := range runtime.Timeline(time.Time{}, 2000) {
		if e.Kind == "rf.gesture" && e.Gesture == "tap" {
			t.Fatal("held wire sequence emitted a tap")
		}
	}
}
