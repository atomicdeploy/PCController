package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestPWMActionStepSurvivesUnrelatedChannelConfiguration(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	step := 2.5
	channel := appconfig.PWMChannel(config.UI, 0)
	channel.StepPercent = &step
	config.UI.PWMChannels["pwm.0"] = channel
	update := appconfig.PWMChannel(config.UI, 0)
	update.StepPercent = nil
	update.Gamma = 2.4
	got, err := service.updatePWMChannelConfig(0, update)
	if err != nil || appconfig.PWMStepPercent(got) != 2.5 || got.Gamma != 2.4 {
		t.Fatalf("unrelated curve edit lost action step: %+v %v", got, err)
	}
	defaultStep := 1.0
	update.StepPercent = &defaultStep
	got, err = service.updatePWMChannelConfig(0, update)
	if err != nil || appconfig.PWMStepPercent(got) != 1 {
		t.Fatalf("explicit default reset failed: %+v %v", got, err)
	}
}

func TestPWMRecallPartialConfigAndCrossClientAdvertisement(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	step := 2.5
	before := appconfig.PWMChannel(config.UI, 1)
	before.StepPercent = &step
	config.UI.PWMChannels["pwm.1"] = before
	response := service.Dispatch(context.Background(), Request{Method: "controller.pwm.channel.configure", Params: json.RawMessage(`{"channel":1,"on_behavior":"restore"}`)})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	got := appconfig.PWMChannel(config.UI, 1)
	if got.OnBehavior != "restore" || got.Icon != before.Icon || got.Curve != before.Curve || got.Gamma != before.Gamma || appconfig.PWMStepPercent(got) != step {
		t.Fatalf("partial policy edit lost config: %+v", got)
	}
	if _, err := service.updatePWMChannelConfig(1, appconfig.PWMChannelConfig{Gamma: 2.4}); err != nil {
		t.Fatal(err)
	}
	if appconfig.PWMChannel(config.UI, 1).OnBehavior != "restore" {
		t.Fatal("other client curve edit lost recall")
	}
	settings := service.browserUISettings()
	advertised := false
	for _, control := range settings.Controls {
		if control.Key == "pwm.1" {
			if control.OnBehavior != "restore" {
				t.Fatal("catalog did not share policy")
			}
			for _, action := range control.Actions {
				if action.ID == "pwm.1.toggle" {
					advertised = true
				}
			}
		}
	}
	if !advertised {
		t.Fatal("toggle not advertised")
	}
	for _, invalid := range []string{"previous", "unknown"} {
		if _, err := service.updatePWMChannelConfig(1, appconfig.PWMChannelConfig{OnBehavior: invalid}); err == nil {
			t.Fatal("invalid recall accepted")
		}
		if appconfig.PWMChannel(config.UI, 1).OnBehavior != "restore" {
			t.Fatal("invalid policy persisted")
		}
	}
}

func TestPWMRecallConfigMergesInsideSerializedMutation(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	update := service.UpdateHostConfig
	service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		concurrent := appconfig.PWMChannel(config.UI, 1)
		concurrent.Icon = "sun"
		concurrent.Gamma = 2.4
		config.UI.PWMChannels["pwm.1"] = concurrent
		return update(change)
	}
	got, err := service.updatePWMChannelConfig(1, appconfig.PWMChannelConfig{OnBehavior: "restore"})
	if err != nil || got.Icon != "sun" || got.Gamma != 2.4 || got.OnBehavior != "restore" {
		t.Fatalf("overwrote other client edit: %+v %v", got, err)
	}
}
