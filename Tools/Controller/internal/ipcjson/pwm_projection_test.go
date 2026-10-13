package ipcjson

import (
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func TestPWMProjectionUsesOneConfigReadAndPreservesCalibration(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	config.UI.PeripheralNames["pwm.1"] = "Window"
	reads := 0
	service.HostConfig = func() appconfig.Config { reads++; return *config }
	raw := native.PWMValues{Available: true, SelectedChannel: 1}
	raw.Values[1] = 2048
	values := service.describePWMValuesForSnapshot(raw, service.Client.Snapshot())
	if reads != 1 {
		t.Fatalf("PWM frame allocated %d configuration views, want one", reads)
	}
	if values.Channels[1].Name != "Window" || values.Channels[1].RawValue != 2048 || values.Values != raw.Values {
		t.Fatalf("projection lost authoritative values/name: %+v", values.Channels[1])
	}
	if values.Channels[1].LogicalPercent <= 0 || values.Channels[1].LogicalPercent >= 100 {
		t.Fatal("projection lost calibrated non-endpoint PWM percentage")
	}
}
