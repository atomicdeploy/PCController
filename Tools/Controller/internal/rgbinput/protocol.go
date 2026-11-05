// Package rgbinput accepts bounded RGB frames, never native controller commands.
package rgbinput

import (
	"encoding/binary"
	"errors"
)

var ErrPacket = errors.New("invalid RGB packet or pixel count")

// Decode returns an RGB view into packet. One UDP datagram is one complete
// frame; partial TPM2.NET packets cannot commit an incomplete board frame.
func Decode(protocol string, packet []byte, pixels int) ([]byte, error) {
	if pixels < 1 || pixels > 100 {
		return nil, ErrPacket
	}
	n := pixels * 3
	switch protocol {
	case "openrgb": // OpenRGB LED Strip / Keyboard Visualizer UDP wire format.
		if len(packet) != n+3 || packet[0] != 0xaa {
			return nil, ErrPacket
		}
		var sum uint16
		for _, value := range packet[:n+1] {
			sum += uint16(value)
		}
		if binary.BigEndian.Uint16(packet[n+1:]) != sum {
			return nil, ErrPacket
		}
		return packet[1 : n+1], nil
	case "tpm2net":
		if len(packet) != n+7 || packet[0] != 0x9c || packet[1] != 0xda ||
			int(binary.BigEndian.Uint16(packet[2:4])) != n || packet[4] != 1 || packet[5] != 1 || packet[n+6] != 0x36 {
			return nil, ErrPacket
		}
		return packet[6 : n+6], nil
	case "tpm2":
		if len(packet) != n+5 || packet[0] != 0xc9 || packet[1] != 0xda || int(binary.BigEndian.Uint16(packet[2:4])) != n || packet[n+4] != 0x36 {
			return nil, ErrPacket
		}
		return packet[4 : n+4], nil
	case "adalight", "adalight-openrgb":
		count := pixels - 1 // Standard Adafruit N-1; OpenRGB's driver uses N.
		if protocol == "adalight-openrgb" {
			count = pixels
		}
		if len(packet) != n+6 || string(packet[:3]) != "Ada" ||
			int(binary.BigEndian.Uint16(packet[3:5])) != count || packet[5] != packet[3]^packet[4]^0x55 {
			return nil, ErrPacket
		}
		return packet[6:], nil
	default:
		return nil, ErrPacket
	}
}
