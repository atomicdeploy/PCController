package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"pccontroller.local/controller/internal/appconfig"
	"testing"
	"time"
)

func layeredStripFixture() appconfig.StripProgram {
	return appconfig.StripProgram{Primitive: "layers", PeriodMS: 3000, PixelsPerMeter: 60,
		Segments: []appconfig.StripSegment{{ID: "odd", Name: "Odd pixels", Pixels: []int{4, 0, 2}}},
		Layers:   []appconfig.StripLayer{{ID: "base", Name: "Gradient", SegmentID: "odd", Enabled: true, Primitive: "gradient", Primary: appconfig.StripColor{Red: 255}, Secondary: appconfig.StripColor{Blue: 255}, PeriodMS: 3000, DutyPercent: 50, Easing: "linear"}}}
}
func TestStripLayersPreservePixelOrderAndEndpoints(t *testing.T) {
	p := layeredStripFixture()
	f := renderStripProgram(p, 5, 0)
	if f[12] != 255 || f[8] != 255 || f[0] != 128 || f[2] != 128 || f[3] != 0 {
		t.Fatal(f)
	}
	if err := appconfig.ValidateStripLayers(p, 3); err == nil {
		t.Fatal("accepted missing pixels")
	}
	p.Segments[0].Pixels = []int{0, 0}
	if err := appconfig.ValidateStripLayers(p, 5); err == nil {
		t.Fatal("accepted duplicate pixel")
	}
}
func TestStripLayersLaterEnabledLayerWins(t *testing.T) {
	p := layeredStripFixture()
	l := p.Layers[0]
	l.ID = "overlay"
	l.Primitive = "solid"
	l.Primary = appconfig.StripColor{Green: 255}
	l.Enabled = false
	p.Layers = append(p.Layers, l)
	if renderStripProgram(p, 5, 0)[13] != 0 {
		t.Fatal("disabled overlay rendered")
	}
	p.Layers[1].Enabled = true
	if renderStripProgram(p, 5, 0)[13] != 255 {
		t.Fatal("overlay not rendered")
	}
}
func TestStripLayersPatternsAndEasing(t *testing.T) {
	for _, kind := range []string{"solid", "gradient", "ramp", "blink", "breathe"} {
		for _, ease := range []string{"linear", "ease-in", "ease-out", "ease-in-out"} {
			p := layeredStripFixture()
			p.Layers[0].Primitive = kind
			p.Layers[0].Easing = ease
			if err := appconfig.ValidateStripLayers(p, 5); err != nil {
				t.Fatal(err)
			}
			if len(renderStripProgram(p, 5, time.Second)) != 15 {
				t.Fatal(kind)
			}
		}
	}
}
func TestStripLayersPublishAndPrepareUseHostOwnedDefinition(t *testing.T) {
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	if len(config.StripEffects) != 0 {
		t.Fatal("fresh installs must not seed examples")
	}
	engine := NewCommandEngine(runtime, CommandOptions{HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: func(change func(*appconfig.Config) error) error {
		candidate := config
		if err := change(&candidate); err != nil {
			return err
		}
		if err := candidate.Validate(); err != nil {
			return err
		}
		config = candidate
		return nil
	}})
	effect := EffectDescriptor{ID: "studio-strip", Name: "Studio strip", Category: "Lighting", Kind: "strip-stream", Engine: "host", Editable: true, DefaultFPS: 20, DefaultPixels: 5, DurationMS: 1000, Program: layeredStripFixture()}
	raw, _ := json.Marshal(effect)
	if _, err := engine.Execute(context.Background(), "effect upsert-json "+hex.EncodeToString(raw)); err != nil {
		t.Fatal(err)
	}
	if len(config.StripEffects) != 1 || config.StripEffects[0].Program.Segments[0].Pixels[0] != 4 {
		t.Fatal("host lost pixel mapping")
	}
	runner := NewMacroRunner(nil, nil, nil)
	runner.hostConfig = func() appconfig.Config { return config }
	steps, err := compileMediaTimeline(MediaTimelinePlan{ClientID: "strip-authoring", Revision: 1, Cues: []MediaTimelineCue{{ID: "cue", Reference: "effect:studio-strip", TimeMS: 10000, DurationMS: 1000}}}, runner)
	if err != nil || len(steps) < 3 {
		t.Fatalf("prepare: %v, %d", err, len(steps))
	}
	if steps[0].dueMS != 10000 || steps[len(steps)-1].dueMS != 11000 {
		t.Fatal("cue boundaries changed")
	}
	if _, _, ok := stripEffectByID("police"); ok {
		t.Fatal("implicit demo fallback remains")
	}
}

func TestStripLayersRejectIncompleteJSON(t *testing.T) {
	program := layeredStripFixture()
	raw, _ := json.Marshal(program)
	var document map[string]any
	_ = json.Unmarshal(raw, &document)
	layer := document["layers"].([]any)[0].(map[string]any)
	delete(layer, "enabled")
	raw, _ = json.Marshal(document)
	if json.Unmarshal(raw, &program) == nil {
		t.Fatal("missing enabled was silently interpreted as false")
	}
}
