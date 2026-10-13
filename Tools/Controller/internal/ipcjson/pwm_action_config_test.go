package ipcjson

import (
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
