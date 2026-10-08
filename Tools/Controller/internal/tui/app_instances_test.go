package tui

import (
	"testing"

	"pccontroller.local/controller/internal/hostui"
)

func TestAppSettingsListsConnectedApplicationControls(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	model.instanceID = "tui:local"
	model.appInstances = func() []hostui.AppInstance {
		return []hostui.AppInstance{
			{ID: "tui:local", Surface: "tui", State: "active"},
			{
				ID: "pealayer:desktop", Surface: "pealayer", State: "active",
				Values: map[string]string{hostui.ActionCapabilitiesKey: "pealayer.play,pealayer.pause"},
				Self: &hostui.InstanceSelf{Kind: "native", ProcessID: 42},
			},
		}
	}

	for _, row := range model.appSettingRows() {
		if row.Key != "instance.connected:pealayer:desktop" {
			continue
		}
		if row.Group != "CONNECTED APPLICATIONS" || row.Label != "PEALAYER" || row.Value != "ACTIVE · native · PID 42 · 2 controls" || row.Editable {
			t.Fatalf("unexpected connected application row: %#v", row)
		}
		return
	}
	t.Fatal("connected Pealayer instance was not exposed in App settings")
}
