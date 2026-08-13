package ipcjson

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestLocalIntegrationRPCPersistsLifecycleSafety(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := &Service{
		Client:     client,
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	}

	get := service.Dispatch(context.Background(), Request{Method: "controller.integrations.local.get"})
	if get.Error != nil || !strings.Contains(fmt.Sprint(get.Result), "stop-motion") {
		t.Fatalf("get=%#v", get)
	}
	params, _ := json.Marshal(map[string]any{
		"local_device": map[string]any{"enabled": false},
		"data_hub":     map[string]any{"enabled": false},
		"lifecycle_safety": map[string]any{
			"session_lock": "all-off", "suspend": "stop-motion", "refresh_on_resume": false,
		},
		"buzzer_mirror": map[string]any{
			"enabled": true, "native_enabled": true, "web_audio_enabled": false,
			"backend": "external", "executable": "/usr/local/bin/beep",
		},
	})
	set := service.Dispatch(context.Background(), Request{
		Method: "controller.integrations.local.set", Params: params,
	})
	if set.Error != nil || config.Integrations.Lifecycle.SessionLock != appconfig.LifecycleActionAllOff ||
		config.Integrations.Lifecycle.RefreshOnResume ||
		config.Integrations.BuzzerMirror.Backend != "external" ||
		config.Integrations.BuzzerMirror.Executable != "/usr/local/bin/beep" {
		t.Fatalf("set=%#v lifecycle=%+v", set, config.Integrations.Lifecycle)
	}

	badParams, _ := json.Marshal(map[string]any{
		"local_device": map[string]any{"enabled": false},
		"data_hub":     map[string]any{"enabled": false},
		"lifecycle_safety": map[string]any{
			"session_lock": "shutdown", "suspend": "stop-motion", "refresh_on_resume": true,
		},
	})
	bad := service.Dispatch(context.Background(), Request{
		Method: "controller.integrations.local.set", Params: badParams,
	})
	if bad.Error == nil || !strings.Contains(bad.Error.Message, "lifecycle_safety") ||
		config.Integrations.Lifecycle.SessionLock != appconfig.LifecycleActionAllOff {
		t.Fatalf("bad=%#v lifecycle=%+v", bad, config.Integrations.Lifecycle)
	}
}

func TestLocalIntegrationRPCSeparatesPersistentBuzzerConfigFromRuntimeStatus(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	persistent := appconfig.Defaults()
	effective := persistent
	effective.Integrations.BuzzerMirror.Enabled = true
	status := appconfig.BuzzerRuntimeStatus{
		RequestedPath: "host", EffectivePath: "both", BoardStateKnown: true,
		BoardChangeRequired: true, HostMirror: true, BackendRequested: "auto", BackendEffective: "native",
	}
	service := &Service{
		Client:               client,
		HostConfig:           func() appconfig.Config { return effective },
		PersistentHostConfig: func() appconfig.Config { return persistent },
		BuzzerRuntimeStatus:  func() appconfig.BuzzerRuntimeStatus { return status },
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.integrations.local.get"})
	encoded, _ := json.Marshal(get.Result)
	if get.Error != nil || strings.Contains(string(encoded), `"enabled":true`) || !strings.Contains(string(encoded), `"board_change_required":true`) {
		t.Fatalf("get=%s error=%v", encoded, get.Error)
	}
	statusResult := service.Dispatch(context.Background(), Request{Method: "controller.integrations.status"})
	statusJSON, _ := json.Marshal(statusResult.Result)
	if statusResult.Error != nil || !strings.Contains(string(statusJSON), `"backend_effective":"native"`) {
		t.Fatalf("status=%s error=%v", statusJSON, statusResult.Error)
	}
	if requestCapability("controller.integrations.status", nil) != capabilityIntegrations {
		t.Fatal("runtime integration status is not protected by the integrations capability")
	}
}
