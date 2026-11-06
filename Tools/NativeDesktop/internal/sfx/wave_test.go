package sfx

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/sfxgen"
)

func TestValidateWaveAcceptsBundledPCM(t *testing.T) {
	t.Parallel()
	for _, asset := range sfxgen.GenerateCatalog() {
		info, err := ValidateWave(asset.Data)
		if err != nil {
			t.Fatalf("%s: %v", asset.Cue, err)
		}
		if info.Channels != 1 ||
			info.SampleRate != sfxgen.SampleRate ||
			info.BitsPerSample != sfxgen.BitsPerSample ||
			info.Duration <= 0 ||
			info.Duration > MaxDuration {
			t.Fatalf("%s info = %+v", asset.Cue, info)
		}
	}
}

func TestValidateWaveRejectsMalformedOrUnsafeStreams(t *testing.T) {
	t.Parallel()
	base, err := sfxgen.Generate("message")
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(callback func([]byte)) []byte {
		result := append([]byte(nil), base...)
		callback(result)
		return result
	}
	longWave := pcmFixture(t, 11*time.Second, 44_100, 1, 16)
	emptyWave := pcmFixture(t, 0, 44_100, 1, 16)
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "too large", data: make([]byte, MaxWaveBytes+1), want: ErrWaveTooLarge},
		{name: "short", data: []byte("RIFF"), want: ErrInvalidWave},
		{
			name: "not wave",
			data: mutate(func(data []byte) { copy(data[8:12], "NOPE") }),
			want: ErrInvalidWave,
		},
		{
			name: "wrong RIFF size",
			data: mutate(func(data []byte) {
				binary.LittleEndian.PutUint32(data[4:8], 1)
			}),
			want: ErrInvalidWave,
		},
		{
			name: "compressed",
			data: mutate(func(data []byte) {
				binary.LittleEndian.PutUint16(data[20:22], 3)
			}),
			want: ErrInvalidWave,
		},
		{
			name: "too many channels",
			data: mutate(func(data []byte) {
				binary.LittleEndian.PutUint16(data[22:24], 3)
			}),
			want: ErrInvalidWave,
		},
		{
			name: "bad byte rate",
			data: mutate(func(data []byte) {
				binary.LittleEndian.PutUint32(data[28:32], 1)
			}),
			want: ErrInvalidWave,
		},
		{name: "empty", data: emptyWave, want: ErrInvalidWave},
		{name: "too long", data: longWave, want: ErrInvalidWave},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateWave(test.data)
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateWave() error = %v, want %v", err, test.want)
			}
		})
	}
}

func pcmFixture(
	t *testing.T,
	duration time.Duration,
	sampleRate uint32,
	channels uint16,
	bits uint16,
) []byte {
	t.Helper()
	blockAlign := channels * (bits / 8)
	frames := uint64(duration) * uint64(sampleRate) / uint64(time.Second)
	dataSize := uint32(frames * uint64(blockAlign))
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], channels)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(
		result[28:32],
		sampleRate*uint32(blockAlign),
	)
	binary.LittleEndian.PutUint16(result[32:34], blockAlign)
	binary.LittleEndian.PutUint16(result[34:36], bits)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], dataSize)
	return result
}
