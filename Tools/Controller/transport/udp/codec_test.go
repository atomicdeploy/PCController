package udp

import (
	"bytes"
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestPacketWireRoundTrip(t *testing.T) {
	for _, length := range []int{0, 1, ChunkSize} {
		want := Packet{Kind: Request, Source: [16]byte{1, 2, 3}, ID: ^uint64(0), Count: 1, Data: bytes.Repeat([]byte{0x81}, length)}
		encoded, err := EncodePacket(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodePacket(encoded)
		if err != nil || got.Kind != want.Kind || got.Source != want.Source || got.ID != want.ID || got.Index != want.Index || got.Count != want.Count || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("roundtrip length %d: %#v %v", length, got, err)
		}
		// Same raw envelope and matching CRC, invalid COBS: all data was put in
		// one block even though it contains zeroes. No sockets or hardware.
		if length == 0 {
			raw, err := native.COBSDecode(encoded[:len(encoded)-1])
			if err != nil {
				t.Fatal(err)
			}
			malformed := append([]byte{byte(len(raw) + 1)}, raw...)
			malformed = append(malformed, 0)
			if _, err := DecodePacket(malformed); err == nil {
				t.Fatal("accepted embedded delimiter despite valid envelope CRC")
			}
		}
	}
}

func TestPacketRejectsIncompleteNonfinalFragment(t *testing.T) {
	want := Packet{Kind: Request, Source: [16]byte{1}, ID: 1, Count: 2, Data: []byte{1}}
	if _, err := EncodePacket(want); err == nil {
		t.Fatal("encoder accepted a non-final short fragment rejected by its decoder")
	}
	want.Data = bytes.Repeat([]byte{0}, ChunkSize)
	encoded, err := EncodePacket(want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePacket(encoded); err != nil {
		t.Fatal(err)
	}
}
