//go:build windows

package ui

import (
	"pccontroller.local/desktop/presentation"
	"testing"
)

func TestControllerMeasurementVisibilityAndPrecision(t *testing.T) {
	state := presentation.Snapshot{HavePower: true, HaveTemperature: true, HaveTemperatureBT: true, SupplyMV: 12345, BusMV: 12200, CurrentMA: 1234, PowerMW: 15000, TemperatureC: 32.125, TemperatureBTC: 29.5}
	if got := len(controllerMeasurements(state)); got != 6 {
		t.Fatalf("missing real measurement: %d", got)
	}
	state.Preferences = &presentation.Preferences{ShowSupplyVoltage: true, ShowTemperatureBT: true, VoltageDecimals: 3, TemperatureDecimals: 1}
	values := controllerMeasurements(state)
	if len(values) != 2 || values[0].value != "12.345 V" || values[1].value != "29.5 °C" {
		t.Fatalf("preferences ignored: %+v", values)
	}
	if controllerMeasurementHeight(state, 512) != 84 {
		t.Fatal("unnecessary measurement rows")
	}
	state.HavePower, state.HaveTemperatureBT = false, false
	if got := controllerMeasurements(state); len(got) != 0 {
		t.Fatalf("unknown measurements published: %+v", got)
	}
}
