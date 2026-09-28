package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/programmer"
)

func TestBoardBlankAbortsBeforeProgrammerWhenUARTCloseFails(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	previous := closeBoardRuntime
	closeCalls := 0
	closeBoardRuntime = func(*control.Runtime) error {
		closeCalls++
		if closeCalls == 1 {
			return closeErr
		}
		return nil
	}
	defer func() { closeBoardRuntime = previous }()
	t.Setenv(programmer.HostDataDirectoryEnvironment, t.TempDir())
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = blankBoard(context.Background(), control.New(control.Options{}), []string{
		"--uart", "none", "--confirm", "ERASE-BOARD",
	}, store, &output)
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "before blanking") {
		t.Fatalf("blank close error = %v, want %v", err, closeErr)
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("drain blank Runtime: %v", err)
	}
}

func TestBoardInitializeAbortsBeforeProgrammerWhenUARTCloseFails(t *testing.T) {
	closeErr := errors.New("cancel serial I/O")
	previous := closeBoardRuntime
	closeCalls := 0
	closeBoardRuntime = func(*control.Runtime) error {
		closeCalls++
		if closeCalls == 1 {
			return closeErr
		}
		return nil
	}
	defer func() { closeBoardRuntime = previous }()
	t.Setenv(programmer.HostDataDirectoryEnvironment, t.TempDir())
	project := t.TempDir()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = initializeBoard(context.Background(), control.New(control.Options{}), []string{
		"--bootloader-only", "--skip-toolchain", "--cli", "not-run",
	}, store, project, &output)
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "before initialization") {
		t.Fatalf("initialize close error = %v, want %v", err, closeErr)
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("drain initialize Runtime: %v", err)
	}
}

func TestRunBoardLocallyJoinsExplicitAndDeferredCloseFailures(t *testing.T) {
	firstCloseErr := errors.New("first CancelIoEx failure")
	retryCloseErr := errors.New("retry CancelIoEx failure")
	previous := closeBoardRuntime
	closeCalls := 0
	closeBoardRuntime = func(*control.Runtime) error {
		closeCalls++
		if closeCalls == 1 {
			return firstCloseErr
		}
		if closeCalls == 2 {
			return retryCloseErr
		}
		return nil
	}
	defer func() { closeBoardRuntime = previous }()
	t.Setenv(programmer.HostDataDirectoryEnvironment, t.TempDir())
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	defer runtime.Close()
	var output bytes.Buffer

	err = runBoardLocally(
		context.Background(), "blank", runtime,
		[]string{"--uart", "none", "--confirm", "ERASE-BOARD"},
		store, &output,
	)
	if !errors.Is(err, firstCloseErr) || !errors.Is(err, retryCloseErr) {
		t.Fatalf("board close error = %v, want explicit and deferred failures", err)
	}
	if closeCalls != 2 {
		t.Fatalf("board close calls = %d, want explicit attempt plus deferred retry", closeCalls)
	}
	if retainedCommandRuntimeCount() != 1 {
		t.Fatal("repeated board close failure did not retain the Runtime owner")
	}
	if err := drainCommandRuntimeCleanups(); err != nil {
		t.Fatalf("drain retained board Runtime: %v", err)
	}
	if closeCalls != 3 || retainedCommandRuntimeCount() != 0 {
		t.Fatalf("drained board owner: close calls=%d retained=%d", closeCalls, retainedCommandRuntimeCount())
	}
}

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

func TestBoardInitializationIgnoresInvalidFeatureDefaultsWithoutCompile(t *testing.T) {
	t.Setenv(firmwareFeaturesEnvironment, "unknown")
	t.Setenv(programmer.HostDataDirectoryEnvironment, t.TempDir())
	project := t.TempDir()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Paths.Project = project
		config.Programming.ToolchainCLI = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--bootloader-only", "--skip-toolchain"},
		{"--firmware", "candidate.hex", "--skip-toolchain"},
	} {
		var output bytes.Buffer
		err := initializeBoard(
			context.Background(), control.New(control.Options{}),
			args, store, project, &output,
		)
		if err == nil || !strings.Contains(err.Error(), "--skip-toolchain requires") {
			t.Fatalf("args=%v output=%q err=%v", args, output.String(), err)
		}
		if strings.Contains(err.Error(), "firmware feature") {
			t.Fatalf("irrelevant invalid feature blocked args=%v: %v", args, err)
		}
	}
}
