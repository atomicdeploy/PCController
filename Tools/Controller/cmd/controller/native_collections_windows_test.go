//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
)

func nativePageValues(page desktop.Page) map[string]string {
	values := map[string]string{}
	for _, field := range page.Fields {
		values[field.Key] = field.Value
	}
	return values
}

func TestNativeHotkeysUseCanonicalInventoryFence(t *testing.T) {
	bindings := []appconfig.Hotkey{{Name: "Panel", Enabled: true, Chord: "Ctrl+Alt+P", Command: "status"}}
	mutations := 0
	p := nativePages{rpc: func(_ context.Context, method string, params, result any) error {
		if method == "controller.hotkeys.get" {
			encoded, _ := json.Marshal(map[string]any{"bindings": bindings})
			return json.Unmarshal(encoded, result)
		}
		if method != "controller.hotkeys.set" {
			t.Fatalf("unexpected %s", method)
		}
		mutations++
		v := params.(map[string]any)
		var canonical any
		encoded, _ := json.Marshal(bindings)
		_ = json.Unmarshal(encoded, &canonical)
		if v["if_match"] != nativeDigest(canonical) {
			t.Fatal("native revision differs from server canonical JSON")
		}
		bindings[0].Command = v["command"].(string)
		return nil
	}}
	page, err := p.loadNativeHotkeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := nativePageValues(page)
	for key, value := range page.RowValues["Panel"] {
		v[key] = value
	}
	v["selected"] = "Panel"
	v["command"] = "hello"
	if _, err := p.submitNativeHotkeys(context.Background(), "save", v); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 {
		t.Fatal("missing mutation")
	}
	if _, err := p.submitNativeHotkeys(context.Background(), "save", v); err == nil || mutations != 1 {
		t.Fatal("stale draft mutated hotkeys")
	}
}

func TestNativeProfileSavePreservesOtherTabDraftAndDirtyBaseline(t *testing.T) {
	p := nativePages{preview: true}
	page, err := p.loadNativeProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page.Rows = [][]string{{"window", "Window", "pwm", "pwm.1"}}
	page.RowValues["window"] = map[string]string{"channel_key": "window", "channel_name": "Window", "channel_kind": "pwm", "channel_target": "pwm.1", "channel_icon": "sun"}
	v := nativePageValues(page)
	v["selected"] = "window"
	v["channel_key"] = "window"
	v["channel_name"] = "Window draft"
	v["channel_kind"] = "pwm"
	v["channel_target"] = "pwm.1"
	v["channel_icon"] = "sun"
	page = preserveNativeProfileDraft(page, "profile-save", v)
	if page.RowValues["window"]["channel_name"] != "Window draft" || page.DraftBaseline["channel_name"] != "Window" {
		t.Fatal("profile save lost routing draft or marked it clean")
	}
	for _, field := range page.Fields {
		if field.Key == "channel_name" && field.Value != "Window draft" {
			t.Fatal("new route draft lost")
		}
	}
}

func TestNativeRFTransmitRejectsInvalidLiteralBeforeRPC(t *testing.T) {
	p := nativePages{rpc: func(context.Context, string, any, any) error { t.Fatal("invalid draft reached transport"); return nil }}
	for _, code := range []string{"0", "-1", "4294967296", "3.5", "invalid"} {
		if _, err := p.submitNativeTools(context.Background(), "rf-transmit", "transmit", map[string]string{"code": code, "bits": "24", "protocol": "1", "pulse_us": "0", "repeats": "1"}); err == nil {
			t.Fatalf("accepted %s", code)
		}
	}
}
