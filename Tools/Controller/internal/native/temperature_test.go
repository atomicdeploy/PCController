package native

import "testing"

func TestStatusTemperatureAvailabilityRequiresFlagAndValidDS18B20Value(t *testing.T) {
	tests := []struct {
		name  string
		flags uint16
		value int16
		want  bool
	}{
		{name: "valid minimum", flags: StatusTemperatureLED, value: MinimumTemperatureCentiC, want: true},
		{name: "valid maximum", flags: StatusTemperatureLED, value: MaximumTemperatureCentiC, want: true},
		{name: "missing availability flag", value: 2500},
		{name: "disconnected sentinel", flags: StatusTemperatureLED, value: InvalidTemperatureCentiC},
		{name: "below sensor range", flags: StatusTemperatureLED, value: MinimumTemperatureCentiC - 1},
		{name: "above sensor range", flags: StatusTemperatureLED, value: MaximumTemperatureCentiC + 1},
		{name: "wrong sensor flag", flags: StatusTemperatureBT, value: 2500},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := TemperatureAvailable(test.flags, test.value, StatusTemperatureLED); got != test.want {
				t.Fatalf("TemperatureAvailable(0x%04X, %d)=%t, want %t", test.flags, test.value, got, test.want)
			}
		})
	}

	status := Status{
		Flags:     StatusTemperatureLED | StatusTemperatureBT,
		TLEDCenti: 2512, TBTCenti: -550,
	}
	if value, ok := status.LEDTemperature(); !ok || value != 2512 {
		t.Fatalf("LEDTemperature()=(%d,%t)", value, ok)
	}
	if value, ok := status.BTAudioTemperature(); !ok || value != -550 {
		t.Fatalf("BTAudioTemperature()=(%d,%t)", value, ok)
	}
}
