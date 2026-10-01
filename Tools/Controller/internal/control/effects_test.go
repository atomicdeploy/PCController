package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

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
