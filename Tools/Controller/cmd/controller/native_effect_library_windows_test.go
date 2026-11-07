//go:build windows && !no_native_gui

package main

import (
	"os"
	"path/filepath"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestNativeEffectLibraryFileRoundTripNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	library := control.EffectLibrary{Effects: []control.EffectDescriptor{{Reference: "effect:1", ID: "1", Name: "Scene", Kind: "sequence", Steps: []appconfig.MacroStep{{Kind: "relay", Target: 4, Value: 1}}}}, Groups: map[string]appconfig.EffectGroup{"Scenes": {Icon: "sparkles"}}}
	if err := nativeWriteEffectLibrary(path, library); err != nil {
		t.Fatal(err)
	}
	got, err := nativeReadEffectLibrary(path)
	if err != nil || len(got.Effects) != 1 || got.Effects[0].Name != "Scene" || got.Groups["Scenes"].Icon != "sparkles" {
		t.Fatal("file roundtrip failed", err)
	}
	library.Effects[0].Name = "Must not overwrite"
	if err := nativeWriteEffectLibrary(path, library); err == nil {
		t.Fatal("existing export overwritten")
	}
	got, err = nativeReadEffectLibrary(path)
	if err != nil || got.Effects[0].Name != "Scene" {
		t.Fatal("original export lost")
	}
	for _, bad := range []string{`{"effects":[],"unknown":true}`, `{"effects":[]} {"effects":[]}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := nativeReadEffectLibrary(path); err == nil {
			t.Fatal("invalid library accepted")
		}
	}
}
func TestNativeEffectFolderRenameIsIsolatedAndPreservesCueTargets(t *testing.T) {
	library := control.EffectLibrary{Effects: []control.EffectDescriptor{{Reference: "effect:1", Name: "Scene", Kind: "sequence", Category: "Old", GroupIcon: "old-icon", Steps: []appconfig.MacroStep{{Kind: "pwm", Target: 3, Value: 4095}}}}, Groups: map[string]appconfig.EffectGroup{"Old": {Icon: "old-icon"}, "Other": {Icon: "folder"}}}
	next, err := nativeRenameEffectGroup(library, "Old", "New", "sun", false)
	if err != nil || next.Effects[0].Category != "New" || next.Groups["New"].Icon != "sun" || next.Effects[0].Steps[0].Target != 3 {
		t.Fatal("rename lost definition", err)
	}
	if library.Effects[0].Category != "Old" || library.Groups["Old"].Icon != "old-icon" {
		t.Fatal("draft rename mutated input")
	}
	if _, err := nativeRenameEffectGroup(library, "Old", "other", "", false); err == nil {
		t.Fatal("case-insensitive folder collision accepted")
	}
}
