package ipcjson

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFullUIPreferencesFencesConcurrentChangesAndPreservesOtherConfig(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	initial := fullUIPreferencesFor(config.UI)
	connection := config.Connection
	changed := initial.Settings
	changed.EventLogLimit = 751
	changed.TUIConsole.FontFace = "Consolas"
	changed.HistoryHours = 12
	params, _ := json.Marshal(map[string]any{"settings": changed, "if_match": initial.ETag})
	response := service.Dispatch(context.Background(), Request{Method: "controller.ui.preferences.set", Params: params})
	if response.Error != nil || *updates != 1 || config.UI.EventLogLimit != 751 || config.UI.HistoryHours != 12 {
		t.Fatalf("save failed: %#v updates=%d", response, *updates)
	}
	if config.Connection != connection {
		t.Fatal("unrelated connection settings changed")
	}
	stale := service.Dispatch(context.Background(), Request{Method: "controller.ui.preferences.set", Params: params})
	if stale.Error == nil || *updates != 1 {
		t.Fatal("stale full preferences overwrote current values")
	}
	current := fullUIPreferencesFor(config.UI)
	noop, _ := json.Marshal(map[string]any{"settings": current.Settings, "if_match": current.ETag})
	if response := service.Dispatch(context.Background(), Request{Method: "controller.ui.preferences.set", Params: noop}); response.Error != nil || *updates != 1 {
		t.Fatal("unchanged save wrote configuration")
	}
	bad := current.Settings
	bad.MeasurementFreshnessMS = 1
	invalid, _ := json.Marshal(map[string]any{"settings": bad, "if_match": current.ETag})
	if response := service.Dispatch(context.Background(), Request{Method: "controller.ui.preferences.set", Params: invalid}); response.Error == nil || *updates != 1 {
		t.Fatal("invalid full UI preferences were accepted")
	}
	if requestCapability("controller.ui.preferences.set", nil) != capabilityHostConfig || requestCapability("controller.ui.preferences.get", nil) != capabilityRead {
		t.Fatal("UI preferences lost capability boundary")
	}
}
