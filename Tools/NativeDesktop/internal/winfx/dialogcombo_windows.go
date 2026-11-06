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

	"pccontroller.local/desktop/internal/design"
)

const (
	dialogComboSubclassID     = 0x524C4342 // "RLCB"
	dialogComboListSubclassID = 0x524C434C // "RLCL"

	dialogComboWMPaint            = 0x000F
	dialogComboWMEraseBkgnd       = 0x0014
	dialogComboWMShowWindow       = 0x0018
	dialogComboWMCancelMode       = 0x001F
	dialogComboWMWindowPosChanged = 0x0047
	dialogComboWMSetText          = 0x000C
	dialogComboWMEnable           = 0x000A
	dialogComboWMSetFocus         = 0x0007
	dialogComboWMKillFocus        = 0x0008
	dialogComboWMSize             = 0x0005
	dialogComboWMSetFont          = 0x0030
	dialogComboWMStyleChanged     = 0x007D
	dialogComboWMKeyDown          = 0x0100
	dialogComboWMKeyUp            = 0x0101
	dialogComboWMChar             = 0x0102
	dialogComboWMSysKeyDown       = 0x0104
	dialogComboWMSysKeyUp         = 0x0105
	dialogComboWMLButtonDown      = 0x0201
	dialogComboWMLButtonUp        = 0x0202
	dialogComboWMMouseMove        = 0x0200
	dialogComboWMMouseLeave       = 0x02A3
	dialogComboWMPrintClient      = 0x0318
	dialogComboWMThemeChanged     = 0x031A
	dialogComboWMDPIChanged       = 0x02E0
	dialogComboWMAnimate          = 0x8000 + 0x0544

	dialogComboCBShowDropdown    = 0x014F
	dialogComboCBSetCurSel       = 0x014E
	dialogComboCBSelectString    = 0x014D
	dialogComboCBGetDroppedState = 0x0157
	dialogComboCBGetComboBoxInfo = 0x0164
	dialogComboCBSetCueBanner    = 0x1703

	dialogComboPRFClient     = 0x00000004
	dialogComboPRFEraseBkgnd = 0x00000008
	dialogComboPRFChildren   = 0x00000010
	dialogComboSRCCopy       = 0x00CC0020
	dialogComboTMELeave      = 0x00000002
)

type dialogComboBoxInfo struct {
	Size        uint32
	Item        dialogFieldRect
	Button      dialogFieldRect
	ButtonState uint32
	WindowCombo uintptr
	WindowItem  uintptr
	WindowList  uintptr
}

type dialogComboPoint struct {
	X, Y int32
}

type dialogComboPaintStruct struct {
	DC          uintptr
	Erase       int32
	Paint       dialogFieldRect
	Restore     int32
	Incremental int32
	Reserved    [32]byte
}

type dialogComboThemeState struct {
	Dark     bool
	Animate  bool
	Hot      bool
	Focused  bool
	Dropped  bool
	Tracking bool
	Hover    float64
	Focus    float64
	Open     float64
	LastTick time.Time
}

type dialogComboPalette struct {
	Button         uint32
	ButtonHot      uint32
	ButtonOpen     uint32
	Border         uint32
	BorderHot      uint32
	BorderFocus    uint32
	BorderDisabled uint32
	Divider        uint32
	Glyph          uint32
	GlyphDisabled  uint32
}

type dialogComboAnimationScheduler struct {
	token   uint32
	stop    chan struct{}
	pending atomic.Bool
}

var (
	dialogComboUser32          = windows.NewLazySystemDLL("user32.dll")
	dialogComboBeginPaint      = dialogComboUser32.NewProc("BeginPaint")
	dialogComboEndPaint        = dialogComboUser32.NewProc("EndPaint")
	dialogComboGetClientRect   = dialogComboUser32.NewProc("GetClientRect")
	dialogComboGetWindowRect   = dialogComboUser32.NewProc("GetWindowRect")
	dialogComboGetDPIForWindow = dialogComboUser32.NewProc("GetDpiForWindow")
	dialogComboGetFocus        = dialogComboUser32.NewProc("GetFocus")
	dialogComboGetParent       = dialogComboUser32.NewProc("GetParent")
	dialogComboIsWindowEnabled = dialogComboUser32.NewProc("IsWindowEnabled")
	dialogComboSendMessage     = dialogComboUser32.NewProc("SendMessageW")
	dialogComboFillRect        = dialogComboUser32.NewProc("FillRect")
	dialogComboPostMessage     = dialogComboUser32.NewProc("PostMessageW")
	dialogComboTrackMouseEvent = dialogComboUser32.NewProc("TrackMouseEvent")
	dialogComboSetWindowRgn    = dialogComboUser32.NewProc("SetWindowRgn")
	dialogComboInvalidateRect  = dialogComboUser32.NewProc("InvalidateRect")
	dialogComboRedrawWindow    = dialogComboUser32.NewProc("RedrawWindow")

	dialogComboGDI32              = windows.NewLazySystemDLL("gdi32.dll")
	dialogComboCreateRoundRectRgn = dialogComboGDI32.NewProc("CreateRoundRectRgn")
	dialogComboDeleteObject       = dialogComboGDI32.NewProc("DeleteObject")
	dialogComboMoveToEx           = dialogComboGDI32.NewProc("MoveToEx")
	dialogComboLineTo             = dialogComboGDI32.NewProc("LineTo")
	dialogComboSelectObject       = dialogComboGDI32.NewProc("SelectObject")
	dialogComboSaveDC             = dialogComboGDI32.NewProc("SaveDC")
	dialogComboRestoreDC          = dialogComboGDI32.NewProc("RestoreDC")
	dialogComboSelectClipRgn      = dialogComboGDI32.NewProc("SelectClipRgn")

	dialogComboMu     sync.RWMutex
	dialogComboThemes = make(map[uintptr]dialogComboThemeState)

	dialogComboPopupMu     sync.RWMutex
	dialogComboPopupOwners = make(map[uintptr]uintptr)
	dialogComboPopupThemes = make(map[uintptr]bool)
	dialogComboPopupReady  = make(map[uintptr]bool)

	dialogComboSchedulerMu       sync.Mutex
	dialogComboSchedulers        = make(map[uintptr]*dialogComboAnimationScheduler)
	dialogComboSchedulerSequence uint32
)

// setDialogComboTheme deliberately keeps the native COMBOBOX and ComboLBox
// controls. Retrofitting CBS_OWNERDRAW after Walk populated a string model can
// change item-data ownership and break binding, accessibility and type-ahead.
// We custom-render only the stable chrome and let the themed native list own
// item layout, selection and keyboard navigation.
func setDialogComboTheme(
	hwnd uintptr,
	dark, animate, enabled bool,
) {
	if hwnd == 0 {
		return
	}
	if !enabled {
		clearDialogComboTheme(hwnd)
		return
	}

	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if !exists {
		state.Focused = dialogComboHasFocus(hwnd)
		state.Dropped = dialogComboDropped(hwnd)
		state.Focus = dialogBoolLevel(state.Focused)
		state.Open = dialogBoolLevel(state.Dropped)
	}
	state.Dark = dark
	state.Animate = animate
	if !animate {
		state.Hover = dialogBoolLevel(state.Hot)
		state.Focus = dialogBoolLevel(state.Focused)
		state.Open = dialogBoolLevel(state.Dropped)
		state.LastTick = time.Time{}
	}
	dialogComboThemes[hwnd] = state
	dialogComboMu.Unlock()

	if !exists {
		if err := SetWindowSubclass(
			hwnd,
			dialogComboSubclassID,
			dialogComboWindowMessage,
		); err != nil && !errors.Is(err, ErrSubclassExists) {
			dialogComboMu.Lock()
			delete(dialogComboThemes, hwnd)
			dialogComboMu.Unlock()
			return
		}
	}
	if !animate {
		stopDialogComboAnimationScheduler(hwnd)
	}
	updateDialogComboRegion(hwnd)
	// Our colored focus ring replaces only the native dotted rectangle.
	callDefSubclassProc(hwnd, 0x0128 /* WM_UPDATEUISTATE */, 0x00010001 /* SET HIDEFOCUS */, 0)
	themeDialogComboPopup(hwnd, dark)
	presentDialogComboChrome(hwnd)
}

func clearDialogComboTheme(hwnd uintptr) {
	stopDialogComboAnimationScheduler(hwnd)
	releaseDialogFieldPaintBuffer(hwnd)
	dialogComboMu.Lock()
	_, exists := dialogComboThemes[hwnd]
	delete(dialogComboThemes, hwnd)
	dialogComboMu.Unlock()
	if !exists {
		return
	}
	if isWindow(hwnd) {
		_, _, _ = dialogComboSetWindowRgn.Call(hwnd, 0, 1)
		parent, _, _ := dialogComboGetParent.Call(hwnd)
		ui, _, _ := dialogThemeSendMessage.Call(parent, 0x0129, 0, 0)
		action := uintptr(2) // UIS_CLEAR
		if ui&1 != 0 {
			action = 1
		}
		callDefSubclassProc(hwnd, 0x0128, action|0x00010000, 0)
		clearDialogComboPopup(hwnd)
	}
	_ = RemoveWindowSubclass(hwnd, dialogComboSubclassID)
}

func setDialogComboMotion(hwnd uintptr, animate bool) {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if exists {
		state.Animate = animate
		if !animate {
			state.Hover = dialogBoolLevel(state.Hot)
			state.Focus = dialogBoolLevel(state.Focused)
			state.Open = dialogBoolLevel(state.Dropped)
			state.LastTick = time.Time{}
		}
		dialogComboThemes[hwnd] = state
	}
	dialogComboMu.Unlock()
	if !exists {
		return
	}
	if !animate {
		stopDialogComboAnimationScheduler(hwnd)
	}
	presentDialogComboChrome(hwnd)
}

func dialogComboWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	if hot, pointerMessage := dialogComboNativePointerTarget(
		uint32(message),
	); pointerMessage {
		// USER32 owns the actual COMBOBOX hover/capture lifecycle. Let it
		// settle first, then commit our buffered chrome in the same dispatch;
		// painting before DefSubclassProc allowed native hot chrome to overwrite
		// the custom rounded frame on every pointer move.
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if hot {
			beginDialogComboMouseTracking(hwnd)
		} else {
			endDialogComboMouseTracking(hwnd)
		}
		// A state transition presents one complete buffered frame. Ordinary
		// motion inside an already-hot combo must not force another synchronous
		// paint for every WM_MOUSEMOVE; if USER32 invalidated native content,
		// the normal WM_PAINT branch will add our chrome after that content.
		setDialogComboTargets(hwnd, &hot, nil, nil)
		return result, true
	}

	switch uint32(message) {
	case wmNCDestroy:
		stopDialogComboAnimationScheduler(hwnd)
		releaseDialogFieldPaintBuffer(hwnd)
		clearDialogComboPopup(hwnd)
		dialogComboMu.Lock()
		delete(dialogComboThemes, hwnd)
		dialogComboMu.Unlock()
		return 0, false

	case dialogComboWMPaint:
		if paintDialogCombo(hwnd) {
			return 0, true
		}

	case dialogComboWMPrintClient:
		if printDialogCombo(hwnd, wParam, lParam) {
			return 0, true
		}

	case dialogComboWMEraseBkgnd:
		// The complete native face and custom rounded chrome are both rendered
		// into the retained offscreen surface. Letting USER32 erase the live
		// child first creates exactly the rectangular flash this compositor is
		// intended to avoid.
		return 1, true

	case dialogComboWMAnimate:
		if consumeDialogComboAnimationFrame(hwnd, uint32(wParam)) {
			if !advanceDialogComboAnimation(hwnd, time.Now()) {
				stopDialogComboAnimationScheduler(hwnd)
			}
			return 0, true
		}
		return 0, true

	case dialogComboWMSetFocus:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		setDialogComboTargets(hwnd, nil, boolPointer(true), nil)
		return result, true

	case dialogComboWMKillFocus:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		setDialogComboTargets(
			hwnd,
			nil,
			boolPointer(dialogComboHasFocus(hwnd)),
			boolPointer(dialogComboDropped(hwnd)),
		)
		return result, true

	case dialogComboCBShowDropdown:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		dropped := wParam != 0 && dialogComboDropped(hwnd)
		setDialogComboTargets(hwnd, nil, nil, &dropped)
		if dropped {
			if state, ok := dialogComboTheme(hwnd); ok {
				themeDialogComboPopup(hwnd, state.Dark)
			}
		}
		return result, true

	case dialogComboWMSize, dialogComboWMDPIChanged:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			releaseDialogFieldPaintBuffer(hwnd)
			updateDialogComboRegion(hwnd)
			presentDialogComboChrome(hwnd)
		}
		return result, true

	case dialogComboWMSetText,
		dialogComboWMEnable,
		dialogComboWMSetFont,
		dialogComboWMStyleChanged,
		dialogComboWMThemeChanged,
		dialogComboCBSetCurSel,
		dialogComboCBSelectString,
		dialogComboCBSetCueBanner:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		if isWindow(hwnd) {
			presentDialogComboChrome(hwnd)
		}
		return result, true
	}

	if dialogComboObservesAfterNative(uint32(message)) {
		// Native processing happens first so F4, Alt+Down, arrows, Enter and
		// mouse capture retain exact COMBOBOX semantics. We only observe the
		// resulting drop state for the chevron transition.
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		dropped := dialogComboDropped(hwnd)
		focused := dialogComboHasFocus(hwnd)
		setDialogComboTargets(hwnd, nil, &focused, &dropped)
		if dropped {
			if state, ok := dialogComboTheme(hwnd); ok {
				themeDialogComboPopup(hwnd, state.Dark)
			}
		}
		return result, true
	}

	// WM_CHAR is intentionally untouched: native dropdown-list type-ahead
	// and editable-combo text/IME composition must flow through unmodified.
	return 0, false
}

func dialogComboNativePointerTarget(message uint32) (hot, ok bool) {
	switch message {
	case dialogComboWMMouseMove:
		return true, true
	case dialogComboWMMouseLeave:
		return false, true
	default:
		return false, false
	}
}

func setDialogComboTargets(
	hwnd uintptr,
	hot, focused, dropped *bool,
) bool {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	changed := false
	if exists && hot != nil && state.Hot != *hot {
		state.Hot = *hot
		changed = true
	}
	if exists && focused != nil && state.Focused != *focused {
		state.Focused = *focused
		changed = true
	}
	if exists && dropped != nil && state.Dropped != *dropped {
		state.Dropped = *dropped
		changed = true
	}
	active := false
	if changed {
		if state.Animate {
			state.LastTick = time.Now()
			active = state.Hover != dialogBoolLevel(state.Hot) ||
				state.Focus != dialogBoolLevel(state.Focused) ||
				state.Open != dialogBoolLevel(state.Dropped)
		} else {
			state.Hover = dialogBoolLevel(state.Hot)
			state.Focus = dialogBoolLevel(state.Focused)
			state.Open = dialogBoolLevel(state.Dropped)
			state.LastTick = time.Time{}
		}
		dialogComboThemes[hwnd] = state
	}
	dialogComboMu.Unlock()

	if !changed {
		return false
	}
	presentDialogComboChrome(hwnd)
	if active {
		if !startDialogComboAnimationScheduler(hwnd) {
			settleDialogComboAnimation(hwnd)
			presentDialogComboChrome(hwnd)
		}
	}
	return true
}

func advanceDialogComboAnimation(hwnd uintptr, now time.Time) bool {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if !exists {
		dialogComboMu.Unlock()
		return false
	}
	elapsed := dialogMotionTimerInterval
	if !state.LastTick.IsZero() {
		elapsed = now.Sub(state.LastTick)
	}
	state.LastTick = now
	var hoverActive, focusActive, openActive bool
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
	state.Open, openActive = advanceDialogMotion(
		state.Open,
		dialogBoolLevel(state.Dropped),
		elapsed,
		design.Default.Motion.FastMS,
	)
	active := hoverActive || focusActive || openActive
	if !active {
		state.LastTick = time.Time{}
	}
	dialogComboThemes[hwnd] = state
	dialogComboMu.Unlock()

	presentDialogComboChrome(hwnd)
	return active
}

func presentDialogComboChrome(hwnd uintptr) {
	if hwnd == 0 || !isWindow(hwnd) {
		return
	}
	// Invalidate without erasing and synchronously dispatch WM_PAINT. This
	// keeps native content plus custom chrome in one buffered presentation
	// instead of drawing animation frames directly into a live window DC.
	_, _, _ = dialogComboInvalidateRect.Call(hwnd, 0, 0)
	presentDialogMotionFrame(hwnd)
}

func settleDialogComboAnimation(hwnd uintptr) {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if exists {
		state.Hover = dialogBoolLevel(state.Hot)
		state.Focus = dialogBoolLevel(state.Focused)
		state.Open = dialogBoolLevel(state.Dropped)
		state.LastTick = time.Time{}
		dialogComboThemes[hwnd] = state
	}
	dialogComboMu.Unlock()
}

func startDialogComboAnimationScheduler(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	dialogComboSchedulerMu.Lock()
	if dialogComboSchedulers[hwnd] != nil {
		dialogComboSchedulerMu.Unlock()
		return true
	}
	token := atomic.AddUint32(&dialogComboSchedulerSequence, 1)
	if token == 0 {
		token = atomic.AddUint32(&dialogComboSchedulerSequence, 1)
	}
	scheduler := &dialogComboAnimationScheduler{
		token: token,
		stop:  make(chan struct{}),
	}
	dialogComboSchedulers[hwnd] = scheduler
	dialogComboSchedulerMu.Unlock()

	go runDialogComboAnimationScheduler(hwnd, scheduler)
	return true
}

func runDialogComboAnimationScheduler(
	hwnd uintptr,
	scheduler *dialogComboAnimationScheduler,
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
			posted, _, _ := dialogComboPostMessage.Call(
				hwnd,
				dialogComboWMAnimate,
				uintptr(scheduler.token),
				0,
			)
			if posted == 0 {
				scheduler.pending.Store(false)
			}
		}
	}
}

func consumeDialogComboAnimationFrame(hwnd uintptr, token uint32) bool {
	dialogComboSchedulerMu.Lock()
	scheduler := dialogComboSchedulers[hwnd]
	valid := scheduler != nil && scheduler.token == token
	if valid {
		scheduler.pending.Store(false)
	}
	dialogComboSchedulerMu.Unlock()
	return valid
}

func stopDialogComboAnimationScheduler(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogComboSchedulerMu.Lock()
	scheduler := dialogComboSchedulers[hwnd]
	delete(dialogComboSchedulers, hwnd)
	dialogComboSchedulerMu.Unlock()
	if scheduler != nil {
		close(scheduler.stop)
		scheduler.pending.Store(false)
	}
}

func beginDialogComboMouseTracking(hwnd uintptr) {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if !exists || state.Tracking {
		dialogComboMu.Unlock()
		return
	}
	state.Tracking = true
	dialogComboThemes[hwnd] = state
	dialogComboMu.Unlock()

	tracking := dialogFieldTrackMouse{
		Size:   uint32(unsafe.Sizeof(dialogFieldTrackMouse{})),
		Flags:  dialogComboTMELeave,
		Window: hwnd,
	}
	_, _, _ = dialogComboTrackMouseEvent.Call(
		uintptr(unsafe.Pointer(&tracking)),
	)
	runtime.KeepAlive(&tracking)
}

func endDialogComboMouseTracking(hwnd uintptr) {
	dialogComboMu.Lock()
	state, exists := dialogComboThemes[hwnd]
	if exists {
		state.Tracking = false
		dialogComboThemes[hwnd] = state
	}
	dialogComboMu.Unlock()
}

func paintDialogCombo(hwnd uintptr) bool {
	var paint dialogComboPaintStruct
	dc, _, _ := dialogComboBeginPaint.Call(
		hwnd,
		uintptr(unsafe.Pointer(&paint)),
	)
	if dc == 0 {
		return false
	}
	defer func() {
		_, _, _ = dialogComboEndPaint.Call(
			hwnd,
			uintptr(unsafe.Pointer(&paint)),
		)
		runtime.KeepAlive(&paint)
	}()

	if renderDialogComboFrame(
		hwnd,
		dc,
		dialogComboNativePrintFlags(0),
	) {
		return true
	}

	// A GDI allocation failure must not leave the native control blank. This
	// fallback keeps the COMBOBOX usable, but deliberately omits custom chrome
	// instead of reintroducing the old native-visible-then-overlay paint path.
	callDefSubclassProc(
		hwnd,
		dialogComboWMPrintClient,
		dc,
		dialogComboNativePrintFlags(0),
	)
	return true
}

// printDialogCombo serves parent/page compositors without consuming this
// HWND's pending paint. It uses the same native-plus-chrome offscreen frame as
// the live WM_PAINT path and commits it to the caller's DC exactly once.
func printDialogCombo(hwnd, dc, flags uintptr) bool {
	if dc == 0 {
		return false
	}
	return renderDialogComboFrame(
		hwnd,
		dc,
		dialogComboNativePrintFlags(flags),
	)
}

func renderDialogComboFrame(
	hwnd, targetDC, printFlags uintptr,
) bool {
	state, exists := dialogComboTheme(hwnd)
	if !exists || hwnd == 0 || targetDC == 0 {
		return false
	}

	var client dialogFieldRect
	ok, _, _ := dialogComboGetClientRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&client)),
	)
	width := client.Right - client.Left
	height := client.Bottom - client.Top
	if ok == 0 || width < 2 || height < 2 {
		return false
	}
	info, infoOK := dialogComboInfo(hwnd)
	if !infoOK {
		return false
	}

	dpi := dialogComboDPI(hwnd)
	buffer := acquireDialogFieldPaintBuffer(
		hwnd,
		targetDC,
		width,
		height,
		dpi,
	)
	if buffer == nil {
		return false
	}

	return composeDialogComboAtomicFrame(
		buffer.memory,
		targetDC,
		func(surface uintptr) {
			// Keyboard cue updates can restore USER32's dotted rectangle. Keep
			// only that native ornament hidden; our focused chrome remains visible.
			callDefSubclassProc(hwnd, 0x0128, 0x00010001, 0)
			// DefSubclassProc calls the native COMBOBOX directly, bypassing
			// this subclass's WM_PRINTCLIENT branch and therefore avoiding a
			// recursive composition pass. Keyboard, IME, type-ahead, list
			// ownership and accessibility remain native control behavior.
			callDefSubclassProc(
				hwnd,
				dialogComboWMPrintClient,
				surface,
				printFlags,
			)
		},
		func(surface uintptr) bool {
			return drawDialogComboChrome(
				hwnd,
				surface,
				state,
				info,
				width,
				height,
				dpi,
			)
		},
		func(surface, target uintptr) bool {
			committed, _, _ := dialogFieldBitBlt.Call(
				target,
				0,
				0,
				uintptr(width),
				uintptr(height),
				surface,
				0,
				0,
				dialogFieldSRCCopy,
			)
			return committed != 0
		},
	)
}

// composeDialogComboAtomicFrame is the only presentation sequence for themed
// combo boxes: native content and app-owned chrome both draw into an offscreen
// surface, followed by one final transfer to the destination. Keeping this
// small orchestration function pure also makes regressions back to a live
// native paint followed by a GetWindowDC overlay directly testable.
func composeDialogComboAtomicFrame(
	surface, target uintptr,
	renderNative func(uintptr),
	renderChrome func(uintptr) bool,
	commit func(uintptr, uintptr) bool,
) bool {
	if surface == 0 || target == 0 || surface == target ||
		renderNative == nil || renderChrome == nil || commit == nil {
		return false
	}
	renderNative(surface)
	if !renderChrome(surface) {
		return false
	}
	return commit(surface, target)
}

func dialogComboNativePrintFlags(flags uintptr) uintptr {
	return flags |
		dialogComboPRFClient |
		dialogComboPRFEraseBkgnd |
		dialogComboPRFChildren
}

func drawDialogComboChrome(
	hwnd, dc uintptr,
	state dialogComboThemeState,
	info dialogComboBoxInfo,
	width, height, dpi int32,
) bool {
	if hwnd == 0 || dc == 0 || width < 2 || height < 2 {
		return false
	}
	radius := dialogComboRadius(width, height, dpi)
	restoreDialogAACorners(hwnd, dc, width, height, radius, state.Dark)
	enabled, _, _ := dialogComboIsWindowEnabled.Call(hwnd)
	palette := dialogComboColors(state.Dark)
	interaction := state.Hover
	if state.Open > interaction {
		interaction = state.Open
	}
	buttonColor := dialogMixColor(
		palette.Button,
		palette.ButtonHot,
		dialogEase(interaction),
	)
	buttonColor = dialogMixColor(
		buttonColor,
		palette.ButtonOpen,
		dialogEase(state.Open),
	)
	if enabled == 0 {
		buttonColor = palette.Button
	}

	button := dialogComboButtonRect(
		info.Button,
		width,
		height,
		dpi,
	)
	rtl := windowRTLReading(hwnd)
	if rtl {
		button.Left, button.Right = mirrorHorizontalEdges(
			0,
			width,
			button.Left,
			button.Right,
		)
	}
	fillDialogComboRoundedButton(
		dc,
		button,
		buttonColor,
		width,
		height,
		radius,
	)

	border := dialogMixColor(
		palette.Border,
		palette.BorderHot,
		dialogEase(state.Hover),
	)
	border = dialogMixColor(
		border,
		palette.BorderFocus,
		dialogEase(state.Focus),
	)
	if enabled == 0 {
		border = palette.BorderDisabled
	}
	strokeDialogFieldRoundRect(
		dc,
		dialogFieldRect{Right: width, Bottom: height},
		border,
		radius,
		// A stable two-pixel footprint prevents the retained previous frame
		// from leaving a focus-colored inner edge during focus-out.
		dialogFieldStroke(dpi, 1),
	)

	divider := palette.Divider
	if state.Focus > 0 {
		divider = dialogMixColor(
			divider,
			palette.BorderFocus,
			dialogEase(state.Focus)*0.48,
		)
	}
	drawDialogComboDivider(dc, button, divider, dpi, rtl)

	glyph := palette.Glyph
	if enabled == 0 {
		glyph = palette.GlyphDisabled
	}
	drawDialogComboChevron(
		dc,
		button,
		glyph,
		state.Open,
		dpi,
	)
	return true
}

func dialogComboButtonRect(
	native dialogFieldRect,
	width, height, dpi int32,
) dialogFieldRect {
	inset := dialogFieldStroke(dpi, 0)
	buttonWidth := native.Right - native.Left
	if buttonWidth <= 0 || buttonWidth >= width {
		buttonWidth = scaleDialogFieldMetric(32, dpi)
	}
	button := dialogFieldRect{
		Left:   width - inset - buttonWidth,
		Top:    inset,
		Right:  width - inset,
		Bottom: height - inset,
	}
	if button.Left < inset {
		button.Left = inset
	}
	return button
}

func dialogComboObservesAfterNative(message uint32) bool {
	switch message {
	case dialogComboWMKeyDown,
		dialogComboWMKeyUp,
		dialogComboWMSysKeyDown,
		dialogComboWMSysKeyUp,
		dialogComboWMLButtonDown,
		dialogComboWMLButtonUp:
		return true
	default:
		return false
	}
}

func drawDialogComboDivider(
	dc uintptr,
	button dialogFieldRect,
	color uint32,
	dpi int32,
	rtl bool,
) {
	x := button.Left
	if rtl {
		x = button.Right
	}
	inset := scaleDialogFieldMetric(6, dpi)
	drawDialogComboLine(
		dc,
		dialogComboPoint{X: x, Y: button.Top + inset},
		dialogComboPoint{X: x, Y: button.Bottom - inset},
		color,
		dialogFieldStroke(dpi, 0),
	)
}

func drawDialogComboChevron(
	dc uintptr,
	button dialogFieldRect,
	color uint32,
	open float64,
	dpi int32,
) {
	left, middle, right := dialogComboChevronPoints(button, open, dpi)
	stroke := dialogFieldStroke(dpi, 1)
	if drawDialogAAPolyline(dc, []dialogComboPoint{left, middle, right}, color, stroke) {
		return
	}
	drawDialogComboLine(dc, left, middle, color, stroke)
	drawDialogComboLine(dc, middle, right, color, stroke)
}

func dialogComboChevronPoints(
	button dialogFieldRect,
	open float64,
	dpi int32,
) (left, middle, right dialogComboPoint) {
	open = dialogEase(open)
	centerX := (button.Left + button.Right) / 2
	centerY := (button.Top + button.Bottom) / 2
	halfWidth := scaleDialogFieldMetric(4, dpi)
	closedEndOffset := -float64(scaleDialogFieldMetric(1, dpi))
	openEndOffset := float64(scaleDialogFieldMetric(1, dpi))
	closedMiddleOffset := float64(scaleDialogFieldMetric(2, dpi))
	openMiddleOffset := -float64(scaleDialogFieldMetric(2, dpi))
	endY := centerY + dialogComboRoundMetric(
		closedEndOffset*(1-open)+openEndOffset*open,
	)
	middleY := centerY + dialogComboRoundMetric(
		closedMiddleOffset*(1-open)+openMiddleOffset*open,
	)
	return dialogComboPoint{X: centerX - halfWidth, Y: endY},
		dialogComboPoint{X: centerX, Y: middleY},
		dialogComboPoint{X: centerX + halfWidth, Y: endY}
}

func dialogComboRoundMetric(value float64) int32 {
	if value < 0 {
		return int32(value - 0.5)
	}
	return int32(value + 0.5)
}

func drawDialogComboLine(
	dc uintptr,
	first, second dialogComboPoint,
	color uint32,
	stroke int32,
) {
	if drawDialogAAPolyline(dc, []dialogComboPoint{first, second}, color, stroke) {
		return
	}
	pen, _, _ := dialogFieldGetStockObject.Call(dialogFieldDCPen)
	if pen == 0 {
		return
	}
	previous, _, _ := dialogComboSelectObject.Call(dc, pen)
	_, _, _ = dialogFieldSetDCPenColor.Call(dc, uintptr(color))
	if stroke < 1 {
		stroke = 1
	}
	for layer := int32(0); layer < stroke; layer++ {
		_, _, _ = dialogComboMoveToEx.Call(
			dc,
			uintptr(first.X),
			uintptr(first.Y+layer),
			0,
		)
		_, _, _ = dialogComboLineTo.Call(
			dc,
			uintptr(second.X),
			uintptr(second.Y+layer),
		)
	}
	if previous != 0 {
		_, _, _ = dialogComboSelectObject.Call(dc, previous)
	}
}

func fillDialogComboRect(
	dc uintptr,
	rect dialogFieldRect,
	color uint32,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	brush, _, _ := dialogFieldGetStockObject.Call(dialogFieldDCBrush)
	if brush == 0 {
		return
	}
	previous, _, _ := dialogComboSelectObject.Call(dc, brush)
	_, _, _ = dialogFieldSetDCBrushColor.Call(dc, uintptr(color))
	_, _, _ = dialogComboFillRect.Call(
		dc,
		uintptr(unsafe.Pointer(&rect)),
		brush,
	)
	if previous != 0 {
		_, _, _ = dialogComboSelectObject.Call(dc, previous)
	}
	runtime.KeepAlive(&rect)
}

func fillDialogComboRoundedButton(
	dc uintptr,
	rect dialogFieldRect,
	color uint32,
	width, height, radius int32,
) {
	if fillDialogAARoundedPart(dc, rect, width, height, radius, color) {
		return
	}
	if dc == 0 || width < 1 || height < 1 || radius < 1 {
		fillDialogComboRect(dc, rect, color)
		return
	}
	region := createDialogComboRoundedRegion(width, height, radius)
	if region == 0 {
		fillDialogComboRect(dc, rect, color)
		return
	}
	defer dialogComboDeleteObject.Call(region)

	saved, _, _ := dialogComboSaveDC.Call(dc)
	if saved == 0 {
		fillDialogComboRect(dc, rect, color)
		return
	}
	defer dialogComboRestoreDC.Call(dc, saved)
	selected, _, _ := dialogComboSelectClipRgn.Call(dc, region)
	if selected == 0 {
		fillDialogComboRect(dc, rect, color)
		return
	}
	fillDialogComboRect(dc, rect, color)
}

func updateDialogComboRegion(hwnd uintptr) {
	if hwnd == 0 || !isWindow(hwnd) {
		return
	}
	_, _, _ = dialogComboSetWindowRgn.Call(hwnd, 0, 1)
}

func createDialogComboRoundedRegion(
	width, height, radius int32,
) uintptr {
	if width < 1 || height < 1 || radius < 1 {
		return 0
	}
	// CreateRoundRectRgn's ellipse rasterizer drops the final right/bottom
	// device pixel when passed the nominal exclusive bounds. Supplying the
	// extra endpoint yields an observed GetRgnBox of exactly
	// [0,width)×[0,height), which is the same pixel domain as the control DC.
	// The region/symmetry regression test below protects this Win32 quirk.
	region, _, _ := dialogComboCreateRoundRectRgn.Call(
		0,
		0,
		uintptr(width+1),
		uintptr(height+1),
		uintptr(radius*2),
		uintptr(radius*2),
	)
	return region
}

func themeDialogComboPopup(hwnd uintptr, dark bool) {
	info, ok := dialogComboInfo(hwnd)
	if !ok || info.WindowList == 0 || !isWindow(info.WindowList) {
		return
	}
	needsApply := rememberDialogComboPopupTheme(
		info.WindowList,
		hwnd,
		dark,
	)
	if err := SetWindowSubclass(
		info.WindowList,
		dialogComboListSubclassID,
		dialogComboPopupWindowMessage,
	); err != nil && !errors.Is(err, ErrSubclassExists) {
		forgetDialogComboPopup(info.WindowList, hwnd)
		return
	}
	if needsApply {
		applyDialogComboPopupTheme(info.WindowList, dark)
	}
}

func rememberDialogComboPopupTheme(
	list, owner uintptr,
	dark bool,
) bool {
	if list == 0 || owner == 0 {
		return false
	}
	dialogComboPopupMu.Lock()
	defer dialogComboPopupMu.Unlock()
	needsApply := !dialogComboPopupReady[list] ||
		dialogComboPopupThemes[list] != dark
	dialogComboPopupOwners[list] = owner
	dialogComboPopupThemes[list] = dark
	dialogComboPopupReady[list] = true
	return needsApply
}

func forgetDialogComboPopup(list, owner uintptr) {
	if list == 0 {
		return
	}
	dialogComboPopupMu.Lock()
	if owner == 0 || dialogComboPopupOwners[list] == owner {
		delete(dialogComboPopupOwners, list)
		delete(dialogComboPopupThemes, list)
		delete(dialogComboPopupReady, list)
	}
	dialogComboPopupMu.Unlock()
}

func applyDialogComboPopupTheme(list uintptr, dark bool) {
	if list == 0 || !isWindow(list) {
		return
	}
	allowDialogDarkMode(list, dark)
	themeName := "Explorer"
	if dark {
		themeName = "DarkMode_Explorer"
	}
	theme, err := windows.UTF16PtrFromString(themeName)
	themeChangeDelivered := false
	if err == nil && dialogThemeSetWindowTheme.Find() == nil {
		result, _, _ := dialogThemeSetWindowTheme.Call(
			list,
			uintptr(unsafe.Pointer(theme)),
			0,
		)
		themeChangeDelivered = dialogThemeHRESULTSucceeded(result)
		runtime.KeepAlive(theme)
	}
	_ = setDWMInt32(
		list,
		dwmwaWindowCornerPref,
		dialogDWMCornerRound,
	)
	if !themeChangeDelivered {
		// SetWindowTheme normally sends WM_THEMECHANGED synchronously. Only
		// supply the notification when that API was unavailable or failed;
		// doubling it forces two erase/frame cycles in an open ComboLBox.
		_, _, _ = dialogComboSendMessage.Call(
			list,
			dialogComboWMThemeChanged,
			0,
			0,
		)
	}
	_, _, _ = dialogComboRedrawWindow.Call(
		list,
		0,
		0,
		dialogRDWInvalidate|
			dialogRDWErase|
			dialogRDWFrame|
			dialogRDWAllChildren,
	)
}

func clearDialogComboPopup(owner uintptr) {
	if owner == 0 {
		return
	}
	var lists []uintptr
	if info, ok := dialogComboInfo(owner); ok && info.WindowList != 0 {
		lists = append(lists, info.WindowList)
	}
	dialogComboPopupMu.Lock()
	for list, candidate := range dialogComboPopupOwners {
		if candidate == owner {
			known := false
			for _, existing := range lists {
				if existing == list {
					known = true
					break
				}
			}
			if !known {
				lists = append(lists, list)
			}
			delete(dialogComboPopupOwners, list)
			delete(dialogComboPopupThemes, list)
			delete(dialogComboPopupReady, list)
		}
	}
	dialogComboPopupMu.Unlock()
	for _, list := range lists {
		_ = RemoveWindowSubclass(list, dialogComboListSubclassID)
		applyDialogComboPopupTheme(list, false)
		_ = setDWMInt32(
			list,
			dwmwaWindowCornerPref,
			0,
		)
	}
}

func dialogComboPopupWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	switch uint32(message) {
	case wmNCDestroy:
		forgetDialogComboPopup(hwnd, 0)
		return 0, false
	}
	if dialogComboPopupObservesAfterNative(uint32(message)) {
		// Selection, Escape, type-ahead and click-away are completed by the
		// real ComboLBox first. We then observe whether USER32 closed it so the
		// chevron cannot remain stuck in its open state.
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		syncDialogComboPopupOwner(hwnd)
		return result, true
	}
	return 0, false
}

func dialogComboPopupObservesAfterNative(message uint32) bool {
	switch message {
	case dialogComboWMShowWindow,
		dialogComboWMWindowPosChanged,
		dialogComboWMCancelMode,
		dialogComboWMKillFocus,
		dialogComboWMKeyDown,
		dialogComboWMKeyUp,
		dialogComboWMChar,
		dialogComboWMLButtonUp:
		return true
	default:
		return false
	}
}

func syncDialogComboPopupOwner(list uintptr) {
	dialogComboPopupMu.RLock()
	owner := dialogComboPopupOwners[list]
	dialogComboPopupMu.RUnlock()
	if owner == 0 {
		return
	}
	dropped := dialogComboDropped(owner)
	focused := dialogComboHasFocus(owner)
	setDialogComboTargets(owner, nil, &focused, &dropped)
}

func dialogComboInfo(hwnd uintptr) (dialogComboBoxInfo, bool) {
	info := dialogComboBoxInfo{
		Size: uint32(unsafe.Sizeof(dialogComboBoxInfo{})),
	}
	result, _, _ := dialogComboSendMessage.Call(
		hwnd,
		dialogComboCBGetComboBoxInfo,
		0,
		uintptr(unsafe.Pointer(&info)),
	)
	runtime.KeepAlive(&info)
	return info, result != 0
}

func dialogComboDropped(hwnd uintptr) bool {
	result, _, _ := dialogComboSendMessage.Call(
		hwnd,
		dialogComboCBGetDroppedState,
		0,
		0,
	)
	return result != 0
}

func dialogComboHasFocus(hwnd uintptr) bool {
	focus, _, _ := dialogComboGetFocus.Call()
	if focus == hwnd {
		return true
	}
	if focus == 0 {
		return false
	}
	parent, _, _ := dialogComboGetParent.Call(focus)
	return parent == hwnd
}

func dialogComboTheme(hwnd uintptr) (dialogComboThemeState, bool) {
	dialogComboMu.RLock()
	state, exists := dialogComboThemes[hwnd]
	dialogComboMu.RUnlock()
	return state, exists
}

func dialogComboColors(dark bool) dialogComboPalette {
	if dark {
		tokens := design.Default.Colors.Dark
		return dialogComboPalette{
			Button:         dialogFieldToken(tokens.SurfaceHigh),
			ButtonHot:      dialogFieldTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.16),
			ButtonOpen:     dialogFieldTokenMix(tokens.SurfaceHigh, tokens.Accent, 0.25),
			Border:         dialogFieldToken(tokens.Stroke),
			BorderHot:      dialogFieldTokenMix(tokens.Stroke, tokens.AccentHover, 0.62),
			BorderFocus:    dialogFieldToken(tokens.AccentHover),
			BorderDisabled: dialogFieldTokenMix(tokens.Stroke, tokens.Surface, 0.58),
			Divider:        dialogFieldTokenMix(tokens.Stroke, tokens.SurfaceHigh, 0.34),
			Glyph:          dialogFieldToken(tokens.TextMuted),
			GlyphDisabled:  dialogFieldToken(tokens.TextSubtle),
		}
	}
	tokens := design.Default.Colors.Light
	return dialogComboPalette{
		Button:         dialogFieldToken(tokens.Surface),
		ButtonHot:      dialogFieldTokenMix(tokens.Surface, tokens.Accent, 0.10),
		ButtonOpen:     dialogFieldTokenMix(tokens.Surface, tokens.Accent, 0.17),
		Border:         dialogFieldToken(tokens.Stroke),
		BorderHot:      dialogFieldTokenMix(tokens.Stroke, tokens.Accent, 0.64),
		BorderFocus:    dialogFieldToken(tokens.Accent),
		BorderDisabled: dialogFieldTokenMix(tokens.Stroke, tokens.Canvas, 0.58),
		Divider:        dialogFieldTokenMix(tokens.Stroke, tokens.Surface, 0.22),
		Glyph:          dialogFieldToken(tokens.TextMuted),
		GlyphDisabled:  dialogFieldToken(tokens.TextSubtle),
	}
}

func dialogComboDPI(hwnd uintptr) int32 {
	dpi, _, _ := dialogComboGetDPIForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

func dialogComboRadius(width, height, dpi int32) int32 {
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

func boolPointer(value bool) *bool {
	return &value
}
