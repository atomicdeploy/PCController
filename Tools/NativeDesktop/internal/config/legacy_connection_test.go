package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviousConnectionSettingsMigrateWithoutLosingPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.json")
	data := `{"lamp":{"base_url":"http://127.0.0.1:1","request_timeout":"7s"},"ui":{"theme":"light","view_mode":"compact","animations":false},"automation":{"on_script":"saved script.cmd"}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Device.BaseURL != "http://127.0.0.1:1" || cfg.Device.RequestTimeout.String() != "7s" || cfg.UI.Theme != "light" || cfg.UI.ViewMode != ViewCompact || cfg.UI.Animations || cfg.Automation.OnScript != "saved script.cmd" {
		t.Fatalf("saved settings changed: %+v", cfg)
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != data {
		t.Fatal("loading migrated settings rewrote the user's file")
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), `"lamp"`) || !strings.Contains(string(saved), `"device"`) {
		t.Fatal("new save emitted historical connection naming")
	}
}

func TestConnectionMigrationRejectsAmbiguityAndPreservesStrictValidation(t *testing.T) {
	for _, data := range []string{`{"lamp":{},"device":{}}`, `{"lamp":{},"misspelled":true}`} {
		path := filepath.Join(t.TempDir(), "desktop.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("ambiguous or unknown settings accepted: %s", data)
		}
	}
}
