//go:build windows

package winfx

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/design"
)

const (
	dialogFieldSubclassID = 0x524C4644 // "RLFD"

	dialogFieldWMPaint        = 0x000F
	dialogFieldWMSetText      = 0x000C
	dialogFieldWMEnable       = 0x000A
	dialogFieldWMSetFocus     = 0x0007
	dialogFieldWMKillFocus    = 0x0008
	dialogFieldWMSize         = 0x0005
	dialogFieldWMSetFont      = 0x0030
	dialogFieldWMStyleChanged = 0x007D
	dialogFieldWMMouseMove    = 0x0200
	dialogFieldWMMouseLeave   = 0x02A3
	dialogFieldWMPrintClient  = 0x0318
	dialogFieldWMThemeChanged = 0x031A
	dialogFieldWMDPIChanged   = 0x02E0
	dialogFieldWMAnimate      = 0x8000 + 0x0543
	dialogFieldEMSetReadOnly  = 0x00CF
	dialogFieldEMSetMargins   = 0x00D3
	dialogFieldEMGetMargins   = 0x00D4

	dialogFieldGWLStyle   = -16
	dialogFieldGWLExStyle = -20

	dialogFieldWSExClientEdge = 0x00000200
	dialogFieldWSExStaticEdge = 0x00020000
	dialogFieldESMultiline    = 0x0004
	dialogFieldESPassword     = 0x0020
	dialogFieldESReadOnly     = 0x0800

	dialogFieldECLeftMargin  = 0x0001
	dialogFieldECRightMargin = 0x0002

	dialogFieldSWPNoSize       = 0x0001
	dialogFieldSWPNoMove       = 0x0002
	dialogFieldSWPNoZOrder     = 0x0004
	dialogFieldSWPNoActivate   = 0x0010
	dialogFieldSWPFrameChanged = 0x0020

	dialogFieldTMELeave      = 0x00000002
	dialogFieldNullBrush     = 5
	dialogFieldDCBrush       = 18
	dialogFieldDCPen         = 19
	dialogFieldBkTransparent = 1
	dialogFieldSRCCopy       = 0x00CC0020
)

type dialogFieldRect struct {
	Left, Top, Right, Bottom int32
}

type dialogFieldTrackMouse struct {
	Size      uint32
	Flags     uint32
	Window    uintptr
	HoverTime uint32
}

type dialogFieldThemeState struct {
	Dark            bool
	Animate         bool
	Hot             bool
	Focused         bool
	Tracking        bool
	Hover           float64
	Focus           float64
	LastTick        time.Time
	OriginalExStyle uint32
	OriginalMargins uintptr
	ChromeInstalled bool
}

type dialogFieldStyleTraits struct {
	Multiline bool
	Password  bool
	ReadOnly  bool
}

type dialogFieldAnimationScheduler struct {
	token   uint32
	stop    chan struct{}
	pending atomic.Bool
}

type dialogFieldPaintBuffer struct {
	width, height int32
	dpi           int32
	memory        uintptr
	bitmap        uintptr
	oldBitmap     uintptr
}

type dialogFieldPalette struct {
	Border         uint32
	BorderHot      uint32
	BorderFocus    uint32
	BorderDisabled uint32
	Inner          uint32
	InnerFocus     uint32
}

var (
	dialogFieldUser32          = windows.NewLazySystemDLL("user32.dll")
	dialogFieldGetWindowLong   = dialogFieldUser32.NewProc("GetWindowLongW")
	dialogFieldSetWindowLong   = dialogFieldUser32.NewProc("SetWindowLongW")
	dialogFieldSetWindowPos    = dialogFieldUser32.NewProc("SetWindowPos")
	dialogFieldSendMessage     = dialogFieldUser32.NewProc("SendMessageW")
	dialogFieldGetWindowRect   = dialogFieldUser32.NewProc("GetWindowRect")
	dialogFieldGetWindowDC     = dialogFieldUser32.NewProc("GetWindowDC")
	dialogFieldReleaseDC       = dialogFieldUser32.NewProc("ReleaseDC")
	dialogFieldGetDPIForWindow = dialogFieldUser32.NewProc("GetDpiForWindow")
	dialogFieldGetFocus        = dialogFieldUser32.NewProc("GetFocus")
	dialogFieldGetParent       = dialogFieldUser32.NewProc("GetParent")
	dialogFieldIsWindowEnabled = dialogFieldUser32.NewProc("IsWindowEnabled")
	dialogFieldPostMessage     = dialogFieldUser32.NewProc("PostMessageW")
	dialogFieldTrackMouseEvent = dialogFieldUser32.NewProc("TrackMouseEvent")
	dialogFieldSetWindowRgn    = dialogFieldUser32.NewProc("SetWindowRgn")

	dialogFieldGDI32               = windows.NewLazySystemDLL("gdi32.dll")
	dialogFieldCreateRoundRectRgn  = dialogFieldGDI32.NewProc("CreateRoundRectRgn")
	dialogFieldCreateCompatibleDC  = dialogFieldGDI32.NewProc("CreateCompatibleDC")
	dialogFieldCreateCompatibleBMP = dialogFieldGDI32.NewProc("CreateCompatibleBitmap")
	dialogFieldBitBlt              = dialogFieldGDI32.NewProc("BitBlt")
	dialogFieldDeleteDC            = dialogFieldGDI32.NewProc("DeleteDC")
	dialogFieldDeleteObject        = dialogFieldGDI32.NewProc("DeleteObject")
	dialogFieldGetStockObject      = dialogFieldGDI32.NewProc("GetStockObject")
	dialogFieldRoundRect           = dialogFieldGDI32.NewProc("RoundRect")
	dialogFieldSelectObject        = dialogFieldGDI32.NewProc("SelectObject")
	dialogFieldSetBkMode           = dialogFieldGDI32.NewProc("SetBkMode")
	dialogFieldSetDCBrushColor     = dialogFieldGDI32.NewProc("SetDCBrushColor")
	dialogFieldSetDCPenColor       = dialogFieldGDI32.NewProc("SetDCPenColor")

	dialogFieldMu     sync.RWMutex
	dialogFieldThemes = make(map[uintptr]dialogFieldThemeState)

	dialogFieldSchedulerMu       sync.Mutex
	dialogFieldSchedulers        = make(map[uintptr]*dialogFieldAnimationScheduler)
	dialogFieldSchedulerSequence uint32

	dialogFieldBufferMu sync.Mutex
	dialogFieldBuffers  = make(map[uintptr]*dialogFieldPaintBuffer)
)

// setDialogFieldTheme modernizes only the field's visual chrome. The EDIT
// window continues to receive every text, IME, selection, clipboard and
// accessibility message through DefSubclassProc.
func setDialogFieldTheme(
	hwnd uintptr,
	dark, animate, enabled bool,
) {
	if hwnd == 0 {
		return
	}
	if !enabled {
		clearDialogFieldTheme(hwnd)
		return
	}

	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if !exists {
		exStyle, _, _ := dialogFieldGetWindowLong.Call(
			hwnd,
			dialogFieldWindowLongIndex(dialogFieldGWLExStyle),
		)
		margins, _, _ := dialogFieldSendMessage.Call(
			hwnd,
			dialogFieldEMGetMargins,
			0,
			0,
		)
		focus, _, _ := dialogFieldGetFocus.Call()
		state.OriginalExStyle = uint32(exStyle)
		state.OriginalMargins = margins
		state.Focused = focus == hwnd
		if state.Focused {
			state.Focus = 1
		}
	}
	state.Dark = dark
	state.Animate = animate
	state.ChromeInstalled = true
	if !animate {
		state.Hover = dialogBoolLevel(state.Hot)
		state.Focus = dialogBoolLevel(state.Focused)
		state.LastTick = time.Time{}
	}
	dialogFieldThemes[hwnd] = state
	dialogFieldMu.Unlock()

	if !exists {
		if err := SetWindowSubclass(
			hwnd,
			dialogFieldSubclassID,
			dialogFieldWindowMessage,
		); err != nil && !errors.Is(err, ErrSubclassExists) {
			dialogFieldMu.Lock()
			delete(dialogFieldThemes, hwnd)
			dialogFieldMu.Unlock()
			return
		}
		installDialogFieldChrome(hwnd, state)
	}
	if !animate {
		stopDialogFieldAnimationScheduler(hwnd)
	}
	applyDialogFieldMargins(hwnd)
	updateDialogFieldRegion(hwnd)
	paintDialogFieldChrome(hwnd, 0)
}

func clearDialogFieldTheme(hwnd uintptr) {
	stopDialogFieldAnimationScheduler(hwnd)
	releaseDialogFieldPaintBuffer(hwnd)

	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	delete(dialogFieldThemes, hwnd)
	dialogFieldMu.Unlock()
	if !exists {
		return
	}

	if state.ChromeInstalled && isWindow(hwnd) {
		currentExStyle, _, _ := dialogFieldGetWindowLong.Call(
			hwnd,
			dialogFieldWindowLongIndex(dialogFieldGWLExStyle),
		)
		_, _, _ = dialogFieldSetWindowLong.Call(
			hwnd,
			dialogFieldWindowLongIndex(dialogFieldGWLExStyle),
			uintptr(dialogFieldRestoreExStyle(
				uint32(currentExStyle),
				state.OriginalExStyle,
			)),
		)
		_, _, _ = dialogFieldSendMessage.Call(
			hwnd,
			dialogFieldEMSetMargins,
			dialogFieldECLeftMargin|dialogFieldECRightMargin,
			state.OriginalMargins,
		)
		_, _, _ = dialogFieldSetWindowRgn.Call(hwnd, 0, 1)
		dialogFieldRefreshFrame(hwnd)
	}
	_ = RemoveWindowSubclass(hwnd, dialogFieldSubclassID)
}

func setDialogFieldMotion(hwnd uintptr, animate bool) {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if exists {
		state.Animate = animate
		if !animate {
			state.Hover = dialogBoolLevel(state.Hot)
			state.Focus = dialogBoolLevel(state.Focused)
			state.LastTick = time.Time{}
		}
		dialogFieldThemes[hwnd] = state
	}
	dialogFieldMu.Unlock()
	if !exists {
		return
	}
	if !animate {
		stopDialogFieldAnimationScheduler(hwnd)
	}
	paintDialogFieldChrome(hwnd, 0)
}

func installDialogFieldChrome(
	hwnd uintptr,
	state dialogFieldThemeState,
) {
	exStyle := dialogFieldChromeExStyle(state.OriginalExStyle)
	_, _, _ = dialogFieldSetWindowLong.Call(
		hwnd,
		dialogFieldWindowLongIndex(dialogFieldGWLExStyle),
		uintptr(exStyle),
	)
	dialogFieldRefreshFrame(hwnd)
}

func dialogFieldRefreshFrame(hwnd uintptr) {
	_, _, _ = dialogFieldSetWindowPos.Call(
		hwnd,
		0,
		0,
		0,
		0,
		0,
		dialogFieldSWPNoSize|
			dialogFieldSWPNoMove|
			dialogFieldSWPNoZOrder|
			dialogFieldSWPNoActivate|
			dialogFieldSWPFrameChanged,
	)
}

func dialogFieldWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	switch uint32(message) {
	case wmNCDestroy:
		stopDialogFieldAnimationScheduler(hwnd)
		releaseDialogFieldPaintBuffer(hwnd)
		dialogFieldMu.Lock()
		delete(dialogFieldThemes, hwnd)
		dialogFieldMu.Unlock()
		return 0, false

	case dialogFieldWMPaint:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		paintDialogFieldChrome(hwnd, 0)
		return result, true

	case dialogFieldWMPrintClient:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if wParam != 0 {
			paintDialogFieldChrome(hwnd, wParam)
		}
		return result, true

	case dialogFieldWMAnimate:
		if consumeDialogFieldAnimationFrame(hwnd, uint32(wParam)) {
			if !advanceDialogFieldAnimation(hwnd, time.Now()) {
				stopDialogFieldAnimationScheduler(hwnd)
			}
			return 0, true
		}
		return 0, true

	case dialogFieldWMMouseMove:
		setDialogFieldHot(hwnd, true)
		beginDialogFieldMouseTracking(hwnd)

	case dialogFieldWMMouseLeave:
		endDialogFieldMouseTracking(hwnd)
		setDialogFieldHot(hwnd, false)

	case dialogFieldWMSetFocus:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		setDialogFieldFocused(hwnd, true)
		return result, true

	case dialogFieldWMKillFocus:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		setDialogFieldFocused(hwnd, false)
		return result, true

	case dialogFieldWMSize, dialogFieldWMDPIChanged:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			releaseDialogFieldPaintBuffer(hwnd)
			applyDialogFieldMargins(hwnd)
			updateDialogFieldRegion(hwnd)
			paintDialogFieldChrome(hwnd, 0)
		}
		return result, true

	case dialogFieldWMSetText,
		dialogFieldWMEnable,
		dialogFieldWMThemeChanged,
		dialogFieldEMSetReadOnly:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			paintDialogFieldChrome(hwnd, 0)
		}
		return result, true

	case dialogFieldWMSetFont, dialogFieldWMStyleChanged:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			applyDialogFieldMargins(hwnd)
			updateDialogFieldRegion(hwnd)
			paintDialogFieldChrome(hwnd, 0)
		}
		return result, true
	}

	// Deliberately do not consume keyboard, character, IME, clipboard,
	// selection, undo, cue-banner, password, validation, dialog-code or
	// accessibility messages.
	return 0, false
}

func setDialogFieldHot(hwnd uintptr, hot bool) {
	dialogFieldSetTarget(hwnd, &hot, nil)
}

func setDialogFieldFocused(hwnd uintptr, focused bool) {
	dialogFieldSetTarget(hwnd, nil, &focused)
}

func dialogFieldSetTarget(
	hwnd uintptr,
	hot, focused *bool,
) {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	changed := false
	if exists && hot != nil && state.Hot != *hot {
		state.Hot = *hot
		changed = true
	}
	if exists && focused != nil && state.Focused != *focused {
		state.Focused = *focused
		changed = true
	}
	active := false
	if changed {
		if state.Animate {
			state.LastTick = time.Now()
			active = state.Hover != dialogBoolLevel(state.Hot) ||
				state.Focus != dialogBoolLevel(state.Focused)
		} else {
			state.Hover = dialogBoolLevel(state.Hot)
			state.Focus = dialogBoolLevel(state.Focused)
			state.LastTick = time.Time{}
		}
		dialogFieldThemes[hwnd] = state
	}
	dialogFieldMu.Unlock()

	if !changed {
		return
	}
	paintDialogFieldChrome(hwnd, 0)
	if active {
		if !startDialogFieldAnimationScheduler(hwnd) {
			settleDialogFieldAnimation(hwnd)
			paintDialogFieldChrome(hwnd, 0)
		}
	}
}

func advanceDialogFieldAnimation(hwnd uintptr, now time.Time) bool {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if !exists {
		dialogFieldMu.Unlock()
		return false
	}
	elapsed := dialogMotionTimerInterval
	if !state.LastTick.IsZero() {
		elapsed = now.Sub(state.LastTick)
	}
	state.LastTick = now
	var hoverActive, focusActive bool
	state.Hover, hoverActive = advanceDialogMotion(
		state.Hover,
		dialogBoolLevel(state.Hot),
		elapsed,
		design.Default.Motion.FastMS,
	)
	state.Focus, focusActive = advanceDialogMotion(
		state.Focus,
		dialogBoolLevel(state.Focused),
		elapsed,
		design.Default.Motion.FastMS,
	)
	active := hoverActive || focusActive
	if !active {
		state.LastTick = time.Time{}
	}
	dialogFieldThemes[hwnd] = state
	dialogFieldMu.Unlock()

	paintDialogFieldChrome(hwnd, 0)
	return active
}

func settleDialogFieldAnimation(hwnd uintptr) {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if exists {
		state.Hover = dialogBoolLevel(state.Hot)
		state.Focus = dialogBoolLevel(state.Focused)
		state.LastTick = time.Time{}
		dialogFieldThemes[hwnd] = state
	}
	dialogFieldMu.Unlock()
}

func startDialogFieldAnimationScheduler(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	dialogFieldSchedulerMu.Lock()
	if dialogFieldSchedulers[hwnd] != nil {
		dialogFieldSchedulerMu.Unlock()
		return true
	}
	token := atomic.AddUint32(&dialogFieldSchedulerSequence, 1)
	if token == 0 {
		token = atomic.AddUint32(&dialogFieldSchedulerSequence, 1)
	}
	scheduler := &dialogFieldAnimationScheduler{
		token: token,
		stop:  make(chan struct{}),
	}
	dialogFieldSchedulers[hwnd] = scheduler
	dialogFieldSchedulerMu.Unlock()

	go runDialogFieldAnimationScheduler(hwnd, scheduler)
	return true
}

func runDialogFieldAnimationScheduler(
	hwnd uintptr,
	scheduler *dialogFieldAnimationScheduler,
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
			posted, _, _ := dialogFieldPostMessage.Call(
				hwnd,
				dialogFieldWMAnimate,
				uintptr(scheduler.token),
				0,
			)
			if posted == 0 {
				scheduler.pending.Store(false)
			}
		}
	}
}

func consumeDialogFieldAnimationFrame(hwnd uintptr, token uint32) bool {
	dialogFieldSchedulerMu.Lock()
	scheduler := dialogFieldSchedulers[hwnd]
	valid := scheduler != nil && scheduler.token == token
	if valid {
		scheduler.pending.Store(false)
	}
	dialogFieldSchedulerMu.Unlock()
	return valid
}

func stopDialogFieldAnimationScheduler(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogFieldSchedulerMu.Lock()
	scheduler := dialogFieldSchedulers[hwnd]
	delete(dialogFieldSchedulers, hwnd)
	dialogFieldSchedulerMu.Unlock()
	if scheduler != nil {
		close(scheduler.stop)
		scheduler.pending.Store(false)
	}
}

func beginDialogFieldMouseTracking(hwnd uintptr) {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if !exists || state.Tracking {
		dialogFieldMu.Unlock()
		return
	}
	state.Tracking = true
	dialogFieldThemes[hwnd] = state
	dialogFieldMu.Unlock()

	tracking := dialogFieldTrackMouse{
		Size:   uint32(unsafe.Sizeof(dialogFieldTrackMouse{})),
		Flags:  dialogFieldTMELeave,
		Window: hwnd,
	}
	_, _, _ = dialogFieldTrackMouseEvent.Call(
		uintptr(unsafe.Pointer(&tracking)),
	)
	runtime.KeepAlive(&tracking)
}

func endDialogFieldMouseTracking(hwnd uintptr) {
	dialogFieldMu.Lock()
	state, exists := dialogFieldThemes[hwnd]
	if exists {
		state.Tracking = false
		dialogFieldThemes[hwnd] = state
	}
	dialogFieldMu.Unlock()
}

func applyDialogFieldMargins(hwnd uintptr) {
	dpi := dialogFieldDPI(hwnd)
	margin := scaleDialogFieldMetric(9, dpi)
	if dialogFieldTraits(dialogWindowStyle(hwnd)).Multiline {
		margin = scaleDialogFieldMetric(10, dpi)
	}
	packed := uintptr(uint16(margin)) | uintptr(uint16(margin))<<16
	_, _, _ = dialogFieldSendMessage.Call(
		hwnd,
		dialogFieldEMSetMargins,
		dialogFieldECLeftMargin|dialogFieldECRightMargin,
		packed,
	)
}

func updateDialogFieldRegion(hwnd uintptr) {
	if hwnd == 0 || !isWindow(hwnd) {
		return
	}
	var window dialogFieldRect
	ok, _, _ := dialogFieldGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&window)),
	)
	width := window.Right - window.Left
	height := window.Bottom - window.Top
	if ok == 0 || width < 1 || height < 1 {
		return
	}
	radius := dialogFieldRadius(
		width,
		height,
		dialogFieldTraits(dialogWindowStyle(hwnd)).Multiline,
		dialogFieldDPI(hwnd),
	)
	region, _, _ := dialogFieldCreateRoundRectRgn.Call(
		0,
		0,
		uintptr(width+1),
		uintptr(height+1),
		uintptr(radius*2),
		uintptr(radius*2),
	)
	if region == 0 {
		return
	}
	applied, _, _ := dialogFieldSetWindowRgn.Call(hwnd, region, 1)
	if applied == 0 {
		_, _, _ = dialogFieldDeleteObject.Call(region)
	}
	runtime.KeepAlive(&window)
}

func paintDialogFieldChrome(hwnd, suppliedDC uintptr) bool {
	state, exists := dialogFieldTheme(hwnd)
	if !exists || hwnd == 0 {
		return false
	}

	var window dialogFieldRect
	ok, _, _ := dialogFieldGetWindowRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&window)),
	)
	width := window.Right - window.Left
	height := window.Bottom - window.Top
	if ok == 0 || width < 2 || height < 2 {
		return false
	}

	dc := suppliedDC
	if dc == 0 {
		dc, _, _ = dialogFieldGetWindowDC.Call(hwnd)
		if dc == 0 {
			return false
		}
		defer dialogFieldReleaseDC.Call(hwnd, dc)
	}
	targetDC := dc
	dpi := dialogFieldDPI(hwnd)
	if buffer := acquireDialogFieldPaintBuffer(
		hwnd,
		targetDC,
		width,
		height,
		dpi,
	); buffer != nil {
		_, _, _ = dialogFieldBitBlt.Call(
			buffer.memory,
			0,
			0,
			uintptr(width),
			uintptr(height),
			targetDC,
			0,
			0,
			dialogFieldSRCCopy,
		)
		dc = buffer.memory
		defer func() {
			_, _, _ = dialogFieldBitBlt.Call(
				targetDC,
				0,
				0,
				uintptr(width),
				uintptr(height),
				buffer.memory,
				0,
				0,
				dialogFieldSRCCopy,
			)
		}()
	}

	palette := dialogFieldColors(state.Dark)
	enabled, _, _ := dialogFieldIsWindowEnabled.Call(hwnd)
	traits := dialogFieldTraits(dialogWindowStyle(hwnd))
	border := dialogFieldAnimatedBorder(
		palette,
		state.Hover,
		state.Focus,
		enabled != 0,
		traits.ReadOnly,
	)
	multiline := traits.Multiline
	radius := dialogFieldRadius(width, height, multiline, dpi)
	// Keep the physical stroke footprint constant across the transition.
	// Changing from one to two pixels would leave a stale inner pixel when
	// the retained buffer seeds the next frame from the previous one.
	stroke := dialogFieldStroke(dpi, 1)

	_, _, _ = dialogFieldSetBkMode.Call(dc, dialogFieldBkTransparent)
	rect := dialogFieldRect{Right: width, Bottom: height}
	strokeDialogFieldRoundRect(dc, rect, border, radius, stroke)

	// The inset highlight gives the field depth without obscuring native edit
	// content. It is intentionally subtle in dark mode and strengthens only
	// while the keyboard focus ring enters.
	inset := dialogFieldStroke(dpi, 1)
	inner := rect
	inner.Left += inset
	inner.Top += inset
	inner.Right -= inset
	inner.Bottom -= inset
	innerColor := dialogMixColor(
		palette.Inner,
		palette.InnerFocus,
		dialogEase(state.Focus),
	)
	strokeDialogFieldRoundRect(
		dc,
		inner,
		innerColor,
		maxDialogFieldInt32(1, radius-inset),
		dialogFieldStroke(dpi, 0),
	)
	runtime.KeepAlive(&window)
	return true
}

func strokeDialogFieldRoundRect(
	dc uintptr,
	rect dialogFieldRect,
	color uint32,
	radius, stroke int32,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	pen, _, _ := dialogFieldGetStockObject.Call(dialogFieldDCPen)
	nullBrush, _, _ := dialogFieldGetStockObject.Call(dialogFieldNullBrush)
	if pen == 0 || nullBrush == 0 {
		return
	}
	oldPen, _, _ := dialogFieldSelectObject.Call(dc, pen)
	oldBrush, _, _ := dialogFieldSelectObject.Call(dc, nullBrush)
	_, _, _ = dialogFieldSetDCPenColor.Call(dc, uintptr(color))
	if stroke < 1 {
		stroke = 1
	}
	for layer := int32(0); layer < stroke; layer++ {
		layerRect := rect
		layerRect.Left += layer
		layerRect.Top += layer
		layerRect.Right -= layer
		layerRect.Bottom -= layer
		layerRadius := maxDialogFieldInt32(1, radius-layer)
		diameter := uintptr(layerRadius * 2)
		_, _, _ = dialogFieldRoundRect.Call(
			dc,
			uintptr(layerRect.Left),
			uintptr(layerRect.Top),
			uintptr(layerRect.Right),
			uintptr(layerRect.Bottom),
			diameter,
			diameter,
		)
	}
	if oldBrush != 0 {
		_, _, _ = dialogFieldSelectObject.Call(dc, oldBrush)
	}
	if oldPen != 0 {
		_, _, _ = dialogFieldSelectObject.Call(dc, oldPen)
	}
}

func acquireDialogFieldPaintBuffer(
	hwnd, targetDC uintptr,
	width, height, dpi int32,
) *dialogFieldPaintBuffer {
	if hwnd == 0 || targetDC == 0 || width < 1 || height < 1 {
		return nil
	}
	if dpi <= 0 {
		dpi = 96
	}

	dialogFieldBufferMu.Lock()
	buffer := dialogFieldBuffers[hwnd]
	if buffer != nil &&
		!dialogFieldPaintBufferCompatible(buffer, width, height, dpi) {
		delete(dialogFieldBuffers, hwnd)
	}
	dialogFieldBufferMu.Unlock()
	if buffer != nil &&
		!dialogFieldPaintBufferCompatible(buffer, width, height, dpi) {
		closeDialogFieldPaintBuffer(buffer)
		buffer = nil
	}
	if buffer != nil {
		return buffer
	}

	buffer = newDialogFieldPaintBuffer(targetDC, width, height, dpi)
	if buffer == nil {
		return nil
	}
	dialogFieldBufferMu.Lock()
	if existing := dialogFieldBuffers[hwnd]; existing != nil {
		dialogFieldBufferMu.Unlock()
		closeDialogFieldPaintBuffer(buffer)
		return existing
	}
	dialogFieldBuffers[hwnd] = buffer
	dialogFieldBufferMu.Unlock()
	return buffer
}

func dialogFieldPaintBufferCompatible(
	buffer *dialogFieldPaintBuffer,
	width, height, dpi int32,
) bool {
	return buffer != nil &&
		buffer.memory != 0 &&
		buffer.bitmap != 0 &&
		buffer.width == width &&
		buffer.height == height &&
		buffer.dpi == dpi
}

func newDialogFieldPaintBuffer(
	targetDC uintptr,
	width, height, dpi int32,
) *dialogFieldPaintBuffer {
	memory, _, _ := dialogFieldCreateCompatibleDC.Call(targetDC)
	if memory == 0 {
		return nil
	}
	bitmap, _, _ := dialogFieldCreateCompatibleBMP.Call(
		targetDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogFieldDeleteDC.Call(memory)
		return nil
	}
	oldBitmap, _, _ := dialogFieldSelectObject.Call(memory, bitmap)
	if oldBitmap == 0 || oldBitmap == ^uintptr(0) {
		_, _, _ = dialogFieldDeleteObject.Call(bitmap)
		_, _, _ = dialogFieldDeleteDC.Call(memory)
		return nil
	}
	return &dialogFieldPaintBuffer{
		width:     width,
		height:    height,
		dpi:       dpi,
		memory:    memory,
		bitmap:    bitmap,
		oldBitmap: oldBitmap,
	}
}

func releaseDialogFieldPaintBuffer(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogFieldBufferMu.Lock()
	buffer := dialogFieldBuffers[hwnd]
	delete(dialogFieldBuffers, hwnd)
	dialogFieldBufferMu.Unlock()
	closeDialogFieldPaintBuffer(buffer)
}

func closeDialogFieldPaintBuffer(buffer *dialogFieldPaintBuffer) {
	if buffer == nil {
		return
	}
	if buffer.memory != 0 &&
		buffer.oldBitmap != 0 &&
		buffer.oldBitmap != ^uintptr(0) {
		_, _, _ = dialogFieldSelectObject.Call(
			buffer.memory,
			buffer.oldBitmap,
		)
	}
	if buffer.bitmap != 0 {
		_, _, _ = dialogFieldDeleteObject.Call(buffer.bitmap)
	}
	if buffer.memory != 0 {
		_, _, _ = dialogFieldDeleteDC.Call(buffer.memory)
	}
	buffer.bitmap = 0
	buffer.memory = 0
}

func dialogFieldColors(dark bool) dialogFieldPalette {
	if dark {
		tokens := design.Default.Colors.Dark
		return dialogFieldPalette{
			Border:         dialogFieldToken(tokens.Stroke),
			BorderHot:      dialogFieldTokenMix(tokens.Stroke, tokens.AccentHover, 0.62),
			BorderFocus:    dialogFieldToken(tokens.AccentHover),
			BorderDisabled: dialogFieldTokenMix(tokens.Stroke, tokens.Surface, 0.58),
			Inner:          dialogFieldTokenMix(tokens.SurfaceHigh, tokens.StrokeHighlight, 0.30),
			InnerFocus:     dialogFieldTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.34),
		}
	}
	tokens := design.Default.Colors.Light
	return dialogFieldPalette{
		Border:         dialogFieldToken(tokens.Stroke),
		BorderHot:      dialogFieldTokenMix(tokens.Stroke, tokens.Accent, 0.64),
		BorderFocus:    dialogFieldToken(tokens.Accent),
		BorderDisabled: dialogFieldTokenMix(tokens.Stroke, tokens.Canvas, 0.58),
		Inner:          dialogFieldToken(tokens.StrokeHighlight),
		InnerFocus:     dialogFieldTokenMix(tokens.StrokeHighlight, tokens.Accent, 0.26),
	}
}

func dialogFieldAnimatedBorder(
	palette dialogFieldPalette,
	hover, focus float64,
	enabled, readOnly bool,
) uint32 {
	if !enabled {
		return palette.BorderDisabled
	}
	border := dialogMixColor(
		palette.Border,
		palette.BorderHot,
		dialogEase(hover),
	)
	border = dialogMixColor(
		border,
		palette.BorderFocus,
		dialogEase(focus),
	)
	if readOnly {
		border = dialogMixColor(border, palette.BorderDisabled, 0.26)
	}
	return border
}

func dialogFieldTheme(hwnd uintptr) (dialogFieldThemeState, bool) {
	dialogFieldMu.RLock()
	state, exists := dialogFieldThemes[hwnd]
	dialogFieldMu.RUnlock()
	return state, exists
}

func dialogFieldDPI(hwnd uintptr) int32 {
	dpi, _, _ := dialogFieldGetDPIForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

func dialogFieldWindowLongIndex(index int32) uintptr {
	return uintptr(uint32(index))
}

func dialogFieldChromeExStyle(original uint32) uint32 {
	return original &
		^(uint32(dialogFieldWSExClientEdge) | uint32(dialogFieldWSExStaticEdge))
}

func dialogFieldRestoreExStyle(current, original uint32) uint32 {
	const edgeMask = uint32(
		dialogFieldWSExClientEdge |
			dialogFieldWSExStaticEdge,
	)
	return current&^edgeMask | original&edgeMask
}

func dialogFieldTraits(style uint32) dialogFieldStyleTraits {
	return dialogFieldStyleTraits{
		Multiline: style&dialogFieldESMultiline != 0,
		Password:  style&dialogFieldESPassword != 0,
		ReadOnly:  style&dialogFieldESReadOnly != 0,
	}
}

func dialogFieldRadius(
	width, height int32,
	_ bool,
	dpi int32,
) int32 {
	radius := scaleDialogFieldMetric(
		int32(design.Default.Radii.ControlPX),
		dpi,
	)
	maximum := minDialogFieldInt32(width, height) / 2
	if radius > maximum {
		radius = maximum
	}
	return maxDialogFieldInt32(2, radius)
}

func dialogFieldStroke(dpi int32, focus float64) int32 {
	if dpi >= 168 || focus >= 0.55 {
		return 2
	}
	return 1
}

func scaleDialogFieldMetric(value, dpi int32) int32 {
	if dpi <= 0 {
		dpi = 96
	}
	if value <= 0 {
		return 0
	}
	return (value*dpi + 48) / 96
}

func dialogFieldToken(value string) uint32 {
	color := design.MustColor(value)
	return dialogFieldRGB(color.R, color.G, color.B)
}

func dialogFieldTokenMix(first, second string, secondWeight float64) uint32 {
	a := design.MustColor(first)
	b := design.MustColor(second)
	return dialogFieldRGB(
		dialogFieldMixByte(a.R, b.R, secondWeight),
		dialogFieldMixByte(a.G, b.G, secondWeight),
		dialogFieldMixByte(a.B, b.B, secondWeight),
	)
}

func dialogFieldMixByte(first, second byte, secondWeight float64) byte {
	secondWeight = dialogClamp(secondWeight)
	return byte(
		float64(first)*(1-secondWeight) +
			float64(second)*secondWeight +
			0.5,
	)
}

func dialogFieldRGB(red, green, blue byte) uint32 {
	return uint32(red) | uint32(green)<<8 | uint32(blue)<<16
}

func dialogBoolLevel(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func isDialogFieldClass(className string) bool {
	switch strings.ToLower(strings.TrimSpace(className)) {
	case "edit", "richedit", "richedit20w", "richedit50w":
		return true
	default:
		return false
	}
}

func isDialogComboClass(className string) bool {
	return strings.EqualFold(strings.TrimSpace(className), "ComboBox")
}

func isDialogComboEditChild(hwnd uintptr) bool {
	parent, _, _ := dialogFieldGetParent.Call(hwnd)
	if parent == 0 {
		return false
	}
	return isDialogComboClass(classNameForDialogControl(parent))
}

func minDialogFieldInt32(first, second int32) int32 {
	if first < second {
		return first
	}
	return second
}

func maxDialogFieldInt32(first, second int32) int32 {
	if first > second {
		return first
	}
	return second
}
