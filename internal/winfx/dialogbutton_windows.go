//go:build windows

package winfx

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/design"
)

const (
	dialogButtonSubclassID = 0x524C4254 // "RLBT"

	dialogButtonWMPaint         = 0x000F
	dialogButtonWMEraseBkgnd    = 0x0014
	dialogButtonWMSize          = 0x0005
	dialogButtonWMSetText       = 0x000C
	dialogButtonWMEnable        = 0x000A
	dialogButtonWMSetFocus      = 0x0007
	dialogButtonWMKillFocus     = 0x0008
	dialogButtonWMCancelMode    = 0x001F
	dialogButtonWMStyleChanged  = 0x007D
	dialogButtonWMKeyDown       = 0x0100
	dialogButtonWMKeyUp         = 0x0101
	dialogButtonWMUpdateUIState = 0x0128
	dialogButtonWMQueryUIState  = 0x0129
	// Walk's ContainerBase supplies and aligns effective ancestor brushes for
	// WM_CTLCOLORSTATIC, not WM_CTLCOLORBTN. Asking through the supported path
	// keeps the transparent pixels around our rounded light button continuous
	// with the dialog material.
	dialogButtonWMControlColorStatic = 0x0138
	dialogButtonWMLButtonDown        = 0x0201
	dialogButtonWMLButtonUp          = 0x0202
	dialogButtonWMMouseMove          = 0x0200
	dialogButtonWMMouseLeave         = 0x02A3
	dialogButtonWMCaptureChanged     = 0x0215
	dialogButtonWMPrintClient        = 0x0318
	dialogButtonWMThemeChange        = 0x031A
	dialogButtonWMDPIChanged         = 0x02E0
	dialogButtonWMAnimate            = 0x8000 + 0x0542

	dialogButtonBMGetState = 0x00F2
	dialogButtonBMSetState = 0x00F3
	dialogButtonBMSetStyle = 0x00F4
	dialogButtonBMClick    = 0x00F5

	dialogButtonBSTPushed = 0x0004
	dialogButtonBSTFocus  = 0x0008

	dialogButtonBSPushButton    = 0x0000
	dialogButtonBSDefPushButton = 0x0001
	dialogButtonBSTypeMask      = 0x000F

	dialogButtonGWLStyle = -16

	dialogButtonBkTransparent = 1
	dialogButtonPSStyleSolid  = 0
	dialogButtonNullBrush     = 5
	dialogButtonDCBrush       = 18
	dialogButtonDCPen         = 19

	// One-pixel button strokes use the per-DC stock pen and therefore require
	// no GDI allocation at all. Wider high-DPI pens and rounded gradient clip
	// regions are retained with the paint buffer. These generous hard limits
	// keep every normal/dark/pressed animation variant warm without allowing a
	// pathological stream of colors or geometries to retain unbounded handles.
	dialogButtonWidePenCacheLimit    = 128
	dialogButtonClipRegionCacheLimit = 8

	dialogButtonDTCenter      = 0x00000001
	dialogButtonDTVCenter     = 0x00000004
	dialogButtonDTSingleLine  = 0x00000020
	dialogButtonDTNoPrefix    = 0x00000800
	dialogButtonDTHidePrefix  = 0x00100000
	dialogButtonDTRTLReading  = 0x00020000
	dialogButtonDTEndEllipsis = 0x00008000
	dialogButtonDTCalcRect    = 0x00000400

	dialogButtonUISFHideFocus = 0x0001
	dialogButtonUISFHideAccel = 0x0002

	dialogButtonTMELeave = 0x00000002
	dialogButtonSRCCopy  = 0x00CC0020

	dialogButtonGradientFillRectV = 0x00000001
)

type dialogButtonRect struct {
	Left, Top, Right, Bottom int32
}

type dialogButtonPaintStruct struct {
	DC          uintptr
	Erase       int32
	Paint       dialogButtonRect
	Restore     int32
	Incremental int32
	Reserved    [32]byte
}

type dialogButtonTrackMouse struct {
	Size      uint32
	Flags     uint32
	Window    uintptr
	HoverTime uint32
}

type dialogButtonThemeState struct {
	Dark     bool
	Hot      bool
	Hover    float64
	Animate  bool
	LastTick time.Time
}

type dialogButtonPalette struct {
	Window         uint32
	NormalFill     uint32
	NormalHot      uint32
	NormalPressed  uint32
	NormalBorder   uint32
	NormalInner    uint32
	PrimaryFill    uint32
	PrimaryHot     uint32
	PrimaryPressed uint32
	PrimaryBorder  uint32
	PrimaryInner   uint32
	Text           uint32
	PrimaryText    uint32
	DisabledFill   uint32
	DisabledBorder uint32
	DisabledText   uint32
	Focus          uint32
	PrimaryFocus   uint32
	Highlight      uint32
	Shadow         uint32
}

type dialogButtonVisual struct {
	Fill   uint32
	Top    uint32
	Bottom uint32
	Border uint32
	Inner  uint32
	Text   uint32
	Focus  uint32
	Shadow uint32
}

type dialogButtonGradientVertex struct {
	X, Y             int32
	Red, Green, Blue uint16
	Alpha            uint16
}

type dialogButtonGradientRect struct {
	UpperLeft, LowerRight uint32
}

type dialogButtonPenKey struct {
	Color  uint32
	Stroke int32
}

type dialogButtonClipRegionKey struct {
	Rect   dialogButtonRect
	Radius int32
}

type dialogButtonPaintResources struct {
	mu          sync.Mutex
	dcBrush     uintptr
	dcPen       uintptr
	nullBrush   uintptr
	widePens    map[dialogButtonPenKey]uintptr
	clipRegions map[dialogButtonClipRegionKey]uintptr
}

type dialogButtonPaintBuffer struct {
	width, height int32
	dpi           int32
	memory        uintptr
	bitmap        uintptr
	oldBitmap     uintptr
	iconFont      uintptr
	resources     dialogButtonPaintResources
}

type dialogButtonAnimationScheduler struct {
	token   uint32
	stop    chan struct{}
	pending atomic.Bool
}

var (
	dialogButtonUser32              = windows.NewLazySystemDLL("user32.dll")
	dialogButtonBeginPaint          = dialogButtonUser32.NewProc("BeginPaint")
	dialogButtonEndPaint            = dialogButtonUser32.NewProc("EndPaint")
	dialogButtonFillRect            = dialogButtonUser32.NewProc("FillRect")
	dialogButtonGetClientRect       = dialogButtonUser32.NewProc("GetClientRect")
	dialogButtonGetDPIForWindow     = dialogButtonUser32.NewProc("GetDpiForWindow")
	dialogButtonGetParent           = dialogButtonUser32.NewProc("GetParent")
	dialogButtonGetWindowLong       = dialogButtonUser32.NewProc("GetWindowLongW")
	dialogButtonGetWindowText       = dialogButtonUser32.NewProc("GetWindowTextW")
	dialogButtonGetWindowTextLength = dialogButtonUser32.NewProc("GetWindowTextLengthW")
	dialogButtonInvalidateRect      = dialogButtonUser32.NewProc("InvalidateRect")
	dialogButtonIsWindowEnabled     = dialogButtonUser32.NewProc("IsWindowEnabled")
	dialogButtonPostMessage         = dialogButtonUser32.NewProc("PostMessageW")
	dialogButtonTrackMouseEvent     = dialogButtonUser32.NewProc("TrackMouseEvent")
	dialogButtonSetWindowRgn        = dialogButtonUser32.NewProc("SetWindowRgn")
	dialogButtonGDI32               = windows.NewLazySystemDLL("gdi32.dll")
	dialogButtonCreateBrush         = dialogButtonGDI32.NewProc("CreateSolidBrush")
	dialogButtonCreatePen           = dialogButtonGDI32.NewProc("CreatePen")
	dialogButtonCreateRoundRectRgn  = dialogButtonGDI32.NewProc("CreateRoundRectRgn")
	dialogButtonDeleteObject        = dialogButtonGDI32.NewProc("DeleteObject")
	dialogButtonDrawText            = dialogButtonUser32.NewProc("DrawTextW")
	dialogButtonGetStockObject      = dialogButtonGDI32.NewProc("GetStockObject")
	dialogButtonRoundRect           = dialogButtonGDI32.NewProc("RoundRect")
	dialogButtonSelectObject        = dialogButtonGDI32.NewProc("SelectObject")
	dialogButtonSetBkMode           = dialogButtonGDI32.NewProc("SetBkMode")
	dialogButtonSetDCBrushColor     = dialogButtonGDI32.NewProc("SetDCBrushColor")
	dialogButtonSetDCPenColor       = dialogButtonGDI32.NewProc("SetDCPenColor")
	dialogButtonSetTextColor        = dialogButtonGDI32.NewProc("SetTextColor")
	dialogButtonCreateCompatibleDC  = dialogButtonGDI32.NewProc("CreateCompatibleDC")
	dialogButtonCreateCompatibleBMP = dialogButtonGDI32.NewProc("CreateCompatibleBitmap")
	dialogButtonBitBlt              = dialogButtonGDI32.NewProc("BitBlt")
	dialogButtonDeleteDC            = dialogButtonGDI32.NewProc("DeleteDC")
	dialogButtonSaveDC              = dialogButtonGDI32.NewProc("SaveDC")
	dialogButtonRestoreDC           = dialogButtonGDI32.NewProc("RestoreDC")
	dialogButtonSelectClipRgn       = dialogButtonGDI32.NewProc("SelectClipRgn")
	dialogButtonMSIMG32             = windows.NewLazySystemDLL("msimg32.dll")
	dialogButtonGradientFill        = dialogButtonMSIMG32.NewProc("GradientFill")

	dialogButtonMu     sync.RWMutex
	dialogButtonThemes = make(map[uintptr]dialogButtonThemeState)

	dialogButtonBufferMu sync.Mutex
	dialogButtonBuffers  = make(map[uintptr]*dialogButtonPaintBuffer)

	dialogButtonSchedulerMu       sync.Mutex
	dialogButtonSchedulers        = make(map[uintptr]*dialogButtonAnimationScheduler)
	dialogButtonSchedulerSequence uint32
)

func setDialogButtonTheme(hwnd uintptr, dark, animate, enabled bool) {
	if hwnd == 0 {
		return
	}
	if !enabled {
		stopDialogButtonAnimationScheduler(hwnd)
		releaseDialogButtonPaintBuffer(hwnd)
		_ = RemoveWindowSubclass(hwnd, dialogButtonSubclassID)
		dialogButtonMu.Lock()
		delete(dialogButtonThemes, hwnd)
		dialogButtonMu.Unlock()
		updateDialogButtonRegion(hwnd, false)
		invalidateDialogButton(hwnd)
		return
	}

	dialogButtonMu.Lock()
	state, exists := dialogButtonThemes[hwnd]
	state.Dark = dark
	state.Animate = animate
	if !animate {
		state.Hover = 0
		if state.Hot {
			state.Hover = 1
		}
		state.LastTick = time.Time{}
	}
	dialogButtonThemes[hwnd] = state
	dialogButtonMu.Unlock()
	if !animate {
		stopDialogButtonAnimationScheduler(hwnd)
	}

	if !exists {
		if err := SetWindowSubclass(
			hwnd,
			dialogButtonSubclassID,
			dialogButtonWindowMessage,
		); err != nil && !errors.Is(err, ErrSubclassExists) {
			dialogButtonMu.Lock()
			delete(dialogButtonThemes, hwnd)
			dialogButtonMu.Unlock()
			return
		}
	}
	updateDialogButtonRegion(hwnd, dark)
	invalidateDialogButton(hwnd)
}

func setDialogButtonMotion(hwnd uintptr, animate bool) {
	dialogButtonMu.Lock()
	state, exists := dialogButtonThemes[hwnd]
	if exists {
		state.Animate = animate
		if !animate {
			state.Hover = 0
			if state.Hot {
				state.Hover = 1
			}
			state.LastTick = time.Time{}
		}
		dialogButtonThemes[hwnd] = state
	}
	dialogButtonMu.Unlock()
	if !exists {
		return
	}
	if !animate {
		stopDialogButtonAnimationScheduler(hwnd)
	}
	invalidateDialogButton(hwnd)
}

func dialogButtonWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	switch uint32(message) {
	case wmNCDestroy:
		stopDialogButtonAnimationScheduler(hwnd)
		releaseDialogButtonPaintBuffer(hwnd)
		dialogButtonMu.Lock()
		delete(dialogButtonThemes, hwnd)
		dialogButtonMu.Unlock()
		return 0, false

	case dialogButtonWMPaint:
		if paintDialogButton(hwnd) {
			return 0, true
		}

	case dialogButtonWMPrintClient:
		// WM_PRINT/PRF_CHILDREN supplies the compositor's destination DC.
		// Render the exact same complete buffered frame used on screen, but
		// never enter BeginPaint or advance animation state while capturing.
		if printDialogButton(hwnd, wParam) {
			return 0, true
		}

	case dialogButtonWMEraseBkgnd:
		return 1, true

	case dialogButtonWMAnimate:
		if consumeDialogButtonAnimationFrame(hwnd, uint32(wParam)) {
			if !advanceDialogButtonAnimation(hwnd, time.Now()) {
				stopDialogButtonAnimationScheduler(hwnd)
			}
			return 0, true
		}
		return 0, true

	case dialogButtonWMMouseMove:
		setDialogButtonHot(hwnd, true)
		trackDialogButtonMouseLeave(hwnd)

	case dialogButtonWMMouseLeave:
		setDialogButtonHot(hwnd, false)

	}
	if dialogButtonDelegatesNativeState(uint32(message)) {
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			if uint32(message) == dialogButtonWMSize ||
				uint32(message) == dialogButtonWMDPIChanged {
				releaseDialogButtonPaintBuffer(hwnd)
				if state, exists := dialogButtonTheme(hwnd); exists {
					updateDialogButtonRegion(hwnd, state.Dark)
				}
			}
			invalidateDialogButton(hwnd)
			presentDialogMotionFrame(hwnd)
		}
		return result, true
	}
	return 0, false
}

func dialogButtonDelegatesNativeState(message uint32) bool {
	switch message {
	case dialogButtonWMSetText,
		dialogButtonWMEnable,
		dialogButtonWMSetFocus,
		dialogButtonWMKillFocus,
		dialogButtonWMCancelMode,
		dialogButtonWMStyleChanged,
		dialogButtonWMKeyDown,
		dialogButtonWMKeyUp,
		dialogButtonWMUpdateUIState,
		dialogButtonWMLButtonDown,
		dialogButtonWMLButtonUp,
		dialogButtonWMCaptureChanged,
		dialogButtonWMThemeChange,
		dialogButtonWMSize,
		dialogButtonWMDPIChanged,
		dialogButtonBMSetState,
		dialogButtonBMSetStyle:
		return true
	default:
		// BM_CLICK, WM_CHAR, WM_GETDLGCODE, and parent-dialog Enter/Escape
		// routing remain entirely native and continue through the unhandled
		// subclass path.
		return false
	}
}

func paintDialogButton(hwnd uintptr) bool {
	var paint dialogButtonPaintStruct
	dc, _, _ := dialogButtonBeginPaint.Call(
		hwnd,
		uintptr(unsafe.Pointer(&paint)),
	)
	if dc == 0 {
		return false
	}
	defer func() {
		_, _, _ = dialogButtonEndPaint.Call(
			hwnd,
			uintptr(unsafe.Pointer(&paint)),
		)
		runtime.KeepAlive(&paint)
	}()
	return renderDialogButtonFrame(hwnd, dc)
}

// printDialogButton is deliberately separate from paintDialogButton. Page
// transitions ask child controls to render through WM_PRINT/PRF_CHILDREN while
// their live redraw is blocked; calling BeginPaint in that path can return an
// unrelated update DC or consume the HWND's pending paint. The supplied DC is
// therefore rendered atomically through the same persistent back buffer, with
// no animation clock or scheduler mutation.
func printDialogButton(hwnd, dc uintptr) bool {
	if dc == 0 {
		return false
	}
	return renderDialogButtonFrame(hwnd, dc)
}

func renderDialogButtonFrame(hwnd, targetDC uintptr) bool {
	state, exists := dialogButtonTheme(hwnd)
	if !exists || targetDC == 0 {
		return false
	}

	var client dialogButtonRect
	ok, _, _ := dialogButtonGetClientRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&client)),
	)
	if ok == 0 || client.Right <= client.Left || client.Bottom <= client.Top {
		return false
	}

	dpi := dialogButtonDPI(hwnd)
	iconFont := uintptr(0)
	var resources *dialogButtonPaintResources
	if bufferedDC, finish, buffer, buffered :=
		beginCachedDialogButtonBuffer(hwnd, targetDC, client, dpi); buffered {
		targetDC = bufferedDC
		iconFont = buffer.iconFont
		resources = &buffer.resources
		defer finish()
	} else if bufferedDC, finish, buffered :=
		beginDialogButtonBuffer(targetDC, client); buffered {
		targetDC = bufferedDC
		defer finish()
		iconFont = createDialogButtonIconFont(targetDC, dpi)
		if iconFont != 0 {
			defer func() {
				_, _, _ = dialogButtonDeleteObject.Call(iconFont)
			}()
		}
	}
	palette := dialogButtonColors(state.Dark)
	if state.Dark {
		fillDialogButtonRect(targetDC, client, palette.Window, resources)
	} else {
		// Preserve the existing light-theme behavior, including the exact
		// parent brush used by Walk around the inset rounded body.
		fillDialogButtonParent(
			targetDC,
			hwnd,
			client,
			palette.Window,
			resources,
		)
	}

	buttonState, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogButtonBMGetState,
		0,
		0,
	)
	enabled, _, _ := dialogButtonIsWindowEnabled.Call(hwnd)
	style := dialogWindowStyle(hwnd)
	isDefault := dialogButtonIsDefaultStyle(style)
	isPressed := buttonState&dialogButtonBSTPushed != 0
	isFocused := buttonState&dialogButtonBSTFocus != 0
	visual := dialogButtonAnimatedVisualFor(
		palette,
		isDefault,
		dialogEase(state.Hover),
		isPressed,
		isFocused,
		enabled != 0,
	)

	body := dialogButtonBodyRect(client, state.Dark, dpi)
	radius := scaleDialogButtonMetric(7, dpi)
	face := drawDialogButtonBody(
		targetDC,
		body,
		visual,
		radius,
		dialogButtonStroke(dpi),
		isPressed,
		dpi,
		resources,
	)

	uiState, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogButtonWMQueryUIState,
		0,
		0,
	)
	if isFocused && uiState&dialogButtonUISFHideFocus == 0 {
		focusRect := face
		focusRect.Left += scaleDialogButtonMetric(2, dpi)
		focusRect.Top += scaleDialogButtonMetric(2, dpi)
		focusRect.Right -= scaleDialogButtonMetric(2, dpi)
		focusRect.Bottom -= scaleDialogButtonMetric(2, dpi)
		strokeDialogButtonRoundRect(
			targetDC,
			focusRect,
			visual.Focus,
			radius-scaleDialogButtonMetric(2, dpi),
			dialogButtonStroke(dpi),
			resources,
		)
	}

	drawDialogButtonContent(
		targetDC,
		hwnd,
		face,
		visual.Text,
		uiState&dialogButtonUISFHideAccel != 0,
		dpi,
		iconFont,
	)
	runtime.KeepAlive(&client)
	return true
}

func dialogButtonColors(dark bool) dialogButtonPalette {
	if dark {
		tokens := design.Default.Colors.Dark
		return dialogButtonPalette{
			Window:        dialogButtonToken(tokens.CanvasAlt),
			NormalFill:    dialogButtonToken(tokens.SurfaceHigh),
			NormalHot:     dialogButtonTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.18),
			NormalPressed: dialogButtonTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.30),
			NormalBorder:  dialogButtonToken(tokens.Stroke),
			// A second inset stroke reads as a duplicate rectangular body on
			// the dark material. The native-looking light treatment retains
			// its inner highlight below.
			NormalInner:    0,
			PrimaryFill:    dialogButtonToken(tokens.Accent),
			PrimaryHot:     dialogButtonToken(tokens.AccentHover),
			PrimaryPressed: dialogButtonTokenMix(tokens.Accent, tokens.Canvas, 0.20),
			PrimaryBorder:  dialogButtonTokenMix(tokens.Accent, tokens.Stroke, 0.34),
			PrimaryInner:   0,
			Text:           dialogButtonToken(tokens.Text),
			PrimaryText:    dialogButtonToken(design.Default.Colors.Dark.Canvas),
			DisabledFill:   dialogButtonToken(tokens.Surface),
			DisabledBorder: dialogButtonToken(tokens.Stroke),
			DisabledText:   dialogButtonToken(tokens.TextSubtle),
			Focus:          dialogButtonToken(tokens.AccentHover),
			PrimaryFocus:   dialogButtonToken(tokens.Text),
			Highlight:      dialogButtonToken(tokens.StrokeHighlight),
			Shadow:         dialogButtonToken(tokens.Shadow),
		}
	}
	tokens := design.Default.Colors.Light
	return dialogButtonPalette{
		Window:         dialogButtonToken(tokens.CanvasAlt),
		NormalFill:     dialogButtonToken(tokens.Surface),
		NormalHot:      dialogButtonTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.18),
		NormalPressed:  dialogButtonTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.30),
		NormalBorder:   dialogButtonToken(tokens.Stroke),
		NormalInner:    dialogButtonToken(tokens.StrokeHighlight),
		PrimaryFill:    dialogButtonToken(tokens.Accent),
		PrimaryHot:     dialogButtonToken(tokens.AccentHover),
		PrimaryPressed: dialogButtonTokenMix(tokens.Accent, tokens.Canvas, 0.20),
		PrimaryBorder:  dialogButtonTokenMix(tokens.Accent, tokens.Stroke, 0.34),
		PrimaryInner:   dialogButtonTokenMix(tokens.AccentHover, tokens.StrokeHighlight, 0.34),
		Text:           dialogButtonToken(tokens.Text),
		PrimaryText:    dialogButtonToken(design.Default.Colors.Dark.Canvas),
		DisabledFill:   dialogButtonToken(tokens.Canvas),
		DisabledBorder: dialogButtonToken(tokens.Stroke),
		DisabledText:   dialogButtonToken(tokens.TextSubtle),
		Focus:          dialogButtonToken(tokens.Accent),
		PrimaryFocus:   dialogButtonToken(tokens.StrokeHighlight),
		Highlight:      dialogButtonToken(tokens.StrokeHighlight),
		Shadow:         dialogButtonToken(tokens.Shadow),
	}
}

func dialogButtonToken(value string) uint32 {
	color := design.MustColor(value)
	return dialogButtonRGB(color.R, color.G, color.B)
}

func dialogButtonTokenMix(first, second string, secondWeight float64) uint32 {
	a := design.MustColor(first)
	b := design.MustColor(second)
	if secondWeight < 0 {
		secondWeight = 0
	} else if secondWeight > 1 {
		secondWeight = 1
	}
	mix := func(first, second byte) byte {
		return byte(float64(first)*(1-secondWeight) + float64(second)*secondWeight + 0.5)
	}
	return dialogButtonRGB(mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B))
}

func dialogButtonVisualFor(
	palette dialogButtonPalette,
	primary, hot, pressed, focused, enabled bool,
) dialogButtonVisual {
	visual := dialogButtonVisual{
		Fill:   palette.NormalFill,
		Border: palette.NormalBorder,
		Inner:  palette.NormalInner,
		Text:   palette.Text,
		Focus:  palette.Focus,
	}
	if primary {
		visual.Fill = palette.PrimaryFill
		visual.Border = palette.PrimaryBorder
		visual.Inner = palette.PrimaryInner
		visual.Text = palette.PrimaryText
		visual.Focus = palette.PrimaryFocus
	}
	if hot {
		if primary {
			visual.Fill = palette.PrimaryHot
		} else {
			visual.Fill = palette.NormalHot
		}
	}
	if pressed {
		if primary {
			visual.Fill = palette.PrimaryPressed
		} else {
			visual.Fill = palette.NormalPressed
		}
	}
	if !enabled {
		visual.Fill = palette.DisabledFill
		visual.Border = palette.DisabledBorder
		visual.Inner = palette.DisabledBorder
		visual.Text = palette.DisabledText
	}
	topWeight := 0.16
	bottomWeight := 0.14
	shadowWeight := 1.0
	if primary {
		topWeight = 0.22
		bottomWeight = 0.18
	}
	if hot {
		topWeight += 0.05
	}
	if pressed {
		topWeight *= 0.45
		if primary {
			// Keep the dark accent button's lower gradient bright enough for
			// its dark foreground. Depression already comes from the physical
			// face translation and collapsed shadow, so crushing the accent
			// toward the dark canvas only harms legibility.
			bottomWeight = 0.14
		} else {
			bottomWeight += 0.12
		}
		shadowWeight = 0.52
	}
	if !enabled {
		topWeight = 0.05
		bottomWeight = 0.06
		shadowWeight = 0.24
	}
	visual.Top = dialogMixColor(visual.Fill, palette.Highlight, topWeight)
	visual.Bottom = dialogMixColor(visual.Fill, palette.Window, bottomWeight)
	visual.Shadow = dialogMixColor(palette.Window, palette.Shadow, shadowWeight)
	_ = focused // Focus changes the separately painted inner ring.
	return visual
}

func dialogButtonAnimatedVisualFor(
	palette dialogButtonPalette,
	primary bool,
	hover float64,
	pressed, focused, enabled bool,
) dialogButtonVisual {
	normal := dialogButtonVisualFor(
		palette,
		primary,
		false,
		pressed,
		focused,
		enabled,
	)
	if !enabled || pressed {
		return normal
	}
	hover = dialogClamp(hover)
	if hover == 0 {
		return normal
	}
	hot := dialogButtonVisualFor(
		palette,
		primary,
		true,
		false,
		focused,
		true,
	)
	hot.Border = dialogMixColor(hot.Border, hot.Focus, 0.42)
	if hot.Inner != 0 {
		hot.Inner = dialogMixColor(hot.Inner, hot.Focus, 0.18)
	}
	return dialogButtonVisual{
		Fill:   dialogMixColor(normal.Fill, hot.Fill, hover),
		Top:    dialogMixColor(normal.Top, hot.Top, hover),
		Bottom: dialogMixColor(normal.Bottom, hot.Bottom, hover),
		Border: dialogMixColor(normal.Border, hot.Border, hover),
		Inner:  dialogMixOptionalColor(normal.Inner, hot.Inner, hover),
		Text:   dialogMixColor(normal.Text, hot.Text, hover),
		Focus:  dialogMixColor(normal.Focus, hot.Focus, hover),
		Shadow: dialogMixColor(normal.Shadow, hot.Shadow, hover),
	}
}

func dialogMixOptionalColor(first, second uint32, amount float64) uint32 {
	if first == 0 || second == 0 {
		if amount >= 1 {
			return second
		}
		return first
	}
	return dialogMixColor(first, second, amount)
}

func isDialogPushButtonStyle(style uint32) bool {
	buttonType := style & dialogButtonBSTypeMask
	return buttonType == dialogButtonBSPushButton ||
		buttonType == dialogButtonBSDefPushButton
}

func dialogButtonIsDefaultStyle(style uint32) bool {
	return style&dialogButtonBSTypeMask == dialogButtonBSDefPushButton
}

func dialogWindowStyle(hwnd uintptr) uint32 {
	index := int32(dialogButtonGWLStyle)
	value, _, _ := dialogButtonGetWindowLong.Call(
		hwnd,
		uintptr(index),
	)
	return uint32(value)
}

func dialogButtonTheme(hwnd uintptr) (dialogButtonThemeState, bool) {
	dialogButtonMu.RLock()
	state, exists := dialogButtonThemes[hwnd]
	dialogButtonMu.RUnlock()
	return state, exists
}

func setDialogButtonHot(hwnd uintptr, hot bool) {
	dialogButtonMu.Lock()
	state, exists := dialogButtonThemes[hwnd]
	changed := exists && state.Hot != hot
	animate := false
	if changed {
		state.Hot = hot
		target := 0.0
		if hot {
			target = 1
		}
		if state.Animate {
			state.LastTick = time.Now()
			animate = state.Hover != target
		} else {
			state.Hover = target
			state.LastTick = time.Time{}
		}
		dialogButtonThemes[hwnd] = state
	}
	dialogButtonMu.Unlock()
	if changed {
		presentNow := !animate
		if animate && !startDialogButtonAnimationScheduler(hwnd) {
			settleDialogButtonAnimation(hwnd)
			presentNow = true
		}
		invalidateDialogButton(hwnd)
		if presentNow {
			presentDialogMotionFrame(hwnd)
		}
	}
}

func advanceDialogButtonAnimation(hwnd uintptr, now time.Time) bool {
	dialogButtonMu.Lock()
	state, exists := dialogButtonThemes[hwnd]
	if !exists {
		dialogButtonMu.Unlock()
		return false
	}
	target := 0.0
	if state.Hot {
		target = 1
	}
	if !state.Animate {
		state.Hover = target
		state.LastTick = time.Time{}
		dialogButtonThemes[hwnd] = state
		dialogButtonMu.Unlock()
		invalidateDialogButton(hwnd)
		presentDialogMotionFrame(hwnd)
		return false
	}
	elapsed := now.Sub(state.LastTick)
	var active bool
	state.Hover, active = advanceDialogMotion(
		state.Hover,
		target,
		elapsed,
		design.Default.Motion.FastMS,
	)
	if active {
		state.LastTick = now
	} else {
		state.LastTick = time.Time{}
	}
	dialogButtonThemes[hwnd] = state
	dialogButtonMu.Unlock()
	invalidateDialogButton(hwnd)
	presentDialogMotionFrame(hwnd)
	return active
}

func settleDialogButtonAnimation(hwnd uintptr) {
	dialogButtonMu.Lock()
	state, exists := dialogButtonThemes[hwnd]
	if exists {
		state.Hover = 0
		if state.Hot {
			state.Hover = 1
		}
		state.LastTick = time.Time{}
		dialogButtonThemes[hwnd] = state
	}
	dialogButtonMu.Unlock()
}

func startDialogButtonAnimationScheduler(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	dialogButtonSchedulerMu.Lock()
	if dialogButtonSchedulers[hwnd] != nil {
		dialogButtonSchedulerMu.Unlock()
		return true
	}
	token := atomic.AddUint32(&dialogButtonSchedulerSequence, 1)
	if token == 0 {
		token = atomic.AddUint32(&dialogButtonSchedulerSequence, 1)
	}
	scheduler := &dialogButtonAnimationScheduler{
		token: token,
		stop:  make(chan struct{}),
	}
	dialogButtonSchedulers[hwnd] = scheduler
	dialogButtonSchedulerMu.Unlock()

	go runDialogButtonAnimationScheduler(hwnd, scheduler)
	return true
}

func runDialogButtonAnimationScheduler(
	hwnd uintptr,
	scheduler *dialogButtonAnimationScheduler,
) {
	if scheduler == nil {
		return
	}
	ticker := time.NewTicker(dialogMotionTimerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-scheduler.stop:
			return
		case <-ticker.C:
			if !scheduler.pending.CompareAndSwap(false, true) {
				continue
			}
			posted, _, _ := dialogButtonPostMessage.Call(
				hwnd,
				dialogButtonWMAnimate,
				uintptr(scheduler.token),
				0,
			)
			if posted == 0 {
				scheduler.pending.Store(false)
			}
		}
	}
}

func consumeDialogButtonAnimationFrame(hwnd uintptr, token uint32) bool {
	dialogButtonSchedulerMu.Lock()
	scheduler := dialogButtonSchedulers[hwnd]
	valid := scheduler != nil && scheduler.token == token
	if valid {
		scheduler.pending.Store(false)
	}
	dialogButtonSchedulerMu.Unlock()
	return valid
}

func stopDialogButtonAnimationScheduler(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogButtonSchedulerMu.Lock()
	scheduler := dialogButtonSchedulers[hwnd]
	delete(dialogButtonSchedulers, hwnd)
	dialogButtonSchedulerMu.Unlock()
	if scheduler != nil {
		close(scheduler.stop)
		scheduler.pending.Store(false)
	}
}

func trackDialogButtonMouseLeave(hwnd uintptr) {
	tracking := dialogButtonTrackMouse{
		Size:   uint32(unsafe.Sizeof(dialogButtonTrackMouse{})),
		Flags:  dialogButtonTMELeave,
		Window: hwnd,
	}
	_, _, _ = dialogButtonTrackMouseEvent.Call(
		uintptr(unsafe.Pointer(&tracking)),
	)
	runtime.KeepAlive(&tracking)
}

func fillDialogButtonParent(
	dc, hwnd uintptr,
	rect dialogButtonRect,
	fallback uint32,
	resources *dialogButtonPaintResources,
) {
	brush := dialogControlParentBrush(dc, hwnd)
	if brush != 0 {
		_, _, _ = dialogButtonFillRect.Call(
			dc,
			uintptr(unsafe.Pointer(&rect)),
			brush,
		)
		runtime.KeepAlive(&rect)
		return
	}
	fillDialogButtonRect(dc, rect, fallback, resources)
}

// beginDialogButtonBuffer gives every animation frame one atomic presentation.
// Repainting a button at roughly 60 Hz directly into its HWND otherwise exposes
// the cleared parent-colored corners before the rounded body is complete.
func beginDialogButtonBuffer(
	targetDC uintptr,
	client dialogButtonRect,
) (dc uintptr, finish func(), ok bool) {
	width := client.Right - client.Left
	height := client.Bottom - client.Top
	if targetDC == 0 || width < 1 || height < 1 {
		return 0, nil, false
	}
	memoryDC, _, _ := dialogButtonCreateCompatibleDC.Call(targetDC)
	if memoryDC == 0 {
		return 0, nil, false
	}
	bitmap, _, _ := dialogButtonCreateCompatibleBMP.Call(
		targetDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogButtonDeleteDC.Call(memoryDC)
		return 0, nil, false
	}
	previous, _, _ := dialogButtonSelectObject.Call(memoryDC, bitmap)
	if previous == 0 {
		_, _, _ = dialogButtonDeleteObject.Call(bitmap)
		_, _, _ = dialogButtonDeleteDC.Call(memoryDC)
		return 0, nil, false
	}
	finish = func() {
		_, _, _ = dialogButtonBitBlt.Call(
			targetDC,
			uintptr(client.Left),
			uintptr(client.Top),
			uintptr(width),
			uintptr(height),
			memoryDC,
			0,
			0,
			dialogButtonSRCCopy,
		)
		_, _, _ = dialogButtonSelectObject.Call(memoryDC, previous)
		_, _, _ = dialogButtonDeleteObject.Call(bitmap)
		_, _, _ = dialogButtonDeleteDC.Call(memoryDC)
	}
	return memoryDC, finish, true
}

func beginCachedDialogButtonBuffer(
	hwnd, targetDC uintptr,
	client dialogButtonRect,
	dpi int32,
) (dc uintptr, finish func(), buffer *dialogButtonPaintBuffer, ok bool) {
	width := client.Right - client.Left
	height := client.Bottom - client.Top
	if hwnd == 0 || targetDC == 0 || width < 1 || height < 1 {
		return 0, nil, nil, false
	}
	if dpi <= 0 {
		dpi = 96
	}

	dialogButtonBufferMu.Lock()
	buffer = dialogButtonBuffers[hwnd]
	if buffer != nil &&
		!dialogButtonPaintBufferCompatible(buffer, width, height, dpi) {
		delete(dialogButtonBuffers, hwnd)
	}
	dialogButtonBufferMu.Unlock()
	if buffer != nil &&
		!dialogButtonPaintBufferCompatible(buffer, width, height, dpi) {
		closeDialogButtonPaintBuffer(buffer)
		buffer = nil
	}
	if buffer == nil {
		buffer = newDialogButtonPaintBuffer(targetDC, width, height, dpi)
		if buffer == nil {
			return 0, nil, nil, false
		}
		dialogButtonBufferMu.Lock()
		if existing := dialogButtonBuffers[hwnd]; existing != nil {
			dialogButtonBufferMu.Unlock()
			closeDialogButtonPaintBuffer(buffer)
			buffer = existing
		} else {
			dialogButtonBuffers[hwnd] = buffer
			dialogButtonBufferMu.Unlock()
		}
	}

	finish = func() {
		_, _, _ = dialogButtonBitBlt.Call(
			targetDC,
			uintptr(client.Left),
			uintptr(client.Top),
			uintptr(width),
			uintptr(height),
			buffer.memory,
			0,
			0,
			dialogButtonSRCCopy,
		)
	}
	return buffer.memory, finish, buffer, true
}

func dialogButtonPaintBufferCompatible(
	buffer *dialogButtonPaintBuffer,
	width, height, dpi int32,
) bool {
	return buffer != nil &&
		buffer.memory != 0 &&
		buffer.bitmap != 0 &&
		buffer.width == width &&
		buffer.height == height &&
		buffer.dpi == dpi
}

func newDialogButtonPaintBuffer(
	targetDC uintptr,
	width, height, dpi int32,
) *dialogButtonPaintBuffer {
	memory, _, _ := dialogButtonCreateCompatibleDC.Call(targetDC)
	if memory == 0 {
		return nil
	}
	bitmap, _, _ := dialogButtonCreateCompatibleBMP.Call(
		targetDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogButtonDeleteDC.Call(memory)
		return nil
	}
	oldBitmap, _, _ := dialogButtonSelectObject.Call(memory, bitmap)
	if oldBitmap == 0 || oldBitmap == ^uintptr(0) {
		_, _, _ = dialogButtonDeleteObject.Call(bitmap)
		_, _, _ = dialogButtonDeleteDC.Call(memory)
		return nil
	}
	buffer := &dialogButtonPaintBuffer{
		width:     width,
		height:    height,
		dpi:       dpi,
		memory:    memory,
		bitmap:    bitmap,
		oldBitmap: oldBitmap,
		iconFont:  createDialogButtonIconFont(memory, dpi),
	}
	buffer.resources.initialize()
	return buffer
}

func releaseDialogButtonPaintBuffer(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogButtonBufferMu.Lock()
	buffer := dialogButtonBuffers[hwnd]
	delete(dialogButtonBuffers, hwnd)
	dialogButtonBufferMu.Unlock()
	closeDialogButtonPaintBuffer(buffer)
}

func closeDialogButtonPaintBuffer(buffer *dialogButtonPaintBuffer) {
	if buffer == nil {
		return
	}
	if buffer.iconFont != 0 {
		_, _, _ = dialogButtonDeleteObject.Call(buffer.iconFont)
	}
	buffer.resources.close()
	if buffer.memory != 0 && buffer.oldBitmap != 0 &&
		buffer.oldBitmap != ^uintptr(0) {
		_, _, _ = dialogButtonSelectObject.Call(
			buffer.memory,
			buffer.oldBitmap,
		)
	}
	if buffer.bitmap != 0 {
		_, _, _ = dialogButtonDeleteObject.Call(buffer.bitmap)
	}
	if buffer.memory != 0 {
		_, _, _ = dialogButtonDeleteDC.Call(buffer.memory)
	}
	buffer.iconFont = 0
	buffer.bitmap = 0
	buffer.memory = 0
}

func (resources *dialogButtonPaintResources) initialize() {
	if resources == nil {
		return
	}
	resources.mu.Lock()
	defer resources.mu.Unlock()
	resources.dcBrush = dialogButtonStockObject(dialogButtonDCBrush)
	resources.dcPen = dialogButtonStockObject(dialogButtonDCPen)
	resources.nullBrush = dialogButtonStockObject(dialogButtonNullBrush)
	resources.widePens = make(
		map[dialogButtonPenKey]uintptr,
		dialogButtonWidePenCacheLimit,
	)
	resources.clipRegions = make(
		map[dialogButtonClipRegionKey]uintptr,
		dialogButtonClipRegionCacheLimit,
	)
}

func (resources *dialogButtonPaintResources) close() {
	if resources == nil {
		return
	}
	resources.mu.Lock()
	for key, pen := range resources.widePens {
		if pen != 0 {
			_, _, _ = dialogButtonDeleteObject.Call(pen)
		}
		delete(resources.widePens, key)
	}
	for key, region := range resources.clipRegions {
		if region != 0 {
			_, _, _ = dialogButtonDeleteObject.Call(region)
		}
		delete(resources.clipRegions, key)
	}
	resources.widePens = nil
	resources.clipRegions = nil
	resources.dcBrush = 0
	resources.dcPen = 0
	resources.nullBrush = 0
	resources.mu.Unlock()
}

func dialogButtonStockObject(index uintptr) uintptr {
	object, _, _ := dialogButtonGetStockObject.Call(index)
	if object == ^uintptr(0) {
		return 0
	}
	return object
}

func dialogButtonStockObjectFor(
	resources *dialogButtonPaintResources,
	index uintptr,
) uintptr {
	if resources != nil {
		switch index {
		case dialogButtonDCBrush:
			if resources.dcBrush != 0 {
				return resources.dcBrush
			}
		case dialogButtonDCPen:
			if resources.dcPen != 0 {
				return resources.dcPen
			}
		case dialogButtonNullBrush:
			if resources.nullBrush != 0 {
				return resources.nullBrush
			}
		}
	}
	return dialogButtonStockObject(index)
}

func acquireDialogButtonBrush(
	dc uintptr,
	resources *dialogButtonPaintResources,
	color uint32,
) (uintptr, func()) {
	brush := dialogButtonStockObjectFor(resources, dialogButtonDCBrush)
	if brush != 0 {
		_, _, _ = dialogButtonSetDCBrushColor.Call(dc, uintptr(color))
		return brush, releaseDialogButtonGDIObjectNothing
	}
	brush, _, _ = dialogButtonCreateBrush.Call(uintptr(color))
	if brush == 0 {
		return 0, releaseDialogButtonGDIObjectNothing
	}
	return brush, func() {
		_, _, _ = dialogButtonDeleteObject.Call(brush)
	}
}

func acquireDialogButtonPen(
	dc uintptr,
	resources *dialogButtonPaintResources,
	color uint32,
	stroke int32,
) (uintptr, func()) {
	if stroke < 1 {
		stroke = 1
	}
	if stroke == 1 {
		pen := dialogButtonStockObjectFor(resources, dialogButtonDCPen)
		if pen != 0 {
			_, _, _ = dialogButtonSetDCPenColor.Call(dc, uintptr(color))
			return pen, releaseDialogButtonGDIObjectNothing
		}
	}

	key := dialogButtonPenKey{Color: color, Stroke: stroke}
	if resources != nil {
		resources.mu.Lock()
		if pen := resources.widePens[key]; pen != 0 {
			resources.mu.Unlock()
			return pen, releaseDialogButtonGDIObjectNothing
		}
		if len(resources.widePens) < dialogButtonWidePenCacheLimit {
			pen, _, _ := dialogButtonCreatePen.Call(
				dialogButtonPSStyleSolid,
				uintptr(stroke),
				uintptr(color),
			)
			if pen != 0 {
				if resources.widePens == nil {
					resources.widePens = make(
						map[dialogButtonPenKey]uintptr,
						dialogButtonWidePenCacheLimit,
					)
				}
				resources.widePens[key] = pen
				resources.mu.Unlock()
				return pen, releaseDialogButtonGDIObjectNothing
			}
		}
		resources.mu.Unlock()
	}

	pen, _, _ := dialogButtonCreatePen.Call(
		dialogButtonPSStyleSolid,
		uintptr(stroke),
		uintptr(color),
	)
	if pen == 0 {
		return 0, releaseDialogButtonGDIObjectNothing
	}
	return pen, func() {
		_, _, _ = dialogButtonDeleteObject.Call(pen)
	}
}

func acquireDialogButtonClipRegion(
	resources *dialogButtonPaintResources,
	key dialogButtonClipRegionKey,
	diameter int32,
) (uintptr, func()) {
	if resources != nil {
		resources.mu.Lock()
		if region := resources.clipRegions[key]; region != 0 {
			resources.mu.Unlock()
			return region, releaseDialogButtonGDIObjectNothing
		}
		if len(resources.clipRegions) < dialogButtonClipRegionCacheLimit {
			region := createDialogButtonClipRegion(key.Rect, diameter)
			if region != 0 {
				if resources.clipRegions == nil {
					resources.clipRegions = make(
						map[dialogButtonClipRegionKey]uintptr,
						dialogButtonClipRegionCacheLimit,
					)
				}
				resources.clipRegions[key] = region
				resources.mu.Unlock()
				return region, releaseDialogButtonGDIObjectNothing
			}
		}
		resources.mu.Unlock()
	}

	region := createDialogButtonClipRegion(key.Rect, diameter)
	if region == 0 {
		return 0, releaseDialogButtonGDIObjectNothing
	}
	return region, func() {
		_, _, _ = dialogButtonDeleteObject.Call(region)
	}
}

func createDialogButtonClipRegion(
	rect dialogButtonRect,
	diameter int32,
) uintptr {
	region, _, _ := dialogButtonCreateRoundRectRgn.Call(
		uintptr(rect.Left),
		uintptr(rect.Top),
		uintptr(rect.Right),
		uintptr(rect.Bottom),
		uintptr(diameter),
		uintptr(diameter),
	)
	return region
}

func dialogButtonNullBrushFor(
	resources *dialogButtonPaintResources,
) uintptr {
	return dialogButtonStockObjectFor(resources, dialogButtonNullBrush)
}

func releaseDialogButtonGDIObjectNothing() {}

func dialogControlParentBrush(dc, hwnd uintptr) uintptr {
	parent, _, _ := dialogButtonGetParent.Call(hwnd)
	if parent == 0 {
		return 0
	}
	brush, _, _ := dialogThemeSendMessage.Call(
		parent,
		dialogButtonWMControlColorStatic,
		dc,
		hwnd,
	)
	return brush
}

func drawDialogButtonBody(
	dc uintptr,
	rect dialogButtonRect,
	visual dialogButtonVisual,
	radius, stroke int32,
	pressed bool,
	dpi int32,
	resources *dialogButtonPaintResources,
) dialogButtonRect {
	face, shadow := dialogButtonFaceAndShadowRects(rect, pressed, dpi)
	drawDialogButtonRoundRect(
		dc,
		shadow,
		visual.Shadow,
		visual.Shadow,
		radius,
		stroke,
		resources,
	)
	if !fillDialogButtonGradientRoundRect(
		dc,
		face,
		visual.Top,
		visual.Bottom,
		radius,
		resources,
	) {
		drawDialogButtonRoundRect(
			dc,
			face,
			visual.Fill,
			visual.Border,
			radius,
			stroke,
			resources,
		)
	} else {
		strokeDialogButtonRoundRect(
			dc,
			face,
			visual.Border,
			radius,
			stroke,
			resources,
		)
	}
	inner := face
	inner.Left += stroke
	inner.Top += stroke
	inner.Right -= stroke
	inner.Bottom -= stroke
	if visual.Inner != 0 {
		strokeDialogButtonRoundRect(
			dc,
			inner,
			visual.Inner,
			radius-stroke,
			1,
			resources,
		)
	}
	return face
}

func dialogButtonFaceAndShadowRects(
	body dialogButtonRect,
	pressed bool,
	dpi int32,
) (face, shadow dialogButtonRect) {
	depth := max(int32(1), scaleDialogButtonMetric(1, dpi))
	face = body
	if face.Bottom-face.Top > depth+2 {
		face.Bottom -= depth
	}
	if pressed {
		face.Top += depth
		face.Bottom += depth
		if face.Bottom > body.Bottom {
			face.Bottom = body.Bottom
		}
	}
	shadow = face
	shadow.Top += depth
	shadow.Bottom += depth
	if shadow.Bottom > body.Bottom {
		shadow.Bottom = body.Bottom
	}
	return face, shadow
}

func fillDialogButtonGradientRoundRect(
	dc uintptr,
	rect dialogButtonRect,
	top, bottom uint32,
	radius int32,
	resources *dialogButtonPaintResources,
) bool {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return false
	}
	if dialogButtonGradientFill.Find() != nil {
		return false
	}
	if radius < 1 {
		radius = 1
	}
	diameter := radius * 2
	region, releaseRegion := acquireDialogButtonClipRegion(
		resources,
		dialogButtonClipRegionKey{Rect: rect, Radius: radius},
		diameter,
	)
	if region == 0 {
		return false
	}
	defer releaseRegion()
	saved, _, _ := dialogButtonSaveDC.Call(dc)
	if saved == 0 {
		return false
	}
	defer func() {
		_, _, _ = dialogButtonRestoreDC.Call(dc, ^uintptr(0))
	}()
	selected, _, _ := dialogButtonSelectClipRgn.Call(dc, region)
	if int32(selected) == -1 {
		return false
	}

	vertices := [2]dialogButtonGradientVertex{
		dialogButtonGradientVertexFor(rect.Left, rect.Top, top),
		dialogButtonGradientVertexFor(rect.Right, rect.Bottom, bottom),
	}
	gradient := dialogButtonGradientRect{UpperLeft: 0, LowerRight: 1}
	filled, _, _ := dialogButtonGradientFill.Call(
		dc,
		uintptr(unsafe.Pointer(&vertices[0])),
		uintptr(len(vertices)),
		uintptr(unsafe.Pointer(&gradient)),
		1,
		dialogButtonGradientFillRectV,
	)
	runtime.KeepAlive(&vertices)
	runtime.KeepAlive(&gradient)
	return filled != 0
}

func dialogButtonGradientVertexFor(
	x, y int32,
	color uint32,
) dialogButtonGradientVertex {
	return dialogButtonGradientVertex{
		X:     x,
		Y:     y,
		Red:   uint16(byte(color)) << 8,
		Green: uint16(byte(color>>8)) << 8,
		Blue:  uint16(byte(color>>16)) << 8,
		Alpha: 0,
	}
}

func dialogButtonBodyRect(
	client dialogButtonRect,
	dark bool,
	dpi int32,
) dialogButtonRect {
	if dark {
		// The dark control owns a rounded HWND region, so its body can cover
		// the entire child surface without first painting a mismatched
		// rectangular parent-color slab.
		if client.Right > client.Left {
			client.Right--
		}
		if client.Bottom > client.Top {
			client.Bottom--
		}
		return client
	}
	outerInset := dialogButtonStroke(dpi)
	client.Left += outerInset
	client.Top += outerInset
	client.Right -= outerInset
	client.Bottom -= outerInset
	return client
}

func updateDialogButtonRegion(hwnd uintptr, dark bool) {
	if hwnd == 0 || !isWindow(hwnd) {
		return
	}
	if !dark {
		_, _, _ = dialogButtonSetWindowRgn.Call(hwnd, 0, 1)
		return
	}

	var client dialogButtonRect
	ok, _, _ := dialogButtonGetClientRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&client)),
	)
	if ok == 0 || client.Right <= client.Left || client.Bottom <= client.Top {
		return
	}
	radius := scaleDialogButtonMetric(8, dialogButtonDPI(hwnd))
	diameter := radius * 2
	region, _, _ := dialogButtonCreateRoundRectRgn.Call(
		uintptr(client.Left),
		uintptr(client.Top),
		uintptr(client.Right+1),
		uintptr(client.Bottom+1),
		uintptr(diameter),
		uintptr(diameter),
	)
	if region == 0 {
		return
	}
	applied, _, _ := dialogButtonSetWindowRgn.Call(hwnd, region, 1)
	if applied == 0 {
		// SetWindowRgn owns the region only after success.
		_, _, _ = dialogButtonDeleteObject.Call(region)
	}
	runtime.KeepAlive(&client)
}

func drawDialogButtonContent(
	dc, hwnd uintptr,
	rect dialogButtonRect,
	color uint32,
	hidePrefix bool,
	dpi int32,
	iconFont uintptr,
) {
	raw, ok := dialogButtonWindowText(hwnd)
	if !ok {
		return
	}
	content := dialogButtonContentFor(raw)
	text, err := windows.UTF16FromString(content.Text)
	if err != nil || len(text) < 2 {
		return
	}
	textLength := uintptr(len(text) - 1)

	font, _, _ := dialogThemeSendMessage.Call(hwnd, dialogTabWMGetFont, 0, 0)
	var oldFont uintptr
	if font != 0 {
		oldFont, _, _ = dialogButtonSelectObject.Call(dc, font)
		defer func() {
			if oldFont != 0 {
				_, _, _ = dialogButtonSelectObject.Call(dc, oldFont)
			}
		}()
	}
	_, _, _ = dialogButtonSetBkMode.Call(dc, dialogButtonBkTransparent)
	_, _, _ = dialogButtonSetTextColor.Call(dc, uintptr(color))

	measure := dialogButtonRect{}
	_, _, _ = dialogButtonDrawText.Call(
		dc,
		uintptr(unsafe.Pointer(&text[0])),
		textLength,
		uintptr(unsafe.Pointer(&measure)),
		dialogButtonDTSingleLine|dialogButtonDTCalcRect,
	)
	textWidth := max(int32(0), measure.Right-measure.Left)
	iconRect, textRect, drawIcon := dialogButtonContentLayout(
		rect,
		textWidth,
		dpi,
		content.Icon != dialogButtonIconNone && iconFont != 0,
	)
	rtl := windowRTLReading(hwnd)
	if drawIcon && rtl {
		iconRect.Left, iconRect.Right = mirrorHorizontalEdges(
			rect.Left,
			rect.Right,
			iconRect.Left,
			iconRect.Right,
		)
		textRect.Left, textRect.Right = mirrorHorizontalEdges(
			rect.Left,
			rect.Right,
			textRect.Left,
			textRect.Right,
		)
	}
	if drawIcon {
		glyph := [2]uint16{uint16(content.Icon), 0}
		previous, _, _ := dialogButtonSelectObject.Call(dc, iconFont)
		if previous != 0 && previous != ^uintptr(0) {
			_, _, _ = dialogButtonDrawText.Call(
				dc,
				uintptr(unsafe.Pointer(&glyph[0])),
				1,
				uintptr(unsafe.Pointer(&iconRect)),
				dialogButtonDTCenter|
					dialogButtonDTVCenter|
					dialogButtonDTSingleLine|
					dialogButtonDTNoPrefix,
			)
			_, _, _ = dialogButtonSelectObject.Call(dc, previous)
		} else {
			drawIcon = false
			_, _, _ = dialogButtonSetTextColor.Call(dc, uintptr(color))
		}
		runtime.KeepAlive(&glyph)
		runtime.KeepAlive(&iconRect)
	}
	if !drawIcon {
		_, textRect, _ = dialogButtonContentLayout(
			rect,
			textWidth,
			dpi,
			false,
		)
	}
	format := uintptr(
		dialogButtonDTVCenter |
			dialogButtonDTSingleLine |
			dialogButtonDTEndEllipsis,
	)
	if !drawIcon {
		format |= dialogButtonDTCenter
	}
	if hidePrefix {
		format |= dialogButtonDTHidePrefix
	}
	if rtl {
		format |= dialogButtonDTRTLReading
	}
	_, _, _ = dialogButtonDrawText.Call(
		dc,
		uintptr(unsafe.Pointer(&text[0])),
		textLength,
		uintptr(unsafe.Pointer(&textRect)),
		format,
	)
	runtime.KeepAlive(text)
	runtime.KeepAlive(&textRect)
}

func dialogButtonWindowText(hwnd uintptr) (string, bool) {
	length, _, _ := dialogButtonGetWindowTextLength.Call(hwnd)
	if length == 0 || length > 4096 {
		return "", false
	}
	text := make([]uint16, length+1)
	copied, _, _ := dialogButtonGetWindowText.Call(
		hwnd,
		uintptr(unsafe.Pointer(&text[0])),
		uintptr(len(text)),
	)
	if copied == 0 {
		return "", false
	}
	return windows.UTF16ToString(text[:copied]), true
}

func drawDialogButtonRoundRect(
	dc uintptr,
	rect dialogButtonRect,
	fill, border uint32,
	radius, stroke int32,
	resources *dialogButtonPaintResources,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	brush, releaseBrush := acquireDialogButtonBrush(dc, resources, fill)
	pen, releasePen := acquireDialogButtonPen(
		dc,
		resources,
		border,
		stroke,
	)
	if brush == 0 || pen == 0 {
		releaseBrush()
		releasePen()
		return
	}
	oldBrush, _, _ := dialogButtonSelectObject.Call(dc, brush)
	oldPen, _, _ := dialogButtonSelectObject.Call(dc, pen)
	drawDialogButtonNativeRoundRect(dc, rect, radius)
	restoreDialogButtonObjects(dc, oldBrush, oldPen)
	releaseBrush()
	releasePen()
}

func strokeDialogButtonRoundRect(
	dc uintptr,
	rect dialogButtonRect,
	color uint32,
	radius, stroke int32,
	resources *dialogButtonPaintResources,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	pen, releasePen := acquireDialogButtonPen(
		dc,
		resources,
		color,
		stroke,
	)
	nullBrush := dialogButtonNullBrushFor(resources)
	if pen == 0 || nullBrush == 0 {
		releasePen()
		return
	}
	oldBrush, _, _ := dialogButtonSelectObject.Call(dc, nullBrush)
	oldPen, _, _ := dialogButtonSelectObject.Call(dc, pen)
	drawDialogButtonNativeRoundRect(dc, rect, radius)
	restoreDialogButtonObjects(dc, oldBrush, oldPen)
	releasePen()
}

func drawDialogButtonNativeRoundRect(
	dc uintptr,
	rect dialogButtonRect,
	radius int32,
) {
	if radius < 1 {
		radius = 1
	}
	diameter := uintptr(radius * 2)
	_, _, _ = dialogButtonRoundRect.Call(
		dc,
		uintptr(rect.Left),
		uintptr(rect.Top),
		uintptr(rect.Right),
		uintptr(rect.Bottom),
		diameter,
		diameter,
	)
}

func restoreDialogButtonObjects(dc, oldBrush, oldPen uintptr) {
	if oldPen != 0 {
		_, _, _ = dialogButtonSelectObject.Call(dc, oldPen)
	}
	if oldBrush != 0 {
		_, _, _ = dialogButtonSelectObject.Call(dc, oldBrush)
	}
}

func fillDialogButtonRect(
	dc uintptr,
	rect dialogButtonRect,
	color uint32,
	resources *dialogButtonPaintResources,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	brush, releaseBrush := acquireDialogButtonBrush(dc, resources, color)
	if brush == 0 {
		return
	}
	_, _, _ = dialogButtonFillRect.Call(
		dc,
		uintptr(unsafe.Pointer(&rect)),
		brush,
	)
	releaseBrush()
	runtime.KeepAlive(&rect)
}

func dialogButtonDPI(hwnd uintptr) int32 {
	dpi, _, _ := dialogButtonGetDPIForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

func scaleDialogButtonMetric(value, dpi int32) int32 {
	if value <= 0 {
		return 0
	}
	if dpi <= 0 {
		dpi = 96
	}
	return (value*dpi + 48) / 96
}

func dialogButtonStroke(dpi int32) int32 {
	if dpi >= 192 {
		return 2
	}
	return 1
}

func dialogButtonRGB(red, green, blue byte) uint32 {
	return uint32(red) | uint32(green)<<8 | uint32(blue)<<16
}

func invalidateDialogButton(hwnd uintptr) {
	_, _, _ = dialogButtonInvalidateRect.Call(hwnd, 0, 0)
}
