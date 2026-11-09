//go:build windows

package ui

import (
	"image"
	"slices"

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
		for _, layer := range panels {
			drawGlassRoundedBody(cache.background, layer.rect, layer.radius, layer.style)
		}
		cache.panels = keys
	}
	if !slices.Equal(cache.masks, masks) || len(cache.controls) != len(controls) {
		cache.controls = make([]glassControlRaster, len(controls))
		cache.masks = append(cache.masks[:0], masks...)
	}
	for i, layer := range controls {
		entry := &cache.controls[i]
		key := layer.key()
		rect := image.Rect(layer.rect.X, layer.rect.Y, layer.rect.X+layer.rect.Width, layer.rect.Y+layer.rect.Height)
		bodyBounds := rect.Intersect(bounds)
		spread := glassShadowExtent(layer.style)
		shadowBounds := rect.Inset(-spread).Intersect(bounds)
		if entry.body != nil && entry.key == key && entry.body.Bounds() == bodyBounds && entry.shadow != nil && entry.shadow.Bounds() == shadowBounds {
			continue
		}
		if entry.body == nil || entry.body.Bounds() != bodyBounds {
			entry.body = reuseRGBA(entry.body, bodyBounds)
		} else {
			clear(entry.body.Pix)
		}
		if entry.shadow == nil || entry.shadow.Bounds() != shadowBounds {
			entry.shadow = reuseRGBA(entry.shadow, shadowBounds)
		} else {
			clear(entry.shadow.Pix)
		}
		drawGlassRoundedShadowMasked(entry.shadow, layer.rect, layer.radius, layer.style, masks)
		drawGlassRoundedBody(entry.body, layer.rect, layer.radius, layer.style)
		entry.key = key
	}
}
