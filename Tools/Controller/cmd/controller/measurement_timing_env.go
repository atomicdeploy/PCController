package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

const (
	measurementRefreshEnvironment   = "PCCONTROLLER_UI_STATUS_INTERVAL_MS"
	measurementFreshnessEnvironment = "PCCONTROLLER_UI_MEASUREMENT_FRESHNESS_MS"
)

func applyMeasurementTimingEnvironment(store *appconfig.Store) error {
	refreshMS, err := optionalEnvironmentInt(measurementRefreshEnvironment)
	if err != nil {
		return err
	}
	freshnessMS, err := optionalEnvironmentInt(measurementFreshnessEnvironment)
	if err != nil {
		return err
	}
	if refreshMS == 0 && freshnessMS == 0 {
		return nil
	}
	return store.SetMeasurementTimingOverrides(refreshMS, freshnessMS)
}

func optionalEnvironmentInt(name string) (int, error) {
	raw, present := os.LookupEnv(name)
	if !present || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of milliseconds", name)
	}
	return value, nil
}
