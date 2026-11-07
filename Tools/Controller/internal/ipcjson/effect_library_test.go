package ipcjson

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

func TestEffectLibraryCASNoOpAndFailedValidationPreserveConfig(t *testing.T) {
	s, config, updates := appearanceTestService(t)
	config.Macros = []appconfig.Macro{{ID: 9, Name: "Scene", Mode: "host", Color: "violet", Steps: []appconfig.MacroStep{{Kind: "relay", Target: 4, Value: 1}}}}
	config.EffectGroups = map[string]appconfig.EffectGroup{"Scenes": {Icon: "sparkles"}}
	initial := effectLibraryFor(*config)
	call := func(library control.EffectLibrary, revision string) Response {
		raw, _ := json.Marshal(map[string]any{"library": library, "if_match": revision, "mode": "merge"})
		return s.Dispatch(context.Background(), Request{Method: "controller.effects.library.set", Params: raw})
	}
	if r := call(initial.Library, initial.ETag); r.Error != nil || *updates != 0 {
		t.Fatalf("no-op saved: %+v", r.Error)
	}
	changed := initial.Library
	changed.Effects[0].Name = "Scene renamed"
	if r := call(changed, initial.ETag); r.Error != nil || *updates != 1 || config.Macros[0].Name != "Scene renamed" {
		t.Fatalf("save failed: %+v", r.Error)
	}
	if r := call(changed, initial.ETag); r.Error == nil || *updates != 1 {
		t.Fatal("stale library overwrote config")
	}
	current := effectLibraryFor(*config)
	current.Library.Effects[0].Steps = []appconfig.MacroStep{{Kind: "display", Text: strings.Repeat("x", 41)}}
	current.Library.Groups["Invalid"] = appconfig.EffectGroup{Icon: "new"}
	if r := call(current.Library, current.ETag); r.Error == nil || *updates != 1 {
		t.Fatal("invalid library saved")
	}
	if _, exists := config.EffectGroups["Invalid"]; exists {
		t.Fatal("failed candidate leaked group mutation")
	}
	if config.Macros[0].Steps[0].Kind != "relay" {
		t.Fatal("failed validation changed live steps")
	}
	if requestCapability("controller.effects.library.get", nil) != capabilityRead || requestCapability("controller.effects.library.set", nil) != capabilityHostConfig {
		t.Fatal("library authorization boundary changed")
	}
}

func TestEffectLibraryRevisionIsRecheckedInsideStoreWriter(t *testing.T) {
	s, config, _ := appearanceTestService(t)
	config.Macros = []appconfig.Macro{{ID: 1, Name: "Scene", Mode: "host", Color: "green"}}
	initial := effectLibraryFor(*config)
	initial.Library.Effects[0].Name = "Desktop draft"
	original := s.UpdateHostConfig
	s.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		config.Macros[0].Name = "Other client"
		return original(change)
	}
	raw, _ := json.Marshal(map[string]any{"library": initial.Library, "if_match": initial.ETag, "mode": "merge"})
	if _, err := s.updateEffectLibrary(raw); err == nil {
		t.Fatal("writer race accepted")
	}
	if config.Macros[0].Name != "Other client" {
		t.Fatal("concurrent author's draft was lost")
	}
}
