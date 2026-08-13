package appconfig

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeasurementTimingDefaultsAndValidation(t *testing.T) {
	value := Defaults()
	if value.UI.StatusIntervalMS != DefaultMeasurementRefreshMS ||
		value.UI.MeasurementFreshnessMS != DefaultMeasurementFreshnessMS {
		t.Fatalf("timing defaults=%d/%d", value.UI.StatusIntervalMS, value.UI.MeasurementFreshnessMS)
	}
	for _, test := range []struct {
		refresh, freshness int
		want               string
	}{
		{MeasurementRefreshMinMS - 1, 1500, "status_interval_ms"},
		{MeasurementRefreshMaxMS + 1, 1500, "status_interval_ms"},
		{500, 599, "measurement_freshness_ms"},
		{250, MeasurementFreshnessMaxMS + 1, "measurement_freshness_ms"},
	} {
		candidate := value
		candidate.UI.StatusIntervalMS = test.refresh
		candidate.UI.MeasurementFreshnessMS = test.freshness
		if err := candidate.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Validate(%d,%d)=%v, want %q", test.refresh, test.freshness, err, test.want)
		}
	}
}

func TestDefaultHostMenuUsesCanonicalMeasurementTimingActions(t *testing.T) {
	var settings *HostMenu
	for index := range Defaults().HostMenus.Menus {
		if Defaults().HostMenus.Menus[index].ID == "pc-settings" {
			value := Defaults().HostMenus.Menus[index]
			settings = &value
			break
		}
	}
	if settings == nil {
		t.Fatal("default pc-settings host menu is missing")
	}
	want := map[string]struct {
		value string
		min   float64
		max   float64
		read  string
	}{
		"poll":  {"250", 200, 500, "pc.ui.status_interval_ms"},
		"fresh": {"1500", 300, 10_000, "pc.ui.measurement_freshness_ms"},
	}
	for _, item := range settings.Items {
		expected, ok := want[item.ID]
		if !ok {
			continue
		}
		if item.Value != expected.value || item.Min != expected.min || item.Max != expected.max ||
			item.ReadAction != expected.read || item.WriteAction != expected.read {
			t.Errorf("default %s timing item=%+v", item.ID, item)
		}
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatalf("default host menu omits timing items: %v", want)
	}
}

func TestMeasurementTimingOverridesAreEffectiveWatchedAndNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := store.Subscribe(ctx)
	secondUpdates := store.Subscribe(ctx)
	<-updates
	<-secondUpdates
	if err := store.SetMeasurementTimingOverrides(300, 1600); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		if update.UI.StatusIntervalMS != 300 || update.UI.MeasurementFreshnessMS != 1600 {
			t.Fatalf("effective subscriber timing=%d/%d", update.UI.StatusIntervalMS, update.UI.MeasurementFreshnessMS)
		}
	case <-time.After(time.Second):
		t.Fatal("timing override was not pushed to subscribers")
	}
	select {
	case update := <-secondUpdates:
		if update.UI.StatusIntervalMS != 300 || update.UI.MeasurementFreshnessMS != 1600 {
			t.Fatalf("second subscriber timing=%d/%d", update.UI.StatusIntervalMS, update.UI.MeasurementFreshnessMS)
		}
	case <-time.After(time.Second):
		t.Fatal("timing override was not pushed to the second subscriber")
	}
	ui := store.Current().UI
	ui.AppTitle = "Timing override test"
	if _, err := store.UpdateUI(ui); err != nil {
		t.Fatal(err)
	}
	persisted, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.UI.StatusIntervalMS != DefaultMeasurementRefreshMS ||
		persisted.UI.MeasurementFreshnessMS != DefaultMeasurementFreshnessMS {
		t.Fatalf("process override leaked to disk: %d/%d", persisted.UI.StatusIntervalMS, persisted.UI.MeasurementFreshnessMS)
	}
	if effective := store.Current().UI; effective.StatusIntervalMS != 300 || effective.MeasurementFreshnessMS != 1600 {
		t.Fatalf("override lost after UI save: %d/%d", effective.StatusIntervalMS, effective.MeasurementFreshnessMS)
	}
}
