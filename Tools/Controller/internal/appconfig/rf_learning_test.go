package appconfig

import "testing"

func TestRFLearningPreferencesDefaultAndValidation(t *testing.T) {
	config := DefaultRFConfig()
	if config.Learning.Timed || config.Learning.Continuous || config.Learning.TimeoutSeconds != 60 {
		t.Fatalf("incorrect defaults: %+v", config.Learning)
	}
	if err := validateRFConfig(config); err != nil {
		t.Fatal(err)
	}
	config.Learning.TimeoutSeconds = 121
	if validateRFConfig(config) == nil {
		t.Fatal("unbounded duration accepted")
	}
	config.Learning.TimeoutSeconds = 0
	if canonicalizeRFConfig(config).Learning.TimeoutSeconds != 60 {
		t.Fatal("missing preference default not applied")
	}
}
