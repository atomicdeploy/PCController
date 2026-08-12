package appconfig

import "testing"

func TestMeasurementTimingDefaultsAndHeadroomAreValidated(t *testing.T) {
	value := Defaults()
	if value.UI.StatusIntervalMS != DefaultMeasurementRefreshMS || value.UI.MeasurementFreshnessMS != DefaultMeasurementFreshnessMS {
		t.Fatalf("defaults=%+v", value.UI)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("default timing rejected: %v", err)
	}
	value.UI.StatusIntervalMS = MeasurementRefreshMinMS - 1
	if err := value.Validate(); err == nil {
		t.Fatal("below-minimum refresh was accepted")
	}
	value = Defaults()
	value.UI.StatusIntervalMS = 500
	value.UI.MeasurementFreshnessMS = 599
	if err := value.Validate(); err == nil {
		t.Fatal("freshness without headroom was accepted")
	}
}
