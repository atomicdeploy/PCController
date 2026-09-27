package tui

import (
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestFormatStatusTemperatureUsesAvailabilityFlagSentinelAndRange(t *testing.T) {
	if got := formatStatusTemperature(2512, native.StatusTemperatureLED, native.StatusTemperatureLED, 2); got != "25.12 °C" {
		t.Fatalf("valid temperature=%q", got)
	}
	for _, test := range []struct {
		value int16
		flags uint16
	}{
		{value: 2512},
		{value: native.InvalidTemperatureCentiC, flags: native.StatusTemperatureLED},
		{value: native.MaximumTemperatureCentiC + 1, flags: native.StatusTemperatureLED},
	} {
		if got := formatStatusTemperature(test.value, test.flags, native.StatusTemperatureLED, 2); got != "Unavailable" {
			t.Fatalf("invalid temperature %d/0x%04X=%q", test.value, test.flags, got)
		}
	}
}

func TestTemperatureSampleValuesDropsUnavailableSamples(t *testing.T) {
	samples := []measurementSample{
		{Flags: native.StatusTemperatureLED, TLEDCenti: 2500},
		{TLEDCenti: 2600},
		{Flags: native.StatusTemperatureLED, TLEDCenti: native.InvalidTemperatureCentiC},
		{Flags: native.StatusTemperatureLED, TLEDCenti: 2700},
	}
	values := temperatureSampleValues(samples, native.StatusTemperatureLED, func(sample measurementSample) int16 {
		return sample.TLEDCenti
	})
	if len(values) != 2 || values[0] != 2500 || values[1] != 2700 {
		t.Fatalf("temperature graph values=%v", values)
	}
}
