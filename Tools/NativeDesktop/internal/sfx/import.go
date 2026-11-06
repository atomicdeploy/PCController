package sfx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/mewkiz/flac"
)

// ImportFileFilter is shared by the native picker and its localization key.
const ImportFileFilter = "Audio (*.wav;*.mp3;*.flac)|*.wav;*.mp3;*.flac|All files (*.*)|*.*"

// decodeImport keeps the playback/storage contract PCM WAV, but does not impose
// that contract on the user's source file. Decode only at import, never on the
// notification worker. Detection uses content, not a trusted extension or MIME.
func decodeImport(data []byte) (wave []byte, err error) {
	if len(data) > MaxWaveBytes {
		return nil, ErrWaveTooLarge
	}
	// A malformed codec input must fail the import, not tear down the desktop.
	defer func() {
		if recovered := recover(); recovered != nil {
			wave = nil
			err = fmt.Errorf("%w: malformed audio", ErrInvalidWave)
		}
	}()
	if bytes.HasPrefix(data, []byte("RIFF")) {
		_, err := ValidateWave(data)
		return data, err // Preserve existing WAV identities byte-for-byte.
	}
	switch {
	case bytes.HasPrefix(data, []byte("fLaC")):
		wave, err = decodeFLAC(data)
	case bytes.HasPrefix(data, []byte("ID3")) ||
		(len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0):
		wave, err = decodeMP3(data)
	default:
		return nil, fmt.Errorf("%w: choose PCM WAV, MP3 or FLAC", ErrInvalidWave)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidWave, err)
	}
	_, err = ValidateWave(wave)
	return wave, err
}

// pcmImport bounds expansion as samples arrive, independently of codec metadata.
// A long compressed file is rejected, never silently clipped to ten seconds.
type pcmImport struct {
	rate, channels, bits int
	limit                int
	data                 []byte
}

func newPCMImport(rate, channels, bits int) (*pcmImport, error) {
	if rate < 8000 || rate > 96000 || channels < 1 || channels > 2 ||
		(bits != 16 && bits != 24 && bits != 32) {
		return nil, fmt.Errorf("unsupported audio format: %d Hz, %d channels, %d bits", rate, channels, bits)
	}
	align := channels * (bits / 8)
	limit := min(rate*int(MaxDuration/time.Second)*align, (MaxWaveBytes-44)/align*align)
	return &pcmImport{rate: rate, channels: channels, bits: bits, limit: limit}, nil
}

func (p *pcmImport) append(data []byte) error {
	if len(data) > p.limit-len(p.data) {
		return fmt.Errorf("audio exceeds %s or 4 MiB decoded PCM", MaxDuration)
	}
	p.data = append(p.data, data...)
	return nil
}

func (p *pcmImport) wave() []byte {
	wave := make([]byte, 44+len(p.data)+(len(p.data)&1))
	copy(wave, "RIFF")
	binary.LittleEndian.PutUint32(wave[4:], uint32(len(wave)-8))
	copy(wave[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wave[16:], 16)
	binary.LittleEndian.PutUint16(wave[20:], 1)
	binary.LittleEndian.PutUint16(wave[22:], uint16(p.channels))
	binary.LittleEndian.PutUint32(wave[24:], uint32(p.rate))
	align := p.channels * (p.bits / 8)
	binary.LittleEndian.PutUint32(wave[28:], uint32(p.rate*align))
	binary.LittleEndian.PutUint16(wave[32:], uint16(align))
	binary.LittleEndian.PutUint16(wave[34:], uint16(p.bits))
	copy(wave[36:], "data")
	binary.LittleEndian.PutUint32(wave[40:], uint32(len(p.data)))
	copy(wave[44:], p.data)
	return wave
}

func decodeMP3(data []byte) ([]byte, error) {
	decoder, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// go-mp3 emits signed little-endian 16-bit stereo even for mono sources.
	pcm, err := newPCMImport(decoder.SampleRate(), 2, 16)
	if err != nil {
		return nil, err
	}
	decoded, err := io.ReadAll(io.LimitReader(decoder, int64(pcm.limit)+1))
	if err != nil {
		return nil, err
	}
	if err := pcm.append(decoded); err != nil {
		return nil, err
	}
	if decoder.Length() >= 0 && decoder.Length() != int64(len(decoded)) {
		return nil, fmt.Errorf("truncated MP3 audio")
	}
	return pcm.wave(), nil
}

func decodeFLAC(data []byte) ([]byte, error) {
	decoder, err := flac.New(bytes.NewReader(data)) // Skip artwork/optional metadata.
	if err != nil {
		return nil, err
	}
	info := decoder.Info
	bits := max(16, (int(info.BitsPerSample)+7)/8*8)
	pcm, err := newPCMImport(int(info.SampleRate), int(info.NChannels), bits)
	if err != nil {
		return nil, err
	}
	for {
		frame, err := decoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// FLAC headers may inherit sample rate/precision from STREAMINFO.
		if frame.SampleRate == 0 {
			frame.SampleRate = info.SampleRate
		}
		if frame.BitsPerSample == 0 {
			frame.BitsPerSample = info.BitsPerSample
		}
		if frame.Channels.Count() != pcm.channels || frame.SampleRate != info.SampleRate ||
			frame.BitsPerSample != info.BitsPerSample {
			return nil, fmt.Errorf("FLAC format changes within stream")
		}
		if int(frame.BlockSize)*pcm.channels*(bits/8) > pcm.limit-len(pcm.data) {
			return nil, fmt.Errorf("audio exceeds %s or 4 MiB decoded PCM", MaxDuration)
		}
		if err := frame.Parse(); err != nil {
			return nil, err
		}
		n := len(frame.Subframes[0].Samples)
		length := n * pcm.channels * (bits / 8)
		if length > pcm.limit-len(pcm.data) {
			return nil, fmt.Errorf("audio exceeds %s or 4 MiB decoded PCM", MaxDuration)
		}
		block := make([]byte, 0, length)
		for i := 0; i < n; i++ {
			for _, subframe := range frame.Subframes {
				if len(subframe.Samples) != n {
					return nil, fmt.Errorf("inconsistent FLAC channel length")
				}
				value := uint32(subframe.Samples[i]) << (bits - int(info.BitsPerSample))
				for b := 0; b < bits/8; b++ {
					block = append(block, byte(value>>(b*8)))
				}
			}
		}
		if err := pcm.append(block); err != nil {
			return nil, err
		}
	}
	frames := len(pcm.data) / (pcm.channels * (bits / 8))
	if info.NSamples > 0 && uint64(frames) != info.NSamples {
		return nil, fmt.Errorf("truncated FLAC audio")
	}
	return pcm.wave(), nil
}
