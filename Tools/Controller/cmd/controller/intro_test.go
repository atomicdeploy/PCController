package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestConfigOpenDoesNotParseExistingBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	broken := []byte(`{"ui":`)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := openConfiguration
	t.Cleanup(func() { openConfiguration = previous })
	called := false
	openConfiguration = func(file, target string) error { called = file == path && target == "file"; return nil }
	var output bytes.Buffer
	if err := runConfigMaintenance([]string{"config", "open", "file"}, path, &output); err != nil || !called {
		t.Fatalf("repair editor blocked: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, broken) {
		t.Fatal("opening rewrote the broken configuration")
	}
}

func TestConfigIntroResetPreservesOtherSettingsAndOpenTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(value *appconfig.Config) error {
		value.UI.SetupComplete = true
		value.UI.AppTitle = "Preserved"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for _, mode := range []string{"skip", "always", "auto", "reset"} {
		if err := runConfigMaintenance([]string{"config", "intro", mode}, path, &output); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := appconfig.Load(path)
	if err != nil || got.UI.SetupComplete || got.UI.AppTitle != "Preserved" || got.UI.IntroMode != "auto" {
		t.Fatalf("reset affected other settings: %+v %v", got.UI, err)
	}
	if err := runConfigMaintenance([]string{"config", "intro", "bad"}, path, &output); err == nil {
		t.Fatal("invalid mode accepted")
	}
	previous := openConfiguration
	t.Cleanup(func() { openConfiguration = previous })
	called := false
	openConfiguration = func(file, target string) error { called = file == path && target == "file"; return nil }
	if err := runConfigMaintenance([]string{"config", "open", "file"}, path, &output); err != nil || !called {
		t.Fatalf("open target: %v", err)
	}
}

func TestGlobalIntroFlag(t *testing.T) {
	for _, args := range [][]string{{"--intro", "skip", "tui"}, {"tui", "--intro=always"}} {
		remaining, _, settings, err := extractGlobalArguments(args)
		if err != nil || len(remaining) != 1 || remaining[0] != "tui" || settings.IntroMode == "" {
			t.Fatalf("args=%v parsed=%v %v", args, remaining, err)
		}
	}
	if _, _, _, err := extractGlobalArguments([]string{"--intro=bad"}); err == nil {
		t.Fatal("invalid intro flag accepted")
	}
}
