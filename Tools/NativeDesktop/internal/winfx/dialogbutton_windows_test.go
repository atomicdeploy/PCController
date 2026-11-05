//go:build windows

package winfx

import (
	"math"
	"testing"
	"unsafe"
)

func TestDialogPushButtonStyleFilterPreservesOtherButtonKinds(t *testing.T) {
	tests := []struct {
		style uint32
		want  bool
	}{
		{dialogButtonBSPushButton, true},
		{dialogButtonBSDefPushButton, true},
		{0x0002, false}, // BS_CHECKBOX
		{0x0003, false}, // BS_AUTOCHECKBOX
		{0x0007, false}, // BS_GROUPBOX
		{0x0009, false}, // BS_AUTORADIOBUTTON
		{0x000E, false}, // BS_COMMANDLINK
	}
	for _, test := range tests {
		if got := isDialogPushButtonStyle(test.style); got != test.want {
			t.Fatalf(
				"isDialogPushButtonStyle(%#x) = %v, want %v",
				test.style,
				got,
				test.want,
			)
		}
	}
}

func TestDialogButtonDelegatesNativeActivationLifecycle(t *testing.T) {
	delegated := []uint32{
		dialogButtonWMSetFocus,
		dialogButtonWMKillFocus,
		dialogButtonWMCancelMode,
		dialogButtonWMKeyDown,
		dialogButtonWMKeyUp,
		dialogButtonWMLButtonDown,
		dialogButtonWMLButtonUp,
		dialogButtonWMCaptureChanged,
		dialogButtonBMSetState,
		dialogButtonBMSetStyle,
	}
	for _, message := range delegated {
		if !dialogButtonDelegatesNativeState(message) {
			t.Fatalf("native state message %#x is no longer delegated", message)
		}
	}
	for _, untouched := range []uint32{
		dialogButtonBMClick,
		0x0102, // WM_CHAR / mnemonic input
		0x0087, // WM_GETDLGCODE
	} {
		if dialogButtonDelegatesNativeState(untouched) {
			t.Fatalf(
				"native activation message %#x was unnecessarily intercepted",
				untouched,
			)
		}
	}
}

func TestDialogButtonPrintClientUsesExplicitSuppliedDCPath(t *testing.T) {
	if dialogButtonWMPrintClient != 0x0318 {
		t.Fatalf(
			"print-client message = %#x, want WM_PRINTCLIENT",
			dialogButtonWMPrintClient,
		)
	}
	if dialogButtonDelegatesNativeState(dialogButtonWMPrintClient) {
		t.Fatal("WM_PRINTCLIENT was delegated instead of custom rendered")
	}
	if printDialogButton(0, 0) {
		t.Fatal("print path accepted a missing compositor DC")
	}
	if renderDialogButtonFrame(0, 1) {
		t.Fatal("frame renderer accepted an unthemed HWND")
	}
}

func TestDialogButtonRequestsWalkSupportedParentBrush(t *testing.T) {
	const wmCtlColorStatic = 0x0138
	if dialogButtonWMControlColorStatic != wmCtlColorStatic {
		t.Fatalf(
			"parent brush message = %#x, want WM_CTLCOLORSTATIC (%#x)",
			dialogButtonWMControlColorStatic,
			wmCtlColorStatic,
		)
	}
}

func TestDialogButtonPrimaryAndInteractionStatesAreDistinct(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := dialogButtonColors(dark)
		normal := dialogButtonVisualFor(
			palette,
			false,
			false,
			false,
			false,
			true,
		)
		hot := dialogButtonVisualFor(
			palette,
			false,
			true,
			false,
			false,
			true,
		)
		pressed := dialogButtonVisualFor(
			palette,
			false,
			true,
			true,
			false,
			true,
		)
		primary := dialogButtonVisualFor(
			palette,
			true,
			false,
			false,
			false,
			true,
		)
		disabled := dialogButtonVisualFor(
			palette,
			true,
			true,
			true,
			true,
			false,
		)
		if normal.Fill == hot.Fill || hot.Fill == pressed.Fill {
			t.Fatalf("dark=%v normal/hot/pressed fills are not distinct", dark)
		}
		if primary.Fill == normal.Fill || primary.Text != palette.PrimaryText {
			t.Fatalf("dark=%v primary treatment is not distinct", dark)
		}
		if disabled.Fill != palette.DisabledFill ||
			disabled.Text != palette.DisabledText {
			t.Fatalf("dark=%v disabled treatment leaked an active color", dark)
		}
		for name, visual := range map[string]dialogButtonVisual{
			"normal": normal, "hot": hot, "pressed": pressed, "primary": primary,
		} {
			if visual.Top == visual.Bottom ||
				visual.Shadow == 0 ||
				visual.Border == 0 {
				t.Fatalf(
					"dark=%v %s lacks gradient/depth layers: %+v",
					dark,
					name,
					visual,
				)
			}
		}
	}
}

func TestDialogButtonTextContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := dialogButtonColors(dark)
		tests := []struct {
			name string
			text uint32
			fill uint32
		}{
			{"normal", palette.Text, palette.NormalFill},
			{"normal hot", palette.Text, palette.NormalHot},
			{"primary", palette.PrimaryText, palette.PrimaryFill},
			{"primary hot", palette.PrimaryText, palette.PrimaryHot},
			{"primary pressed", palette.PrimaryText, palette.PrimaryPressed},
		}
		for _, test := range tests {
			if contrast := dialogButtonContrastRatio(
				test.text,
				test.fill,
			); contrast < 4.5 {
				t.Fatalf(
					"dark=%v %s contrast = %.2f, want at least 4.5",
					dark,
					test.name,
					contrast,
				)
			}
		}
		for _, state := range []struct {
			name    string
			primary bool
			hot     bool
			pressed bool
		}{
			{"normal", false, false, false},
			{"normal hot", false, true, false},
			{"normal pressed", false, true, true},
			{"primary", true, false, false},
			{"primary hot", true, true, false},
			{"primary pressed", true, true, true},
		} {
			visual := dialogButtonVisualFor(
				palette,
				state.primary,
				state.hot,
				state.pressed,
				false,
				true,
			)
			for endpoint, background := range map[string]uint32{
				"top": visual.Top, "bottom": visual.Bottom,
			} {
				if contrast := dialogButtonContrastRatio(
					visual.Text,
					background,
				); contrast < 4.5 {
					t.Fatalf(
						"dark=%v %s %s contrast = %.2f, want at least 4.5",
						dark,
						state.name,
						endpoint,
						contrast,
					)
				}
			}
		}
	}
}

func TestDialogButtonDPIHelpers(t *testing.T) {
	if got := scaleDialogButtonMetric(7, 144); got != 11 {
		t.Fatalf("7 at 144 DPI = %d, want 11", got)
	}
	if got := dialogButtonStroke(144); got != 1 {
		t.Fatalf("stroke at 144 DPI = %d, want 1", got)
	}
	if got := dialogButtonStroke(192); got != 2 {
		t.Fatalf("stroke at 192 DPI = %d, want 2", got)
	}
}

func TestDialogButtonFaceDepressesWithoutLeavingShadowBounds(t *testing.T) {
	body := dialogButtonRect{Right: 120, Bottom: 36}
	raised, raisedShadow := dialogButtonFaceAndShadowRects(body, false, 96)
	pressed, pressedShadow := dialogButtonFaceAndShadowRects(body, true, 96)
	if raised.Top != body.Top || raised.Bottom >= body.Bottom {
		t.Fatalf("raised face = %+v body=%+v", raised, body)
	}
	if pressed.Top <= raised.Top || pressed.Bottom <= raised.Bottom {
		t.Fatalf("pressed face did not translate down: raised=%+v pressed=%+v", raised, pressed)
	}
	for name, rect := range map[string]dialogButtonRect{
		"raised shadow":  raisedShadow,
		"pressed shadow": pressedShadow,
	} {
		if rect.Bottom > body.Bottom || rect.Top < body.Top {
			t.Fatalf("%s escaped body: rect=%+v body=%+v", name, rect, body)
		}
	}
}

func TestDialogButtonGradientVertexUsesCOLOR16Channels(t *testing.T) {
	got := dialogButtonGradientVertexFor(3, 7, dialogButtonRGB(0x12, 0x34, 0x56))
	if got.X != 3 || got.Y != 7 ||
		got.Red != 0x1200 ||
		got.Green != 0x3400 ||
		got.Blue != 0x5600 {
		t.Fatalf("gradient vertex = %+v", got)
	}
}

func TestDialogButtonPaintBufferCompatibilityIncludesDPI(t *testing.T) {
	buffer := &dialogButtonPaintBuffer{
		width: 120, height: 36, dpi: 144, memory: 1, bitmap: 2,
	}
	if !dialogButtonPaintBufferCompatible(buffer, 120, 36, 144) {
		t.Fatal("matching persistent paint buffer was rejected")
	}
	for _, candidate := range [][3]int32{
		{121, 36, 144},
		{120, 37, 144},
		{120, 36, 192},
	} {
		if dialogButtonPaintBufferCompatible(
			buffer,
			candidate[0],
			candidate[1],
			candidate[2],
		) {
			t.Fatalf("incompatible buffer accepted metrics %v", candidate)
		}
	}
}

func TestDialogButtonPaintResourcesReuseGDIObjectsAcrossFrames(t *testing.T) {
	dc, _, _ := dialogButtonCreateCompatibleDC.Call(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	defer func() {
		_, _, _ = dialogButtonDeleteDC.Call(dc)
	}()

	var resources dialogButtonPaintResources
	resources.initialize()
	defer resources.close()
	if resources.dcBrush == 0 || resources.dcPen == 0 ||
		resources.nullBrush == 0 {
		t.Fatal("required stock GDI objects were not initialized")
	}

	brushFirst, releaseBrushFirst := acquireDialogButtonBrush(
		dc,
		&resources,
		dialogButtonRGB(0x20, 0x30, 0x40),
	)
	releaseBrushFirst()
	brushNext, releaseBrushNext := acquireDialogButtonBrush(
		dc,
		&resources,
		dialogButtonRGB(0x70, 0x80, 0x90),
	)
	releaseBrushNext()
	if brushFirst != resources.dcBrush || brushNext != brushFirst {
		t.Fatalf(
			"solid brushes were not served by one DC_BRUSH: first=%#x next=%#x stock=%#x",
			brushFirst,
			brushNext,
			resources.dcBrush,
		)
	}

	penFirst, releasePenFirst := acquireDialogButtonPen(
		dc,
		&resources,
		dialogButtonRGB(0x10, 0x20, 0x30),
		1,
	)
	releasePenFirst()
	penNext, releasePenNext := acquireDialogButtonPen(
		dc,
		&resources,
		dialogButtonRGB(0xA0, 0xB0, 0xC0),
		1,
	)
	releasePenNext()
	if penFirst != resources.dcPen || penNext != penFirst {
		t.Fatalf(
			"one-pixel pens were not served by one DC_PEN: first=%#x next=%#x stock=%#x",
			penFirst,
			penNext,
			resources.dcPen,
		)
	}

	wideColor := dialogButtonRGB(0x26, 0x78, 0xC5)
	wideFirst, releaseWideFirst := acquireDialogButtonPen(
		dc,
		&resources,
		wideColor,
		2,
	)
	releaseWideFirst()
	wideNext, releaseWideNext := acquireDialogButtonPen(
		dc,
		&resources,
		wideColor,
		2,
	)
	releaseWideNext()
	if wideFirst == 0 || wideNext != wideFirst {
		t.Fatalf(
			"high-DPI pen was not retained: first=%#x next=%#x",
			wideFirst,
			wideNext,
		)
	}
	if got := len(resources.widePens); got != 1 {
		t.Fatalf("cached high-DPI pen count = %d, want 1", got)
	}

	key := dialogButtonClipRegionKey{
		Rect:   dialogButtonRect{Left: 1, Top: 1, Right: 119, Bottom: 35},
		Radius: 7,
	}
	regionFirst, releaseRegionFirst := acquireDialogButtonClipRegion(
		&resources,
		key,
		key.Radius*2,
	)
	releaseRegionFirst()
	regionNext, releaseRegionNext := acquireDialogButtonClipRegion(
		&resources,
		key,
		key.Radius*2,
	)
	releaseRegionNext()
	if regionFirst == 0 || regionNext != regionFirst {
		t.Fatalf(
			"rounded clip region was not retained: first=%#x next=%#x",
			regionFirst,
			regionNext,
		)
	}
	if got := len(resources.clipRegions); got != 1 {
		t.Fatalf("cached rounded clip-region count = %d, want 1", got)
	}
}

func TestDialogButtonSchedulerRestartRejectsStaleFrame(t *testing.T) {
	const hwnd = uintptr(0x4646)
	current := &dialogButtonAnimationScheduler{
		token: 2,
		stop:  make(chan struct{}),
	}
	current.pending.Store(true)
	dialogButtonSchedulerMu.Lock()
	dialogButtonSchedulers[hwnd] = current
	dialogButtonSchedulerMu.Unlock()
	t.Cleanup(func() {
		dialogButtonSchedulerMu.Lock()
		delete(dialogButtonSchedulers, hwnd)
		dialogButtonSchedulerMu.Unlock()
		close(current.stop)
	})

	if consumeDialogButtonAnimationFrame(hwnd, 1) {
		t.Fatal("stale scheduler token was accepted")
	}
	if !current.pending.Load() {
		t.Fatal("stale scheduler token consumed current pending frame")
	}
	if !consumeDialogButtonAnimationFrame(hwnd, 2) {
		t.Fatal("current scheduler token was rejected")
	}
	if current.pending.Load() {
		t.Fatal("current scheduler frame remained pending after consume")
	}
}

func TestDialogButtonDarkBodyAvoidsRectangularBackdropAndDoubleStroke(
	t *testing.T,
) {
	client := dialogButtonRect{Right: 120, Bottom: 36}
	dark := dialogButtonBodyRect(client, true, 144)
	if dark.Left != 0 || dark.Top != 0 ||
		dark.Right != 119 || dark.Bottom != 35 {
		t.Fatalf("dark body = %+v, want full rounded HWND surface", dark)
	}
	light := dialogButtonBodyRect(client, false, 144)
	if light.Left != 1 || light.Top != 1 ||
		light.Right != 119 || light.Bottom != 35 {
		t.Fatalf("light body = %+v, want preserved one-pixel inset", light)
	}

	darkPalette := dialogButtonColors(true)
	if darkPalette.NormalInner != 0 || darkPalette.PrimaryInner != 0 {
		t.Fatalf(
			"dark inner strokes = normal:%#x primary:%#x, want disabled",
			darkPalette.NormalInner,
			darkPalette.PrimaryInner,
		)
	}
	lightPalette := dialogButtonColors(false)
	if lightPalette.NormalInner == 0 || lightPalette.PrimaryInner == 0 {
		t.Fatal("light inner highlight was not preserved")
	}
}

func TestDialogButtonNativeStructLayouts(t *testing.T) {
	paintSize := unsafe.Sizeof(dialogButtonPaintStruct{})
	if paintSize != 64 && paintSize != 72 {
		t.Fatalf("PAINTSTRUCT-compatible size = %d, want 64 or 72", paintSize)
	}
	trackSize := unsafe.Sizeof(dialogButtonTrackMouse{})
	if trackSize != 16 && trackSize != 24 {
		t.Fatalf("TRACKMOUSEEVENT-compatible size = %d, want 16 or 24", trackSize)
	}
	if got := unsafe.Sizeof(dialogButtonGradientVertex{}); got != 16 {
		t.Fatalf("TRIVERTEX-compatible size = %d, want 16", got)
	}
	if got := unsafe.Sizeof(dialogButtonGradientRect{}); got != 8 {
		t.Fatalf("GRADIENT_RECT-compatible size = %d, want 8", got)
	}
}

func dialogButtonContrastRatio(foreground, background uint32) float64 {
	lighter := dialogButtonRelativeLuminance(foreground)
	darker := dialogButtonRelativeLuminance(background)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func dialogButtonRelativeLuminance(color uint32) float64 {
	channel := func(value byte) float64 {
		normalized := float64(value) / 255
		if normalized <= 0.04045 {
			return normalized / 12.92
		}
		return math.Pow((normalized+0.055)/1.055, 2.4)
	}
	red := channel(byte(color))
	green := channel(byte(color >> 8))
	blue := channel(byte(color >> 16))
	return 0.2126*red + 0.7152*green + 0.0722*blue
}
