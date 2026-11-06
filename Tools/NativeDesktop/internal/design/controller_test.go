package design

import "testing"

func TestControllerBrandUsesNeutralSurfacesAndPurpleAccent(t *testing.T) {
	before := Default
	controller := ControllerTokens()
	if controller.Colors.Dark.Accent != "#a78bfa" || controller.Colors.Dark.Surface != "#1d1b22" || controller.Colors.Light.Accent != "#6845bf" || controller.Colors.Light.Surface != "#ffffff" {
		t.Fatal("controller brand reverted to donor cyan/blue")
	}
	if Default != before {
		t.Fatal("requesting controller tokens changed standalone donor defaults")
	}
	if controller.Radii != before.Radii || controller.Motion != before.Motion {
		t.Fatal("branding replacement discarded donor behavior")
	}
}
