package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestDiscoveryConfigurationRPCPersistsValidatedTransportSelection(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := Service{
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
	want := appconfig.Discovery{
		MDNSEnabled: true, DNSSDenabled: true, SSDPEnabled: true, UPnPEnabled: true,
		BroadcastEnabled: true, BroadcastPort: 37901, InstanceName: "Workshop",
	}
	params, _ := json.Marshal(want)
	set := service.Dispatch(context.Background(), Request{Method: "controller.discovery.config.set", Params: params})
	if set.Error != nil || config.Integrations.Discovery != want {
		t.Fatalf("set=%#v discovery=%#v", set, config.Integrations.Discovery)
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.discovery.config.get"})
	if get.Error != nil || get.Result != want {
		t.Fatalf("get=%#v want=%#v", get, want)
	}
	if got := requestCapability("controller.discovery.config.set", nil); got != capabilityIntegrations {
		t.Fatalf("set capability=%q", got)
	}
}
