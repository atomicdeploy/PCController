//go:build windows

package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/lxn/walk"
	"rayanlamp.local/app/presentation"
)

func glassAnimationFixture() (*Shell, *image.RGBA) {
	s := &Shell{controller: &presentation.Options{}, dark: true}
	s.cfg.UI.ControllerChrome = "windows"
	s.layout.controllerCards = []walk.Rectangle{{X: 24, Y: 88, Width: 592, Height: 264}}
	for i := 0; i < 8; i++ {
		s.layout.controllerButtons = append(s.layout.controllerButtons, controllerButton{
			rect: walk.Rectangle{X: 32 + (i%4)*144, Y: 280 + (i/4)*90, Width: 132, Height: 44}, action: actionID(32 + i), enabled: true,
		})
	}
	return s, image.NewRGBA(image.Rect(0, 0, 640, 720))
}

func TestGlassHoverFrameTiming(t *testing.T) {
	s, foreground := glassAnimationFixture()
	s.composeGlassControls(foreground)
	const frames = 12
	started := time.Now()
	for i := 0; i < frames; i++ {
		s.hoverLevels[32] = float64(i) / float64(frames-1)
		s.visualPointer = walk.Point{X: 60 + i*3, Y: 300}
		s.composeGlassControls(foreground)
	}
	t.Logf("glass hover compositor: %.2f ms/frame over %d frames (640x720)", float64(time.Since(started).Microseconds())/1000/frames, frames)
}

func TestGlassCacheMatchesColdFramesAcrossVisualChanges(t *testing.T) {
	s, foreground := glassAnimationFixture()
	for index, change := range []func(){
		func() {},
		func() { s.hoverLevels[32] = 0.4; s.visualPointer = walk.Point{X: 70, Y: 300} },
		func() { s.hoverLevels[32] = 1; s.pressLevels[32] = 0.7 },
		func() { s.dark = false },
		func() {
			s.layout.controllerButtons[1].active = true
			s.layout.controllerButtons[1].tint = walk.RGB(30, 200, 80)
		},
		func() { s.layout.controllerCards[0].Width -= 20; s.layout.controllerButtons[2].rect.X -= 10 },
		func() { foreground = image.NewRGBA(image.Rect(0, 0, 800, 1000)) },
		func() { s.highContrast = true },
		func() { s.layout.controllerButtons = s.layout.controllerButtons[:3] },
	} {
		change()
		// A live foreground update must not get captured in the glass cache.
		liveColor := color.RGBA{R: uint8(120 + index), G: 80, B: 40, A: 255}
		foreground.SetRGBA(35, 305, liveColor)
		frame := s.composeGlassControls(foreground)
		if frame.RGBAAt(35, 305) != liveColor {
			t.Fatal("live foreground was captured in the glass cache")
		}
		cached := append([]byte(nil), frame.Pix...)
		s.glassCache = glassCompositionCache{}
		cold := s.composeGlassControls(foreground)
		if !bytes.Equal(cached, cold.Pix) {
			t.Fatal("cached glass differs from a fresh render")
		}
	}
}

func TestGlassCachedLayersPreserveMaterial(t *testing.T) {
	for _, dark := range []bool{false, true} {
		bounds := image.Rect(0, 0, 320, 200)
		p := controllerPalette(dark)
		panels := []glassControlLayer{{rect: walk.Rectangle{Width: 320, Height: 200}, style: glassStyle{Palette: &p, Dark: dark, Panel: true, Alpha: 72}}}
		controls := []glassControlLayer{
			{rect: walk.Rectangle{X: 20, Y: 40, Width: 132, Height: 44}, radius: 10, style: glassStyle{Palette: &p, Dark: dark, Hover: 0.6, RevealX: 65, RevealY: 55, Alpha: 150, BorderAlpha: 72, Elevation: 4}},
			{rect: walk.Rectangle{X: 156, Y: 40, Width: 132, Height: 44}, radius: 10, style: glassStyle{Palette: &p, Dark: dark, Primary: true, Pressed: 0.4, Alpha: 150, BorderAlpha: 72, Elevation: 4}},
			{rect: walk.Rectangle{X: -20, Y: 120, Width: 132, Height: 44}, radius: 10, style: glassStyle{Palette: &p, Dark: dark, Pending: true, Phase: 0.3, Alpha: 150, BorderAlpha: 72, Elevation: 4}},
			{rect: walk.Rectangle{X: 400, Y: 400, Width: 100, Height: 40}, radius: 10},
		}
		masks := make([]glassRoundedMask, len(controls))
		for i, layer := range controls {
			masks[i] = glassRoundedMask{Rect: layer.rect, Radius: layer.radius}
		}
		// Reference is the previous direct-to-frame composition, with all
		// shadows preceding all bodies so adjacent buttons never receive halos.
		reference := image.NewRGBA(bounds)
		for _, layer := range panels {
			drawGlassRoundedBody(reference, layer.rect, layer.radius, layer.style)
		}
		for _, layer := range controls {
			drawGlassRoundedShadowMasked(reference, layer.rect, layer.radius, layer.style, masks)
		}
		for _, layer := range controls {
			drawGlassRoundedBody(reference, layer.rect, layer.radius, layer.style)
		}
		var cache glassCompositionCache
		cache.prepare(bounds, panels, controls, masks)
		cached := image.NewRGBA(bounds)
		copy(cached.Pix, cache.background.Pix)
		for _, layer := range cache.controls {
			draw.Draw(cached, layer.shadow.Bounds(), layer.shadow, layer.shadow.Bounds().Min, draw.Over)
		}
		for _, layer := range cache.controls {
			draw.Draw(cached, layer.body.Bounds(), layer.body, layer.body.Bounds().Min, draw.Over)
		}
		maxDelta := 0
		for i, value := range reference.Pix {
			delta := int(value) - int(cached.Pix[i])
			if delta < 0 {
				delta = -delta
			}
			if delta > maxDelta {
				maxDelta = delta
			}
			// The original material truncates at each of up to seven veneer,
			// bevel and rim passes. Regrouping alpha-over can accumulate one
			// rounding unit per pass, but must not alter the material/geometry.
			if delta > 7 {
				t.Fatalf("material changed: dark=%v pixel=%d channel=%d delta=%d", dark, i/4, i%4, delta)
			}
		}
		t.Logf("dark=%v: maximum 8-bit material rounding difference=%d", dark, maxDelta)
	}
}

func TestGlassCacheOnlyInvalidatesAffectedLayers(t *testing.T) {
	s, foreground := glassAnimationFixture()
	s.composeGlassControls(foreground)
	background := append([]byte(nil), s.glassCache.background.Pix...)
	idleKey := s.glassCache.controls[1].key
	idleBody := append([]byte(nil), s.glassCache.controls[1].body.Pix...)
	first := s.glassCache.controls[0].key
	s.hoverLevels[32] = 0.5
	s.visualPointer = walk.Point{X: 70, Y: 300}
	s.composeGlassControls(foreground)
	if !bytes.Equal(background, s.glassCache.background.Pix) {
		t.Fatal("hover changed static material")
	}
	if idleKey != s.glassCache.controls[1].key || !bytes.Equal(idleBody, s.glassCache.controls[1].body.Pix) {
		t.Fatal("pointer invalidated an idle sibling")
	}
	if first == s.glassCache.controls[0].key {
		t.Fatal("hovered control did not invalidate")
	}
}

func TestGlassLayerKeyOwnsPaletteAndIgnoresUnusedPointer(t *testing.T) {
	p := controllerPalette(true)
	layer := glassControlLayer{style: glassStyle{Palette: &p}}
	first := layer.key()
	layer.style.RevealX = 123
	layer.style.RevealY = 456
	layer.style.Phase = 0.9
	if first != layer.key() {
		t.Fatal("unused pointer/phase changed idle key")
	}
	p.accent = walk.RGB(10, 20, 30)
	if first == layer.key() {
		t.Fatal("mutated palette was not detected")
	}
}

func TestVisualTimingCatchesUpAfterSlowFrame(t *testing.T) {
	start := time.Now()
	elapsed := visualFrameElapsed(start, start.Add(180*time.Millisecond))
	if elapsed != 180*time.Millisecond {
		t.Fatalf("lost wall-clock animation time: %v", elapsed)
	}
	if value, active := advanceButtonPressLevel(1, false, elapsed); value != 0 || active {
		t.Fatal("release stretched past its 140 ms duration")
	}
	if visualFrameElapsed(time.Time{}, start) != 16*time.Millisecond || visualFrameElapsed(start, start) != 16*time.Millisecond {
		t.Fatal("invalid initial interval")
	}
}
