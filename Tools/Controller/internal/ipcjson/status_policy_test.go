package ipcjson

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestNativeStatusPolicyCASAndReadCapability(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	initial := config.Integrations.StatusLED
	revision := canonicalSettingsDigest(initial)
	encoded, _ := json.Marshal(initial)
	var params map[string]any
	_ = json.Unmarshal(encoded, &params)
	params["if_match"] = revision
	params["transition_ms"] = 777
	raw, _ := json.Marshal(params)
	response := service.Dispatch(context.Background(), Request{Method: "controller.status_led.policy.set", Params: raw})
	if response.Error != nil || config.Integrations.StatusLED.TransitionMS != 777 || *updates != 1 {
		t.Fatalf("status policy failed: %+v", response)
	}
	if response := service.Dispatch(context.Background(), Request{Method: "controller.status_led.policy.set", Params: raw}); response.Error == nil || *updates != 1 {
		t.Fatal("stale policy overwrote configuration")
	}
	if requestCapability("controller.status_led.policy.set", nil) != capabilityHostConfig || requestCapability("controller.status_led.policy.get", nil) != capabilityRead {
		t.Fatal("policy capability boundary changed")
	}
}

func TestNativePeerMetadataPreservesHiddenCredentialsAndFencesDrafts(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	config.Integrations.WebSocketClients = []appconfig.WebSocketClient{{Name: "peer", URL: "ws://127.0.0.1:9191/ipc", AuthToken: "never-display", Protocol: "jsonrpc"}}
	current := service.networkPeersConfig()
	encoded, _ := json.Marshal(current)
	if strings.Contains(string(encoded), "never-display") {
		t.Fatal("credential leaked into peer inventory")
	}
	revision := canonicalSettingsDigest(current)
	current.Peers[0].ForwardEvents = true
	current.IfMatch = revision
	raw, _ := json.Marshal(current)
	if _, err := service.setNetworkPeers(raw); err != nil {
		t.Fatal(err)
	}
	if config.Integrations.WebSocketClients[0].AuthToken != "never-display" || *updates != 1 {
		t.Fatal("metadata edit discarded hidden credential")
	}
	if _, err := service.setNetworkPeers(raw); err == nil || *updates != 1 {
		t.Fatal("stale peer draft accepted")
	}
}

func TestNativeHotkeyCASProtectsConcurrentBindings(t *testing.T) {
	service, config, updates := appearanceTestService(t)
	config.Integrations.Hotkeys = []appconfig.Hotkey{{Name: "Panel", Enabled: true, Chord: "Ctrl+Alt+P", Command: "status"}}
	revision := canonicalSettingsDigest(config.Integrations.Hotkeys)
	command := "hello"
	if _, err := service.applyHotkeyMutation(hotkeyMutation{Operation: "upsert", Name: "Panel", Command: &command, IfMatch: revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.applyHotkeyMutation(hotkeyMutation{Operation: "remove", Name: "Panel", IfMatch: revision}); err == nil || *updates != 1 {
		t.Fatal("stale editor removed changed hotkey")
	}
	if config.Integrations.Hotkeys[0].Command != "hello" {
		t.Fatal("binding lost")
	}
}
