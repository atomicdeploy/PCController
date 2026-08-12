package appconfig

import (
	"fmt"
	"strings"

	"pccontroller.local/controller/internal/native"
)

// RGBColor is a host-owned status-light color. The board still owns the
// safety-critical Boot, HOT, Fault, and offline fallback modes.
type RGBColor struct {
	Red   byte `json:"red"`
	Green byte `json:"green"`
	Blue  byte `json:"blue"`
}

// StatusLEDVisual defines one configurable visual state. AlternateColor is
// used by flash/cycle/transition effects. The legacy name crossfade remains a
// compatibility alias for cycle when older HOST configuration is loaded.
type StatusLEDVisual struct {
	Effect            string   `json:"effect"`
	Color             RGBColor `json:"color"`
	AlternateColor    RGBColor `json:"alternate_color"`
	Brightness        byte     `json:"brightness"`
	MinimumBrightness byte     `json:"minimum_brightness"`
	PeriodMS          int      `json:"period_ms"`
}

// StatusLEDPolicy maps live host/board state to the shared PWM RGB indicator.
// It belongs to HOST configuration; it is deliberately not stored in MCU EEPROM.
type StatusLEDPolicy struct {
	Enabled                 bool            `json:"enabled"`
	TransitionMS            int             `json:"transition_ms"`
	StepMS                  int             `json:"step_ms"`
	RFHoldMS                int             `json:"rf_hold_ms"`
	DoorCueHoldMS           int             `json:"door_cue_hold_ms"`
	HotThresholdCentiC      int16           `json:"hot_threshold_centi_c"`
	Idle                    StatusLEDVisual `json:"idle"`
	Running                 StatusLEDVisual `json:"running"`
	BluetoothAudioConnected StatusLEDVisual `json:"bluetooth_audio_connected"`
	BluetoothAudioSearching StatusLEDVisual `json:"bluetooth_audio_searching"`
	BluetoothAudioOff       StatusLEDVisual `json:"bluetooth_audio_off"`
	RFActivity              StatusLEDVisual `json:"rf_activity"`
	DoorOpened              StatusLEDVisual `json:"door_opened"`
	DoorClosed              StatusLEDVisual `json:"door_closed"`
	Hot                     StatusLEDVisual `json:"hot"`
	RunningDoorOpen         StatusLEDVisual `json:"running_door_open"`
	PCOffline               StatusLEDVisual `json:"pc_offline"`
}

// DefaultStatusLEDPolicy follows the priority palette: offline, HOT,
// Running+door-open, transient door edge, RF, Running, BT Audio, then Idle.
func DefaultStatusLEDPolicy() StatusLEDPolicy {
	return StatusLEDPolicy{
		Enabled: true, TransitionMS: 420, StepMS: 50,
		RFHoldMS: 1400, DoorCueHoldMS: 1600, HotThresholdCentiC: 5000,
		Idle:                    steadyVisual(RGBColor{Green: 255}, 128),
		Running:                 steadyVisual(RGBColor{Red: 255, Green: 150}, 160),
		BluetoothAudioConnected: steadyVisual(RGBColor{Blue: 255}, 150),
		BluetoothAudioSearching: StatusLEDVisual{
			Effect: "breathe", Color: RGBColor{Green: 80, Blue: 255},
			Brightness: 145, MinimumBrightness: 18, PeriodMS: 1800,
		},
		BluetoothAudioOff: StatusLEDVisual{
			Effect: "cycle", Color: RGBColor{Green: 255},
			AlternateColor: RGBColor{Red: 255}, Brightness: 120,
			PeriodMS: 1800,
		},
		RFActivity: StatusLEDVisual{
			Effect: "breathe", Color: RGBColor{Red: 190, Blue: 255},
			Brightness: 190, MinimumBrightness: 20, PeriodMS: 900,
		},
		// Door edge audio is deliberately not host-owned here. The existing
		// Settings.DoorAudioEnabled MCU EEPROM setting remains authoritative.
		DoorOpened: steadyVisual(RGBColor{Red: 255, Green: 96}, 190),
		DoorClosed: steadyVisual(RGBColor{Green: 255, Blue: 72}, 170),
		Hot: StatusLEDVisual{
			Effect: "breathe", Color: RGBColor{Red: 255},
			AlternateColor: RGBColor{Red: 255, Green: 72},
			Brightness:     255, MinimumBrightness: 72, PeriodMS: 1000,
		},
		RunningDoorOpen: StatusLEDVisual{
			Effect: "flash", Color: RGBColor{Red: 255},
			Brightness: 255, PeriodMS: 640,
		},
		PCOffline: steadyVisual(RGBColor{Red: 255}, 180),
	}
}

func steadyVisual(color RGBColor, brightness byte) StatusLEDVisual {
	return StatusLEDVisual{Effect: "steady", Color: color, Brightness: brightness}
}

func validateStatusLEDPolicy(policy StatusLEDPolicy) error {
	if policy.TransitionMS < 0 || policy.TransitionMS > 10_000 {
		return fmt.Errorf("integrations.status_led.transition_ms must be 0..10000")
	}
	if policy.StepMS < 20 || policy.StepMS > 1000 {
		return fmt.Errorf("integrations.status_led.step_ms must be 20..1000")
	}
	if policy.RFHoldMS < 100 || policy.RFHoldMS > 60_000 {
		return fmt.Errorf("integrations.status_led.rf_hold_ms must be 100..60000")
	}
	if policy.DoorCueHoldMS < 100 || policy.DoorCueHoldMS > 60_000 {
		return fmt.Errorf("integrations.status_led.door_cue_hold_ms must be 100..60000")
	}
	if policy.HotThresholdCentiC < 3000 || policy.HotThresholdCentiC > 12500 {
		return fmt.Errorf("integrations.status_led.hot_threshold_centi_c must be 3000..12500")
	}
	visuals := []struct {
		name  string
		value StatusLEDVisual
	}{
		{"idle", policy.Idle},
		{"running", policy.Running},
		{"bluetooth_audio_connected", policy.BluetoothAudioConnected},
		{"bluetooth_audio_searching", policy.BluetoothAudioSearching},
		{"bluetooth_audio_off", policy.BluetoothAudioOff},
		{"rf_activity", policy.RFActivity},
		{"door_opened", policy.DoorOpened},
		{"door_closed", policy.DoorClosed},
		{"hot", policy.Hot},
		{"running_door_open", policy.RunningDoorOpen},
		{"pc_offline", policy.PCOffline},
	}
	for _, item := range visuals {
		if err := validateStatusLEDVisual(item.value); err != nil {
			return fmt.Errorf("integrations.status_led.%s: %w", item.name, err)
		}
	}
	return nil
}

func validateStatusLEDVisual(visual StatusLEDVisual) error {
	if visual.MinimumBrightness > visual.Brightness {
		return fmt.Errorf("minimum_brightness exceeds brightness")
	}
	switch strings.ToLower(strings.TrimSpace(visual.Effect)) {
	case "steady":
		if visual.PeriodMS != 0 {
			return fmt.Errorf("steady period_ms must be zero")
		}
	case "flash":
		if visual.PeriodMS < int(native.StatusEffectMinimumPeriodMS) || visual.PeriodMS > int(native.StatusEffectMaximumPeriodMS) {
			return fmt.Errorf("flash period_ms must be %d..%d", native.StatusEffectMinimumPeriodMS, native.StatusEffectMaximumPeriodMS)
		}
	case "breathe", "cycle", "transition", "crossfade":
		if visual.PeriodMS < int(native.StatusEffectMinimumPeriodMS) || visual.PeriodMS > int(native.StatusEffectMaximumPeriodMS) {
			return fmt.Errorf("%s period_ms must be %d..%d", visual.Effect, native.StatusEffectMinimumPeriodMS, native.StatusEffectMaximumPeriodMS)
		}
	default:
		return fmt.Errorf("effect must be steady, flash, breathe, cycle, or transition")
	}
	return nil
}
