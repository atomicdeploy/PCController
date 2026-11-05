package appconfig

import (
	"reflect"
	"testing"
)

func TestStripHostMenuDefaultsAndLegacyUpgrade(t *testing.T) {
	defaults := DefaultHostMenus()
	if err := validateHostMenus(defaults); err != nil {
		t.Fatal(err)
	}
	if len(defaults.Menus) != 7 || defaults.Menus[6].ID != "strip-controls" {
		t.Fatal("missing strip node")
	}
	legacy := cloneHostMenus(defaults)
	legacy.Menus = legacy.Menus[:6]
	legacy.Menus[0].Items = legacy.Menus[0].Items[:len(legacy.Menus[0].Items)-1]
	if got := normalizeHostMenus(legacy); !reflect.DeepEqual(got, defaults) {
		t.Fatal("shipped legacy menus did not upgrade")
	}
	if got := normalizeHostMenus(defaults); !reflect.DeepEqual(got, defaults) {
		t.Fatal("upgrade not idempotent")
	}
	legacy.Menus[0].Title = "My Panel"
	if got := normalizeHostMenus(legacy); !reflect.DeepEqual(got, legacy) {
		t.Fatal("custom menu was overwritten")
	}
	legacy.Menus[0].Title = "HOST"
	legacy.BuiltinOverrides = []BuiltinMenuOverride{
		{StableID: 0, Label: "DOOR", Title: "Door", Content: "Door", Brightness: 5, EditVisual: "blink", Flags: HostMenuFlags{Visible: true}},
		{StableID: 1, Label: "VOLT", Title: "Voltage", Content: "Voltage", Brightness: 5, EditVisual: "blink", Flags: HostMenuFlags{Visible: true}},
	}
	if got := normalizeHostMenus(legacy); len(got.Menus)+len(got.BuiltinOverrides) != 8 {
		t.Fatal("full directory was extended")
	}
}
