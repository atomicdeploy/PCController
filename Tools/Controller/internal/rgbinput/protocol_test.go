package rgbinput

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func testPacket(protocol string, rgb []byte) []byte {
	n := len(rgb)
	switch protocol {
	case "openrgb":
		packet := append([]byte{0xaa}, rgb...)
		var sum uint16
		for _, b := range packet {
			sum += uint16(b)
		}
		return append(packet, byte(sum>>8), byte(sum))
	case "tpm2net":
		return append(append([]byte{0x9c, 0xda, byte(n >> 8), byte(n), 1, 1}, rgb...), 0x36)
	default:
		count := uint16(n/3 - 1)
		return append([]byte{'A', 'd', 'a', byte(count >> 8), byte(count), byte(count>>8) ^ byte(count) ^ 0x55}, rgb...)
	}
}

func TestDecodeSupportedDatagrams(t *testing.T) {
	for _, protocol := range []string{"openrgb", "tpm2net", "adalight"} {
		for _, count := range []int{1, 60, 100} {
			rgb := bytes.Repeat([]byte{0x12, 0xa5, 0xff}, count)
			packet := testPacket(protocol, rgb)
			got, err := Decode(protocol, packet, count)
			if err != nil || !bytes.Equal(got, rgb) {
				t.Fatalf("%s/%d: %x %v", protocol, count, got, err)
			}
			for length := 0; length < len(packet); length++ {
				if _, err := Decode(protocol, packet[:length], count); err == nil {
					t.Fatalf("accepted truncated %s at %d", protocol, length)
				}
			}
			if _, err := Decode(protocol, append(packet, 0), count); err == nil {
				t.Fatalf("accepted trailing byte: %s", protocol)
			}
			if _, err := Decode(protocol, packet, count+1); err == nil {
				t.Fatalf("accepted wrong pixel count: %s", protocol)
			}
		}
	}
}

func TestRejectChecksumsCommandsFragmentsAndOversize(t *testing.T) {
	white := bytes.Repeat([]byte{255, 255, 255}, 100)
	if rgb, err := Decode("openrgb", testPacket("openrgb", white), 100); err != nil || !bytes.Equal(rgb, white) {
		t.Fatal("16-bit checksum overflow must wrap")
	}
	rgb := []byte{1, 2, 3}
	for _, protocol := range []string{"openrgb", "adalight"} {
		p := testPacket(protocol, rgb)
		if protocol == "openrgb" {
			p[len(p)-1] ^= 1
		} else {
			p[5] ^= 1
		}
		if _, err := Decode(protocol, p, 1); err == nil {
			t.Fatal("accepted checksum corruption")
		}
	}
	for _, offset := range []int{0, 1, 4, 5, 9} {
		p := testPacket("tpm2net", rgb)
		p[offset] ^= 1
		if _, err := Decode("tpm2net", p, 1); err == nil {
			t.Fatalf("accepted invalid TPM field %d", offset)
		}
	}
	p := testPacket("tpm2net", rgb)
	binary.BigEndian.PutUint16(p[2:4], 65535)
	if _, err := Decode("tpm2net", p, 1); err == nil {
		t.Fatal("accepted invalid length")
	}
	if _, err := Decode("openrgb", testPacket("openrgb", bytes.Repeat(rgb, 101)), 101); err == nil {
		t.Fatal("accepted unsupported strip size")
	}
}
