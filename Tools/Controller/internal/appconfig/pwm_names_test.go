package appconfig

import (
	"strings"
	"testing"
)

func TestPWMDisplayNamesMatchFullProfileCatalog(t *testing.T) {
	ui := Defaults().UI
	ui.PeripheralNames = map[string]string{"pwm.1": "old", "pwm.2": "Chandelier / Loostr"}
	presentation := map[string]PeripheralPresentation{
		"pwm.1": {Name: " Window "}, "pwm.3": {Name: "Stage Light"},
		"pwm.4": {Name: "Garden"}, "pwm.5": {Name: " "},
	}
	for _, mode := range []string{BoardModeUnconfigured, BoardModeOrdinaryRelays, BoardModeCinemaSeatMotion} {
		for _, raw := range []bool{false, true} {
			names := PWMDisplayNames(ui, presentation)
			peripherals, controls := ProfileDescriptors(mode, raw, ui.PeripheralNames, presentation)
			ApplyPWMChannelConfig(peripherals, controls, ui)
			seen := 0
			for _, item := range peripherals {
				if item.Kind != "pwm" {
					continue
				}
				want := strings.TrimSpace(item.Name)
				if want == "" {
					want, _ = PeripheralDefaultName(item.Key)
				}
				if names[item.Index] != want {
					t.Fatalf("%s raw=%v: %s=%q, catalog=%q", mode, raw, item.Key, names[item.Index], want)
				}
				seen++
			}
			if seen != PWMChannelCount {
				t.Fatalf("missing PWM channels: %d", seen)
			}
			if names[1] != "Window" || names[2] != "Chandelier / Loostr" {
				t.Fatal("profile/legacy rename precedence changed")
			}
		}
	}
}
