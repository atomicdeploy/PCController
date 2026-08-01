package appconfig

import (
	"path/filepath"
	"testing"
)

func TestHostMenusRoundTripJSONYAMLTOML(t *testing.T) {
	for _, extension := range []string{".json", ".yaml", ".toml"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "controller"+extension)
			value := Defaults()
			value.HostMenus.Menus[0].Items = append(value.HostMenus.Menus[0].Items,
				HostMenuItem{
					ID: "profile", Label: "PROF", Title: "Profile", Type: "select", Value: "quiet",
					Options:    []HostMenuOption{{Label: "Quiet", Value: "quiet"}, {Label: "Fast", Value: "fast"}},
					ReadAction: "shell:settings", WriteAction: "command:profile ${value}",
				},
			)
			if err := Write(path, value); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			item := loaded.HostMenus.Menus[0].Items[len(loaded.HostMenus.Menus[0].Items)-1]
			if item.ID != "profile" || item.Type != "select" || len(item.Options) != 2 ||
				item.WriteAction != "command:profile ${value}" {
				t.Fatalf("host menu did not round trip: %+v", item)
			}
		})
	}
}

func TestHostMenusRejectUnguardedOSActions(t *testing.T) {
	value := Defaults()
	value.HostMenus.Menus[0].Items = append(value.HostMenus.Menus[0].Items,
		HostMenuItem{ID: "unsafe", Label: "OFF", Title: "Unsafe", Type: "action", WriteAction: "os.shutdown"},
	)
	if err := value.Validate(); err == nil {
		t.Fatal("unguarded OS host-menu action was accepted")
	}
}

func TestDefaultSystemMenuExposesPolicyGatedPowerAndBrightness(t *testing.T) {
	value := Defaults()
	if value.OSActions.Power.Enabled || value.OSActions.Brightness.Enabled {
		t.Fatal("default system actions must be policy-disabled")
	}
	var system *HostMenu
	for index := range value.HostMenus.Menus {
		if value.HostMenus.Menus[index].ID == "system-actions" {
			system = &value.HostMenus.Menus[index]
			break
		}
	}
	if system == nil {
		t.Fatal("default System Actions menu is missing")
	}
	want := map[string]string{
		"brightness": "os.brightness", "suspend": "os.sleep",
		"hibernate": "os.hibernate", "restart": "os.restart",
	}
	for _, item := range system.Items {
		if action, ok := want[item.ID]; ok {
			if item.WriteAction != action || !item.Guarded || item.Disabled {
				t.Fatalf("system item %s=%+v", item.ID, item)
			}
			delete(want, item.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing default system actions: %v", want)
	}
}
