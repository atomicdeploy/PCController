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

func TestLegacyFeedbackMelodiesRemainExactHostDefinitions(t *testing.T) {
	want := map[string][]MelodyNote{
		FinishMelodyName:        {{659, 100, 0}, {784, 100, 0}, {880, 250, 0}},
		LostMelodyName:          {{392, 100, 0}, {330, 100, 0}, {262, 100, 0}, {196, 100, 0}},
		IncorrectBeepMelodyName: {{2000, 100, 100}, {2000, 100, 100}, {2000, 100, 100}},
		ErrorBeepMelodyName:     {{2000, 10, 10}, {2000, 10, 10}, {2000, 10, 10}, {2000, 10, 10}, {2000, 10, 10}},
		FaultBeepMelodyName:     {{1000, 250, 0}, {500, 500, 5000}},
		SuccessCueMelodyName:    {{1047, 70, 30}, {1319, 110, 0}},
		ErrorCueMelodyName:      {{330, 90, 50}, {262, 160, 0}},
	}
	for _, melody := range DefaultMelodies() {
		if notes, ok := want[melody.Name]; ok {
			if len(notes) != len(melody.Notes) {
				t.Fatalf("%s note count=%d want %d", melody.Name, len(melody.Notes), len(notes))
			}
			for index := range notes {
				if melody.Notes[index] != notes[index] {
					t.Fatalf("%s note %d=%+v want %+v", melody.Name, index, melody.Notes[index], notes[index])
				}
			}
			delete(want, melody.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing host legacy melodies: %v", want)
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

func TestEmptyOutputDefinitionsRestoreLegacyHostMelodiesOnly(t *testing.T) {
	value := Defaults()
	value.Melodies = nil
	value.StatusEffects = nil
	if got := EffectiveMelodies(value); len(got) != 7 {
		t.Fatalf("legacy host melody count=%d want 7", len(got))
	}
	if len(EffectiveStatusLEDEffects(value)) != 0 {
		t.Fatal("empty status effects were replaced with implicit defaults")
	}

	override := DefaultFinishMelody()
	override.Notes[0].FrequencyHz = 777
	value.Melodies = []Melody{override}
	got := EffectiveMelodies(value)
	if len(got) != 7 || got[0].Notes[0].FrequencyHz != 777 {
		t.Fatalf("configured legacy override was not authoritative: %#v", got)
	}
}
