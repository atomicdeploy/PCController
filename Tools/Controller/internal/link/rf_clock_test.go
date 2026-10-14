package link

import (
	"encoding/binary"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

func rfClockTestFrame(micros uint32) native.Frame {
	payload := make([]byte, 14)
	payload[0] = native.EventRFReceived | 0x80
	binary.LittleEndian.PutUint32(payload[1:5], 0x123456)
	payload[5], payload[6], payload[9] = 24, 1, 5
	binary.LittleEndian.PutUint16(payload[7:9], 380)
	binary.LittleEndian.PutUint32(payload[10:14], micros)
	return native.Frame{Opcode: native.OpEvent, Payload: payload}
}

func rfClockTestAck(micros uint32) native.Frame {
	payload := make([]byte, 6)
	payload[0] = native.OpPWMSet
	binary.LittleEndian.PutUint32(payload[2:], micros)
	return native.Frame{Opcode: native.OpACK, Payload: payload}
}

func TestRFClockQuietProofPreservesWireContinuityAcrossHostBurstsAndWrap(t *testing.T) {
	for _, start := range []uint32{1000000, 0xffff0000} {
		s := &Session{}
		s.observeRFClock(rfClockTestFrame(start))
		s.observeRFClock(rfClockTestFrame(start + 99000))
		v, ok := s.RFReceiveProgress(0x123456, 24, 1)
		if !ok || v.QuietFor(RFContinuityGap) || !v.ContainsReceipt(start) {
			t.Fatal("continuous wire receipts lost")
		}
		s.observeRFClock(rfClockTestAck(start + 348000))
		v, _ = s.RFReceiveProgress(0x123456, 24, 1)
		if v.QuietFor(RFContinuityGap) {
			t.Fatal("quiet was inferred too early")
		}
		s.observeRFClock(rfClockTestAck(start + 350000))
		v, _ = s.RFReceiveProgress(0x123456, 24, 1)
		if !v.QuietFor(RFContinuityGap) {
			t.Fatal("confirmed clock progress not recognized")
		}
		s.observeRFClock(rfClockTestFrame(start))
		stale, _ := s.RFReceiveProgress(0x123456, 24, 1)
		if stale.ReceiptMicros != v.ReceiptMicros || !stale.ReceiptSeen.Equal(v.ReceiptSeen) {
			t.Fatal("stale frame revived a released input")
		}
	}
}

func TestRFClockObserverRunsBeforePresentationConsumption(t *testing.T) {
	p := newFakePort()
	s := NewForPort("TEST", p)
	defer s.Close()
	encoded, _ := native.Encode(rfClockTestFrame(1000000))
	p.reads <- encoded
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := s.RFReceiveProgress(0x123456, 24, 1); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("wire evidence depended on consuming presentation events")
}
