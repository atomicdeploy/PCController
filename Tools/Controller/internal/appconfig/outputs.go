package appconfig

import (
	"fmt"
	"strings"
)

const (
	MaxOutputDefinitions = 32
	MaxMelodyNotes       = 64
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

// StatusLEDEffect describes a host-streamed animation for the board's status
// RGB output. Kind is "flash" or "breathe"; DurationMS zero means run until
// explicitly stopped.
type StatusLEDEffect struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Red           byte   `json:"red"`
	Green         byte   `json:"green"`
	Blue          byte   `json:"blue"`
	Brightness    byte   `json:"brightness"`
	MinBrightness byte   `json:"min_brightness,omitempty"`
	PeriodMS      int    `json:"period_ms"`
	DurationMS    int    `json:"duration_ms,omitempty"`
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
	}
}

func DefaultStatusLEDEffects() []StatusLEDEffect {
	return []StatusLEDEffect{
		{
			Name: "attention", Kind: "flash",
			Red: 255, Green: 96, Blue: 0, Brightness: 220,
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

// Effective* provides useful built-ins for old configuration files that
// predate these optional fields. A non-empty configured list replaces the
// built-ins, so a file-watcher reload takes effect on the next play command.
func EffectiveMelodies(config Config) []Melody {
	if len(config.Melodies) == 0 {
		return DefaultMelodies()
	}
	return cloneMelodies(config.Melodies)
}

func EffectiveStatusLEDEffects(config Config) []StatusLEDEffect {
	if len(config.StatusEffects) == 0 {
		return DefaultStatusLEDEffects()
	}
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
		case "flash":
			if effect.PeriodMS < 200 || effect.PeriodMS > 60000 {
				return fmt.Errorf(
					"status_effects[%d].period_ms for flash must be 200..60000",
					index,
				)
			}
		case "breathe":
			if effect.PeriodMS < 400 || effect.PeriodMS > 60000 {
				return fmt.Errorf(
					"status_effects[%d].period_ms for breathe must be 400..60000",
					index,
				)
			}
		default:
			return fmt.Errorf(
				"status_effects[%d].kind must be flash or breathe",
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
