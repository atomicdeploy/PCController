// Package sfx owns Rayan Lamp's shared bundled and user-imported sound assets.
package sfx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	// MaxWaveBytes bounds both imported-file memory and the per-user asset
	// store. The bundled effects are intentionally much smaller.
	MaxWaveBytes = 4 << 20
	MaxDuration  = 10 * time.Second
)

var (
	ErrWaveTooLarge = errors.New("sfx: WAV exceeds 4 MiB")
	ErrInvalidWave  = errors.New("sfx: invalid PCM WAV")
)

// WaveInfo describes the validated PCM stream contained in one WAV asset.
type WaveInfo struct {
	Channels      uint16        `json:"channels"`
	SampleRate    uint32        `json:"sample_rate"`
	BitsPerSample uint16        `json:"bits_per_sample"`
	DataBytes     uint32        `json:"data_bytes"`
	Duration      time.Duration `json:"duration"`
}

// ValidateWave accepts a bounded, structurally complete RIFF/WAVE file using
// uncompressed integer PCM. Strict block and byte-rate checks ensure WinMM and
// browser decoders receive the same unambiguous stream.
func ValidateWave(wave []byte) (WaveInfo, error) {
	if len(wave) > MaxWaveBytes {
		return WaveInfo{}, ErrWaveTooLarge
	}
	if len(wave) < 12 ||
		!bytes.Equal(wave[0:4], []byte("RIFF")) ||
		!bytes.Equal(wave[8:12], []byte("WAVE")) {
		return WaveInfo{}, fmt.Errorf("%w: missing RIFF/WAVE header", ErrInvalidWave)
	}
	if declared := uint64(binary.LittleEndian.Uint32(wave[4:8])) + 8; declared != uint64(len(wave)) {
		return WaveInfo{}, fmt.Errorf(
			"%w: RIFF size is %d, file size is %d",
			ErrInvalidWave,
			declared,
			len(wave),
		)
	}

	var (
		formatSeen bool
		dataSeen   bool
		audioPCM   uint16
		channels   uint16
		sampleRate uint32
		byteRate   uint32
		blockAlign uint16
		bits       uint16
		dataBytes  uint32
	)
	for offset := 12; offset < len(wave); {
		if len(wave)-offset < 8 {
			return WaveInfo{}, fmt.Errorf(
				"%w: truncated chunk header",
				ErrInvalidWave,
			)
		}
		chunkID := string(wave[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(wave[offset+4 : offset+8])
		start := uint64(offset) + 8
		end := start + uint64(chunkSize)
		paddedEnd := end + uint64(chunkSize&1)
		if end > uint64(len(wave)) || paddedEnd > uint64(len(wave)) {
			return WaveInfo{}, fmt.Errorf(
				"%w: truncated %q chunk",
				ErrInvalidWave,
				chunkID,
			)
		}
		chunk := wave[int(start):int(end)]

		switch chunkID {
		case "fmt ":
			if formatSeen {
				return WaveInfo{}, fmt.Errorf(
					"%w: duplicate format chunk",
					ErrInvalidWave,
				)
			}
			if len(chunk) < 16 {
				return WaveInfo{}, fmt.Errorf(
					"%w: short format chunk",
					ErrInvalidWave,
				)
			}
			formatSeen = true
			audioPCM = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			byteRate = binary.LittleEndian.Uint32(chunk[8:12])
			blockAlign = binary.LittleEndian.Uint16(chunk[12:14])
			bits = binary.LittleEndian.Uint16(chunk[14:16])
		case "data":
			if dataSeen {
				return WaveInfo{}, fmt.Errorf(
					"%w: duplicate data chunk",
					ErrInvalidWave,
				)
			}
			dataSeen = true
			dataBytes = chunkSize
		}
		offset = int(paddedEnd)
	}

	if !formatSeen || !dataSeen {
		return WaveInfo{}, fmt.Errorf(
			"%w: format and data chunks are required",
			ErrInvalidWave,
		)
	}
	if audioPCM != 1 {
		return WaveInfo{}, fmt.Errorf(
			"%w: format %d is not integer PCM",
			ErrInvalidWave,
			audioPCM,
		)
	}
	if channels < 1 || channels > 2 {
		return WaveInfo{}, fmt.Errorf(
			"%w: channel count %d is outside 1..2",
			ErrInvalidWave,
			channels,
		)
	}
	if sampleRate < 8_000 || sampleRate > 96_000 {
		return WaveInfo{}, fmt.Errorf(
			"%w: sample rate %d is outside 8000..96000 Hz",
			ErrInvalidWave,
			sampleRate,
		)
	}
	switch bits {
	case 8, 16, 24, 32:
	default:
		return WaveInfo{}, fmt.Errorf(
			"%w: unsupported %d-bit PCM",
			ErrInvalidWave,
			bits,
		)
	}

	expectedAlign := uint32(channels) * uint32(bits/8)
	if expectedAlign == 0 ||
		uint32(blockAlign) != expectedAlign ||
		byteRate != sampleRate*expectedAlign {
		return WaveInfo{}, fmt.Errorf(
			"%w: inconsistent PCM block or byte rate",
			ErrInvalidWave,
		)
	}
	if dataBytes == 0 || dataBytes%uint32(blockAlign) != 0 {
		return WaveInfo{}, fmt.Errorf(
			"%w: PCM data is empty or not block-aligned",
			ErrInvalidWave,
		)
	}
	frames := uint64(dataBytes) / uint64(blockAlign)
	if frames > uint64(sampleRate)*uint64(MaxDuration/time.Second) {
		return WaveInfo{}, fmt.Errorf(
			"%w: duration exceeds %s",
			ErrInvalidWave,
			MaxDuration,
		)
	}
	duration := time.Duration(
		frames * uint64(time.Second) / uint64(sampleRate),
	)
	if duration <= 0 {
		return WaveInfo{}, fmt.Errorf(
			"%w: duration is zero",
			ErrInvalidWave,
		)
	}
	return WaveInfo{
		Channels:      channels,
		SampleRate:    sampleRate,
		BitsPerSample: bits,
		DataBytes:     dataBytes,
		Duration:      duration,
	}, nil
}
