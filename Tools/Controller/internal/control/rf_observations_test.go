package control

import (
	"encoding/binary"
	"testing"

	"pccontroller.local/controller/internal/native"
)

func rfReceptionFixture(code uint32, bits, protocol byte) Event {
	payload := make([]byte, 10)
	payload[0] = native.EventRFReceived
	binary.LittleEndian.PutUint32(payload[1:], code)
	payload[5], payload[6], payload[9] = bits, protocol, 0xff
	return Event{Kind: "rf.receive", Source: "board", Frame: native.Frame{Opcode: native.OpEvent, Payload: payload}}
}

func TestRFLastSeenOnlyUsesGenuineMatchingReception(t *testing.T) {
	runtime := New(Options{})
	first := runtime.PublishStructuredEvent(rfReceptionFixture(42, 24, 1))
	seen := runtime.RFLastSeen(42, 24, 1)
	if seen.IsZero() || !seen.Equal(first.Time) {
		t.Fatal("genuine reception not retained")
	}
	if !runtime.RFLastSeen(42, 23, 1).IsZero() || !runtime.RFLastSeen(42, 24, 2).IsZero() {
		t.Fatal("partial waveform matched")
	}
	for _, changed := range []Event{
		{Kind: "rf.receive", Source: "host", RFCode: 42, RFBits: 24, RFProtocol: 1},
		{Kind: "rf.transmit", Source: "board", Frame: first.Frame},
		{Kind: "rf.learn.started", Source: "board", Frame: first.Frame},
		{Kind: "rf.receive", Source: "bridge", Frame: first.Frame},
	} {
		runtime.PublishStructuredEvent(changed)
	}
	if !runtime.RFLastSeen(42, 24, 1).Equal(seen) {
		t.Fatal("transmit/refresh/bridge/synthetic activity invented a time")
	}
	for code := uint32(100); code < 130; code++ {
		runtime.PublishStructuredEvent(rfReceptionFixture(code, 24, 1))
	}
	if !runtime.RFLastSeen(42, 24, 1).Equal(seen) {
		t.Fatal("RF activity-ring eviction lost seen time")
	}
	for code := uint32(200); code < 500; code++ {
		runtime.PublishStructuredEvent(rfReceptionFixture(code, 24, 1))
	}
	if len(runtime.rfLastSeen) > rfLastSeenLimit {
		t.Fatal("reception cache unbounded")
	}
	if !New(Options{}).RFLastSeen(42, 24, 1).IsZero() {
		t.Fatal("new session invented historical time")
	}
}
