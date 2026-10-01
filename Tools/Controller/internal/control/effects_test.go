package control

import (
	"context"
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
	if _, err := engine.Execute(context.Background(), "effect update sequence:9 Seat-lift Cinema violet"); err != nil {
		t.Fatal(err)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != "Seat-lift" || config.Macros[0].Category != "Cinema" || config.Macros[0].Color != "violet" {
		t.Fatalf("persisted sequence effect=%+v", config.Macros)
	}

	listed, err := engine.Execute(context.Background(), "effect list")
	if err != nil || !strings.Contains(listed, `"reference":"strip:police"`) ||
		!strings.Contains(listed, `"kind":"strip-stream"`) {
		t.Fatalf("effect list=%q err=%v", listed, err)
	}
	if _, err := engine.Execute(context.Background(), "effect update strip:police Police Lighting real police 24 4200 32"); err != nil {
		t.Fatal(err)
	}
	if got := config.StripEffects[0]; got.Name != "Police" || got.DefaultFPS != 24 || got.DefaultDurationMS != 4200 || got.DefaultPixels != 32 {
		t.Fatalf("persisted effect=%+v", got)
	}
	inspected, err := engine.Execute(context.Background(), "effect inspect strip:police")
	if err != nil || !strings.Contains(inspected, `"duration_ms":4200`) {
		t.Fatalf("inspect=%q err=%v", inspected, err)
	}
	if _, err := engine.Execute(context.Background(), "effect delete strip:police"); err != nil {
		t.Fatal(err)
	}
	if len(config.StripEffects) != 2 {
		t.Fatalf("delete retained %d effects", len(config.StripEffects))
	}
}
