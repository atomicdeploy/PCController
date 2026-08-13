package tui

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
)

func TestMeasurementCadenceStaysWithinTwoToFiveHertzWithoutDoorOverride(t *testing.T) {
	for _, intervalMS := range []int{200, 250, 300, 400, 500} {
		ui := appconfig.Defaults().UI
		ui.StatusIntervalMS = intervalMS
		model := Model{
			page: PageDashboard, uiValue: ui, prefs: preferencesFromUI(ui),
		}
		if got, want := model.statusInterval(), time.Duration(intervalMS)*time.Millisecond; got != want {
			t.Fatalf("statusInterval(%d)=%s want=%s", intervalMS, got, want)
		}
	}
}

func TestPushedMeasurementTimingInvalidatesStaleTickImmediately(t *testing.T) {
	ui := appconfig.Defaults().UI
	model := Model{page: PageDashboard, uiValue: ui, prefs: preferencesFromUI(ui)}
	updatedUI := ui
	updatedUI.StatusIntervalMS = 300
	updatedUI.MeasurementFreshnessMS = 1600

	updated, command := model.Update(uiConfigUpdateMsg{value: updatedUI, ok: true})
	next := updated.(Model)
	if command == nil || next.statusTickGeneration != 1 ||
		next.prefs.PollInterval != 300*time.Millisecond ||
		next.prefs.FreshnessWindow != 1600*time.Millisecond {
		t.Fatalf("pushed timing generation=%d refresh=%s freshness=%s command=%v",
			next.statusTickGeneration, next.prefs.PollInterval, next.prefs.FreshnessWindow, command)
	}

	stale, staleCommand := next.Update(tickMsg{at: time.Now(), generation: 0})
	staleModel := stale.(Model)
	if staleCommand != nil || staleModel.statusPending || staleModel.statusTickGeneration != 1 {
		t.Fatalf("stale tick was not ignored: pending=%v generation=%d command=%v",
			staleModel.statusPending, staleModel.statusTickGeneration, staleCommand)
	}
}
