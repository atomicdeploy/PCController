//go:build windows

package ui

import (
	"fmt"
	"pccontroller.local/desktop/presentation"
)

type controllerMeasurement struct{ label, value string }

func controllerMetricVisible(preferences *presentation.Preferences, name string) bool {
	if preferences == nil {
		return true
	}
	switch name {
	case "Supply", "Voltage":
		return preferences.ShowSupplyVoltage
	case "Bus voltage":
		return preferences.ShowBusVoltage
	case "Current":
		return preferences.ShowCurrent
	case "Power":
		return preferences.ShowPower
	case "LED temperature":
		return preferences.ShowTemperatureLED
	case "BT temperature":
		return preferences.ShowTemperatureBT
	}
	return true
}

func controllerMetricValue(preferences *presentation.Preferences, value float64, unit string) string {
	precision := 2
	if preferences != nil {
		switch unit {
		case "V":
			precision = preferences.VoltageDecimals
		case "A":
			precision = preferences.CurrentDecimals
		case "W":
			precision = preferences.PowerDecimals
		case "°C":
			precision = preferences.TemperatureDecimals
		}
	}
	return fmt.Sprintf("%.*f %s", max(0, min(3, precision)), value, unit)
}

func controllerMeasurements(state presentation.Snapshot) []controllerMeasurement {
	var result []controllerMeasurement
	add := func(known bool, label string, value float64, unit string) {
		if known && controllerMetricVisible(state.Preferences, label) {
			result = append(result, controllerMeasurement{label, controllerMetricValue(state.Preferences, value, unit)})
		}
	}
	add(state.HavePower, "Supply", float64(state.SupplyMV)/1000, "V")
	add(state.HavePower, "Bus voltage", float64(state.BusMV)/1000, "V")
	add(state.HavePower, "Current", float64(state.CurrentMA)/1000, "A")
	add(state.HavePower, "Power", float64(state.PowerMW)/1000, "W")
	add(state.HaveTemperature, "LED temperature", state.TemperatureC, "°C")
	add(state.HaveTemperatureBT, "BT temperature", state.TemperatureBTC, "°C")
	return result
}

func controllerMeasurementColumns(width, count int) int {
	columns := 3
	if width < 480 {
		columns = 2
	}
	return max(1, min(columns, count))
}
func controllerMeasurementHeight(state presentation.Snapshot, width int) int {
	count := len(controllerMeasurements(state))
	if count == 0 {
		return 0
	}
	columns := controllerMeasurementColumns(width, count)
	return ((count + columns - 1) / columns) * 84
}
