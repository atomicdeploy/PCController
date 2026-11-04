package ipcjson

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIntroPreferencesAndFixedConfigurationOpenTargets(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	config.UI.SetupComplete = true
	result := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: json.RawMessage(`{"intro_mode":"skip","setup_complete":false}`)})
	if result.Error != nil || config.UI.IntroMode != "skip" || config.UI.SetupComplete || config.UI.AppTitle != "Workshop Controller" {
		t.Fatalf("reset=%+v", result)
	}
	result = service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: json.RawMessage(`{"intro_mode":"bad"}`)})
	if result.Error == nil || config.UI.IntroMode != "skip" {
		t.Fatal("invalid mode persisted")
	}
	called := ""
	service.ConfigFile = "config.json"
	service.OpenConfig = func(target string) error { called = target; return nil }
	for _, target := range []string{"file", "folder"} {
		params, _ := json.Marshal(map[string]string{"target": target})
		result = service.Dispatch(context.Background(), Request{Method: "controller.ui.config.open", Params: params})
		if result.Error != nil || called != target {
			t.Fatalf("open=%+v", result)
		}
	}
	called = ""
	result = service.Dispatch(context.Background(), Request{Method: "controller.ui.config.open", Params: json.RawMessage(`{"target":"file","path":"arbitrary.exe"}`)})
	if result.Error == nil || called != "" {
		t.Fatal("arbitrary path accepted")
	}
	if requestCapability("controller.ui.config.open", nil) != capabilityHostConfig {
		t.Fatal("opening configuration lost mutation authorization")
	}
}
