package native

import "fmt"

const StripMaximumPixels = 100

// StripFramePayloads stages contiguous RGB triples and commits exactly once.
// The transport must ACK each payload before sending the next, including show.
func StripFramePayloads(rgb []byte) ([][]byte, error) {
	if len(rgb) == 0 || len(rgb)%3 != 0 || len(rgb)/3 > StripMaximumPixels {
		return nil, fmt.Errorf("strip frame must contain 1..%d RGB triples", StripMaximumPixels)
	}
	const chunkBytes = ((MaxPayload - 2) / 3) * 3
	var frames [][]byte
	for offset := 0; offset < len(rgb); offset += chunkBytes {
		end := min(offset+chunkBytes, len(rgb))
		payload := []byte{0xFD, byte(offset / 3)}
		payload = append(payload, rgb[offset:end]...)
		frames = append(frames, payload)
	}
	return append(frames, []byte{0xFC}), nil
}

func StripConfigurePayload(count int) ([]byte, error) {
	if count < 1 || count > StripMaximumPixels {
		return nil, fmt.Errorf("strip count must be 1..%d", StripMaximumPixels)
	}
	return []byte{0xFE, byte(count)}, nil
}
