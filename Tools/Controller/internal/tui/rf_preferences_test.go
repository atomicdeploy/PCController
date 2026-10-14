package tui

import (
	"errors"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestRFLearningPreferencesSaveFailureKeepsDraftOpen(t *testing.T) {
	model := readyModel(t, PageRF)
	model.rfPreferencesOpen = true
	model.rfValue = appconfig.DefaultRFConfig()
	model.rfValue.Learning.TimeoutSeconds = 45
	model.saveRF = func(appconfig.RFConfig) error { return errors.New("save failed") }
	updated, _, _ := model.handleRFLearningPreferencesKey("s")
	if !updated.rfPreferencesOpen || updated.rfValue.Learning.TimeoutSeconds != 45 {
		t.Fatal("failed save closed or discarded the settings draft")
	}
	updated.saveRF = func(appconfig.RFConfig) error { return nil }
	updated, _, _ = updated.handleRFLearningPreferencesKey("s")
	if updated.rfPreferencesOpen {
		t.Fatal("successful save did not close the settings editor")
	}
}
