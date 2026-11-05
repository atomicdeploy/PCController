package rgbinput

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func lightingFixture(protocol string, pixels, channel int) []byte {
	rgb := bytes.Repeat([]byte{17, 128, 250}, pixels)
	switch protocol {
	case "ddp":
		return append([]byte{0x41, 1, 0x0b, 1, 0, 0, 0, 0, byte(len(rgb) >> 8), byte(len(rgb))}, rgb...)
	case "wled":
		return append([]byte{2, 2}, rgb...)
	case "hyperion":
		return rgb
	case "artnet":
		length := channel - 1 + len(rgb)
		if length%2 != 0 {
			length++
		}
		p := make([]byte, 18+length)
		copy(p, "Art-Net\x00")
		p[9] = 0x50
		p[11] = 14
		p[12] = 1
		p[14] = 1
		binary.BigEndian.PutUint16(p[16:18], uint16(length))
		copy(p[18+channel-1:], rgb)
		return p
	case "sacn":
		p := make([]byte, 126+channel-1+len(rgb))
		p[1] = 16
		copy(p[4:16], "ASC-E1.17\x00\x00\x00")
		for _, offset := range []int{16, 38, 115} {
			binary.BigEndian.PutUint16(p[offset:offset+2], 0x7000|uint16(len(p)-offset))
		}
		p[21] = 4
		p[22] = 7
		p[43] = 2
		p[108] = 100
		p[111] = 1
		p[114] = 1
		p[117] = 2
		p[118] = 0xa1
		p[122] = 1
		binary.BigEndian.PutUint16(p[123:125], uint16(len(p)-125))
		copy(p[126+channel-1:], rgb)
		return p
	}
	return nil
}

func TestLightingProtocolsCompleteFrames(t *testing.T) {
	for _, protocol := range []string{"ddp", "artnet", "sacn", "wled", "hyperion"} {
		for _, pixels := range []int{1, 60, 100} {
			cfg := Config{Protocol: protocol, Pixels: pixels, Universe: 1, Channel: 7}
			p := lightingFixture(protocol, pixels, 7)
			want := bytes.Repeat([]byte{17, 128, 250}, pixels)
			got, err := decodePacket(cfg, p)
			if err != nil || !bytes.Equal(got.RGB, want) {
				t.Fatalf("%s/%d: %x %v", protocol, pixels, got.RGB, err)
			}
			for end := 0; end < len(p); end++ {
				if _, err := decodePacket(cfg, p[:end]); err == nil {
					t.Fatalf("accepted truncated %s/%d", protocol, end)
				}
			}
			if _, err := decodePacket(cfg, append(p, 0)); err == nil {
				t.Fatalf("accepted trailing %s", protocol)
			}
		}
	}
}

func TestDMXAdmission(t *testing.T) {
	for _, protocol := range []string{"sacn", "artnet"} {
		cfg := Config{Protocol: protocol, Pixels: 60, Universe: 2, Channel: 1}
		p := lightingFixture(protocol, 60, 1)
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatal("wrong universe admitted")
		}
		cfg.Universe = 1
		cfg.Channel = 512
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatal("out of bounds slots admitted")
		}
	}
	cfg := Config{Protocol: "sacn", Pixels: 60, Universe: 1, Channel: 1}
	for _, offset := range []int{0, 2, 4, 16, 18, 38, 40, 109, 115, 117, 118, 119, 121, 123, 125} {
		p := lightingFixture("sacn", 60, 1)
		p[offset] ^= 1
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatalf("invalid sACN field %d admitted", offset)
		}
	}
	for _, options := range []byte{0x80, 0x20, 1} {
		p := lightingFixture("sacn", 60, 1)
		p[112] = options
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatal("preview/sync/reserved packet admitted")
		}
	}
	p := lightingFixture("sacn", 60, 1)
	p[112] = 0x40
	got, err := decodePacket(cfg, p)
	if err != nil || !got.Ended || got.RGB != nil {
		t.Fatal("termination must not write RGB")
	}
}

func TestDDPAdmission(t *testing.T) {
	cfg := Config{Protocol: "ddp", Pixels: 60}
	for _, offset := range []int{0, 2, 3, 4, 8, 9} {
		p := lightingFixture("ddp", 60, 1)
		p[offset] ^= 2
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatalf("DDP invalid field %d admitted", offset)
		}
	}
	for _, flags := range []byte{0x40, 0x43, 0x45, 0x49, 0x51, 0x81} {
		p := lightingFixture("ddp", 60, 1)
		p[0] = flags
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatal("non-display or partial DDP admitted")
		}
	}
}

func TestWLEDIndexedAndOffsetFrames(t *testing.T) {
	cfg := Config{Protocol: "wled", Pixels: 2}
	want := []byte{1, 2, 3, 4, 5, 6}
	for _, p := range [][]byte{{4, 2, 0, 0, 1, 2, 3, 4, 5, 6}, {1, 2, 1, 4, 5, 6, 0, 1, 2, 3}} {
		got, err := decodePacket(cfg, p)
		if err != nil || !bytes.Equal(got.RGB, want) {
			t.Fatal(got, err)
		}
	}
	for _, p := range [][]byte{{4, 2, 0, 1, 1, 2, 3, 4, 5, 6}, {1, 2, 0, 1, 2, 3, 0, 4, 5, 6}, {1, 2, 2, 1, 2, 3, 0, 4, 5, 6}, {2, 0, 1, 2, 3, 4, 5, 6}, {0, 2, 1, 2, 3, 4, 5, 6}} {
		if _, err := decodePacket(cfg, p); err == nil {
			t.Fatal("bad WLED frame admitted")
		}
	}
}

func TestPacketSourceAndOrder(t *testing.T) {
	for _, modulo := range []int{16, 256} {
		order := packetOrder{}
		if !order.accept(Packet{Identity: "a", Sequence: byte(modulo - 1), SequenceModulo: modulo}) || !order.accept(Packet{Identity: "a", Sequence: 1, SequenceModulo: modulo}) {
			t.Fatal("sequence wrap rejected")
		}
		for _, p := range []Packet{{Identity: "b", Sequence: 2, SequenceModulo: modulo}, {Identity: "a", Sequence: 1, SequenceModulo: modulo}, {Identity: "a", Sequence: byte(modulo - 2), SequenceModulo: modulo}} {
			if order.accept(p) {
				t.Fatal("foreign, duplicate or stale frame admitted")
			}
		}
		if !order.accept(Packet{Identity: "a", Sequence: 2, SequenceModulo: modulo}) {
			t.Fatal("rejection poisoned state")
		}
	}
	order := packetOrder{}
	if !order.accept(Packet{SequenceModulo: 256}) || order.accept(Packet{SequenceModulo: 256}) || !order.accept(Packet{Sequence: 1, SequenceModulo: 256}) {
		t.Fatal("sACN sequence zero is ordered")
	}
}

func TestSerialFramingAndCountVariants(t *testing.T) {
	rgb := []byte{9, 8, 7, 6, 5, 4}
	for _, protocol := range []string{"openrgb", "adalight", "adalight-openrgb", "tpm2"} {
		p := testPacket(protocol, rgb)
		if protocol == "tpm2" {
			p = append(append([]byte{0xc9, 0xda, 0, 6}, rgb...), 0x36)
		}
		if protocol == "adalight-openrgb" {
			p[4] = 2
			p[5] = p[3] ^ p[4] ^ 0x55
		}
		f := newStreamFramer(protocol, 2)
		count := 0
		stream := append([]byte{0, 0xff, 0x41, 0}, p...)
		stream = append(stream, p...)
		for _, b := range stream {
			if got := f.push(b); got != nil {
				decoded, err := Decode(protocol, got, 2)
				if err != nil || !bytes.Equal(decoded, rgb) {
					t.Fatal(protocol, err)
				}
				count++
			}
			if len(f.pending) > f.size {
				t.Fatal("unbounded serial buffer")
			}
		}
		if count != 2 {
			t.Fatalf("%s: recovered %d frames", protocol, count)
		}
	}
	p := testPacket("adalight", rgb)
	if _, err := Decode("adalight-openrgb", p, 2); err == nil {
		t.Fatal("count variants conflated")
	}
}

func FuzzLightingDecode(f *testing.F) {
	f.Add([]byte{0x41, 1, 0x0b}, uint8(60))
	f.Fuzz(func(t *testing.T, p []byte, count uint8) {
		for _, protocol := range []string{"ddp", "artnet", "sacn", "wled", "hyperion"} {
			cfg := Config{Protocol: protocol, Pixels: int(count), Universe: 1, Channel: 1}
			got, err := decodePacket(cfg, p)
			if err == nil && !got.Ended && len(got.RGB) != int(count)*3 {
				t.Fatal("partial RGB frame")
			}
		}
	})
}
