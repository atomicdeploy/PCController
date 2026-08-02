package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/defaultassets"
)

func TestBackupManifestPathUsesValidatedAbsoluteResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations", "manifest.json")
	output := "program output\nBackup complete; manifest: " + path + "\nprogrammer operation completed"
	actual, err := backupManifestPath(output)
	if err != nil {
		t.Fatal(err)
	}
	if actual != filepath.Clean(path) {
		t.Fatalf("path=%q want=%q", actual, path)
	}
	if _, err := backupManifestPath("Backup complete; manifest: relative.json"); err == nil {
		t.Fatal("relative manifest path accepted")
	}
}

func TestPrimaryArtifactServiceRegistersHostAndOptionalDefaultsWithoutHardware(t *testing.T) {
	t.Setenv("PCCONTROLLER_DATA_DIR", filepath.Join(t.TempDir(), "host-data"))
	configPath := filepath.Join(t.TempDir(), "config.json")
	store, err := appconfig.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	client := controllerapi.New(controllerapi.Options{})
	defer client.Shutdown()
	service, err := newArtifactHostService(client, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manifest, err := service.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Current.Host == nil || manifest.Current.Host.SHA256 == "" || manifest.Current.Host.Platform == "" {
		t.Fatalf("current host=%#v", manifest.Current.Host)
	}
	bundle, err := defaultassets.Load()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DefaultsEnabled != bundle.Enabled {
		t.Fatalf("defaults enabled=%t bundle=%t", manifest.DefaultsEnabled, bundle.Enabled)
	}
	if bundle.Enabled && (manifest.Defaults.Firmware == nil || manifest.Defaults.EEPROM == nil) {
		t.Fatalf("defaults=%#v", manifest.Defaults)
	}
}

func TestPrimaryFirmwareUpdatePropagatesDevelopmentEEPROMReinitialization(t *testing.T) {
	var command string
	executor := &primaryArtifactExecutor{execute: func(_ context.Context, value string) (string, error) {
		command = value
		return "guarded firmware flash completed", nil
	}}
	var states []string
	err := executor.ProgramFirmware(context.Background(), artifacts.Descriptor{
		Kind: artifacts.KindFirmware, LocalPath: filepath.Join(t.TempDir(), "candidate firmware.hex"),
	}, artifacts.UpdateRequest{
		Method: "urclock", Port: "COM18", ReinitializeEEPROM: true,
	}, func(state string, _ int, _ string) {
		states = append(states, state)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "program flash") ||
		!strings.Contains(command, "--reinitialize-eeprom") ||
		!strings.Contains(command, "COM18") {
		t.Fatalf("command=%q", command)
	}
	if strings.Join(states, ",") != "programming,verifying" {
		t.Fatalf("states=%v", states)
	}
}

func TestPrimaryFirmwareUpdateRejectsReinitializationWithoutMandatoryBackup(t *testing.T) {
	called := false
	executor := &primaryArtifactExecutor{execute: func(_ context.Context, _ string) (string, error) {
		called = true
		return "", nil
	}}
	err := executor.ProgramFirmware(context.Background(), artifacts.Descriptor{
		Kind: artifacts.KindFirmware, LocalPath: "candidate.hex",
	}, artifacts.UpdateRequest{
		Method: "urclock", Port: "COM18", ReinitializeEEPROM: true,
		AllowIncompleteBackup: true,
	}, func(string, int, string) {})
	if err == nil || !strings.Contains(err.Error(), "requires a complete verified raw backup") || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

func TestPrimaryCapturedFlashRestoreUsesGuardedTransactionAndExplicitISPFallback(t *testing.T) {
	image := filepath.Join(t.TempDir(), "captured flash.hex")
	tests := []struct {
		name       string
		method     string
		wantMethod string
		wantPort   bool
	}{
		{name: "UART is the restore default", wantMethod: "urclock", wantPort: true},
		{name: "ISP must be named explicitly", method: "usbasp", wantMethod: "usbasp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var command string
			executor := &primaryArtifactExecutor{execute: func(_ context.Context, value string) (string, error) {
				command = value
				return "guarded firmware flash completed", nil
			}}
			var states []string
			err := executor.RestoreFlash(context.Background(), artifacts.Descriptor{
				Kind: artifacts.KindFlashBackup, LocalPath: image,
			}, artifacts.UpdateRequest{Method: test.method, Port: "COM18"}, func(state string, _ int, _ string) {
				states = append(states, state)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(command, "program flash") || !strings.Contains(command, "--method "+test.wantMethod) {
				t.Fatalf("command=%q", command)
			}
			if strings.Contains(command, "controller.update.firmware") {
				t.Fatalf("restore was routed through firmware update: %q", command)
			}
			if gotPort := strings.Contains(command, "COM18"); gotPort != test.wantPort {
				t.Fatalf("command=%q contains port=%t want %t", command, gotPort, test.wantPort)
			}
			if strings.Join(states, ",") != "backing-up,verifying" {
				t.Fatalf("states=%v", states)
			}
		})
	}
}

func TestPrimaryCapturedFlashRestoreRejectsOtherArtifactKinds(t *testing.T) {
	called := false
	executor := &primaryArtifactExecutor{execute: func(context.Context, string) (string, error) {
		called = true
		return "", nil
	}}
	err := executor.RestoreFlash(context.Background(), artifacts.Descriptor{
		Kind: artifacts.KindFirmware, LocalPath: "firmware.hex",
	}, artifacts.UpdateRequest{Port: "COM18"}, func(string, int, string) {})
	if err == nil || !strings.Contains(err.Error(), "captured-flash restore requires") || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

func TestProgrammingFailureTranslatesBootloaderErrorsToTypedTelemetry(t *testing.T) {
	value := programmingExecutionFailure("urclock", errors.New("programmer is not responding"))
	var failure *artifacts.ExecutionFailure
	if !errors.As(value, &failure) {
		t.Fatalf("failure type=%T", value)
	}
	if failure.Method != artifacts.ProgrammingMethodUrclock ||
		failure.BootloaderOutcome != artifacts.BootloaderUnavailable ||
		failure.Code != "bootloader_unavailable" || !failure.ISPFallbackSuggested {
		t.Fatalf("failure=%#v", failure)
	}
	timeout := programmingExecutionFailure("urclock", context.DeadlineExceeded)
	if !errors.As(timeout, &failure) || failure.BootloaderOutcome != artifacts.BootloaderTimedOut ||
		failure.Code != "bootloader_timeout" || !failure.ISPFallbackSuggested {
		t.Fatalf("timeout=%#v", failure)
	}
}

func TestCapturedFlashRestoreMethodAllowsOnlyUARTOrExplicitISP(t *testing.T) {
	if method, err := explicitRestoreMethod(""); err != nil || method != "urclock" {
		t.Fatalf("default method=%q err=%v", method, err)
	}
	if method, err := explicitRestoreMethod("usbasp"); err != nil || method != "usbasp" {
		t.Fatalf("explicit ISP method=%q err=%v", method, err)
	}
	if _, err := explicitRestoreMethod("avrdude"); err == nil {
		t.Fatal("generic avrdude restore bypass was accepted")
	}
}
