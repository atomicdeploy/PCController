package native

import (
	"errors"
	"fmt"
)

const (
	Magic            byte = 0xA5
	EnvelopeRevision byte = 0x01
	MaxPayload       = 48
	MaxRawFrame      = 5 + MaxPayload + 1
)

var (
	ErrEmptyFrame      = errors.New("empty frame")
	ErrMalformedCOBS   = errors.New("malformed COBS frame")
	ErrFrameTooLong    = errors.New("frame exceeds protocol limit")
	ErrBadMagic        = errors.New("invalid frame magic")
	ErrBadLength       = errors.New("payload length does not match frame")
	ErrBadCRC          = errors.New("frame CRC mismatch")
	ErrPayloadTooLong  = errors.New("payload exceeds 48 bytes")
	ErrReceiveOverflow = errors.New("encoded receive frame overflow")
)

type Frame struct {
	Opcode  byte
	Seq     byte
	Payload []byte
}

func Encode(frame Frame) ([]byte, error) {
	if len(frame.Payload) > MaxPayload {
		return nil, ErrPayloadTooLong
	}

	raw := make([]byte, 0, 6+len(frame.Payload))
	raw = append(raw, Magic, EnvelopeRevision, frame.Opcode, frame.Seq, byte(len(frame.Payload)))
	raw = append(raw, frame.Payload...)
	raw = append(raw, CRC8(raw))

	encoded := COBSEncode(raw)
	encoded = append(encoded, 0)
	return encoded, nil
}

func Decode(encoded []byte) (Frame, error) {
	if len(encoded) == 0 {
		return Frame{}, ErrEmptyFrame
	}
	if encoded[len(encoded)-1] == 0 {
		encoded = encoded[:len(encoded)-1]
	}
	raw, err := COBSDecode(encoded)
	if err != nil {
		return Frame{}, err
	}
	if len(raw) > MaxRawFrame {
		return Frame{}, ErrFrameTooLong
	}
	if len(raw) < 6 {
		return Frame{}, fmt.Errorf("%w: got %d raw bytes", ErrBadLength, len(raw))
	}
	if raw[0] != Magic {
		return Frame{}, fmt.Errorf("%w: got 0x%02X", ErrBadMagic, raw[0])
	}
	payloadLength := int(raw[4])
	if payloadLength > MaxPayload || len(raw) != 6+payloadLength {
		return Frame{}, fmt.Errorf(
			"%w: header=%d raw=%d",
			ErrBadLength,
			payloadLength,
			len(raw),
		)
	}
	wantCRC := CRC8(raw[:len(raw)-1])
	if raw[len(raw)-1] != wantCRC {
		return Frame{}, fmt.Errorf(
			"%w: got 0x%02X want 0x%02X",
			ErrBadCRC,
			raw[len(raw)-1],
			wantCRC,
		)
	}

	payload := append([]byte(nil), raw[5:len(raw)-1]...)
	return Frame{Opcode: raw[2], Seq: raw[3], Payload: payload}, nil
}

// CRC8 implements CRC-8/ATM with polynomial 0x07 and initial value 0x00.
func CRC8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for bit := 0; bit < 8; bit++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
