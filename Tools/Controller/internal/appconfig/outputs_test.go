package appconfig

import "testing"

func TestDefaultOutputDefinitionsValidateAndClone(t *testing.T) {
	value := Defaults()
	if len(value.Melodies) == 0 || len(value.StatusEffects) == 0 {
		t.Fatal("default output definitions are missing")
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	copyValue := clone(value)
	copyValue.Melodies[0].Notes[0].FrequencyHz++
	if copyValue.Melodies[0].Notes[0].FrequencyHz ==
		value.Melodies[0].Notes[0].FrequencyHz {
		t.Fatal("melody notes were shallow-copied")
	}
}

func TestOutputDefinitionValidation(t *testing.T) {
	value := Defaults()
	value.Melodies = []Melody{{
		Name: "bad",
		Notes: []MelodyNote{{
			FrequencyHz: 1,
			DurationMS:  10,
		}},
	}}
	if err := value.Validate(); err == nil {
		t.Fatal("expected invalid melody frequency")
	}

	value = Defaults()
	value.StatusEffects = []StatusLEDEffect{{
		Name: "bad", Kind: "breathe",
		Brightness: 10, MinBrightness: 20,
		PeriodMS: 100,
	}}
	if err := value.Validate(); err == nil {
		t.Fatal("expected invalid status effect")
	}
}

func TestOldConfigGetsEffectiveBuiltinsWithoutPersistenceMixing(t *testing.T) {
	value := Defaults()
	value.Melodies = nil
	value.StatusEffects = nil
	if len(EffectiveMelodies(value)) == 0 ||
		len(EffectiveStatusLEDEffects(value)) == 0 {
		t.Fatal("legacy configuration did not receive effective built-ins")
	}
}
