package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/programmer"
)

func TestBoardBlankConfirmationRequiresExactAuthenticatedName(t *testing.T) {
	if err := validateBoardBlankConfirmation("TEST-01", "COM4", "TEST-01"); err != nil {
		t.Fatal(err)
	}
	for _, supplied := range []string{"test-01", "ERASE-BOARD", "TEST-02", ""} {
		if err := validateBoardBlankConfirmation(supplied, "COM4", "TEST-01"); err == nil {
			t.Fatalf("confirmation %q was accepted", supplied)
		}
	}
}

func TestBoardBlankConfirmationWithoutUARTRequiresLiteral(t *testing.T) {
	if err := validateBoardBlankConfirmation("ERASE-BOARD", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := validateBoardBlankConfirmation("TEST-01", "", ""); err == nil || !strings.Contains(err.Error(), "UART identity is unavailable") {
		t.Fatalf("missing-UART confirmation error=%v", err)
	}
}

func TestBoardInitializationCompilePlanCarriesTypedFeatures(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(project, "PCController.ino"),
		[]byte("void setup() {}\nvoid loop() {}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	options, identity, err := planBoardInitializationCompile(
		project, "arduino-cli", "", programmer.DefaultFQBN(),
		[]programmer.FirmwareFeature{
			programmer.FirmwareFeatureEEPROMMenuLabels,
			programmer.FirmwareFeatureEEPROMBootOpcodes,
			programmer.FirmwareFeatureEEPROMMenuLabels,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"eeprom-boot-opcodes", "eeprom-menu-labels"}
	if !reflect.DeepEqual(
		programmer.FirmwareFeatureNames(options.FirmwareFeatures), want,
	) || !reflect.DeepEqual(
		programmer.FirmwareFeatureNames(identity.Features), want,
	) {
		t.Fatalf("options=%v identity=%v", options.FirmwareFeatures, identity.Features)
	}
}

func TestBoardInitializationRejectsFeaturesWhenItWillNotCompile(t *testing.T) {
	t.Setenv(firmwareFeaturesEnvironment, "")
	project := t.TempDir()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Paths.Project = project
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--bootloader-only", "--firmware-feature", "eeprom-menu-labels"},
		{"--firmware", "candidate.hex", "--firmware-feature=eeprom-menu-labels"},
	} {
		var output bytes.Buffer
		err := initializeBoard(
			context.Background(), control.New(control.Options{}),
			args, store, project, &output,
		)
		if err == nil || !strings.Contains(err.Error(), "require board initialization to compile") {
			t.Fatalf("args=%v output=%q err=%v", args, output.String(), err)
		}
	}
}
