// Package sfxgen deterministically synthesizes Rayan Lamp's bundled sound
// effects. The generated assets are small, dependency-free PCM WAV files that
// can be played byte-for-byte by both Win32 and browser audio engines.
package sfxgen

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	SampleRate    = 44_100
	Channels      = 1
	BitsPerSample = 16
)

// Asset is one deterministic bundled sound.
type Asset struct {
	Cue         string
	FileName    string
	DisplayName string
	Data        []byte
}

type specification struct {
	cue         string
	fileName    string
	displayName string
	duration    time.Duration
	sample      func(float64, float64) float64
}

var specifications = []specification{
	{
		cue:         "on",
		fileName:    "on.wav",
		displayName: "Lamp on",
		duration:    360 * time.Millisecond,
		sample:      onSample,
	},
	{
		cue:         "off",
		fileName:    "off.wav",
		displayName: "Lamp off",
		duration:    330 * time.Millisecond,
		sample:      offSample,
	},
	{
		cue:         "message",
		fileName:    "message.wav",
		displayName: "Lamp message",
		duration:    390 * time.Millisecond,
		sample:      messageSample,
	},
	{
		cue:         "error",
		fileName:    "error.wav",
		displayName: "Lamp error",
		duration:    440 * time.Millisecond,
		sample:      errorSample,
	},
}

// GenerateCatalog returns a fresh copy of every bundled sound in stable cue
// order.
func GenerateCatalog() []Asset {
	result := make([]Asset, 0, len(specifications))
	for _, spec := range specifications {
		result = append(result, Asset{
			Cue:         spec.cue,
			FileName:    spec.fileName,
			DisplayName: spec.displayName,
			Data:        render(spec),
		})
	}
	return result
}

// Generate returns one bundled sound by semantic cue.
func Generate(cue string) ([]byte, error) {
	for _, spec := range specifications {
		if spec.cue == cue {
			return render(spec), nil
		}
	}
	return nil, fmt.Errorf("sfxgen: unknown cue %q", cue)
}

func render(spec specification) []byte {
	sampleCount := int(
		(spec.duration.Nanoseconds()*SampleRate +
			int64(time.Second)/2) / int64(time.Second),
	)
	const bytesPerSample = BitsPerSample / 8
	dataSize := sampleCount * Channels * bytesPerSample
	wave := make([]byte, 44+dataSize)

	copy(wave[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wave[4:8], uint32(len(wave)-8))
	copy(wave[8:12], "WAVE")
	copy(wave[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wave[16:20], 16)
	binary.LittleEndian.PutUint16(wave[20:22], 1)
	binary.LittleEndian.PutUint16(wave[22:24], Channels)
	binary.LittleEndian.PutUint32(wave[24:28], SampleRate)
	blockAlign := uint16(Channels * bytesPerSample)
	binary.LittleEndian.PutUint32(
		wave[28:32],
		SampleRate*uint32(blockAlign),
	)
	binary.LittleEndian.PutUint16(wave[32:34], blockAlign)
	binary.LittleEndian.PutUint16(wave[34:36], BitsPerSample)
	copy(wave[36:40], "data")
	binary.LittleEndian.PutUint32(wave[40:44], uint32(dataSize))

	durationSeconds := spec.duration.Seconds()
	for index := 0; index < sampleCount; index++ {
		t := float64(index) / SampleRate
		value := spec.sample(t, durationSeconds)
		value *= edgeEnvelope(t, durationSeconds, 8*time.Millisecond, 70*time.Millisecond)
		value = math.Max(-0.92, math.Min(0.92, value))
		encoded := int16(math.Round(value * math.MaxInt16))
		binary.LittleEndian.PutUint16(
			wave[44+index*bytesPerSample:],
			uint16(encoded),
		)
	}
	return wave
}

func onSample(t, duration float64) float64 {
	progress := clamp01(t / duration)
	fundamental := chirp(t, duration, 330, 690)
	harmony := chirp(t, duration, 495, 920)
	shimmer := 0.0
	if t >= 0.13 {
		local := t - 0.13
		shimmer = 0.18 *
			segmentEnvelope(local, duration-0.13, 5*time.Millisecond, 90*time.Millisecond) *
			math.Sin(phase(local, 1_045))
	}
	return (0.33+0.07*progress)*fundamental + 0.18*harmony + shimmer
}

func offSample(t, duration float64) float64 {
	progress := clamp01(t / duration)
	fundamental := chirp(t, duration, 680, 290)
	harmony := chirp(t, duration, 510, 218)
	return (0.38-0.08*progress)*fundamental + 0.17*harmony
}

func messageSample(t, _ float64) float64 {
	first := bell(t, 0, 0.19, 660, 0.42)
	second := bell(t, 0.15, 0.24, 880, 0.37)
	air := bell(t, 0.17, 0.20, 1_320, 0.12)
	return first + second + air
}

func errorSample(t, _ float64) float64 {
	first := softPulse(t, 0, 0.22, 230, 0.38)
	second := softPulse(t, 0.22, 0.22, 185, 0.36)
	return first + second
}

func chirp(t, duration, from, to float64) float64 {
	if duration <= 0 {
		return 0
	}
	slope := (to - from) / duration
	return math.Sin(2 * math.Pi * (from*t + 0.5*slope*t*t))
}

func phase(t, frequency float64) float64 {
	return 2 * math.Pi * frequency * t
}

func bell(t, start, duration, frequency, amplitude float64) float64 {
	local := t - start
	if local < 0 || local >= duration {
		return 0
	}
	envelope := segmentEnvelope(
		local,
		duration,
		4*time.Millisecond,
		85*time.Millisecond,
	)
	decay := math.Exp(-3.1 * local / duration)
	return amplitude * envelope * decay *
		(math.Sin(phase(local, frequency)) +
			0.24*math.Sin(phase(local, frequency*2.01)))
}

func softPulse(t, start, duration, frequency, amplitude float64) float64 {
	local := t - start
	if local < 0 || local >= duration {
		return 0
	}
	envelope := segmentEnvelope(
		local,
		duration,
		12*time.Millisecond,
		55*time.Millisecond,
	)
	return amplitude * envelope *
		(math.Sin(phase(local, frequency)) +
			0.16*math.Sin(phase(local, frequency*1.5)))
}

func edgeEnvelope(
	t float64,
	duration float64,
	attack time.Duration,
	release time.Duration,
) float64 {
	return segmentEnvelope(t, duration, attack, release)
}

func segmentEnvelope(
	t float64,
	duration float64,
	attack time.Duration,
	release time.Duration,
) float64 {
	if t < 0 || t >= duration || duration <= 0 {
		return 0
	}
	attackSeconds := attack.Seconds()
	releaseSeconds := release.Seconds()
	value := 1.0
	if attackSeconds > 0 && t < attackSeconds {
		value *= smoothstep(t / attackSeconds)
	}
	if releaseSeconds > 0 && t > duration-releaseSeconds {
		value *= smoothstep((duration - t) / releaseSeconds)
	}
	return value
}

func smoothstep(value float64) float64 {
	value = clamp01(value)
	return value * value * (3 - 2*value)
}

func clamp01(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}
