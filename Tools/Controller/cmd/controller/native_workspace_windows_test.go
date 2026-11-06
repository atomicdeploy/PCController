//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

func TestNativeHostPreferencesRoundTripRetainsUnrelatedMaps(t *testing.T) {
	p := nativePages{preview: true}
	page, err := p.loadHostPreferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range page.Fields {
		values[field.Key] = field.Value
	}
	p.preview = false
	p.rpc = func(_ context.Context, method string, params, result any) error {
		if method != "controller.ui.preferences.set" {
			t.Fatalf("unexpected RPC %q", method)
		}
		encoded, _ := json.Marshal(params)
		var got struct {
			Settings appconfig.UI `json:"settings"`
		}
		_ = json.Unmarshal(encoded, &got)
		if !reflect.DeepEqual(got.Settings, appconfig.Defaults().UI) {
			t.Fatal("native editor lost unchanged UI preferences")
		}
		return nil
	}
	if err := p.saveHostPreferences(context.Background(), values); err != nil {
		t.Fatal(err)
	}
	values["status_interval_ms"] = "invalid"
	if err := p.saveHostPreferences(context.Background(), values); err == nil {
		t.Fatal("invalid numeric draft silently used previous value")
	}
}

func TestNativeWorkspaceBoardAndMenuPreviewNeverMutates(t *testing.T) {
	p := nativePages{preview: true}
	for _, key := range []string{"board", "menus", "terminal", "diagnostics", "host", "updates", "safety", "i2c", "network", "history", "host-tools", "local-device", "firmware-tools", "status-lighting", "integration-settings", "discovery-settings", "udp-settings", "os-policy", "pwm-settings", "data", "board-automations", "rf-bindings", "releases", "profiles", "status-policy", "peers", "hotkeys", "host-automations", "rf-transmit"} {
		page, err := p.loadWorkspacePage(context.Background(), key, control.Snapshot{})
		if err != nil || page.Title == "" {
			t.Fatalf("%s: %v", key, err)
		}
		for _, action := range page.Actions {
			if action.Enabled {
				t.Fatalf("%s enabled preview mutation", key)
			}
		}
		if _, err := p.submitWorkspacePage(context.Background(), key, "save", nil); err == nil {
			t.Fatalf("%s accepted preview mutation", key)
		}
	}
}

func TestNativeBoardChoiceLabelsMatchWireValues(t *testing.T) {
	page, err := (nativePages{preview: true}).loadWorkspacePage(context.Background(), "board", control.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range page.Fields {
		switch field.Key {
		case "light_mode":
			if !reflect.DeepEqual(field.Choices, []string{"0", "1", "2"}) || !reflect.DeepEqual(field.ChoiceLabels, []string{"Off", "On", "Automatic"}) {
				t.Fatal("lighting policy labels do not match firmware")
			}
		case "status_color":
			if !reflect.DeepEqual(field.ChoiceLabels, []string{"Red", "Blue", "Violet", "Green", "White"}) {
				t.Fatal("status color labels do not match firmware")
			}
		}
	}
}
