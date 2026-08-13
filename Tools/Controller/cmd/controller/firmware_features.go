package main

import (
	"fmt"
	"os"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/programmer"
)

const firmwareFeaturesEnvironment = "PCCONTROLLER_FIRMWARE_FEATURES"

// configuredFirmwareFeatures applies the ordinary config < environment
// precedence. An explicitly empty environment value selects the default-off
// profile for this process.
func configuredFirmwareFeatures(
	config appconfig.Config,
) ([]programmer.FirmwareFeature, error) {
	values := programmer.FirmwareFeatureNames(
		config.Programming.FirmwareFeatures,
	)
	if raw, present := os.LookupEnv(firmwareFeaturesEnvironment); present {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			values = nil
		} else {
			values = strings.Split(raw, ",")
		}
	}
	features, err := programmer.NormalizeFirmwareFeatures(values)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", firmwareFeaturesEnvironment, err)
	}
	return features, nil
}

// firmwareFeatureSelection makes the first explicit flag replace configured
// defaults; subsequent flags accumulate and are canonicalized together.
type firmwareFeatureSelection struct {
	values   []string
	explicit bool
}

func newFirmwareFeatureSelection(
	defaults []programmer.FirmwareFeature,
) *firmwareFeatureSelection {
	return &firmwareFeatureSelection{
		values: programmer.FirmwareFeatureNames(defaults),
	}
}

func (selection *firmwareFeatureSelection) String() string {
	return strings.Join(selection.values, ",")
}

func (selection *firmwareFeatureSelection) Set(value string) error {
	if !selection.explicit {
		selection.values = nil
		selection.explicit = true
	}
	selection.values = append(selection.values, value)
	return nil
}

func (selection *firmwareFeatureSelection) Resolve() (
	[]programmer.FirmwareFeature,
	error,
) {
	return programmer.NormalizeFirmwareFeatures(selection.values)
}
