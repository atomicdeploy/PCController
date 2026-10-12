package appconfig

import "testing"

func TestNativeSensorNamesPreserveAuthoredNamesAndRoles(t *testing.T) {
	original := map[string]string{"sensor.temperature-led": "Host CPU", "relay.4": "Fan"}
	names := NativeSensorNames(original)
	if len(original) != 2 || names["sensor.temperature-led"] != "Host CPU" || names["sensor.temperature-audio"] != "Native temperature 2" {
		t.Fatal("native defaults changed authored names or stored configuration")
	}
	peripherals, _ := ProfileDescriptors(BoardModeUnconfigured, false, names, map[string]PeripheralPresentation{
		"sensor.temperature-audio": {Name: "Battery temperature"},
	})
	for _, peripheral := range peripherals {
		if peripheral.Key == "sensor.temperature-audio" && peripheral.Name != "Battery temperature" {
			t.Fatal("profile name must win over native defaults")
		}
	}
}
