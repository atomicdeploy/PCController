//go:build windows

package ui

import (
	"bytes"
	"image"
	"image/draw"
	"testing"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/winfx"
)

func TestHighContrastPaletteUsesWindowsSystemColors(t *testing.T) {
	t.Parallel()

	colors := highContrastPalette()
	if colors.backgroundTop != walk.Color(win.GetSysColor(win.COLOR_WINDOW)) ||
		colors.backgroundBottom != walk.Color(win.GetSysColor(win.COLOR_WINDOW)) {
		t.Fatal("high-contrast background does not use COLOR_WINDOW")
	}
	if colors.text != walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT)) {
		t.Fatal("high-contrast text does not use COLOR_WINDOWTEXT")
	}
	if colors.accent != walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHT)) {
		t.Fatal("high-contrast accent does not use COLOR_HIGHLIGHT")
	}
	if colors.primaryText !=
		walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHTTEXT)) {
		t.Fatal("high-contrast primary text does not use COLOR_HIGHLIGHTTEXT")
	}
}

func TestDashboardGeometryFitsResizableBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		bounds walk.Rectangle
	}{
		{name: "minimum", bounds: walk.Rectangle{Width: 340, Height: 520}},
		{name: "default", bounds: walk.Rectangle{Width: 380, Height: 600}},
		{name: "maximum", bounds: walk.Rectangle{Width: 900, Height: 1200}},
		{
			name: "custom title bar origin",
			bounds: walk.Rectangle{
				X: 0, Y: 34, Width: 424, Height: 585,
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bounds := test.bounds
			layout := calculateDashboardGeometry(bounds)
			for name, rectangle := range map[string]walk.Rectangle{
				"title": layout.title, "settings": layout.settings,
				"diagnostics": layout.diagnostics, "card": layout.card,
				"lamp": layout.lamp, "lamp hit": layout.lampHit,
				"state": layout.state, "toggle": layout.toggle,
				"on": layout.on, "off": layout.off, "hint": layout.hint,
			} {
				if !rectangleInside(rectangle, bounds) {
					t.Errorf("%s rectangle %+v is outside %+v", name, rectangle, bounds)
				}
			}
			if layout.card.Y+layout.card.Height > layout.toggle.Y {
				t.Errorf("card overlaps toggle: card=%+v toggle=%+v", layout.card, layout.toggle)
			}
			if layout.toggle.Y+layout.toggle.Height > layout.on.Y ||
				layout.toggle.Y+layout.toggle.Height > layout.off.Y {
				t.Errorf("toggle overlaps on/off row")
			}
			if layout.on.Y+layout.on.Height > layout.hint.Y ||
				layout.off.Y+layout.off.Height > layout.hint.Y {
				t.Errorf("on/off row overlaps hint")
			}
		})
	}
}

func TestDashboardGeometryMirrorsExactlyForRTL(t *testing.T) {
	t.Parallel()
	bounds := walk.Rectangle{X: 13, Y: 34, Width: 540, Height: 700}
	ltr := calculateDashboardGeometry(bounds)
	rtl := mirrorDashboardGeometry(ltr, bounds)

	pairs := map[string][2]walk.Rectangle{
		"title":       {ltr.title, rtl.title},
		"settings":    {ltr.settings, rtl.settings},
		"diagnostics": {ltr.diagnostics, rtl.diagnostics},
		"card":        {ltr.card, rtl.card},
		"lamp":        {ltr.lamp, rtl.lamp},
		"lamp hit":    {ltr.lampHit, rtl.lampHit},
		"state":       {ltr.state, rtl.state},
		"toggle":      {ltr.toggle, rtl.toggle},
		"on":          {ltr.on, rtl.on},
		"off":         {ltr.off, rtl.off},
		"hint":        {ltr.hint, rtl.hint},
	}
	for name, pair := range pairs {
		want := mirrorRectangleHorizontally(pair[0], bounds)
		if pair[1] != want {
			t.Errorf("%s = %+v, want exact mirror %+v", name, pair[1], want)
		}
		if !rectangleInside(pair[1], bounds) {
			t.Errorf("%s RTL rectangle %+v is outside %+v", name, pair[1], bounds)
		}
	}
	if rtl.on.X <= rtl.off.X {
		t.Fatalf("RTL on/off actions did not reverse: on=%+v off=%+v", rtl.on, rtl.off)
	}
	if rtl.settings.X <= rtl.diagnostics.X {
		t.Fatalf(
			"RTL header actions did not reverse: settings=%+v diagnostics=%+v",
			rtl.settings,
			rtl.diagnostics,
		)
	}
}

func TestGlassRoundedRectIsClippedAntialiasedAndPremultiplied(t *testing.T) {
	t.Parallel()

	target := image.NewRGBA(image.Rect(0, 0, 32, 32))
	drawGlassRoundedRect(
		target,
		walk.Rectangle{X: 4, Y: 4, Width: 24, Height: 24},
		8,
		glassStyle{Dark: true, Alpha: 72, BorderAlpha: 140},
	)

	if got := target.RGBAAt(16, 16); got.A == 0 {
		t.Fatal("glass center is transparent")
	}
	if got := target.RGBAAt(0, 0); got.R != 0 || got.G != 0 ||
		got.B != 0 || got.A != 0 {
		t.Fatalf("pixel outside control = %#v, want transparent", got)
	}

	partialCoverage := false
	for offset := 0; offset < len(target.Pix); offset += 4 {
		red := target.Pix[offset]
		green := target.Pix[offset+1]
		blue := target.Pix[offset+2]
		alpha := target.Pix[offset+3]
		if red > alpha || green > alpha || blue > alpha {
			t.Fatalf(
				"non-premultiplied pixel at offset %d: rgba=(%d,%d,%d,%d)",
				offset, red, green, blue, alpha,
			)
		}
		if alpha > 0 && alpha < 60 {
			partialCoverage = true
		}
	}
	if !partialCoverage {
		t.Fatal("rounded edge contains no partial-coverage antialiasing")
	}
}

func TestGlassShadowFadesBeforeItsScanBoundary(t *testing.T) {
	t.Parallel()

	for _, hover := range []float64{0, 1} {
		style := glassStyle{
			Dark: true, Hover: hover,
			Elevation: 5.5, Alpha: 48, BorderAlpha: 76,
		}
		extent := glassShadowExtent(style)
		rect := walk.Rectangle{X: 40, Y: 30, Width: 80, Height: 44}
		target := image.NewRGBA(image.Rect(0, 0, 180, 130))
		drawGlassRoundedRect(target, rect, 14, style)

		lastShadowRow := rect.Y + rect.Height + extent - 1
		maxAlpha := uint8(0)
		for x := rect.X - extent; x < rect.X+rect.Width+extent; x++ {
			alpha := target.RGBAAt(x, lastShadowRow).A
			if alpha > maxAlpha {
				maxAlpha = alpha
			}
		}
		if maxAlpha > 1 {
			t.Fatalf(
				"hover %.0f shadow tail alpha at scan edge = %d, want <= 1",
				hover,
				maxAlpha,
			)
		}
		if target.RGBAAt(rect.X+rect.Width/2, rect.Y+rect.Height+4).A == 0 {
			t.Fatalf("hover %.0f has no visible elevation shadow", hover)
		}
	}
}

func TestAdjacentGlassShadowsDoNotDarkenSiblingBodies(t *testing.T) {
	t.Parallel()

	style := glassStyle{
		Dark: true, Elevation: 5.5,
		Alpha: 48, BorderAlpha: 76,
	}
	left := glassRoundedMask{
		Rect:   walk.Rectangle{X: 20, Y: 20, Width: 80, Height: 44},
		Radius: 14,
	}
	right := glassRoundedMask{
		Rect:   walk.Rectangle{X: 108, Y: 20, Width: 80, Height: 44},
		Radius: 14,
	}
	masks := []glassRoundedMask{left, right}
	target := image.NewRGBA(image.Rect(0, 0, 210, 90))
	for _, control := range masks {
		drawGlassRoundedShadowMasked(
			target,
			control.Rect,
			control.Radius,
			style,
			masks,
		)
	}

	for _, point := range []image.Point{{96, 42}, {112, 42}} {
		if got := target.RGBAAt(point.X, point.Y); got.A != 0 {
			t.Fatalf("shadow leaked under sibling at %v: %#v", point, got)
		}
	}
	if got := target.RGBAAt(104, 42); got.A == 0 {
		t.Fatal("elevation shadow unexpectedly vanished from the gap")
	}
}

func TestRevealLightRemainsLocalOnWideControl(t *testing.T) {
	t.Parallel()

	style := glassStyle{
		Dark: true, Hover: 1,
		RevealX: 80, RevealY: 40,
		Alpha: 48, BorderAlpha: 76,
	}
	withReveal := image.NewRGBA(image.Rect(0, 0, 900, 80))
	drawGlassRoundedRect(
		withReveal,
		walk.Rectangle{X: 20, Y: 16, Width: 860, Height: 48},
		14,
		style,
	)
	style.RevealX = -10000
	style.RevealY = -10000
	withoutReveal := image.NewRGBA(image.Rect(0, 0, 900, 80))
	drawGlassRoundedRect(
		withoutReveal,
		walk.Rectangle{X: 20, Y: 16, Width: 860, Height: 48},
		14,
		style,
	)

	near := withReveal.RGBAAt(80, 40)
	nearBase := withoutReveal.RGBAAt(80, 40)
	if near == nearBase {
		t.Fatal("reveal light does not affect pixels near the pointer")
	}
	far := withReveal.RGBAAt(760, 40)
	farBase := withoutReveal.RGBAAt(760, 40)
	if far != farBase {
		t.Fatalf("reveal light leaked across wide control: got %#v base %#v", far, farBase)
	}
}

func TestGlassControlUsesDirectionalMaterialLayers(t *testing.T) {
	t.Parallel()

	rect := walk.Rectangle{X: 20, Y: 16, Width: 180, Height: 48}
	target := image.NewRGBA(image.Rect(0, 0, 220, 84))
	drawGlassRoundedRect(
		target,
		rect,
		14,
		glassStyle{
			Dark: true, Elevation: 5.5,
			Alpha: 48, BorderAlpha: 76,
		},
	)

	sample := func(top, bottom int) float64 {
		var brightness, weight float64
		for y := top; y < bottom; y++ {
			for x := rect.X + rect.Width/4; x < rect.X+rect.Width*3/4; x++ {
				pixel := target.RGBAAt(x, y)
				if pixel.A == 0 {
					continue
				}
				alpha := float64(pixel.A)
				brightness += (float64(pixel.R) +
					float64(pixel.G) +
					float64(pixel.B)) * 255 / alpha
				weight++
			}
		}
		if weight == 0 {
			return 0
		}
		return brightness / weight
	}
	upper := sample(rect.Y+5, rect.Y+13)
	lower := sample(rect.Y+rect.Height-13, rect.Y+rect.Height-5)
	if upper <= lower+10 {
		t.Fatalf(
			"material face has no directional lighting: upper %.1f lower %.1f",
			upper,
			lower,
		)
	}
}

func TestPrimaryGlassControlHasDenserAccentPlate(t *testing.T) {
	t.Parallel()

	rect := walk.Rectangle{X: 20, Y: 16, Width: 180, Height: 48}
	render := func(primary bool) *image.RGBA {
		target := image.NewRGBA(image.Rect(0, 0, 220, 84))
		drawGlassRoundedRect(
			target,
			rect,
			14,
			glassStyle{
				Dark: true, Primary: primary,
				Elevation: 5.5, Alpha: 48, BorderAlpha: 76,
			},
		)
		return target
	}

	secondary := render(false).RGBAAt(
		rect.X+rect.Width/2,
		rect.Y+rect.Height/2,
	)
	primary := render(true).RGBAAt(
		rect.X+rect.Width/2,
		rect.Y+rect.Height/2,
	)
	if primary.A < secondary.A+30 {
		t.Fatalf(
			"accent plate is not materially denser: primary alpha %d secondary %d",
			primary.A,
			secondary.A,
		)
	}
	if primary.B <= primary.R {
		t.Fatalf("accent plate lost its blue tint: %#v", primary)
	}
}

func TestPressedGlassControlDropsElevation(t *testing.T) {
	t.Parallel()

	rect := walk.Rectangle{X: 30, Y: 20, Width: 140, Height: 46}
	render := func(pressed float64) *image.RGBA {
		target := image.NewRGBA(image.Rect(0, 0, 200, 100))
		drawGlassRoundedRect(
			target,
			rect,
			14,
			glassStyle{
				Dark: true, Pressed: pressed,
				Elevation: 5.5, Alpha: 48, BorderAlpha: 76,
			},
		)
		return target
	}

	raised := render(0)
	pressed := render(1)
	shadowPoint := image.Point{
		X: rect.X + rect.Width/2,
		Y: rect.Y + rect.Height + 4,
	}
	raisedShadow := raised.RGBAAt(shadowPoint.X, shadowPoint.Y).A
	pressedShadow := pressed.RGBAAt(shadowPoint.X, shadowPoint.Y).A
	if raisedShadow <= pressedShadow {
		t.Fatalf(
			"pressed control did not drop its elevation: raised %d pressed %d",
			raisedShadow,
			pressedShadow,
		)
	}

	upperFace := image.Point{X: rect.X + rect.Width/2, Y: rect.Y + 4}
	raisedFace := raised.RGBAAt(upperFace.X, upperFace.Y)
	pressedFace := pressed.RGBAAt(upperFace.X, upperFace.Y)
	raisedBrightness := int(raisedFace.R) + int(raisedFace.G) + int(raisedFace.B)
	pressedBrightness := int(pressedFace.R) +
		int(pressedFace.G) +
		int(pressedFace.B)
	if raisedBrightness <= pressedBrightness {
		t.Fatalf(
			"pressed face retained its raised specular: raised %#v pressed %#v",
			raisedFace,
			pressedFace,
		)
	}
}

func TestRecoveredForegroundComposesOverGlass(t *testing.T) {
	t.Parallel()

	glass := image.NewRGBA(image.Rect(0, 0, 1, 1))
	compositeStraightPixel(glass, 0, 0, 0, 0, 255, 128)
	foreground := image.NewRGBA(image.Rect(0, 0, 1, 1))
	foreground.Pix = []byte{128, 0, 0, 128}
	draw.Draw(glass, glass.Bounds(), foreground, image.Point{}, draw.Over)

	got := glass.RGBAAt(0, 0)
	want := [4]uint8{128, 0, 63, 192}
	if [4]uint8{got.R, got.G, got.B, got.A} != want {
		t.Fatalf("foreground over glass = %#v, want %#v", got, want)
	}
}

func TestGlassCompositorConfigurationMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		backdrop string
		layered  bool
		want     bool
	}{
		{"glass layered", winfx.BackdropAcrylic, true, true},
		{"glass compatible", winfx.BackdropAcrylic, false, false},
		{"solid layered", winfx.BackdropSolid, true, false},
		{"solid compatible", winfx.BackdropSolid, false, false},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			shell := &Shell{
				backdrop:        test.backdrop,
				perPixelOverlay: test.layered,
				cfg: config.Config{
					UI: config.UIConfig{
						LayeredForeground: test.layered,
					},
				},
			}
			if got := shell.usesGlassCompositor(); got != test.want {
				t.Fatalf("usesGlassCompositor() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestForegroundFromDualBackgroundRecoversPremultipliedAlpha(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		black       [4]byte
		white       [4]byte
		interactive bool
		want        [4]byte
	}{
		{
			name: "transparent", black: [4]byte{0, 0, 0, 255},
			white: [4]byte{255, 255, 255, 255}, want: [4]byte{0, 0, 0, 0},
		},
		{
			name: "interactive transparent", black: [4]byte{0, 0, 0, 255},
			white: [4]byte{255, 255, 255, 255}, interactive: true,
			want: [4]byte{0, 0, 0, 1},
		},
		{
			name: "half transparent red", black: [4]byte{128, 0, 0, 255},
			white: [4]byte{255, 127, 127, 255},
			want:  [4]byte{128, 0, 0, 128},
		},
		{
			name: "opaque black", black: [4]byte{0, 0, 0, 255},
			white: [4]byte{0, 0, 0, 255}, want: [4]byte{0, 0, 0, 255},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			black := image.NewRGBA(image.Rect(0, 0, 1, 1))
			white := image.NewRGBA(image.Rect(0, 0, 1, 1))
			copy(black.Pix, test.black[:])
			copy(white.Pix, test.white[:])
			got := foregroundFromDualBackground(
				black,
				white,
				test.interactive,
			)
			if got == nil {
				t.Fatal("foregroundFromDualBackground returned nil")
			}
			var gotPixel [4]byte
			copy(gotPixel[:], got.Pix)
			if gotPixel != test.want {
				t.Fatalf("pixel = %v, want %v", gotPixel, test.want)
			}
		})
	}
}

func TestForegroundFromDualBackgroundIntoReusesTargetExactly(t *testing.T) {
	bounds := image.Rect(0, 0, 37, 23)
	black := image.NewRGBA(bounds)
	white := image.NewRGBA(bounds)
	for offset := 0; offset < len(black.Pix); offset += 4 {
		alpha := byte((offset/4*29 + 17) % 256)
		black.Pix[offset+0] = byte(int(alpha) * 3 / 5)
		black.Pix[offset+1] = byte(int(alpha) * 2 / 5)
		black.Pix[offset+2] = byte(int(alpha) / 5)
		black.Pix[offset+3] = 255
		inverse := 255 - alpha
		white.Pix[offset+0] = black.Pix[offset+0] + inverse
		white.Pix[offset+1] = black.Pix[offset+1] + inverse
		white.Pix[offset+2] = black.Pix[offset+2] + inverse
		white.Pix[offset+3] = 255
	}
	want := foregroundFromDualBackground(black, white, true)
	target := image.NewRGBA(bounds)
	firstPixel := &target.Pix[0]
	got := foregroundFromDualBackgroundInto(target, black, white, true)
	if got != target {
		t.Fatal("in-place recovery returned a different image")
	}
	if firstPixel != &target.Pix[0] {
		t.Fatal("in-place recovery replaced the target pixel buffer")
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("in-place recovery differs from allocating recovery")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		foregroundFromDualBackgroundInto(target, black, white, true)
	}); allocations != 0 {
		t.Fatalf("in-place recovery allocations = %.1f, want 0", allocations)
	}
}

func TestConnectionOverlayWaitsForConfirmedDegradation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		snapshot model.Snapshot
		want     bool
	}{
		{
			name:     "zero snapshot is first connection",
			snapshot: model.Snapshot{State: model.StateUnknown},
		},
		{
			name: "websocket online",
			snapshot: model.Snapshot{
				WebSocketOnline: true,
				HTTPReachable:   true,
				LastError:       "stale error must not win",
			},
		},
		{
			name: "confirmed HTTP fallback",
			snapshot: model.Snapshot{
				HTTPReachable: true,
			},
			want: true,
		},
		{
			name: "reconnect attempt",
			snapshot: model.Snapshot{
				ReconnectAttempt: 1,
			},
			want: true,
		},
		{
			name: "connection error",
			snapshot: model.Snapshot{
				LastError: "dial failed",
			},
			want: true,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldDrawConnectionOverlay(test.snapshot); got != test.want {
				t.Fatalf(
					"shouldDrawConnectionOverlay(%+v) = %t, want %t",
					test.snapshot,
					got,
					test.want,
				)
			}
		})
	}
}

func rectangleInside(inner, outer walk.Rectangle) bool {
	return inner.Width > 0 && inner.Height > 0 &&
		inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.Width <= outer.X+outer.Width &&
		inner.Y+inner.Height <= outer.Y+outer.Height
}
