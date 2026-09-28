package main

import (
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/programmer"
)

func TestCompileFeatureDefaultsAreIgnoredWhenNoCompileOccurs(t *testing.T) {
	t.Setenv(firmwareFeaturesEnvironment, "unknown")
	config := appconfig.Defaults()
	config.Programming.FirmwareFeatures = []programmer.FirmwareFeature{"also-unknown"}
	features, err := resolveCompileFirmwareFeatures(
		config, newFirmwareFeatureSelection(nil), false, false,
	)
	if err != nil || len(features) != 0 {
		t.Fatalf("irrelevant compile defaults features=%v err=%v", features, err)
	}
	if _, err := resolveCompileFirmwareFeatures(
		config, newFirmwareFeatureSelection(nil), false, true,
	); err == nil || !strings.Contains(err.Error(), firmwareFeaturesEnvironment) {
		t.Fatalf("compile did not validate environment: %v", err)
	}
}
