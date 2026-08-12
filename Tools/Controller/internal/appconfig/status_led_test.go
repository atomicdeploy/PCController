package appconfig

import (
	"reflect"
	"testing"
)

func TestDefaultStatusLEDPolicyIsValid(t *testing.T) {
	policy := DefaultStatusLEDPolicy()
	if err := validateStatusLEDPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if !policy.Enabled || policy.Idle.Color.Green == 0 ||
		policy.RunningDoorOpen.Effect != "flash" ||
		policy.Hot.Effect != "breathe" || policy.DoorCueHoldMS == 0 ||
		policy.DoorOpened == policy.DoorClosed {
		t.Fatalf("unexpected default status policy: %#v", policy)
	}
}

func TestStatusLEDPolicyRejectsUnsafeCadence(t *testing.T) {
	policy := DefaultStatusLEDPolicy()
	policy.StepMS = 5
	if err := validateStatusLEDPolicy(policy); err == nil {
		t.Fatal("expected status LED cadence validation error")
	}
	policy = DefaultStatusLEDPolicy()
	policy.RunningDoorOpen.PeriodMS = 100
	if err := validateStatusLEDPolicy(policy); err == nil {
		t.Fatal("expected critical flash period validation error")
	}
	policy = DefaultStatusLEDPolicy()
	policy.DoorCueHoldMS = 0
	if err := validateStatusLEDPolicy(policy); err == nil {
		t.Fatal("expected door cue hold validation error")
	}
	policy = DefaultStatusLEDPolicy()
	policy.DoorOpened.PeriodMS = 500
	if err := validateStatusLEDPolicy(policy); err == nil {
		t.Fatal("expected door-open visual validation error")
	}
}

func TestStatusLEDPolicyDoesNotDuplicateDoorAudioEEPROMSetting(t *testing.T) {
	// Door audio remains the MCU-owned Settings.DoorAudioEnabled EEPROM setting;
	// this host policy only configures the transient RGB edge visuals.
	if _, exists := reflect.TypeOf(StatusLEDPolicy{}).FieldByName("DoorAudioEnabled"); exists {
		t.Fatal("host status LED policy must not duplicate the board door-audio setting")
	}
}

func TestStatusLEDPolicyAcceptsCanonicalBoardEffectsAndLegacyAlias(t *testing.T) {
	for _, effect := range []string{"breathe", "flash", "cycle", "transition", "crossfade"} {
		visual := StatusLEDVisual{
			Effect: effect, Color: RGBColor{Red: 240, Green: 80, Blue: 20},
			AlternateColor: RGBColor{Red: 20, Green: 160, Blue: 240},
			Brightness:     200, MinimumBrightness: 20, PeriodMS: 640,
		}
		if err := validateStatusLEDVisual(visual); err != nil {
			t.Fatalf("%s rejected: %v", effect, err)
		}
	}
}
