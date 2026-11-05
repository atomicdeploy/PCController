package rgbinput

import (
	"context"
	"errors"
	"go.bug.st/serial"
	"io"
	"time"
)

// streamFramer holds at most one configured frame. Invalid prefixes slide one
// byte at a time; declared lengths never allocate memory or stall on huge frames.
type streamFramer struct {
	protocol     string
	pixels, size int
	pending      []byte
}

func newStreamFramer(protocol string, pixels int) *streamFramer {
	size := pixels * 3
	switch protocol {
	case "openrgb":
		size += 3
	case "tpm2":
		size += 5
	default:
		size += 6
	}
	return &streamFramer{protocol: protocol, pixels: pixels, size: size, pending: make([]byte, 0, size)}
}

func (f *streamFramer) push(b byte) []byte {
	f.pending = append(f.pending, b)
	if len(f.pending) < f.size {
		return nil
	}
	if _, err := Decode(f.protocol, f.pending, f.pixels); err == nil {
		result := append([]byte(nil), f.pending...)
		f.pending = f.pending[:0]
		return result
	}
	copy(f.pending, f.pending[1:])
	f.pending = f.pending[:len(f.pending)-1]
	return nil
}

func openSerial(cfg Config) (serial.Port, error) {
	switch cfg.Protocol {
	case "adalight", "adalight-openrgb", "tpm2", "openrgb":
	default:
		return nil, errors.New("serial input supports adalight, adalight-openrgb, tpm2 or openrgb")
	}
	if cfg.Source != "" || cfg.Listen != "" {
		return nil, errors.New("serial input cannot also specify a UDP listener or source")
	}
	if cfg.Baud < 1200 || cfg.Baud > 2000000 {
		return nil, errors.New("serial baud must be 1200..2000000")
	}
	p, err := serial.Open(cfg.Serial, &serial.Mode{BaudRate: cfg.Baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false}})
	if err != nil {
		return nil, err
	}
	if err = p.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (r *Receiver) readPacket(ctx context.Context, buffer []byte) (int, string, bool, error) {
	if r.serialPort == nil {
		n, peer, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			return 0, "", false, err
		}
		permitted := peer.IP.IsLoopback()
		if r.allowed != nil {
			permitted = peer.IP.Equal(r.allowed)
		}
		return n, peer.String(), permitted, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", false, err
		}
		if r.serialOffset < len(r.serialBuffer) {
			b := r.serialBuffer[r.serialOffset]
			r.serialOffset++
			if frame := r.framer.push(b); frame != nil {
				copy(buffer, frame)
				return len(frame), r.state.Serial, true, nil
			}
			continue
		}
		chunk := make([]byte, 512)
		n, err := r.serialPort.Read(chunk)
		if err != nil {
			return 0, "", false, err
		}
		if n == 0 {
			r.framer.pending = r.framer.pending[:0]
			continue
		}
		if n > len(chunk) {
			return 0, "", false, io.ErrShortBuffer
		}
		r.serialBuffer = chunk[:n]
		r.serialOffset = 0
	}
}
