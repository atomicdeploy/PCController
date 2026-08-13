package main

import (
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestMeasurementTimingEnvironmentOverridesEffectiveConfig(t *testing.T) {
	t.Setenv(measurementRefreshEnvironment, "300")
	t.Setenv(measurementFreshnessEnvironment, "1600")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMeasurementTimingEnvironment(store); err != nil {
		t.Fatal(err)
	}
	ui := store.Current().UI
	if ui.StatusIntervalMS != 300 || ui.MeasurementFreshnessMS != 1600 {
		t.Fatalf("environment timing=%d/%d", ui.StatusIntervalMS, ui.MeasurementFreshnessMS)
	}
}

func TestMeasurementTimingEnvironmentRejectsMalformedOrUnsafeValues(t *testing.T) {
	t.Setenv(measurementRefreshEnvironment, "fast")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = applyMeasurementTimingEnvironment(store)
	if err == nil || !strings.Contains(err.Error(), measurementRefreshEnvironment) {
		t.Fatalf("malformed environment error=%v", err)
	}

	t.Setenv(measurementRefreshEnvironment, "100")
	t.Setenv(measurementFreshnessEnvironment, "1500")
	err = applyMeasurementTimingEnvironment(store)
	if err == nil || !strings.Contains(err.Error(), "status_interval_ms") {
		t.Fatalf("unsafe environment error=%v", err)
	}
}
