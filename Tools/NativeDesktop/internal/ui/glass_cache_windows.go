//go:build windows

package ui

import (
	"image"
	"runtime"
	"slices"
	"sync"

	"github.com/lxn/walk"
)

type glassControlLayer struct {
	rect    walk.Rectangle
	radius  float64
	style   glassStyle
	pending bool
}

// Keys own palette values, not pointers to a frame-local palette. Pointer
// reveal and spinner phase only invalidate the controls that actually use them.
type glassLayerKey struct {
	rect       walk.Rectangle
	radius     float64
	style      glassStyle
	palette    palette
	hasPalette bool
}

func (layer glassControlLayer) key() glassLayerKey {
	key := glassLayerKey{rect: layer.rect, radius: layer.radius, style: layer.style}
	if layer.style.Palette != nil {
		key.palette = *layer.style.Palette
		key.hasPalette = true
		key.style.Palette = nil
	}
	if key.style.Hover == 0 {
		key.style.RevealX = 0
		key.style.RevealY = 0
	}
	if !key.style.Pending {
		key.style.Phase = 0
	}
	return key
}

type glassControlRaster struct {
	key          glassLayerKey
	shadow, body *image.RGBA
}

// Retains only the current layout and one current raster per control, never an
// unbounded history of hover values. All access is on the window thread.
type glassCompositionCache struct {
	background *image.RGBA
	panels     []glassLayerKey
	masks      []glassRoundedMask
	controls   []glassControlRaster
}

func (cache *glassCompositionCache) prepare(bounds image.Rectangle, panels, controls []glassControlLayer, masks []glassRoundedMask) {
	keys := make([]glassLayerKey, len(panels))
	for i, layer := range panels {
		keys[i] = layer.key()
	}
	resized := cache.background == nil || cache.background.Bounds() != bounds
	if resized || !slices.Equal(cache.panels, keys) {
		if resized {
			cache.background = reuseRGBA(cache.background, bounds)
		} else {
			clear(cache.background.Pix)
		}
		// Each scanline keeps the original panel paint order. Independent
		// bands can rasterize together without changing blending or rounding.
		minRows := max(1, 120000/max(1, bounds.Dx()))
		renderGlassBatches(bounds.Dy(), minRows, func(start, end int) {
			band := cache.background.SubImage(image.Rect(bounds.Min.X, bounds.Min.Y+start, bounds.Max.X, bounds.Min.Y+end)).(*image.RGBA)
			for _, layer := range panels {
				drawGlassRoundedBody(band, layer.rect, layer.radius, layer.style)
			}
		})
		cache.panels = keys
	}
	masksChanged := !slices.Equal(cache.masks, masks)
	if masksChanged {
		cache.masks = append(cache.masks[:0], masks...)
	}
	if len(cache.controls) > len(controls) {
		clear(cache.controls[len(controls):])
		cache.controls = cache.controls[:len(controls)]
	}
	if missing := len(controls) - len(cache.controls); missing > 0 {
		cache.controls = append(cache.controls, make([]glassControlRaster, missing)...)
	}
	drawControls := func(start, end int) {
		for i := start; i < end; i++ {
			layer := controls[i]
			entry := &cache.controls[i]
			key := layer.key()
			rect := image.Rect(layer.rect.X, layer.rect.Y, layer.rect.X+layer.rect.Width, layer.rect.Y+layer.rect.Height)
			bodyBounds := rect.Intersect(bounds)
			shadowBounds := rect.Inset(-glassShadowExtent(layer.style)).Intersect(bounds)
			if entry.body == nil || entry.key != key || entry.body.Bounds() != bodyBounds {
				entry.body = reuseRGBA(entry.body, bodyBounds)
				drawGlassRoundedBody(entry.body, layer.rect, layer.radius, layer.style)
			}
			if masksChanged || entry.shadow == nil || entry.key != key || entry.shadow.Bounds() != shadowBounds {
				entry.shadow = reuseRGBA(entry.shadow, shadowBounds)
				drawGlassRoundedShadowMasked(entry.shadow, layer.rect, layer.radius, layer.style, masks)
			}
			entry.key = key
		}
	}
	if masksChanged {
		renderGlassBatches(len(controls), 3, drawControls)
	} else {
		drawControls(0, len(controls))
	}
}

// Only the raster work fans out. Presentation, layout and the cache lifetime
// stay on the window thread. Small batches avoid goroutine overhead.
func renderGlassBatches(count, minimum int, render func(start, end int)) {
	batches := min(4, runtime.GOMAXPROCS(0), count/minimum)
	if batches < 2 {
		render(0, count)
		return
	}
	var pending sync.WaitGroup
	for batch := 1; batch < batches; batch++ {
		start, end := batch*count/batches, (batch+1)*count/batches
		pending.Go(func() { render(start, end) })
	}
	render(0, count/batches)
	pending.Wait()
}
