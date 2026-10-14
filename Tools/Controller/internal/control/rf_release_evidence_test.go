package control

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestTimedRFReleaseDiscardsLostWireEvidenceWithoutAction(t *testing.T) {
	for _, evidence := range []string{"missing", "evicted", "untimed replacement"} {
		for _, held := range []bool{false, true} {
			name := evidence + "/tap"
			if held {
				name = evidence + "/hold"
			}
			t.Run(name, func(t *testing.T) {
				runtime := New(Options{})
				port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 32)}
				port.handle = func(native.Frame) (native.Frame, error) {
					return native.Frame{}, errors.New("missing release evidence must not request hardware")
				}
				session := link.NewForPort("TEST", port)
				runtime.session, runtime.generation = session, 1
				defer runtime.Close()

				send := func(frame native.Frame) {
					t.Helper()
					encoded, err := native.Encode(frame)
					if err != nil {
						t.Fatal(err)
					}
					select {
					case port.replies <- encoded:
					case <-time.After(time.Second):
						t.Fatal("fixture receive pump stalled")
					}
				}
				wait := func(ready func() bool) {
					t.Helper()
					deadline := time.Now().Add(time.Second)
					for time.Now().Before(deadline) {
						if ready() {
							return
						}
						time.Sleep(time.Millisecond)
					}
					t.Fatal("fixture wire evidence was not observed")
				}

				frame, event := timedRFFrameFixture(1000000)
				key := rfGestureKey{code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol}
				if evidence != "missing" {
					send(frame)
					wait(func() bool {
						value, known := session.RFReceiveProgress(key.code, key.bits, key.protocol)
						return known && value.ReceiptTimed
					})
				}
				switch evidence {
				case "evicted":
					for index := uint32(1); index <= 256; index++ {
						other, _ := timedRFFrameFixture(event.DeviceMicros + index*1000)
						binary.LittleEndian.PutUint32(other.Payload[1:5], key.code+index)
						send(other)
					}
					wait(func() bool {
						_, known := session.RFReceiveProgress(key.code+256, key.bits, key.protocol)
						return known
					})
					if _, known := session.RFReceiveProgress(key.code, key.bits, key.protocol); known {
						t.Fatal("fixture did not exercise bounded-cache eviction")
					}
				case "untimed replacement":
					frame.Payload = frame.Payload[:10]
					frame.Payload[0] &^= 0x80
					send(frame)
					wait(func() bool {
						value, known := session.RFReceiveProgress(key.code, key.bits, key.protocol)
						return known && !value.ReceiptTimed
					})
				}

				state := &rfGestureState{event: event, held: held, deviceTimed: true,
					wireSession: session, lastSeen: time.Now().Add(-rfReleaseAfter - time.Millisecond)}
				runtime.rfMu.Lock()
				runtime.rfGestures = map[rfGestureKey]*rfGestureState{key: state}
				runtime.rfMu.Unlock()
				runtime.finishRFGesture(key, state)
				for _, entry := range runtime.Timeline(time.Time{}, 2000) {
					if entry.Kind == "rf.gesture" {
						t.Fatalf("lost wire evidence emitted an action-bearing gesture: %s", entry.Text)
					}
				}
				runtime.rfMu.Lock()
				defer runtime.rfMu.Unlock()
				if runtime.rfGestures[key] != nil || runtime.rfClicks[key] != nil {
					t.Fatal("unprovable gesture retained a release/click timer")
				}
			})
		}
	}
}
