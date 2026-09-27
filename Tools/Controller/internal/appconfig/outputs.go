package appconfig

import (
	"fmt"
	"strings"

	"pccontroller.local/controller/internal/native"
)

const (
	MaxOutputDefinitions       = 32
	MaxMelodyNotes             = 64
	PowerDownMelodyName        = "power-down"
	ProgrammingReadyMelodyName = "programming-ready"
	FinishMelodyName           = "finish"
	LostMelodyName             = "lost"
	IncorrectBeepMelodyName    = "incorrect-beep"
	ErrorBeepMelodyName        = "error-beep"
	FaultBeepMelodyName        = "fault-beep"
	SuccessCueMelodyName       = "success-cue"
	ErrorCueMelodyName         = "error-cue"
)

// Melody is PC-side configuration. The host streams one note at a time over
// the native opcode protocol; it is never written into the board EEPROM.
type Melody struct {
	Name  string       `json:"name"`
	Notes []MelodyNote `json:"notes"`
}

type MelodyNote struct {
	FrequencyHz uint16 `json:"frequency_hz"`
	DurationMS  uint16 `json:"duration_ms"`
	GapMS       uint16 `json:"gap_ms,omitempty"`
}

// StatusLEDEffect describes one compact MCU-owned status RGB animation. Repeats
// zero means loop until explicitly stopped; DurationMS remains a compatibility
// input and is converted to a bounded cycle count when Repeats is omitted.
type StatusLEDEffect struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Red            byte   `json:"red"`
	Green          byte   `json:"green"`
	Blue           byte   `json:"blue"`
	AlternateRed   byte   `json:"alternate_red,omitempty"`
	AlternateGreen byte   `json:"alternate_green,omitempty"`
	AlternateBlue  byte   `json:"alternate_blue,omitempty"`
	Brightness     byte   `json:"brightness"`
	MinBrightness  byte   `json:"min_brightness,omitempty"`
	PeriodMS       int    `json:"period_ms"`
	DurationMS     int    `json:"duration_ms,omitempty"`
	Repeats        byte   `json:"repeats,omitempty"`
}

func DefaultMelodies() []Melody {
	return []Melody{
		{
			Name: "notify",
			Notes: []MelodyNote{
				{FrequencyHz: 1047, DurationMS: 75, GapMS: 25},
				{FrequencyHz: 1319, DurationMS: 75, GapMS: 25},
				{FrequencyHz: 1568, DurationMS: 150},
			},
		},
		{
			Name: "attention",
			Notes: []MelodyNote{
				{FrequencyHz: 880, DurationMS: 110, GapMS: 45},
				{FrequencyHz: 1175, DurationMS: 110, GapMS: 45},
				{FrequencyHz: 880, DurationMS: 180},
			},
		},
		DefaultPowerDownMelody(),
		DefaultProgrammingReadyMelody(),
		DefaultFinishMelody(),
		DefaultLostMelody(),
		DefaultIncorrectBeepMelody(),
		DefaultErrorBeepMelody(),
		DefaultFaultBeepMelody(),
		DefaultSuccessCueMelody(),
		DefaultErrorCueMelody(),
	}
}

// BuiltInLegacyFeedbackMelodies is the immutable host recovery catalog. A
// watched configuration can override any name, but deleting an override must
// not erase a melody that was deliberately moved out of constrained AVR flash.
func BuiltInLegacyFeedbackMelodies() []Melody {
	return []Melody{
		DefaultFinishMelody(),
		DefaultLostMelody(),
		DefaultIncorrectBeepMelody(),
		DefaultErrorBeepMelody(),
		DefaultFaultBeepMelody(),
		DefaultSuccessCueMelody(),
		DefaultErrorCueMelody(),
	}
}

func DefaultFinishMelody() Melody {
	return Melody{Name: FinishMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 659, DurationMS: 100},
		{FrequencyHz: 784, DurationMS: 100},
		{FrequencyHz: 880, DurationMS: 250},
	}}
}

func DefaultLostMelody() Melody {
	return Melody{Name: LostMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 392, DurationMS: 100},
		{FrequencyHz: 330, DurationMS: 100},
		{FrequencyHz: 262, DurationMS: 100},
		{FrequencyHz: 196, DurationMS: 100},
	}}
}

func DefaultIncorrectBeepMelody() Melody {
	return Melody{Name: IncorrectBeepMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 2000, DurationMS: 100, GapMS: 100},
		{FrequencyHz: 2000, DurationMS: 100, GapMS: 100},
		{FrequencyHz: 2000, DurationMS: 100, GapMS: 100},
	}}
}

func DefaultErrorBeepMelody() Melody {
	return Melody{Name: ErrorBeepMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 2000, DurationMS: 10, GapMS: 10},
		{FrequencyHz: 2000, DurationMS: 10, GapMS: 10},
		{FrequencyHz: 2000, DurationMS: 10, GapMS: 10},
		{FrequencyHz: 2000, DurationMS: 10, GapMS: 10},
		{FrequencyHz: 2000, DurationMS: 10, GapMS: 10},
	}}
}

func DefaultFaultBeepMelody() Melody {
	return Melody{Name: FaultBeepMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 1000, DurationMS: 250},
		{FrequencyHz: 500, DurationMS: 500, GapMS: 5000},
	}}
}

func DefaultSuccessCueMelody() Melody {
	return Melody{Name: SuccessCueMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 1047, DurationMS: 70, GapMS: 30},
		{FrequencyHz: 1319, DurationMS: 110},
	}}
}

func DefaultErrorCueMelody() Melody {
	return Melody{Name: ErrorCueMelodyName, Notes: []MelodyNote{
		{FrequencyHz: 330, DurationMS: 90, GapMS: 50},
		{FrequencyHz: 262, DurationMS: 160},
	}}
}

// DefaultPowerDownMelody is the deterministic short PC-streamed cue used by
// guarded programming when the watched host configuration omits its override.
func DefaultPowerDownMelody() Melody {
	return Melody{
		Name: PowerDownMelodyName,
		Notes: []MelodyNote{
			{FrequencyHz: 784, DurationMS: 80, GapMS: 25},
			{FrequencyHz: 659, DurationMS: 90, GapMS: 25},
			{FrequencyHz: 523, DurationMS: 140},
		},
	}
}

// DefaultProgrammingReadyMelody mirrors the board's short rising startup cue
// after a programming latch intentionally suppressed the MCU boot melody.
func DefaultProgrammingReadyMelody() Melody {
	return Melody{
		Name: ProgrammingReadyMelodyName,
		Notes: []MelodyNote{
			{FrequencyHz: 1032, DurationMS: 70, GapMS: 60},
			{FrequencyHz: 2010, DurationMS: 70, GapMS: 60},
			{FrequencyHz: 2400, DurationMS: 120, GapMS: 150},
		},
	}
}

func DefaultStatusLEDEffects() []StatusLEDEffect {
	return []StatusLEDEffect{
		{
			Name: "attention", Kind: "flash",
			Red: 255, Green: 96, Blue: 0, Brightness: 220,
			AlternateRed: 0, AlternateGreen: 0, AlternateBlue: 0,
			PeriodMS: 700, DurationMS: 0,
		},
		{
			Name: "breathe-blue", Kind: "breathe",
			Red: 30, Green: 120, Blue: 255,
			Brightness: 200, MinBrightness: 8,
			PeriodMS: 1800, DurationMS: 0,
		},
	}
}

// EffectiveMelodies keeps the historical named feedback definitions available
// even when an old/edited watched configuration omits them. Routing remains a
// separate user choice, so availability does not make any melody autonomous.
func EffectiveMelodies(config Config) []Melody {
	result := cloneMelodies(config.Melodies)
	for _, builtIn := range BuiltInLegacyFeedbackMelodies() {
		found := false
		for _, configured := range result {
			if strings.EqualFold(configured.Name, builtIn.Name) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, builtIn)
		}
	}
	return result
}

func EffectiveStatusLEDEffects(config Config) []StatusLEDEffect {
	return append([]StatusLEDEffect(nil), config.StatusEffects...)
}

func ValidateMelody(value Melody) error {
	return validateOutputDefinitions([]Melody{value}, nil)
}

func ValidateStatusLEDEffect(value StatusLEDEffect) error {
	return validateOutputDefinitions(nil, []StatusLEDEffect{value})
}

func validateOutputDefinitions(
	melodies []Melody,
	effects []StatusLEDEffect,
) error {
	if len(melodies) > MaxOutputDefinitions {
		return fmt.Errorf(
			"melodies may contain at most %d entries",
			MaxOutputDefinitions,
		)
	}
	names := make(map[string]bool, len(melodies))
	for index, melody := range melodies {
		name := strings.ToLower(strings.TrimSpace(melody.Name))
		if name == "" || len(melody.Name) > 64 {
			return fmt.Errorf("melodies[%d].name must be 1..64 bytes", index)
		}
		if names[name] {
			return fmt.Errorf("melodies[%d].name %q is duplicated", index, melody.Name)
		}
		names[name] = true
		if len(melody.Notes) == 0 || len(melody.Notes) > MaxMelodyNotes {
			return fmt.Errorf(
				"melodies[%d].notes must contain 1..%d entries",
				index,
				MaxMelodyNotes,
			)
		}
		totalMS := uint64(0)
		for noteIndex, note := range melody.Notes {
			if note.DurationMS == 0 || note.DurationMS > 5000 {
				return fmt.Errorf(
					"melodies[%d].notes[%d].duration_ms must be 1..5000",
					index,
					noteIndex,
				)
			}
			if note.FrequencyHz != 0 &&
				(note.FrequencyHz < 20 || note.FrequencyHz > 20000) {
				return fmt.Errorf(
					"melodies[%d].notes[%d].frequency_hz must be 0 or 20..20000",
					index,
					noteIndex,
				)
			}
			if note.GapMS > 5000 {
				return fmt.Errorf(
					"melodies[%d].notes[%d].gap_ms must be 0..5000",
					index,
					noteIndex,
				)
			}
			totalMS += uint64(note.DurationMS) + uint64(note.GapMS)
		}
		if totalMS > 5*60*1000 {
			return fmt.Errorf("melodies[%d] exceeds five minutes", index)
		}
	}

	if len(effects) > MaxOutputDefinitions {
		return fmt.Errorf(
			"status_effects may contain at most %d entries",
			MaxOutputDefinitions,
		)
	}
	names = make(map[string]bool, len(effects))
	for index, effect := range effects {
		name := strings.ToLower(strings.TrimSpace(effect.Name))
		if name == "" || len(effect.Name) > 64 {
			return fmt.Errorf("status_effects[%d].name must be 1..64 bytes", index)
		}
		if names[name] {
			return fmt.Errorf(
				"status_effects[%d].name %q is duplicated",
				index,
				effect.Name,
			)
		}
		names[name] = true
		kind := strings.ToLower(strings.TrimSpace(effect.Kind))
		switch kind {
		case "flash", "breathe", "cycle", "transition":
			if effect.PeriodMS < int(native.StatusEffectMinimumPeriodMS) ||
				effect.PeriodMS > int(native.StatusEffectMaximumPeriodMS) {
				return fmt.Errorf(
					"status_effects[%d].period_ms must be %d..%d",
					index, native.StatusEffectMinimumPeriodMS,
					native.StatusEffectMaximumPeriodMS,
				)
			}
		default:
			return fmt.Errorf(
				"status_effects[%d].kind must be flash, breathe, cycle, or transition",
				index,
			)
		}
		if effect.MinBrightness > effect.Brightness {
			return fmt.Errorf(
				"status_effects[%d].min_brightness exceeds brightness",
				index,
			)
		}
		if effect.DurationMS < 0 || effect.DurationMS > 3_600_000 {
			return fmt.Errorf(
				"status_effects[%d].duration_ms must be 0..3600000",
				index,
			)
		}
	}
	return nil
}

func cloneMelodies(source []Melody) []Melody {
	result := make([]Melody, len(source))
	for index, melody := range source {
		result[index] = melody
		result[index].Notes = append([]MelodyNote(nil), melody.Notes...)
	}
	return result
}
