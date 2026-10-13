package ipcjson

import (
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
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

func TestPWMRecallProjectionIncludesKnownFlagAndCurrentCurve(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	channel := appconfig.PWMChannel(config.UI, 1)
	channel.OnBehavior = "restore"
	config.UI.PWMChannels["pwm.1"] = channel
	snapshot := controller.Snapshot{}
	service.HostInstanceID = "fixture-host"
	snapshot.ConnectionGeneration, snapshot.PWMRevision = 7, 9
	snapshot.PWMUpdated = time.Unix(1234, 0)
	snapshot.PWMRecall[1] = control.PWMRecallValue{Known: true, Raw: 239}
	values := service.describePWMValuesForSnapshot(native.PWMValues{Available: true}, snapshot)
	if values.HostInstanceID != "fixture-host" || values.ConnectionGeneration != 7 || values.Revision != 9 || !values.Time.Equal(snapshot.PWMUpdated) {
		t.Fatal("semantic projection lost owner/revision receipt")
	}
	got := values.Channels[1]
	if !got.RememberedKnown || got.RememberedRaw != 239 || got.RememberedPercent <= 0 || got.Config.OnBehavior != "restore" {
		t.Fatalf("missing shared recall metadata: %+v", got)
	}
	if values.Channels[0].RememberedKnown {
		t.Fatal("invented unknown memory")
	}
	if service.browserUISettings().PWMChannels["pwm.0"].OnBehavior != "full" {
		t.Fatal("default policy not exposed to UI")
	}
}
