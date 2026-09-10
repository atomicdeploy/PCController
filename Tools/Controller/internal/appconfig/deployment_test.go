package appconfig

import (
	"path/filepath"
	"testing"
)

func TestProgrammingDeploymentPersistsAndRejectsInvalidValues(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *Config) error { config.Programming.Deployment = "development"; return nil }); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(store.Path())
	if err != nil || loaded.Programming.Deployment != "development" {
		t.Fatalf("loaded=%+v err=%v", loaded.Programming, err)
	}
	bad := Defaults()
	bad.Programming.Deployment = "ignore-errors"
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid deployment accepted")
	}
}
