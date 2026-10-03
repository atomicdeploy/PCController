package native

import "testing"

func TestBluetoothOffFactoryProfileIsSteadyInformationalGreen(t *testing.T) {
	profile := DefaultStatusProfiles(128)[StatusConditionBluetoothOff]
	if profile.Kind != StatusEffectNone || profile.Red != 0 || profile.Green != 255 ||
		profile.Blue != 80 || profile.Brightness != 128 || profile.PeriodMS != 0 {
		t.Fatalf("bluetooth-off profile = %+v, want steady #00FF50 at brightness 128", profile)
	}
}
