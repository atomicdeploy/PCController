package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnpublishedStatusGestureIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := os.WriteFile(path, []byte(`{"schema":1,"host_menus":{"request_gesture":"status-hold-k4"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("unpublished gesture spelling was accepted")
	}
}

func TestDefaultHostMenuExposesCurrentDateAndTime(t *testing.T) {
	items := Defaults().HostMenus.Menus[0].Items
	want := map[string]string{"date": "host.date", "time": "host.time"}
	for _, item := range items {
		if action, ok := want[item.ID]; ok {
			if item.Type != "readonly" || item.ReadAction != action {
				t.Fatalf("default %s item=%+v", item.ID, item)
			}
			delete(want, item.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing date/time host-menu items: %v", want)
	}
}

func TestDefaultHostMenusExposeMacroLibraryRecordPlaybackAndCancel(t *testing.T) {
	config := Defaults().HostMenus
	menus := make(map[string]HostMenu, len(config.Menus))
	for _, menu := range config.Menus {
		menus[menu.ID] = menu
	}
	if len(config.Menus) > 8 {
		t.Fatalf("default overlay has %d nodes; firmware directory maximum is 8", len(config.Menus))
	}
	library, ok := menus["macro-library"]
	if !ok || library.ParentID != 0x80 || len(library.Items) < 5 {
		t.Fatalf("macro library menu=%+v", library)
	}
	selected := library.Items[0]
	if selected.Type != "select" || selected.OptionsSource != "macro.library" ||
		selected.ReadAction != "host.macro.selection" || selected.WriteAction != "host.macro.selection" {
		t.Fatalf("macro selector=%+v", selected)
	}
	wantActions := map[string]bool{
		"host.macro.play":           false,
		"host.macro.record.start":   false,
		"host.macro.record.save":    false,
		"host.macro.record.discard": false,
		"host.macro.cancel":         false,
		"host.macro.cancel.keep":    false,
	}
	for _, menuID := range []string{"macro-library", "macro-recording", "macro-playback"} {
		for _, item := range menus[menuID].Items {
			if _, tracked := wantActions[item.WriteAction]; tracked {
				wantActions[item.WriteAction] = true
			}
		}
	}
	for action, present := range wantActions {
		if !present {
			t.Errorf("default host Macro menus omit %s", action)
		}
	}
}

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
