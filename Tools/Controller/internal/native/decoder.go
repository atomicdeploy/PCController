package native

const maxEncodedFrame = MaxRawFrame + MaxRawFrame/254 + 1

type Decoder struct {
	buffer   []byte
	overflow bool
}

// Feed accepts arbitrary serial chunks and returns every complete frame.
// A malformed frame is isolated to its terminating zero; later frames remain
// decodable.
func (d *Decoder) Feed(chunk []byte) ([]Frame, []error) {
	var frames []Frame
	var errs []error

	for _, value := range chunk {
		if value == 0 {
			if d.overflow {
				errs = append(errs, ErrReceiveOverflow)
			} else if len(d.buffer) != 0 {
				frame, err := Decode(d.buffer)
				if err != nil {
					errs = append(errs, err)
				} else {
					frames = append(frames, frame)
				}
			}
			d.buffer = d.buffer[:0]
			d.overflow = false
			continue
		}

		if d.overflow {
			continue
		}
		if len(d.buffer) >= maxEncodedFrame {
			d.buffer = d.buffer[:0]
			d.overflow = true
			continue
		}
		d.buffer = append(d.buffer, value)
	}
	return frames, errs
}

func (d *Decoder) Reset() {
	d.buffer = d.buffer[:0]
	d.overflow = false
}
