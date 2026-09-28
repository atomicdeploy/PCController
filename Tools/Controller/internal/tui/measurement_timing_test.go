package tui

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestMeasurementTimingUsesAuthoritativeCurrentBounds(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	for _, intervalMS := range []int{appconfig.StatusIntervalMinMS, 200, 5000, appconfig.StatusIntervalMaxMS} {
		ui := appconfig.Defaults().UI
		ui.StatusIntervalMS = intervalMS
		ui.MeasurementFreshnessMS = intervalMS + appconfig.MeasurementFreshnessHeadroomMS
		model := NewWithOptions(runtime, shell.New(10), Options{UIConfig: func() appconfig.UI { return ui }})
		model.page = PageDashboard
		if got, want := model.statusInterval(), time.Duration(intervalMS)*time.Millisecond; got != want {
			t.Fatalf("statusInterval(%d)=%s want=%s", intervalMS, got, want)
		}
		if got, want := model.prefs.FreshnessWindow, time.Duration(ui.MeasurementFreshnessMS)*time.Millisecond; got != want {
			t.Fatalf("freshness(%d)=%s want=%s", intervalMS, got, want)
		}
	}
}

func TestSyncUIConfigHotAppliesMeasurementFreshness(t *testing.T) {
	ui := appconfig.Defaults().UI
	model := Model{uiValue: ui, prefs: preferencesFromUI(ui)}
	updated := ui
	updated.StatusIntervalMS = 5000
	updated.MeasurementFreshnessMS = 5200
	model.syncUIConfig(updated)
	if model.prefs.PollInterval != 5*time.Second || model.prefs.FreshnessWindow != 5200*time.Millisecond {
		t.Fatalf("hot-applied timing=%s/%s", model.prefs.PollInterval, model.prefs.FreshnessWindow)
	}
}
