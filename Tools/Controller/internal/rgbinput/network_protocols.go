package rgbinput

import "encoding/binary"

// Packet describes a complete physical strip frame, not a board command. DMX
// slots outside the explicitly selected window never become board outputs.
type Packet struct {
	RGB            []byte
	Identity       string
	Sequence       byte
	SequenceModulo int
	Ended          bool
}

func DefaultEndpoint(protocol string) string {
	switch protocol {
	case "ddp":
		return "127.0.0.1:4048"
	case "artnet":
		return "127.0.0.1:6454"
	case "sacn":
		return "127.0.0.1:5568"
	case "wled":
		return "127.0.0.1:21324"
	case "hyperion":
		return "127.0.0.1:19446"
	default:
		return "127.0.0.1:65506"
	}
}

func supported(protocol string) bool {
	switch protocol {
	case "openrgb", "adalight", "adalight-openrgb", "tpm2", "tpm2net", "ddp", "artnet", "sacn", "wled", "hyperion":
		return true
	}
	return false
}

func decodePacket(cfg Config, p []byte) (Packet, error) {
	bad := func() (Packet, error) { return Packet{}, ErrPacket }
	if cfg.Pixels < 1 || cfg.Pixels > 100 {
		return bad()
	}
	n := cfg.Pixels * 3
	if (cfg.Protocol == "artnet" || cfg.Protocol == "sacn") && (cfg.Channel < 1 || cfg.Channel > 513-n) {
		return bad()
	}
	result := Packet{}
	switch cfg.Protocol {
	case "ddp":
		// RGB24 display data, version 1, PUSH, complete frame at offset zero.
		// Reject queries/configuration/storage/timecodes and fragmented frames.
		if len(p) != 10+n || p[0] != 0x41 || p[1]&0xf0 != 0 || (p[2] != 0 && p[2] != 1 && p[2] != 0x0b) || p[3] != 1 || binary.BigEndian.Uint32(p[4:8]) != 0 || int(binary.BigEndian.Uint16(p[8:10])) != n {
			return bad()
		}
		result.RGB = p[10:]
		result.Sequence = p[1]
		if p[1] != 0 {
			result.SequenceModulo = 16
		}
	case "artnet":
		if len(p) < 20 || string(p[:8]) != "Art-Net\x00" || binary.LittleEndian.Uint16(p[8:10]) != 0x5000 || binary.BigEndian.Uint16(p[10:12]) < 14 || int(binary.LittleEndian.Uint16(p[14:16])) != cfg.Universe {
			return bad()
		}
		length := int(binary.BigEndian.Uint16(p[16:18]))
		start := cfg.Channel - 1
		if length < 2 || length > 512 || length%2 != 0 || len(p) != 18+length || start < 0 || start+n > length {
			return bad()
		}
		result.RGB = p[18+start : 18+start+n]
		result.Identity = string(p[13:14])
		result.Sequence = p[12]
		if p[12] != 0 {
			result.SequenceModulo = 256
		}
	case "sacn":
		if len(p) < 126 || binary.BigEndian.Uint16(p[:2]) != 16 || binary.BigEndian.Uint16(p[2:4]) != 0 || string(p[4:16]) != "ASC-E1.17\x00\x00\x00" || !pduLength(p, 16) || !pduLength(p, 38) || !pduLength(p, 115) || binary.BigEndian.Uint32(p[18:22]) != 4 || binary.BigEndian.Uint32(p[40:44]) != 2 || p[108] > 200 || binary.BigEndian.Uint16(p[109:111]) != 0 || p[112]&^byte(0x40) != 0 || int(binary.BigEndian.Uint16(p[113:115])) != cfg.Universe || p[117] != 2 || p[118] != 0xa1 || binary.BigEndian.Uint16(p[119:121]) != 0 || binary.BigEndian.Uint16(p[121:123]) != 1 || p[125] != 0 {
			return bad()
		}
		count := int(binary.BigEndian.Uint16(p[123:125]))
		start := cfg.Channel - 1
		if count < 1 || count > 513 || len(p) != 125+count {
			return bad()
		}
		result.Identity = string(p[22:38])
		result.Sequence = p[111]
		result.SequenceModulo = 256
		result.Ended = p[112]&0x40 != 0
		if !result.Ended {
			if start < 0 || start+n > count-1 {
				return bad()
			}
			result.RGB = p[126+start : 126+start+n]
		}
	case "wled":
		if len(p) < 2 || p[1] == 0 {
			return bad()
		}
		// The local lease remains authoritative, including when byte 1 is 255.
		switch p[0] {
		case 2:
			if len(p) != n+2 {
				return bad()
			}
			result.RGB = p[2:]
		case 4:
			if len(p) != n+4 || binary.BigEndian.Uint16(p[2:4]) != 0 {
				return bad()
			}
			result.RGB = p[4:]
		case 1:
			// WARLS is accepted as a complete indexed snapshot. No unknown
			// pixels are silently initialized to black on a partial first frame.
			if len(p) != 2+cfg.Pixels*4 {
				return bad()
			}
			seen := [100]bool{}
			result.RGB = make([]byte, n)
			for i := 2; i < len(p); i += 4 {
				index := int(p[i])
				if index >= cfg.Pixels || seen[index] {
					return bad()
				}
				seen[index] = true
				copy(result.RGB[index*3:], p[i+1:i+4])
			}
		default:
			return bad()
		}
	case "hyperion":
		if len(p) != n {
			return bad()
		}
		result.RGB = p
	default:
		var err error
		result.RGB, err = Decode(cfg.Protocol, p, cfg.Pixels)
		if err != nil {
			return bad()
		}
	}
	return result, nil
}

func pduLength(p []byte, offset int) bool {
	v := binary.BigEndian.Uint16(p[offset : offset+2])
	return v&0xf000 == 0x7000 && int(v&0xfff) == len(p)-offset
}

type packetOrder struct {
	identity string
	claimed  bool
	sequence byte
	modulo   int
}

// One source per explicitly started session. Source identities include sACN
// CID and ArtDmx Physical, not just the shared UDP endpoint. Duplicate or old
// sequenced datagrams neither replace a frame nor refresh the owner's lease.
func (o *packetOrder) accept(p Packet) bool {
	if o.claimed && p.Identity != o.identity {
		return false
	}
	if p.SequenceModulo != 0 && o.modulo == p.SequenceModulo {
		delta := (int(p.Sequence) - int(o.sequence) + o.modulo) % o.modulo
		if delta == 0 || delta > o.modulo/2 {
			return false
		}
	}
	o.identity = p.Identity
	o.claimed = true
	o.sequence = p.Sequence
	o.modulo = p.SequenceModulo
	return true
}
