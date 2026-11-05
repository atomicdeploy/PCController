//go:build windows

package ui

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/design"
	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/winfx"
)

type actionID uint8

const (
	actionNone actionID = iota
	actionLamp
	actionToggle
	actionOn
	actionOff
	actionSettings
	actionDiagnostics
	actionReconnect
	actionTitleMinimize
	actionTitleZoom
	actionTitleClose
)

type surfaceLayout struct {
	lamp, toggle, on, off walk.Rectangle
	card                  walk.Rectangle
	settings, diagnostics walk.Rectangle
	reconnect             walk.Rectangle
	titleDrag             walk.Rectangle
	titleMinimize         walk.Rectangle
	titleZoom             walk.Rectangle
	titleClose            walk.Rectangle
}

type dashboardGeometry struct {
	title, settings, diagnostics walk.Rectangle
	card, lamp, lampHit, state   walk.Rectangle
	toggle, on, off, hint        walk.Rectangle
}

func calculateDashboardGeometry(bounds walk.Rectangle) dashboardGeometry {
	width, height := bounds.Width, bounds.Height
	originX, originY := bounds.X, bounds.Y
	margin := width / 16
	if margin < 16 {
		margin = 16
	}
	if margin > 28 {
		margin = 28
	}
	headerTop, headerHeight := 14, 38
	settingsWidth, infoWidth, headerGap := 44, 44, 8
	if width >= 520 {
		settingsWidth, infoWidth = 96, 72
	}
	right := originX + width - margin

	hintHeight := 24
	hint := walk.Rectangle{
		X: originX + margin, Y: originY + height - margin - hintHeight,
		Width: width - 2*margin, Height: hintHeight,
	}
	rowHeight, verticalGap := 42, 10
	rowY := hint.Y - verticalGap - rowHeight
	toggleHeight := 48
	toggleY := rowY - 12 - toggleHeight
	toggle := walk.Rectangle{
		X: originX + margin, Y: toggleY,
		Width: width - 2*margin, Height: toggleHeight,
	}
	buttonGap := 12
	buttonWidth := (toggle.Width - buttonGap) / 2
	on := walk.Rectangle{
		X: originX + margin, Y: rowY, Width: buttonWidth, Height: rowHeight,
	}
	off := walk.Rectangle{
		X: originX + margin + buttonWidth + buttonGap, Y: rowY,
		Width: buttonWidth, Height: rowHeight,
	}

	cardTop := originY + headerTop + headerHeight + 8
	cardBottom := toggle.Y - 12
	card := walk.Rectangle{
		X: originX + margin - 8, Y: cardTop,
		Width: width - 2*(margin-8), Height: cardBottom - cardTop,
	}
	stateHeight := 44
	state := walk.Rectangle{
		X: originX + margin, Y: cardBottom - stateHeight - 8,
		Width: width - 2*margin, Height: stateHeight,
	}
	lampAvailableHeight := state.Y - cardTop - 4
	lampSize := minInt(width-2*margin-8, lampAvailableHeight)
	if lampSize > 512 {
		lampSize = 512
	}
	if lampSize < 1 {
		lampSize = 1
	}
	lamp := walk.Rectangle{
		X:     originX + (width-lampSize)/2,
		Y:     cardTop + (lampAvailableHeight-lampSize)/2,
		Width: lampSize, Height: lampSize,
	}
	lampHit := walk.Rectangle{
		X: lamp.X + lampSize/8, Y: lamp.Y + lampSize/10,
		Width: lampSize * 3 / 4, Height: lampSize * 4 / 5,
	}

	return dashboardGeometry{
		title: walk.Rectangle{
			X: originX + margin, Y: originY + headerTop,
			Width:  width - 2*margin - settingsWidth - infoWidth - 2*headerGap,
			Height: headerHeight,
		},
		settings: walk.Rectangle{
			X: right - infoWidth - headerGap - settingsWidth,
			Y: originY + headerTop, Width: settingsWidth, Height: headerHeight,
		},
		diagnostics: walk.Rectangle{
			X: right - infoWidth, Y: originY + headerTop,
			Width: infoWidth, Height: headerHeight,
		},
		card: card, lamp: lamp, lampHit: lampHit, state: state,
		toggle: toggle, on: on, off: off, hint: hint,
	}
}

func mirrorRectangleHorizontally(
	rect walk.Rectangle,
	bounds walk.Rectangle,
) walk.Rectangle {
	rect.X = bounds.X + bounds.Width - (rect.X - bounds.X) - rect.Width
	return rect
}

func mirrorDashboardGeometry(
	geometry dashboardGeometry,
	bounds walk.Rectangle,
) dashboardGeometry {
	geometry.title = mirrorRectangleHorizontally(geometry.title, bounds)
	geometry.settings = mirrorRectangleHorizontally(geometry.settings, bounds)
	geometry.diagnostics = mirrorRectangleHorizontally(
		geometry.diagnostics,
		bounds,
	)
	geometry.card = mirrorRectangleHorizontally(geometry.card, bounds)
	geometry.lamp = mirrorRectangleHorizontally(geometry.lamp, bounds)
	geometry.lampHit = mirrorRectangleHorizontally(geometry.lampHit, bounds)
	geometry.state = mirrorRectangleHorizontally(geometry.state, bounds)
	geometry.toggle = mirrorRectangleHorizontally(geometry.toggle, bounds)
	geometry.on = mirrorRectangleHorizontally(geometry.on, bounds)
	geometry.off = mirrorRectangleHorizontally(geometry.off, bounds)
	geometry.hint = mirrorRectangleHorizontally(geometry.hint, bounds)
	return geometry
}

func (layout surfaceLayout) hit(x, y int) actionID {
	point := walk.Point{X: x, Y: y}
	for _, item := range []struct {
		action actionID
		rect   walk.Rectangle
	}{
		{actionToggle, layout.toggle},
		{actionOn, layout.on},
		{actionOff, layout.off},
		{actionSettings, layout.settings},
		{actionDiagnostics, layout.diagnostics},
		{actionReconnect, layout.reconnect},
		{actionTitleMinimize, layout.titleMinimize},
		{actionTitleZoom, layout.titleZoom},
		{actionTitleClose, layout.titleClose},
		{actionLamp, layout.lamp},
	} {
		if pointInRect(point, item.rect) {
			return item.action
		}
	}
	return actionNone
}

type palette struct {
	backgroundTop, backgroundBottom walk.Color
	panel, panelHover               walk.Color
	text, secondary                 walk.Color
	primaryText                     walk.Color
	accent, accentHover             walk.Color
	warm, success, warning, danger  walk.Color
	stroke                          walk.Color
}

func darkPalette() palette {
	tokens := design.Default.Colors.Dark
	return palette{
		backgroundTop: nativeWalkColor(tokens.Canvas), backgroundBottom: nativeWalkColor(tokens.CanvasAlt),
		panel: nativeWalkColor(tokens.Surface), panelHover: nativeWalkColor(tokens.SurfaceHigh),
		text: nativeWalkColor(tokens.Text), secondary: nativeWalkColor(tokens.TextMuted),
		primaryText: walk.RGB(4, 28, 40),
		accent:      nativeWalkColor(tokens.Accent), accentHover: nativeWalkColor(tokens.AccentHover),
		warm: nativeWalkColor(tokens.Warm), success: nativeWalkColor(tokens.Success),
		warning: nativeWalkColor(tokens.Warm), danger: nativeWalkColor(tokens.Danger),
		stroke: nativeWalkColor(tokens.Stroke),
	}
}

func lightPalette() palette {
	tokens := design.Default.Colors.Light
	return palette{
		backgroundTop: nativeWalkColor(tokens.CanvasAlt), backgroundBottom: nativeWalkColor(tokens.Canvas),
		panel: nativeWalkColor(tokens.Surface), panelHover: nativeWalkColor(tokens.SurfaceHigh),
		text: nativeWalkColor(tokens.Text), secondary: nativeWalkColor(tokens.TextMuted),
		primaryText: walk.RGB(4, 28, 40),
		accent:      nativeWalkColor(tokens.Accent), accentHover: nativeWalkColor(tokens.AccentHover),
		warm: nativeWalkColor(tokens.Warm), success: nativeWalkColor(tokens.Success),
		warning: nativeWalkColor(tokens.Warm), danger: nativeWalkColor(tokens.Danger),
		stroke: nativeWalkColor(tokens.Stroke),
	}
}

func highContrastPalette() palette {
	window := walk.Color(win.GetSysColor(win.COLOR_WINDOW))
	windowText := walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT))
	button := walk.Color(win.GetSysColor(win.COLOR_BTNFACE))
	highlight := walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHT))
	highlightText := walk.Color(win.GetSysColor(win.COLOR_HIGHLIGHTTEXT))
	hotlight := walk.Color(win.GetSysColor(win.COLOR_HOTLIGHT))
	if hotlight == 0 {
		hotlight = highlight
	}
	return palette{
		backgroundTop: window, backgroundBottom: window,
		panel: button, panelHover: button,
		text: windowText, secondary: walk.Color(win.GetSysColor(win.COLOR_GRAYTEXT)),
		primaryText: highlightText,
		accent:      highlight, accentHover: hotlight,
		warm: highlight, success: highlight,
		warning: highlight, danger: highlight,
		stroke: windowText,
	}
}

func nativeWalkColor(value string) walk.Color {
	color := design.MustColor(value)
	return walk.RGB(color.R, color.G, color.B)
}

func (s *Shell) paint(windowCanvas *walk.Canvas, _ walk.Rectangle) error {
	perPixel := s.usesPerPixelOverlay()
	directX := s.directXForegroundEnabled()
	offscreen := perPixel || directX
	bounds := s.surface.ClientBoundsPixels()
	if perPixel {
		// UpdateLayeredWindow sizes the top-level HWND, so render against that
		// window's exact outer size. During a native resize Walk's child
		// surface can briefly lag one layout pass behind the top-level rectangle.
		windowBounds := s.mw.BoundsPixels()
		bounds = walk.Rectangle{
			Width:  windowBounds.Width,
			Height: windowBounds.Height,
		}
	}
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return nil
	}
	if !offscreen {
		s.hitSurface = nil
		s.presentedSurface = nil
		s.glassFrame = nil
		s.disposeForegroundRecovery()
		return s.paintForeground(windowCanvas, bounds)
	}
	recovery, err := s.ensureForegroundRecovery(bounds)
	if err != nil {
		return err
	}
	s.surfaceTextRuns = s.surfaceTextRuns[:0]
	s.surfaceTextMode = surfaceTextCapture
	defer func() {
		s.surfaceTextMode = surfaceTextDirect
	}()
	black, err := s.renderForegroundPass(
		bounds,
		walk.RGB(0, 0, 0),
		recovery.blackBitmap,
		recovery.black,
	)
	if err != nil {
		return err
	}
	textRuns := s.surfaceTextRuns
	s.surfaceTextMode = surfaceTextSuppress
	white, err := s.renderForegroundPass(
		bounds,
		walk.RGB(255, 255, 255),
		recovery.whiteBitmap,
		recovery.white,
	)
	if err != nil {
		return err
	}
	// Acrylic/blur hosts need a one-alpha drag plane because Windows skips
	// WM_NCHITTEST for alpha zero. The explicit transparent material is the
	// opposite: preserve alpha zero so empty pixels really click through.
	foreground := foregroundFromDualBackgroundInto(
		recovery.recovered,
		black,
		white,
		perPixel && s.backdrop != winfx.BackdropTransparent,
	)
	if foreground == nil {
		return fmt.Errorf("ui: incompatible foreground recovery surfaces")
	}
	if s.usesGlassCompositor() {
		foreground = s.composeGlassControls(foreground)
	} else {
		s.glassFrame = nil
	}
	if err := s.compositeSurfaceTextRuns(foreground, textRuns); err != nil {
		return err
	}
	ui := s.Config().UI
	alphaShaped := s.backdrop == winfx.BackdropTransparent &&
		transparentFramelessWindow(ui)
	if alphaShaped {
		// Native layered hit testing only treats exact alpha zero as absent.
		// Use the same cutoff as WM_NCHITTEST before presenting the pixels.
		canonicalizeTransparentPixels(
			foreground,
			minimumInteractiveAlpha,
		)
	}
	if s.backdrop == winfx.BackdropTransparent {
		s.hitSurface = foreground
	} else {
		s.hitSurface = nil
	}
	s.presentedSurface = foreground
	if directX {
		// The composition visual sits above an ordinary GDI child. A native
		// caption/frame change can leave white pixels in its redirection bitmap.
		// Black is the transparent clear value in the extended DWM glass area;
		// clear only this app-owned surface before presenting the alpha visual.
		if !win.BitBlt(windowCanvas.HDC(), int32(bounds.X), int32(bounds.Y),
			int32(bounds.Width), int32(bounds.Height), 0, 0, 0, win.BLACKNESS) {
			return fmt.Errorf("ui: clear DirectComposition GDI backing")
		}
		if err := s.presentDirectXForeground(foreground); err == nil {
			if s.alphaRegionActive {
				if cornerErr := s.applyConfiguredWindowCorners(ui); cornerErr != nil {
					s.logf("restore window region: %v", cornerErr)
					return cornerErr
				}
			}
			return nil
		} else {
			s.logf("DirectX presentation: %v", err)
			s.handleDirectXPresentationFailure(err)
			perPixel = s.usesPerPixelOverlay()
			if !perPixel {
				s.hitSurface = nil
				clientBounds := s.surface.ClientBoundsPixels()
				if clientBounds.Width <= 0 || clientBounds.Height <= 0 {
					return nil
				}
				return s.paintForeground(windowCanvas, clientBounds)
			}
		}
	}
	if err := winfx.UpdatePerPixelOverlayWithOpacity(
		s.HWND(),
		foreground,
		s.windowFade.Opacity(),
	); err != nil {
		s.logf("UpdateLayeredWindow: %v", err)
		return err
	}
	if alphaShaped {
		if err := s.applyPresentedAlphaRegion(foreground); err != nil {
			s.logf("alpha window region: %v", err)
			return err
		}
	} else if s.alphaRegionActive {
		if err := s.applyConfiguredWindowCorners(ui); err != nil {
			s.logf("restore window region: %v", err)
			return err
		}
	}
	return nil
}

// renderForegroundPass draws the exact same non-text foreground over a known
// opaque background. Most GDI primitives do not write a usable alpha channel,
// so rendering once over black and once over white lets us recover their
// coverage. Text is deliberately captured and composited from a single glyph
// mask after this recovery; GDI font smoothing is not linear across the two
// backgrounds.
func (s *Shell) renderForegroundPass(
	bounds walk.Rectangle,
	background walk.Color,
	bitmap *walk.Bitmap,
	target *image.RGBA,
) (*image.RGBA, error) {
	if bitmap == nil || target == nil ||
		target.Bounds() != image.Rect(0, 0, bounds.Width, bounds.Height) {
		return nil, fmt.Errorf("ui: invalid reusable foreground pass")
	}

	canvas, err := walk.NewCanvasFromImage(bitmap)
	if err != nil {
		return nil, err
	}
	brush, err := walk.NewSolidColorBrush(background)
	if err != nil {
		canvas.Dispose()
		return nil, err
	}
	if err := canvas.FillRectanglePixels(brush, bounds); err != nil {
		brush.Dispose()
		canvas.Dispose()
		return nil, err
	}
	brush.Dispose()
	if err := s.paintForeground(canvas, bounds); err != nil {
		canvas.Dispose()
		return nil, err
	}
	canvas.Dispose()
	win.GdiFlush()
	if err := bitmap.ToImageInto(target); err != nil {
		return nil, err
	}
	return target, nil
}

func foregroundFromDualBackground(
	black *image.RGBA,
	white *image.RGBA,
	interactive bool,
) *image.RGBA {
	if black == nil || white == nil || black.Bounds() != white.Bounds() {
		return nil
	}
	result := image.NewRGBA(black.Bounds())
	return foregroundFromDualBackgroundInto(
		result,
		black,
		white,
		interactive,
	)
}

func foregroundFromDualBackgroundInto(
	result *image.RGBA,
	black *image.RGBA,
	white *image.RGBA,
	interactive bool,
) *image.RGBA {
	if result == nil || black == nil || white == nil ||
		black.Bounds() != white.Bounds() ||
		result.Bounds() != black.Bounds() {
		return nil
	}
	for y := black.Bounds().Min.Y; y < black.Bounds().Max.Y; y++ {
		for x := black.Bounds().Min.X; x < black.Bounds().Max.X; x++ {
			blackOffset := black.PixOffset(x, y)
			whiteOffset := white.PixOffset(x, y)
			resultOffset := result.PixOffset(x, y)

			delta := 0
			for channel := 0; channel < 3; channel++ {
				difference := int(white.Pix[whiteOffset+channel]) -
					int(black.Pix[blackOffset+channel])
				if difference < 0 {
					difference = 0
				} else if difference > 255 {
					difference = 255
				}
				delta += difference
			}
			alpha := 255 - (delta+1)/3
			for channel := 0; channel < 3; channel++ {
				value := int(black.Pix[blackOffset+channel])
				if value > alpha {
					alpha = value
				}
				result.Pix[resultOffset+channel] = byte(value)
			}
			if alpha == 0 && interactive {
				// Fully transparent layered pixels are skipped before Windows
				// dispatches WM_NCHITTEST. Alpha 1 is visually negligible and
				// keeps the native move/resize/context-menu plane reachable.
				alpha = 1
			}
			result.Pix[resultOffset+3] = byte(alpha)
		}
	}
	return result
}

func (s *Shell) composeGlassControls(foreground *image.RGBA) *image.RGBA {
	if foreground == nil {
		return nil
	}
	if s.glassFrame == nil || s.glassFrame.Bounds() != foreground.Bounds() {
		s.glassFrame = image.NewRGBA(foreground.Bounds())
	} else {
		clear(s.glassFrame.Pix)
	}
	glass := s.glassFrame
	type controlLayer struct {
		rect    walk.Rectangle
		radius  float64
		style   glassStyle
		pending bool
	}
	controls := make([]controlLayer, 0, 7)
	if s.layout.titleDrag.Width > 0 && s.layout.titleDrag.Height > 0 {
		drawGlassRoundedBody(
			glass,
			s.layout.titleDrag,
			12,
			glassStyle{
				Dark: s.dark, Panel: true,
				Alpha: 22, BorderAlpha: 38,
			},
		)
	}
	if s.Config().UI.ViewMode == config.ViewDashboard {
		drawGlassRoundedBody(
			glass,
			s.layout.card,
			20,
			glassStyle{
				Dark: s.dark, Panel: true,
				Alpha: 30, BorderAlpha: 56,
			},
		)
		for _, control := range []struct {
			rect    walk.Rectangle
			action  actionID
			primary bool
			pending bool
		}{
			{s.layout.settings, actionSettings, false, false},
			{s.layout.diagnostics, actionDiagnostics, false, false},
			{s.layout.toggle, actionToggle, true, s.snapshot.PendingCommand != ""},
			{s.layout.on, actionOn, false, false},
			{s.layout.off, actionOff, false, false},
		} {
			if control.rect.Width <= 0 || control.rect.Height <= 0 {
				continue
			}
			controls = append(
				controls,
				controlLayer{
					rect: control.rect, radius: 14,
					pending: control.pending,
					style: glassStyle{
						Dark:        s.dark,
						Primary:     control.primary,
						Hover:       s.hoverProgress(control.action),
						Pressed:     s.pressProgress(control.action),
						Pending:     control.pending,
						Phase:       s.visualPhase,
						RevealX:     float64(s.visualPointer.X),
						RevealY:     float64(s.visualPointer.Y),
						Elevation:   5.5,
						Alpha:       48,
						BorderAlpha: 76,
					},
				},
			)
		}
	}

	// Elevation belongs behind every control. Shadows are rendered first and
	// excluded from the union of sibling silhouettes, so a translucent body
	// reveals the card material beneath it rather than a neighbor's dark tail.
	masks := make([]glassRoundedMask, 0, len(controls))
	for _, control := range controls {
		masks = append(masks, glassRoundedMask{
			Rect: control.rect, Radius: control.radius,
		})
	}
	for _, control := range controls {
		drawGlassRoundedShadowMasked(
			glass,
			control.rect,
			control.radius,
			control.style,
			masks,
		)
	}
	for _, control := range controls {
		drawGlassRoundedBody(
			glass,
			control.rect,
			control.radius,
			control.style,
		)
		if control.pending {
			drawFluentSpinner(
				glass,
				walk.Point{
					X: control.rect.X + 28,
					Y: control.rect.Y + control.rect.Height/2,
				},
				10,
				s.visualPhase,
				s.dark,
			)
		}
	}
	s.drawTrafficLightsRGBA(glass)
	draw.Draw(glass, glass.Bounds(), foreground, foreground.Bounds().Min, draw.Over)
	return glass
}

type glassStyle struct {
	Dark        bool
	Primary     bool
	Panel       bool
	Hover       float64
	Pressed     float64
	Pending     bool
	Phase       float64
	RevealX     float64
	RevealY     float64
	Elevation   float64
	Alpha       float64
	BorderAlpha float64
}

type glassRoundedMask struct {
	Rect   walk.Rectangle
	Radius float64
}

func drawGlassRoundedRect(
	target *image.RGBA,
	rect walk.Rectangle,
	radius float64,
	style glassStyle,
) {
	drawGlassRoundedRectLayers(target, rect, radius, style, true, true, nil)
}

func drawGlassRoundedShadow(
	target *image.RGBA,
	rect walk.Rectangle,
	radius float64,
	style glassStyle,
) {
	drawGlassRoundedRectLayers(target, rect, radius, style, true, false, nil)
}

func drawGlassRoundedShadowMasked(
	target *image.RGBA,
	rect walk.Rectangle,
	radius float64,
	style glassStyle,
	masks []glassRoundedMask,
) {
	drawGlassRoundedRectLayers(
		target,
		rect,
		radius,
		style,
		true,
		false,
		masks,
	)
}

func drawGlassRoundedBody(
	target *image.RGBA,
	rect walk.Rectangle,
	radius float64,
	style glassStyle,
) {
	drawGlassRoundedRectLayers(target, rect, radius, style, false, true, nil)
}

func drawGlassRoundedRectLayers(
	target *image.RGBA,
	rect walk.Rectangle,
	radius float64,
	style glassStyle,
	drawShadow bool,
	drawBody bool,
	shadowMasks []glassRoundedMask,
) {
	if target == nil || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	bounds := target.Bounds()
	pressed := clampFloat(style.Pressed, 0, 1)
	shadowOffset := 1.5 + style.Elevation*0.32*(1-pressed)
	shadowSigma := 3.5 + style.Elevation*0.72
	shadowSpread := 0
	if drawShadow {
		shadowSpread = glassShadowExtent(style)
	}
	left := maxInt(rect.X-shadowSpread, bounds.Min.X)
	top := maxInt(rect.Y-shadowSpread, bounds.Min.Y)
	right := minInt(rect.X+rect.Width+shadowSpread, bounds.Max.X)
	bottom := minInt(rect.Y+rect.Height+shadowSpread, bounds.Max.Y)
	if left >= right || top >= bottom {
		return
	}

	// A Fluent button is a small material plate, not a tinted outline.  The
	// neutral control tint is deliberately brighter than the backdrop in dark
	// mode so it keeps its own depth over every live DWM material.  Panels keep
	// the quieter, backdrop-colored tint because they are containers rather
	// than interactive surfaces.
	fillR, fillG, fillB := 248.0, 252.0, 254.0
	borderR, borderG, borderB := 255.0, 255.0, 255.0
	if style.Dark {
		fillR, fillG, fillB = 104, 127, 139
		borderR, borderG, borderB = 226, 245, 251
	}
	if style.Panel {
		fillR, fillG, fillB = 238, 249, 253
		borderR, borderG, borderB = 244, 252, 255
		if style.Dark {
			fillR, fillG, fillB = 18, 40, 54
			borderR, borderG, borderB = 178, 226, 244
		}
	}
	if style.Primary {
		fillR, fillG, fillB = 8, 151, 207
		borderR, borderG, borderB = 182, 243, 255
	}
	hover := clampFloat(style.Hover, 0, 1)
	accentR, accentG, accentB := 64.0, 204.0, 248.0
	fillR += (accentR - fillR) * hover * 0.18
	fillG += (accentG - fillG) * hover * 0.18
	fillB += (accentB - fillB) * hover * 0.18
	fillAlpha := style.Alpha + 18*hover - 10*pressed
	if style.Primary {
		// The accent action is a denser color plate, matching WinUI's filled
		// accent button instead of reading as another transparent outline.
		fillAlpha += 52
	}
	borderAlpha := style.BorderAlpha + 44*hover - 10*pressed

	cx := float64(rect.X) + float64(rect.Width)/2
	cy := float64(rect.Y) + float64(rect.Height)/2
	halfWidth := float64(rect.Width) / 2
	halfHeight := float64(rect.Height) / 2
	radius = math.Min(radius, math.Min(halfWidth, halfHeight))
	revealRadius := clampFloat(float64(rect.Height)*2.35, 56, 112)
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			distance := roundedRectDistance(
				px,
				py,
				cx,
				cy,
				halfWidth,
				halfHeight,
				radius,
			)

			if drawShadow && style.Elevation > 0 && !style.Panel {
				shadowDistance := roundedRectDistance(
					px,
					py-shadowOffset,
					cx,
					cy,
					halfWidth,
					halfHeight,
					radius,
				)
				outsideBody := clampFloat(distance-0.25, 0, 1)
				if outsideBody > 0 &&
					!pointInsideGlassMasks(px, py, shadowMasks) {
					falloff := math.Exp(
						-(math.Max(shadowDistance, 0) *
							math.Max(shadowDistance, 0)) /
							(2 * shadowSigma * shadowSigma),
					)
					shadowAlpha := (27 + 22*hover) *
						(1 - pressed*0.72) * outsideBody * falloff
					compositeStraightPixel(
						target,
						x,
						y,
						2,
						10,
						17,
						shadowAlpha,
					)
				}
			}
			if !drawBody {
				continue
			}

			outer := clampFloat(0.5-distance, 0, 1)
			inner := clampFloat(0.5-(distance+1.25), 0, 1)
			if outer <= 0 {
				continue
			}

			vertical := clampFloat(
				(py-float64(rect.Y))/math.Max(float64(rect.Height), 1),
				0,
				1,
			)
			horizontal := clampFloat(
				(px-float64(rect.X))/math.Max(float64(rect.Width), 1),
				0,
				1,
			)
			reveal := 0.0
			if hover > 0 {
				revealDistance := math.Hypot(
					px-style.RevealX,
					py-style.RevealY,
				)
				reveal = clampFloat(1-revealDistance/revealRadius, 0, 1)
				reveal *= reveal * hover
			}

			localAlpha := fillAlpha
			localR, localG, localB := fillR, fillG, fillB
			// A subtle vertical material gradient and deterministic micro-grain
			// keep the control from reading as one flat translucent rectangle.
			topLight := (1 - vertical) * (0.095 + 0.035*hover)
			bottomShade := vertical * (0.055 + 0.025*pressed)
			localR = (localR + (255-localR)*topLight) * (1 - bottomShade)
			localG = (localG + (255-localG)*topLight) * (1 - bottomShade)
			localB = (localB + (255-localB)*topLight) * (1 - bottomShade)
			// Pointer reveal is strongest on the rim.  The fill receives only
			// a restrained bloom, avoiding a neon/halo look on wide controls.
			localR += (accentR - localR) * reveal * 0.16
			localG += (accentG - localG) * reveal * 0.16
			localB += (accentB - localB) * reveal * 0.16
			localAlpha += reveal * 20
			localAlpha += (glassNoise(x, y) - 0.5) * 5.5
			if style.Pending {
				stripe := math.Mod(
					float64(x+y)+style.Phase*54,
					18,
				)
				if stripe < 7 {
					localAlpha += 32
					localR += (255 - localR) * 0.16
					localG += (255 - localG) * 0.16
					localB += (255 - localB) * 0.16
				}
			}
			compositeStraightPixel(
				target,
				x,
				y,
				localR,
				localG,
				localB,
				localAlpha*inner,
			)

			// A separate translucent veneer creates the layered material read:
			// diffuse light across the upper face, a soft off-axis reflection,
			// and slightly denser glass at the foot.  These layers stay inside
			// the silhouette so they cannot create fringe or outer halos.
			upperVeneer := inner *
				clampFloat(1-vertical*1.55, 0, 1) *
				(1 - pressed*0.72)
			if upperVeneer > 0 && !style.Panel {
				compositeStraightPixel(
					target,
					x,
					y,
					255,
					255,
					255,
					(13+9*hover)*upperVeneer,
				)
			}
			reflectionX := (horizontal - 0.28) / 0.34
			reflectionY := (vertical - 0.12) / 0.72
			reflection := inner *
				math.Exp(-(reflectionX*reflectionX+reflectionY*reflectionY)*1.4) *
				(1 - pressed*0.8)
			if reflection > 0.01 && !style.Panel {
				compositeStraightPixel(
					target,
					x,
					y,
					225,
					248,
					255,
					(8+7*hover)*reflection,
				)
			}
			lowerDensity := inner *
				clampFloat((vertical-0.52)*2.08, 0, 1)
			if lowerDensity > 0 && !style.Panel {
				compositeStraightPixel(
					target,
					x,
					y,
					2,
					15,
					24,
					(7+14*pressed)*lowerDensity,
				)
			}

			// The inside bevel is intentionally directional: the top/left
			// catches light, while the lower edge supplies just enough depth
			// to resemble Windows 11's raised acrylic controls.
			innerEdge := clampFloat(0.62-math.Abs(distance+2.15), 0, 1)
			specularDirection := clampFloat(
				1.15-vertical*1.15-horizontal*0.18,
				0,
				1,
			)
			if innerEdge > 0 && specularDirection > 0 {
				compositeStraightPixel(
					target,
					x,
					y,
					255,
					255,
					255,
					(34+38*hover)*
						innerEdge*
						specularDirection*
						(1-pressed*0.78),
				)
			}
			lowerEdge := innerEdge * clampFloat((vertical-0.52)*2.1, 0, 1)
			if lowerEdge > 0 {
				compositeStraightPixel(
					target,
					x,
					y,
					2,
					15,
					24,
					(30+12*pressed)*lowerEdge*(1-0.45*hover),
				)
			}

			borderCoverage := outer - inner
			if borderCoverage > 0 {
				compositeStraightPixel(
					target,
					x,
					y,
					borderR,
					borderG,
					borderB,
					borderAlpha*borderCoverage,
				)
				topSpecular := clampFloat(1.12-vertical*1.75, 0, 1)
				if topSpecular > 0 {
					compositeStraightPixel(
						target,
						x,
						y,
						255,
						255,
						255,
						(46+48*hover)*
							borderCoverage*
							topSpecular*
							(1-pressed*0.72),
					)
				}
				bottomRim := clampFloat((vertical-0.58)*2.38, 0, 1)
				if bottomRim > 0 {
					compositeStraightPixel(
						target,
						x,
						y,
						2,
						14,
						22,
						(34+16*pressed)*
							borderCoverage*
							bottomRim,
					)
				}
				if reveal > 0 {
					compositeStraightPixel(
						target,
						x,
						y,
						166,
						241,
						255,
						112*reveal*borderCoverage,
					)
				}
			}
		}
	}
}

func pointInsideGlassMasks(
	x float64,
	y float64,
	masks []glassRoundedMask,
) bool {
	for _, mask := range masks {
		rect := mask.Rect
		if rect.Width <= 0 || rect.Height <= 0 ||
			x < float64(rect.X)-0.5 ||
			x > float64(rect.X+rect.Width)+0.5 ||
			y < float64(rect.Y)-0.5 ||
			y > float64(rect.Y+rect.Height)+0.5 {
			continue
		}
		halfWidth := float64(rect.Width) / 2
		halfHeight := float64(rect.Height) / 2
		radius := math.Min(
			mask.Radius,
			math.Min(halfWidth, halfHeight),
		)
		if roundedRectDistance(
			x,
			y,
			float64(rect.X)+halfWidth,
			float64(rect.Y)+halfHeight,
			halfWidth,
			halfHeight,
			radius,
		) <= 0.5 {
			return true
		}
	}
	return false
}

func glassShadowExtent(style glassStyle) int {
	if style.Elevation <= 0 || style.Panel {
		return 0
	}
	pressed := clampFloat(style.Pressed, 0, 1)
	offset := 1.5 + style.Elevation*0.32*(1-pressed)
	sigma := 3.5 + style.Elevation*0.72
	return int(math.Ceil(offset + 3*sigma + 1))
}

func roundedRectDistance(
	x float64,
	y float64,
	centerX float64,
	centerY float64,
	halfWidth float64,
	halfHeight float64,
	radius float64,
) float64 {
	qx := math.Abs(x-centerX) - (halfWidth - radius)
	qy := math.Abs(y-centerY) - (halfHeight - radius)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) +
		math.Min(math.Max(qx, qy), 0) - radius
}

func glassNoise(x, y int) float64 {
	value := uint32(x)*0x9e3779b1 ^ uint32(y)*0x85ebca77
	value ^= value >> 16
	value *= 0x7feb352d
	value ^= value >> 15
	return float64(value&0xff) / 255
}

func drawFluentSpinner(
	target *image.RGBA,
	center walk.Point,
	radius float64,
	phase float64,
	dark bool,
) {
	if target == nil {
		return
	}
	const dots = 10
	head := int(math.Floor(phase * dots))
	for index := 0; index < dots; index++ {
		angle := float64(index)*2*math.Pi/dots - math.Pi/2
		x := float64(center.X) + math.Cos(angle)*radius
		y := float64(center.Y) + math.Sin(angle)*radius
		distance := (head - index + dots) % dots
		alpha := 220.0 - float64(distance)*15
		if alpha < 70 {
			alpha = 70
		}
		r, g, b := 8.0, 54.0, 72.0
		if dark {
			r, g, b = 224, 248, 255
		}
		drawAntialiasedCircle(target, x, y, 2.1, r, g, b, alpha)
	}
}

func (s *Shell) drawTrafficLightsRGBA(target *image.RGBA) {
	if target == nil {
		return
	}
	for _, control := range []struct {
		rect             walk.Rectangle
		action           actionID
		red, green, blue float64
	}{
		{s.layout.titleClose, actionTitleClose, 255, 95, 86},
		{s.layout.titleMinimize, actionTitleMinimize, 255, 189, 46},
		{s.layout.titleZoom, actionTitleZoom, 39, 201, 63},
	} {
		if control.rect.Width <= 0 || control.rect.Height <= 0 {
			continue
		}
		hover := s.hoverProgress(control.action)
		pressed := s.pressProgress(control.action)
		centerX := float64(control.rect.X) + float64(control.rect.Width)/2
		centerY := float64(control.rect.Y) +
			float64(control.rect.Height)/2 +
			pressed*0.8
		radius := 6.2 + hover*0.45 - pressed*0.35
		drawAntialiasedCircle(
			target,
			centerX,
			centerY+0.7,
			radius+1.1,
			12,
			18,
			22,
			95,
		)
		drawAntialiasedCircle(
			target,
			centerX,
			centerY,
			radius,
			control.red,
			control.green,
			control.blue,
			255,
		)
		drawAntialiasedCircle(
			target,
			centerX-1.7,
			centerY-2.1,
			2.15,
			255,
			255,
			255,
			38+hover*34,
		)
		glyphAlpha := hover * 205
		if glyphAlpha <= 1 {
			continue
		}
		switch control.action {
		case actionTitleClose:
			drawAntialiasedLine(
				target,
				centerX-2.1, centerY-2.1,
				centerX+2.1, centerY+2.1,
				1.15,
				80, 42, 38, glyphAlpha,
			)
			drawAntialiasedLine(
				target,
				centerX+2.1, centerY-2.1,
				centerX-2.1, centerY+2.1,
				1.15,
				80, 42, 38, glyphAlpha,
			)
		case actionTitleMinimize:
			drawAntialiasedHorizontalLine(
				target,
				centerX-2.8,
				centerX+2.8,
				centerY,
				1.15,
				92, 65, 24, glyphAlpha,
			)
		case actionTitleZoom:
			drawAntialiasedLine(
				target,
				centerX-2.1, centerY+2.1,
				centerX+2.1, centerY-2.1,
				1.15,
				29, 82, 38, glyphAlpha,
			)
		}
	}
}

func drawAntialiasedCircle(
	target *image.RGBA,
	centerX float64,
	centerY float64,
	radius float64,
	r float64,
	g float64,
	b float64,
	alpha float64,
) {
	left := int(math.Floor(centerX - radius - 1))
	top := int(math.Floor(centerY - radius - 1))
	right := int(math.Ceil(centerX + radius + 1))
	bottom := int(math.Ceil(centerY + radius + 1))
	for y := top; y <= bottom; y++ {
		for x := left; x <= right; x++ {
			distance := math.Hypot(float64(x)+0.5-centerX, float64(y)+0.5-centerY)
			coverage := clampFloat(radius+0.5-distance, 0, 1)
			if coverage > 0 {
				compositeStraightPixel(
					target,
					x,
					y,
					r,
					g,
					b,
					alpha*coverage,
				)
			}
		}
	}
}

func drawAntialiasedLine(
	target *image.RGBA,
	startX float64,
	startY float64,
	endX float64,
	endY float64,
	thickness float64,
	r float64,
	g float64,
	b float64,
	alpha float64,
) {
	if target == nil || thickness <= 0 {
		return
	}
	minX := int(math.Floor(math.Min(startX, endX) - thickness - 1))
	maxX := int(math.Ceil(math.Max(startX, endX) + thickness + 1))
	minY := int(math.Floor(math.Min(startY, endY) - thickness - 1))
	maxY := int(math.Ceil(math.Max(startY, endY) + thickness + 1))
	dx, dy := endX-startX, endY-startY
	lengthSquared := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			pointX, pointY := float64(x)+0.5, float64(y)+0.5
			fraction := 0.0
			if lengthSquared > 0 {
				fraction = ((pointX-startX)*dx + (pointY-startY)*dy) /
					lengthSquared
				fraction = clampFloat(fraction, 0, 1)
			}
			nearestX := startX + fraction*dx
			nearestY := startY + fraction*dy
			distance := math.Hypot(pointX-nearestX, pointY-nearestY)
			coverage := clampFloat(thickness/2+0.5-distance, 0, 1)
			if coverage > 0 {
				compositeStraightPixel(
					target,
					x,
					y,
					r,
					g,
					b,
					alpha*coverage,
				)
			}
		}
	}
}

func drawAntialiasedHorizontalLine(
	target *image.RGBA,
	left float64,
	right float64,
	centerY float64,
	thickness float64,
	r float64,
	g float64,
	b float64,
	alpha float64,
) {
	if target == nil || right <= left || thickness <= 0 {
		return
	}
	minX := int(math.Floor(left - 1))
	maxX := int(math.Ceil(right + 1))
	minY := int(math.Floor(centerY - thickness/2 - 1))
	maxY := int(math.Ceil(centerY + thickness/2 + 1))
	for y := minY; y <= maxY; y++ {
		verticalDistance := math.Abs(float64(y)+0.5-centerY) - thickness/2
		verticalCoverage := clampFloat(0.5-verticalDistance, 0, 1)
		if verticalCoverage <= 0 {
			continue
		}
		for x := minX; x <= maxX; x++ {
			horizontalCoverage := math.Min(
				clampFloat(float64(x)+1-left, 0, 1),
				clampFloat(right-float64(x), 0, 1),
			)
			coverage := verticalCoverage * horizontalCoverage
			if coverage > 0 {
				compositeStraightPixel(
					target, x, y, r, g, b, alpha*coverage,
				)
			}
		}
	}
}

func compositeStraightPixel(
	target *image.RGBA,
	x int,
	y int,
	r float64,
	g float64,
	b float64,
	alpha float64,
) {
	if target == nil || !image.Pt(x, y).In(target.Bounds()) {
		return
	}
	alpha = clampFloat(alpha, 0, 255)
	sourceAlpha := alpha / 255
	offset := target.PixOffset(x, y)
	inverse := 1 - sourceAlpha
	target.Pix[offset+0] = byte(clampFloat(
		r*sourceAlpha+float64(target.Pix[offset+0])*inverse,
		0,
		255,
	))
	target.Pix[offset+1] = byte(clampFloat(
		g*sourceAlpha+float64(target.Pix[offset+1])*inverse,
		0,
		255,
	))
	target.Pix[offset+2] = byte(clampFloat(
		b*sourceAlpha+float64(target.Pix[offset+2])*inverse,
		0,
		255,
	))
	target.Pix[offset+3] = byte(clampFloat(
		alpha+float64(target.Pix[offset+3])*inverse,
		0,
		255,
	))
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (s *Shell) paintForeground(
	canvas *walk.Canvas,
	bounds walk.Rectangle,
) error {
	colors := lightPalette()
	if s.highContrast {
		colors = highContrastPalette()
	} else if s.dark {
		colors = darkPalette()
	}

	if s.backdrop == winfx.BackdropSolid {
		if err := canvas.GradientFillRectanglePixels(
			colors.backgroundTop, colors.backgroundBottom, walk.Vertical, bounds,
		); err != nil {
			return err
		}
	}

	s.layout = surfaceLayout{}
	content := bounds
	if effectiveTitleBarMode(s.Config().UI) == config.TitleBarCustom &&
		bounds.Height >= 96 {
		titleHeight := 38
		s.paintTitleBar(canvas, bounds, titleHeight, colors)
		content.Y += titleHeight
		content.Height -= titleHeight
	}
	if s.Config().UI.ViewMode == config.ViewDashboard {
		return s.paintDashboard(canvas, content, colors)
	}
	return s.paintCompact(canvas, content, colors)
}

func (s *Shell) paintTitleBar(
	canvas *walk.Canvas,
	bounds walk.Rectangle,
	height int,
	colors palette,
) {
	const controlWidth = 24
	s.layout.titleDrag = walk.Rectangle{
		X: bounds.X + 4, Y: bounds.Y + 3,
		Width: bounds.Width - 8, Height: height - 6,
	}
	s.layout.titleClose = walk.Rectangle{
		X: bounds.X + 7, Y: bounds.Y,
		Width: controlWidth, Height: height,
	}
	s.layout.titleMinimize = walk.Rectangle{
		X: s.layout.titleClose.X + controlWidth,
		Y: bounds.Y, Width: controlWidth, Height: height,
	}
	s.layout.titleZoom = walk.Rectangle{
		X: s.layout.titleMinimize.X + controlWidth,
		Y: bounds.Y, Width: controlWidth, Height: height,
	}
	titleRect := walk.Rectangle{
		X:      s.layout.titleZoom.X + s.layout.titleZoom.Width + 9,
		Y:      bounds.Y,
		Width:  bounds.X + bounds.Width - (s.layout.titleZoom.X + s.layout.titleZoom.Width + 18),
		Height: height,
	}
	if titleRect.Width > 0 {
		format := walk.TextLeft | walk.TextVCenter |
			walk.TextSingleLine | walk.TextNoPrefix
		if s.isRTL() {
			format = walk.TextRight | walk.TextRTLReading |
				walk.TextVCenter | walk.TextSingleLine | walk.TextNoPrefix
		}
		_ = s.drawSurfaceText(
			canvas,
			s.tr(i18n.MsgAppName),
			s.bodyFont,
			colors.text,
			titleRect,
			format,
		)
	}
	if !s.usesGlassCompositor() {
		s.paintTrafficLightsGDI(canvas)
	}
}

func (s *Shell) paintTrafficLightsGDI(canvas *walk.Canvas) {
	if canvas == nil {
		return
	}
	for _, control := range []struct {
		rect   walk.Rectangle
		action actionID
		color  walk.Color
	}{
		{s.layout.titleClose, actionTitleClose, walk.RGB(255, 95, 86)},
		{s.layout.titleMinimize, actionTitleMinimize, walk.RGB(255, 189, 46)},
		{s.layout.titleZoom, actionTitleZoom, walk.RGB(39, 201, 63)},
	} {
		if s.highContrast {
			control.color = walk.Color(win.GetSysColor(win.COLOR_BTNFACE))
		}
		hover := s.hoverProgress(control.action)
		pressed := s.pressProgress(control.action)
		size := 13 + int(math.Round(hover-pressed))
		circle := walk.Rectangle{
			X:     control.rect.X + (control.rect.Width-size)/2,
			Y:     control.rect.Y + (control.rect.Height-size)/2,
			Width: size, Height: size,
		}
		brush, _ := walk.NewSolidColorBrush(control.color)
		if brush != nil {
			_ = canvas.FillEllipsePixels(brush, circle)
			brush.Dispose()
		}
		borderColor := blend(
			control.color,
			walk.RGB(40, 35, 35),
			0.34,
		)
		if s.highContrast {
			borderColor = walk.Color(win.GetSysColor(win.COLOR_WINDOWTEXT))
		}
		border, _ := walk.NewCosmeticPen(walk.PenSolid, borderColor)
		if border != nil {
			_ = canvas.DrawEllipsePixels(border, circle)
			border.Dispose()
		}
		if s.highContrast || hover > 0.08 {
			s.paintTrafficGlyphGDI(
				canvas,
				control.action,
				circle,
				s.highContrast,
			)
		}
	}
}

func (s *Shell) paintTrafficGlyphGDI(
	canvas *walk.Canvas,
	action actionID,
	circle walk.Rectangle,
	highContrast bool,
) {
	color := walk.RGB(75, 55, 45)
	if highContrast {
		color = walk.Color(win.GetSysColor(win.COLOR_BTNTEXT))
	}
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, color)
	if pen == nil {
		return
	}
	defer pen.Dispose()
	centerX := circle.X + circle.Width/2
	centerY := circle.Y + circle.Height/2
	switch action {
	case actionTitleClose:
		_ = canvas.DrawLinePixels(
			pen,
			walk.Point{X: centerX - 2, Y: centerY - 2},
			walk.Point{X: centerX + 2, Y: centerY + 2},
		)
		_ = canvas.DrawLinePixels(
			pen,
			walk.Point{X: centerX + 2, Y: centerY - 2},
			walk.Point{X: centerX - 2, Y: centerY + 2},
		)
	case actionTitleMinimize:
		_ = canvas.DrawLinePixels(
			pen,
			walk.Point{X: centerX - 3, Y: centerY},
			walk.Point{X: centerX + 3, Y: centerY},
		)
	case actionTitleZoom:
		_ = canvas.DrawLinePixels(
			pen,
			walk.Point{X: centerX - 2, Y: centerY + 2},
			walk.Point{X: centerX + 2, Y: centerY - 2},
		)
	}
}

func (s *Shell) paintCompact(
	canvas *walk.Canvas,
	bounds walk.Rectangle,
	colors palette,
) error {
	lampSize := minInt(bounds.Width-4, bounds.Height-4)
	lampRect := walk.Rectangle{
		X:     bounds.X + (bounds.Width-lampSize)/2,
		Y:     bounds.Y + (bounds.Height-lampSize)/2,
		Width: lampSize, Height: lampSize,
	}
	s.layout.lamp = lampRect
	s.layout.toggle = walk.Rectangle{}
	s.layout.on = walk.Rectangle{}
	s.layout.off = walk.Rectangle{}
	s.layout.settings = walk.Rectangle{}
	s.layout.diagnostics = walk.Rectangle{}
	s.layout.reconnect = walk.Rectangle{}

	if err := s.drawLamp(canvas, lampRect); err != nil {
		return err
	}
	badge := s.drawConnectionOverlay(canvas, lampRect, colors)
	if badge.Width > 0 {
		s.layout.reconnect = badge
	}
	return nil
}

func (s *Shell) paintDashboard(
	canvas *walk.Canvas,
	bounds walk.Rectangle,
	colors palette,
) error {
	geometry := calculateDashboardGeometry(bounds)
	if s.isRTL() {
		geometry = mirrorDashboardGeometry(geometry, bounds)
	}

	titleFormat := walk.TextLeft | walk.TextVCenter |
		walk.TextSingleLine | walk.TextNoPrefix
	if s.isRTL() {
		titleFormat = walk.TextRight | walk.TextRTLReading |
			walk.TextVCenter | walk.TextSingleLine | walk.TextNoPrefix
	}
	_ = s.drawSurfaceText(
		canvas,
		s.trHeadline(i18n.MsgAppName),
		s.titleFont,
		colors.text,
		geometry.title,
		titleFormat,
	)
	s.layout.settings = geometry.settings
	s.layout.diagnostics = geometry.diagnostics
	s.layout.card = geometry.card
	s.drawIconButton(
		canvas,
		s.layout.settings,
		artworkSettings,
		s.tr(i18n.MsgActionSettings),
		actionSettings,
		colors,
	)
	s.drawIconButton(
		canvas,
		s.layout.diagnostics,
		artworkDiagnostics,
		s.tr(i18n.MsgMainInfo),
		actionDiagnostics,
		colors,
	)

	if !s.usesGlassCompositor() {
		cardPen, _ := walk.NewCosmeticPen(
			walk.PenSolid, blend(colors.stroke, colors.accent, 0.22),
		)
		if cardPen != nil {
			defer cardPen.Dispose()
			_ = canvas.DrawRoundedRectanglePixels(
				cardPen, geometry.card, walk.Size{Width: 20, Height: 20},
			)
		}
	}

	s.layout.lamp = geometry.lampHit
	if err := s.drawLamp(canvas, geometry.lamp); err != nil {
		return err
	}
	badge := s.drawConnectionOverlay(canvas, geometry.lamp, colors)
	if badge.Width > 0 {
		s.layout.reconnect = badge
	}

	stateText, stateColor := s.stateLabel(colors)
	_ = s.drawSurfaceText(
		canvas,
		stateText,
		s.stateFont,
		stateColor,
		geometry.state,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
			walk.TextNoPrefix|walk.TextEndEllipsis|
			s.rtlTextFlag(),
	)

	dp := func(value int) int { return value }
	s.layout.toggle = geometry.toggle
	s.drawButton(
		canvas, s.layout.toggle, s.tr(i18n.MsgActionToggle), actionToggle, true, colors, dp,
	)
	s.layout.on = geometry.on
	s.layout.off = geometry.off
	s.drawButton(canvas, s.layout.on, s.tr(i18n.MsgActionTurnOn), actionOn, false, colors, dp)
	s.drawButton(canvas, s.layout.off, s.tr(i18n.MsgActionTurnOff), actionOff, false, colors, dp)

	hint := s.tr(i18n.MsgMainRightClickHint)
	_ = s.drawSurfaceText(
		canvas, hint, s.captionFont, colors.secondary, geometry.hint,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
			walk.TextNoPrefix|walk.TextEndEllipsis|s.rtlTextFlag(),
	)
	return nil
}

func (s *Shell) drawLamp(canvas *walk.Canvas, lampRect walk.Rectangle) error {
	if err := s.ensureVectorArtwork(lampRect.Width); err != nil {
		return err
	}
	if s.animationActive && s.currentAnimationFrame >= 0 {
		bitmap, bitmapErr := s.animationBitmap(
			s.currentAnimationFrame,
			lampRect.Width,
		)
		if bitmapErr != nil {
			return bitmapErr
		}
		return canvas.DrawImagePixels(
			bitmap,
			walk.Point{X: lampRect.X, Y: lampRect.Y},
		)
	}
	lampBitmap := s.vectorOffBitmap
	if s.snapshot.State == model.StateOn {
		lampBitmap = s.vectorOnBitmap
	}
	if lampBitmap == nil {
		return nil
	}
	if err := canvas.DrawImagePixels(
		lampBitmap,
		walk.Point{X: lampRect.X, Y: lampRect.Y},
	); err != nil {
		return err
	}
	// The hidden first-frame paint establishes the active device-pixel size.
	// Complete its native animation cache in the background so the large
	// decoded APNG sources do not remain resident while the lamp is idle or
	// waiting for its first real state transition.
	s.prepareAnimationCacheAsync(s.animation, lampRect.Width)
	return nil
}

func (s *Shell) drawConnectionOverlay(
	canvas *walk.Canvas,
	lampRect walk.Rectangle,
	colors palette,
) walk.Rectangle {
	if !shouldDrawConnectionOverlay(s.snapshot) {
		return walk.Rectangle{}
	}

	color := colors.accent
	errorState := s.snapshot.LastError != ""
	if errorState {
		color = colors.danger
	} else if s.snapshot.HTTPReachable {
		color = colors.warning
	}
	size := lampRect.Width / 5
	if size < 34 {
		size = 34
	}
	if size > 58 {
		size = 58
	}
	badge := walk.Rectangle{
		X:     lampRect.X + lampRect.Width - size - lampRect.Width/10,
		Y:     lampRect.Y + lampRect.Height - size - lampRect.Height/12,
		Width: size, Height: size,
	}
	artwork := artworkReconnect
	if errorState {
		artwork = artworkClose
	}
	if bitmap, bitmapErr := s.scaledArtwork(
		artwork,
		badge.Width,
		badge.Height,
	); bitmapErr == nil {
		_ = canvas.DrawImagePixels(
			bitmap,
			walk.Point{X: badge.X, Y: badge.Y},
		)
		return badge
	}

	stroke, _ := walk.NewCosmeticPen(walk.PenSolid, color)
	if stroke != nil {
		defer stroke.Dispose()
		_ = canvas.DrawEllipsePixels(stroke, badge)
	}

	symbolBrush, _ := walk.NewSolidColorBrush(color)
	if symbolBrush == nil {
		return badge
	}
	defer symbolBrush.Dispose()
	symbolPen, _ := walk.NewGeometricPen(
		walk.PenSolid|walk.PenCapRound|walk.PenJoinRound,
		4,
		symbolBrush,
	)
	if symbolPen == nil {
		return badge
	}
	defer symbolPen.Dispose()
	padding := size / 3
	left, top := badge.X+padding, badge.Y+padding
	right, bottom := badge.X+size-padding, badge.Y+size-padding
	if errorState {
		_ = canvas.DrawLinePixels(
			symbolPen, walk.Point{X: left, Y: top}, walk.Point{X: right, Y: bottom},
		)
		_ = canvas.DrawLinePixels(
			symbolPen, walk.Point{X: right, Y: top}, walk.Point{X: left, Y: bottom},
		)
		return badge
	}

	middleY := badge.Y + size/2
	_ = canvas.DrawLinePixels(
		symbolPen,
		walk.Point{X: left, Y: middleY},
		walk.Point{X: right, Y: middleY},
	)
	_ = canvas.DrawLinePixels(
		symbolPen,
		walk.Point{X: left, Y: middleY},
		walk.Point{X: left + size/5, Y: middleY - size/5},
	)
	_ = canvas.DrawLinePixels(
		symbolPen,
		walk.Point{X: left, Y: middleY},
		walk.Point{X: left + size/5, Y: middleY + size/5},
	)
	return badge
}

// shouldDrawConnectionOverlay distinguishes an ordinary initial connection
// attempt from a connection that has actually degraded. Core intentionally
// starts after the native shell is constructed, so a zero Snapshot means
// "connecting for the first time", not "reconnect required".
func shouldDrawConnectionOverlay(snapshot model.Snapshot) bool {
	if snapshot.WebSocketOnline {
		return false
	}
	return snapshot.LastError != "" ||
		snapshot.ReconnectAttempt > 0 ||
		snapshot.HTTPReachable
}

func (s *Shell) drawButton(
	canvas *walk.Canvas,
	rect walk.Rectangle,
	text string,
	action actionID,
	primary bool,
	colors palette,
	dp func(int) int,
) {
	color := colors.panelHover
	textColor := colors.text
	if primary {
		color = colors.accent
		textColor = colors.primaryText
	}
	hover := s.hoverProgress(action)
	pressed := s.pressProgress(action)
	if primary {
		color = blend(color, colors.accentHover, hover)
	} else {
		color = blend(color, colors.accent, 0.22*hover)
	}
	if s.snapshot.PendingCommand != "" {
		color = blend(color, colors.secondary, 0.35)
	}
	if !s.usesGlassCompositor() {
		color = blend(color, colors.text, 0.08*pressed)
		brush, _ := walk.NewSolidColorBrush(color)
		if brush != nil {
			defer brush.Dispose()
			_ = canvas.FillRoundedRectanglePixels(
				brush, rect, walk.Size{Width: dp(14), Height: dp(14)},
			)
		}
	} else if primary {
		textColor = colors.text
	}
	label := text
	if primary && s.snapshot.PendingCommand != "" {
		label = s.tr(i18n.MsgMainWorking)
	}
	labelRect := rect
	labelRect.Y += int(math.Round(1.5 * pressed))
	_ = s.drawSurfaceText(
		canvas, label, s.buttonFont, textColor, labelRect,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|
			walk.TextNoPrefix|s.rtlTextFlag(),
	)
}

func (s *Shell) drawIconButton(
	canvas *walk.Canvas,
	rect walk.Rectangle,
	icon artworkID,
	text string,
	action actionID,
	colors palette,
) {
	if !s.usesGlassCompositor() {
		fill := blend(
			colors.panelHover,
			colors.accent,
			0.28*s.hoverProgress(action),
		)
		brush, _ := walk.NewSolidColorBrush(fill)
		if brush != nil {
			defer brush.Dispose()
			_ = canvas.FillRoundedRectanglePixels(
				brush,
				rect,
				walk.Size{Width: 12, Height: 12},
			)
		}
	}
	iconSize := rect.Height - 8
	if iconSize > 30 {
		iconSize = 30
	}
	if iconSize < 16 {
		iconSize = 16
	}
	hover := s.hoverProgress(action)
	pressed := s.pressProgress(action)
	iconSize += int(math.Round(2*hover - pressed))
	if iconSize > rect.Height {
		iconSize = rect.Height
	}
	iconX := rect.X + 4
	iconOnly := rect.Width <= 54
	if iconOnly {
		iconX = rect.X + (rect.Width-iconSize)/2
	} else if s.isRTL() {
		iconX = rect.X + rect.Width - iconSize - 4
	}
	iconRect := walk.Rectangle{
		X:      iconX,
		Y:      rect.Y + (rect.Height-iconSize)/2 + int(math.Round(pressed)),
		Width:  iconSize,
		Height: iconSize,
	}
	if bitmap, bitmapErr := s.scaledArtwork(
		icon,
		iconRect.Width,
		iconRect.Height,
	); bitmapErr == nil {
		_ = canvas.DrawImagePixels(
			bitmap,
			walk.Point{X: iconRect.X, Y: iconRect.Y},
		)
	}
	if iconOnly {
		return
	}
	label := walk.Rectangle{
		X: iconRect.X + iconRect.Width + 3, Y: rect.Y,
		Width:  rect.X + rect.Width - (iconRect.X + iconRect.Width + 3),
		Height: rect.Height,
	}
	labelFormat := walk.TextLeft | walk.TextVCenter |
		walk.TextSingleLine | walk.TextNoPrefix
	if s.isRTL() {
		label = walk.Rectangle{
			X: rect.X, Y: rect.Y,
			Width: iconRect.X - rect.X - 3, Height: rect.Height,
		}
		labelFormat = walk.TextRight | walk.TextRTLReading |
			walk.TextVCenter | walk.TextSingleLine | walk.TextNoPrefix
	}
	textColor := colors.text
	textColor = blend(textColor, colors.accentHover, s.hoverProgress(action))
	_ = s.drawSurfaceText(
		canvas, text, s.captionFont, textColor, label,
		labelFormat,
	)
}

func (s *Shell) usesGlassCompositor() bool {
	if s == nil {
		return false
	}
	return s.backdrop != winfx.BackdropSolid &&
		perPixelForegroundForUI(s.Config().UI)
}

func (s *Shell) usesPerPixelForeground() bool {
	if s == nil {
		return false
	}
	return s.usesPerPixelOverlay() || s.directXForegroundEnabled()
}

func (s *Shell) usesPerPixelOverlay() bool {
	if s == nil {
		return false
	}
	return s.perPixelOverlay
}

func (s *Shell) connectionBadge(colors palette) (string, walk.Color) {
	switch {
	case s.snapshot.WebSocketOnline:
		return s.tr(i18n.MsgStateConnected), colors.success
	case s.snapshot.HTTPReachable:
		return "HTTP", colors.warning
	case s.snapshot.LastError != "":
		return s.tr(i18n.MsgStateOffline), colors.danger
	default:
		return s.tr(i18n.MsgStateConnecting), colors.accent
	}
}

func (s *Shell) stateLabel(colors palette) (string, walk.Color) {
	if s.snapshot.PendingCommand != "" {
		return s.tr(i18n.MsgMainUpdating), colors.accent
	}
	switch s.snapshot.State {
	case model.StateOn:
		return s.tr(i18n.MsgMainLightOn), colors.warm
	case model.StateOff:
		return s.tr(i18n.MsgMainLightOff), colors.secondary
	default:
		return s.tr(i18n.MsgMainFindingLamp), colors.accent
	}
}

func (s *Shell) stateSubtitle() string {
	if s.snapshot.LastError != "" && !s.snapshot.WebSocketOnline {
		return compact(s.snapshot.LastError, 80)
	}
	if s.snapshot.WebSocketOnline {
		if !s.snapshot.LastConnected.IsZero() {
			return "Listening live · connected " + relativeTime(s.snapshot.LastConnected)
		}
		return "Listening for live lamp events"
	}
	if s.snapshot.HTTPReachable {
		return "Reachable · WebSocket is reconnecting"
	}
	return "Resolving rayanlamp.local…"
}

func relativeTime(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	default:
		return at.Local().Format("15:04")
	}
}

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func pointInRect(point walk.Point, rect walk.Rectangle) bool {
	return point.X >= rect.X && point.Y >= rect.Y &&
		point.X < rect.X+rect.Width && point.Y < rect.Y+rect.Height
}

func blend(from, to walk.Color, amount float64) walk.Color {
	if amount < 0 {
		amount = 0
	}
	if amount > 1 {
		amount = 1
	}
	channel := func(shift uint) byte {
		a := float64((uint32(from) >> shift) & 0xff)
		b := float64((uint32(to) >> shift) & 0xff)
		return byte(a + (b-a)*amount)
	}
	return walk.RGB(channel(0), channel(8), channel(16))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
