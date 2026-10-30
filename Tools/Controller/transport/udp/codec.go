// Package udp implements the shared Digitalogic binary host datagram contract.
// It reuses firmware COBS and CRC-8/ATM, but never interprets host RPC as a
// raw board opcode. All hardware admission remains in the host dispatcher.
package udp

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf8"

	"pccontroller.local/controller/internal/native"
)

const MaxMessage = 256 * 1024
const ChunkSize = 1024
const DiscoveryAddress = "239.255.87.87:8790"
const (
	Discover byte = iota + 1
	Announce
	Request
	Response
	Subscribe
	Event
	Unsubscribe
)

type Packet struct {
	Kind         byte
	Source       [16]byte
	ID           uint64
	Index, Count uint16
	Data         []byte
}

func EncodePacket(p Packet) ([]byte, error) {
	if p.Kind < Discover || p.Kind > Unsubscribe || p.ID == 0 || p.Count == 0 || p.Count > 256 || p.Index >= p.Count || len(p.Data) > ChunkSize || (p.Index+1 < p.Count && len(p.Data) != ChunkSize) {
		return nil, errors.New("invalid UDP envelope")
	}
	b := append([]byte("DGLU"), p.Kind)
	b = append(b, p.Source[:]...)
	b = binary.LittleEndian.AppendUint64(b, p.ID)
	b = binary.LittleEndian.AppendUint16(b, p.Index)
	b = binary.LittleEndian.AppendUint16(b, p.Count)
	b = append(b, p.Data...)
	b = append(b, native.CRC8(b))
	return append(native.COBSEncode(b), 0), nil
}

func DecodePacket(b []byte) (Packet, error) {
	var p Packet
	if len(b) < 2 || len(b) > 1100 || b[len(b)-1] != 0 {
		return p, errors.New("invalid datagram size or delimiter")
	}
	raw, err := native.COBSDecode(b[:len(b)-1])
	if err != nil || len(raw) < 34 || string(raw[:4]) != "DGLU" || native.CRC8(raw[:len(raw)-1]) != raw[len(raw)-1] {
		return p, errors.New("invalid datagram framing or CRC")
	}
	p.Kind = raw[4]
	copy(p.Source[:], raw[5:21])
	p.ID = binary.LittleEndian.Uint64(raw[21:29])
	p.Index = binary.LittleEndian.Uint16(raw[29:31])
	p.Count = binary.LittleEndian.Uint16(raw[31:33])
	p.Data = append([]byte(nil), raw[33:len(raw)-1]...)
	if p.Kind < Discover || p.Kind > Unsubscribe || p.ID == 0 || p.Count == 0 || p.Count > 256 || p.Index >= p.Count || len(p.Data) > ChunkSize || (p.Index+1 < p.Count && len(p.Data) != ChunkSize) {
		return Packet{}, errors.New("invalid fragment")
	}
	return p, nil
}

// Marshal transcodes the current application's typed JSON data model into
// binary values. No JSON text is carried over UDP. Integers remain exact.
func Marshal(value any) ([]byte, error) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(jsonBytes))
	d.UseNumber()
	if err = d.Decode(&v); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = encodeValue(&b, v, 0)
	if b.Len() > MaxMessage {
		return nil, errors.New("binary message exceeds 256 KiB")
	}
	return b.Bytes(), err
}

func encodeValue(b *bytes.Buffer, v any, depth int) error {
	if depth > 32 {
		return errors.New("binary nesting exceeds 32")
	}
	length := func(n int) { _ = binary.Write(b, binary.LittleEndian, uint32(n)) }
	switch v := v.(type) {
	case nil:
		b.WriteByte(0)
	case bool:
		if v {
			b.WriteByte(2)
		} else {
			b.WriteByte(1)
		}
	case json.Number:
		if i, e := strconv.ParseInt(string(v), 10, 64); e == nil {
			b.WriteByte(3)
			_ = binary.Write(b, binary.LittleEndian, i)
		} else if u, e := strconv.ParseUint(string(v), 10, 64); e == nil {
			b.WriteByte(4)
			_ = binary.Write(b, binary.LittleEndian, u)
		} else {
			f, e := strconv.ParseFloat(string(v), 64)
			if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return errors.New("invalid number")
			}
			b.WriteByte(5)
			_ = binary.Write(b, binary.LittleEndian, f)
		}
	case string:
		b.WriteByte(6)
		length(len(v))
		b.WriteString(v)
	case []any:
		b.WriteByte(7)
		length(len(v))
		for _, x := range v {
			if err := encodeValue(b, x, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		b.WriteByte(8)
		length(len(v))
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := encodeValue(b, k, depth+1); err != nil {
				return err
			}
			if err := encodeValue(b, v[k], depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported binary value %T", v)
	}
	if b.Len() > MaxMessage {
		return errors.New("binary message exceeds 256 KiB")
	}
	return nil
}

func Unmarshal(b []byte, out any) error {
	if len(b) > MaxMessage {
		return errors.New("oversize binary message")
	}
	r := bytes.NewReader(b)
	v, err := decodeValue(r, 0)
	if err != nil {
		return err
	}
	if r.Len() != 0 {
		return errors.New("trailing binary data")
	}
	j, err := json.Marshal(v)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(j))
	d.UseNumber()
	return d.Decode(out)
}

func decodeValue(r *bytes.Reader, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("binary nesting exceeds 32")
	}
	t, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if t == 0 {
		return nil, nil
	}
	if t == 1 || t == 2 {
		return t == 2, nil
	}
	if t >= 3 && t <= 5 {
		var n uint64
		if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		switch t {
		case 3:
			return int64(n), nil
		case 4:
			return n, nil
		default:
			f := math.Float64frombits(n)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, errors.New("invalid float")
			}
			return f, nil
		}
	}
	var n uint32
	if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if uint64(n) > uint64(r.Len()) {
		return nil, errors.New("invalid binary length")
	}
	switch t {
	case 6:
		b := make([]byte, n)
		_, err = r.Read(b)
		if !utf8.Valid(b) {
			return nil, errors.New("invalid UTF-8")
		}
		return string(b), err
	case 7:
		v := make([]any, 0, n)
		for i := uint32(0); i < n; i++ {
			x, e := decodeValue(r, depth+1)
			if e != nil {
				return nil, e
			}
			v = append(v, x)
		}
		return v, nil
	case 8:
		v := make(map[string]any)
		for i := uint32(0); i < n; i++ {
			k, e := decodeValue(r, depth+1)
			if e != nil {
				return nil, e
			}
			s, ok := k.(string)
			if !ok {
				return nil, errors.New("object key is not text")
			}
			if _, ok = v[s]; ok {
				return nil, errors.New("duplicate object key")
			}
			x, e := decodeValue(r, depth+1)
			if e != nil {
				return nil, e
			}
			v[s] = x
		}
		return v, nil
	default:
		return nil, errors.New("unknown binary tag")
	}
}
