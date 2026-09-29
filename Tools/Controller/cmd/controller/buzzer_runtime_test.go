package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func clearBuzzerEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PCCONTROLLER_BUZZER_PATH", "PCCONTROLLER_BUZZER_MIRROR",
		"PCCONTROLLER_BUZZER_BACKEND", "PCCONTROLLER_BUZZER_EXECUTABLE",
	} {
		value, found := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		t.Cleanup(func() {
			if found {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

func TestBuzzerRuntimePrecedenceFlagEnvironmentConfig(t *testing.T) {
	clearBuzzerEnvironment(t)
	t.Setenv("PCCONTROLLER_BUZZER_PATH", "both")
	t.Setenv("PCCONTROLLER_BUZZER_BACKEND", "external")
	t.Setenv("PCCONTROLLER_BUZZER_EXECUTABLE", "env-beep")
	configured := appconfig.Defaults().Integrations.BuzzerMirror
	configured.Backend = "native"
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options, err := addBuzzerRuntimeFlags(flags, configured)
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{"--buzzer-path", "none", "--buzzer-mirror=true", "--buzzer-backend", "auto", "--buzzer-executable", "flag-beep"}); err != nil {
		t.Fatal(err)
	}
	if err := options.captureOverrides(flags); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := options.apply(store); err != nil {
		t.Fatal(err)
	}
	state := store.BuzzerRuntimeState()
	if state.RequestedPath != appconfig.BuzzerPathHost || !state.Effective.Enabled || state.Effective.Backend != "auto" || state.Effective.Executable != "flag-beep" {
		t.Fatalf("state=%+v", state)
	}
	if store.Persistent().Integrations.BuzzerMirror.Enabled {
		t.Fatal("startup override persisted into watched configuration")
	}
}

func TestBuzzerRuntimeEnvironmentRejectsInvalidValues(t *testing.T) {
	_, err := buzzerEnvironmentOverrides(func(name string) (string, bool) {
		if name == "PCCONTROLLER_BUZZER_PATH" {
			return "speaker", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("invalid buzzer path was accepted")
	}
}
