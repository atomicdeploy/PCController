package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestEffectUpsertJSONPersistsTheCompleteSequenceDefinition(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	config.Macros = nil
	config.StripEffects = nil
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	fadeTarget := uint16(3072)
	effect := EffectDescriptor{
		ID: "12", Name: "Cinema motion", Category: "Motion", Icon: "seat",
		Kind: "sequence", Engine: "host", Editable: true,
		Steps: []appconfig.MacroStep{
			{Kind: "relay-mask", Value: 2, ActionIDs: []string{"seat.a.up"}},
			{AtUS: 750_000, Kind: "relay-mask", ActionIDs: []string{"seat.a.stop"}},
			{AtUS: 900_000, Kind: "display", Text: "DONE", Destination: "segments", DurationMS: 500},
			{AtUS: 1_000_000, Kind: "rf", Code: 0x123456, Bits: 24, Protocol: 1, PulseUS: 350},
			{AtUS: 1_250_000, Kind: "pwm", Target: 11, Value: 128, ToValue: &fadeTarget, DurationMS: 800, Easing: "ease-in-out", SampleRateHz: 30, RepeatCount: 2, RepeatIntervalMS: 1_000},
		},
		Properties: map[string]interface{}{
			"color": "violet", "timing_tolerance_us": 2500,
			"board_profile_key": "cafe-cinema", "board_profile_mode": "cinema-seat-motion",
		},
	}
	encoded, err := json.Marshal(effect)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect upsert-json "+hex.EncodeToString(encoded)); err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 1 {
		t.Fatalf("saved macros=%+v", config.Macros)
	}
	got := config.Macros[0]
	if got.ID != 12 || got.Name != effect.Name || got.Icon != "seat" || got.BoardProfileKey != "cafe-cinema" || len(got.Steps) != 5 {
		t.Fatalf("saved sequence lost descriptor data: %+v", got)
	}
	if got.Steps[0].ActionIDs[0] != "seat.a.up" || got.Steps[2].Text != "DONE" || got.Steps[3].Code != 0x123456 {
		t.Fatalf("saved sequence lost step fields: %+v", got.Steps)
	}
	if got.Steps[4].ToValue == nil || *got.Steps[4].ToValue != fadeTarget || got.Steps[4].Easing != "ease-in-out" || got.Steps[4].RepeatCount != 2 {
		t.Fatalf("saved sequence lost editable timeline fields: %+v", got.Steps[4])
	}

	effect.Name = "Cinema motion revised"
	effect.Steps[1].AtUS = 825_000
	encoded, err = json.Marshal(effect)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect upsert-json "+hex.EncodeToString(encoded)); err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != effect.Name || config.Macros[0].Steps[1].AtUS != 825_000 {
		t.Fatalf("upsert did not replace the complete sequence: %+v", config.Macros)
	}
}

func TestEffectCatalogAndEditsUseOnePCControllerStore(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	if _, err := engine.Execute(context.Background(), "effect create sequence 9 Seat-rise Motion green"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect update 9 Seat-lift Cinema violet"); err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != "Seat-lift" || config.Macros[0].Category != "Cinema" || config.Macros[0].Color != "violet" {
		t.Fatalf("persisted sequence effect=%+v", config.Macros)
	}

	listed, err := engine.Execute(context.Background(), "effect list")
	if err != nil || !strings.Contains(listed, `"reference":"effect:police"`) ||
		!strings.Contains(listed, `"kind":"strip-stream"`) {
		t.Fatalf("effect list=%q err=%v", listed, err)
	}
	if _, err := engine.Execute(context.Background(), "effect update police Police Lighting real alternating-zones 24 4200 32"); err != nil {
		t.Fatal(err)
	}
	if got := config.StripEffects[0]; got.Name != "Police" || got.DefaultFPS != 24 || got.DefaultDurationMS != 4200 || got.DefaultPixels != 32 {
		t.Fatalf("persisted effect=%+v", got)
	}
	inspected, err := engine.Execute(context.Background(), "effect inspect police")
	if err != nil || !strings.Contains(inspected, `"duration_ms":4200`) {
		t.Fatalf("inspect=%q err=%v", inspected, err)
	}
	if _, err := engine.Execute(context.Background(), "effect delete police"); err != nil {
		t.Fatal(err)
	}
	if len(config.StripEffects) != 2 {
		t.Fatalf("delete retained %d effects", len(config.StripEffects))
	}
}

func TestEffectLibraryExportsAndImportsLivingEffectsJSON(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	path := filepath.Join(t.TempDir(), "effects.json")
	if _, err := engine.Execute(context.Background(), fmt.Sprintf("effect export %q", path)); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"primitive": "alternating-zones"`) || strings.Contains(string(content), `"version"`) {
		t.Fatalf("unexpected effects.json: %s", content)
	}
	config.Macros = nil
	config.StripEffects = nil
	if _, err := engine.Execute(context.Background(), fmt.Sprintf("effect import %q replace", path)); err != nil {
		t.Fatal(err)
	}
	if len(config.StripEffects) != 3 || config.StripEffects[0].Program.Primitive != "alternating-zones" {
		t.Fatalf("imported effects=%+v", config.StripEffects)
	}
}

func TestEffectRestoreExamplesOnlyAddsMissingEditableDefaults(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	config.StripEffects = config.StripEffects[:1]
	config.StripEffects[0].Name = "My police lights"
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})

	result, err := engine.Execute(context.Background(), "effect restore-examples")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "restored 2 missing") || len(config.StripEffects) != 3 {
		t.Fatalf("restore result=%q effects=%+v", result, config.StripEffects)
	}
	if config.StripEffects[0].Name != "My police lights" {
		t.Fatalf("existing user edit was overwritten: %+v", config.StripEffects[0])
	}
	result, err = engine.Execute(context.Background(), "effect restore-examples")
	if err != nil || !strings.Contains(result, "restored 0 missing") || len(config.StripEffects) != 3 {
		t.Fatalf("idempotent restore result=%q err=%v effects=%+v", result, err, config.StripEffects)
	}
}

func TestEffectAndGroupPresentationRemainPCControllerOwned(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	if _, err := engine.Execute(context.Background(), "effect create sequence 9 Seat-rise Motion green seat"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect category police Motion"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect icon police lightbulb"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "effect group update Motion Cinema car"); err != nil {
		t.Fatal(err)
	}
	if config.Macros[0].Category != "Cinema" || config.Macros[0].Icon != "seat" ||
		config.StripEffects[0].Category != "Cinema" || config.StripEffects[0].Icon != "lightbulb" ||
		config.EffectGroups["Cinema"].Icon != "car" {
		t.Fatalf("presentation was not persisted: macros=%+v strips=%+v groups=%+v", config.Macros, config.StripEffects, config.EffectGroups)
	}
	listed, err := engine.Execute(context.Background(), "effect list")
	if err != nil || !strings.Contains(listed, `"icon":"seat"`) || !strings.Contains(listed, `"group_icon":"car"`) {
		t.Fatalf("effect list=%q err=%v", listed, err)
	}
}

func TestEffectGroupsPersistWithoutEffectsAndSurviveExportImport(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	config.Macros, config.StripEffects = nil, nil
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			candidate.EffectGroups = make(map[string]appconfig.EffectGroup)
			for name, group := range config.EffectGroups {
				candidate.EffectGroups[name] = group
			}
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	if _, err := engine.Execute(context.Background(), `effect group create "Cinema lighting" lamp`); err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 0 || len(config.StripEffects) != 0 {
		t.Fatal("creating a group created an effect")
	}
	if _, err := engine.Execute(context.Background(), `effect group create "cinema lighting" -`); err == nil {
		t.Fatal("duplicate group accepted")
	}
	if _, err := engine.Execute(context.Background(), `effect group update "Cinema lighting" Lighting -`); err != nil {
		t.Fatal(err)
	}
	listed, err := engine.Execute(context.Background(), "effect group list")
	if err != nil || !strings.Contains(listed, `"name":"Lighting"`) {
		t.Fatalf("groups=%s err=%v", listed, err)
	}
	if len(runtime.Snapshot().EffectGroups) != 1 {
		t.Fatal("empty group missing from live snapshot")
	}
	exported, err := engine.Execute(context.Background(), "effect export")
	if err != nil {
		t.Fatal(err)
	}
	var document effectDocument
	if err := json.Unmarshal([]byte(exported), &document); err != nil {
		t.Fatal(err)
	}
	if _, exists := document.Groups["Lighting"]; !exists {
		t.Fatal("empty group lost during export")
	}
	config.EffectGroups = nil
	if err := importEffectDocument(document, true, func(change func(*appconfig.Config) error) error { return change(&config) }); err != nil {
		t.Fatal(err)
	}
	if _, exists := config.EffectGroups["Lighting"]; !exists {
		t.Fatal("empty group lost during import")
	}
}

func TestEffectDocumentRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	if err := os.WriteFile(path, []byte(`{"effects":[{"id":"1","name":"One","kind":"sequence"}]} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeEffectDocument(path); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing JSON rejection, got %v", err)
	}
}

func TestEffectDocumentReplacementPreservesTheNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.json")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	document := effectDocument{Effects: EffectCatalog(nil, appconfig.DefaultStripEffects())}
	if err := writeEffectDocument(path, document); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "old") || !strings.Contains(string(content), `"effects"`) {
		t.Fatalf("replacement content=%q", content)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".effects-") {
			t.Fatalf("replacement left temporary file %q", entry.Name())
		}
	}
}

func TestUnifiedEffectIdentityRejectsCrossKindCollision(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	config.StripEffects = append(config.StripEffects, appconfig.StripEffect{
		ID: "9", Name: "Reserved", Program: config.StripEffects[0].Program,
		DefaultFPS: 20, DefaultDurationMS: 1000, DefaultPixels: 10,
	})
	engine := NewCommandEngine(runtime, CommandOptions{
		Macros:     func() []appconfig.Macro { return config.Macros },
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	if _, err := engine.Execute(context.Background(), "effect create sequence 9 New Motion green"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected cross-kind collision, got %v", err)
	}
}
