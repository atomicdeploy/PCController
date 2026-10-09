package appconfig

import "testing"

func TestDefaultOutputDefinitionsValidateAndClone(t *testing.T) {
	value := Defaults()
	if len(value.Melodies) == 0 || len(value.StatusEffects) == 0 {
		t.Fatal("default output definitions are missing")
	}
	foundPowerDown := false
	foundProgrammingReady := false
	for _, melody := range value.Melodies {
		if melody.Name == PowerDownMelodyName {
			foundPowerDown = len(melody.Notes) == 3 &&
				melody.Notes[0].FrequencyHz > melody.Notes[2].FrequencyHz
		}
		if melody.Name == ProgrammingReadyMelodyName {
			foundProgrammingReady = len(melody.Notes) == 3 &&
				melody.Notes[0].FrequencyHz < melody.Notes[2].FrequencyHz
		}
	}
	if !foundPowerDown {
		t.Fatal("default short descending power-down melody is missing")
	}
	if !foundProgrammingReady {
		t.Fatal("default rising programming-ready melody is missing")
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

func TestEmptyOutputDefinitionsRemainEmpty(t *testing.T) {
	value := Defaults()
	value.Melodies = nil
	value.StatusEffects = nil
	if len(EffectiveMelodies(value)) != 0 ||
		len(EffectiveStatusLEDEffects(value)) != 0 {
		t.Fatal("empty output definitions were replaced with implicit defaults")
	}
}
