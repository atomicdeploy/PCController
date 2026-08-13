package main

import (
	"context"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestHostMenuReadsAndWritesCanonicalMeasurementTiming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := appconfig.Write(path, appconfig.Defaults()); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := newHostMenuManager(store, control.New(control.Options{}), shell.New(1))
	if err := manager.Open("pc-settings"); err != nil {
		t.Fatal(err)
	}

	// Application title -> refresh interval -> freshness window.
	for range 2 {
		if _, err := manager.HandleKey(context.Background(), 2, "press"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := manager.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ItemID != "fresh" || snapshot.Value != "1500" {
		t.Fatalf("freshness read snapshot=%+v", snapshot)
	}

	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Value != "1600" || store.Current().UI.MeasurementFreshnessMS != 1600 {
		t.Fatalf("freshness write snapshot=%+v config=%+v", snapshot, store.Current().UI)
	}

	// Move back to refresh and verify it uses the same persisted setting.
	if _, err := manager.HandleKey(context.Background(), 1, "press"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = manager.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ItemID != "poll" || snapshot.Value != "250" {
		t.Fatalf("refresh read snapshot=%+v", snapshot)
	}
	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Value != "300" || store.Current().UI.StatusIntervalMS != 300 {
		t.Fatalf("refresh write snapshot=%+v config=%+v", snapshot, store.Current().UI)
	}
}
