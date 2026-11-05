//go:build windows

package traymenu

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/winfx"
)

var (
	errCustomUnavailable  = errors.New("traymenu: custom popup unavailable")
	errPopupAlreadyActive = errors.New("traymenu: popup is already active for this owner")

	popupClassOnce sync.Once
	popupClassErr  error
	popupClassName []uint16
	popupCallback  = windows.NewCallback(popupWindowProc)
	popupSessions  sync.Map // map[win.HWND]*popupSession

	popupOverlayClassName []uint16
	popupOverlayCallback  = windows.NewCallback(popupOverlayWindowProc)

	popupWindowMu     sync.Mutex
	popupWindowCache  = make(map[popupWindowKey]*popupWindow)
	popupActiveWindow = make(map[popupWindowKey]*popupWindow)

	popupAnimationSequence uint32
)

const (
	popupAnimationInterval      = uint32(16)
	popupInitialPresentation    = uint8(96)
	popupRevealDuration         = 120 * time.Millisecond
	popupDismissDuration        = 105 * time.Millisecond
	popupDismissCutoff          = uint8(32)
	popupPresentationFullyShown = uint8(255)
	wmPopupAnimationFrame       = uint32(win.WM_APP + 0x0524)
	wmSysColorChanged           = uint32(0x0015)
	wmDWMCompositionChanged     = uint32(0x031E)
	wmDWMColorizationChanged    = uint32(0x0320)
)

type popupWindowKey struct {
	owner    win.HWND
	threadID uint32
}

// popupWindow contains only thread-affine native window state. Invocation
// data, fonts, decoded images, and caller-owned icon handles are deliberately
// kept in popupSession and released as soon as the menu closes.
type popupWindow struct {
	key         popupWindowKey
	hwnd        win.HWND
	overlay     win.HWND
	dpi         int32
	retired     bool
	composition popupCompositionCache
}

type popupCompositionCache struct {
	backdropValid bool
	requested     Backdrop
	dark          bool
	transparency  bool
	used          Backdrop

	regionValid bool
	width       int32
	height      int32
	radius      int32
}

type popupSession struct {
	window             *popupWindow
	hwnd               win.HWND
	overlay            win.HWND
	options            Options
	dimensions         dimensions
	font               menuFont
	icons              []preparedIcon
	foreground         *foregroundBuffer
	dark               bool
	transparency       bool
	motionEnabled      bool
	palette            palette
	navigation         navigator
	hover              hoverAnimator
	result             Result
	finalX             int32
	finalY             int32
	slideOffset        int32
	presentation       uint8
	presenting         int8
	presentationMotion popupScalarMotion
	lastAnimationTick  time.Time
	animationRunning   bool
	animationToken     uint32
	animationStop      chan struct{}
	animationGate      *popupFrameGate
	shown              bool
	activating         bool
	closing            bool
	done               bool
	renderFailed       bool
	lifecycle          popupLifecycleState
	backdropChanged    bool
}

func showAtCursor(options Options) (Result, error) {
	var cursor win.POINT
	if !win.GetCursorPos(&cursor) {
		return Result{}, errors.New("traymenu: GetCursorPos failed")
	}
	anchor := Rect{
		Left:   cursor.X,
		Top:    cursor.Y,
		Right:  cursor.X,
		Bottom: cursor.Y,
	}
	return showWindows(options, anchor)
}

func showAtAnchor(options Options, anchor Rect) (Result, error) {
	return showWindows(options, anchor)
}

func showWindows(options Options, anchor Rect) (Result, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if owner := win.HWND(options.Owner); owner != 0 {
		ownerThread := win.GetWindowThreadProcessId(owner, nil)
		if ownerThread != 0 && ownerThread != win.GetCurrentThreadId() {
			return Result{}, fmt.Errorf(
				"%w; invoke the menu from the owner window's UI thread",
				ErrWrongThread,
			)
		}
	}

	monitor := monitorFor(anchor, options.Owner)
	workArea, workErr := monitorWorkArea(monitor)
	if workErr != nil {
		// TrackPopupMenu can still position itself when monitor details fail.
		workArea = Rect{
			Left:   anchor.Left - 1920,
			Top:    anchor.Top - 1080,
			Right:  anchor.Right + 1920,
			Bottom: anchor.Bottom + 1080,
		}
	}

	appearance, _ := winfx.ResolveAppearance(string(options.Theme))
	return showWithPolicy(
		options,
		appearance.HighContrast,
		func(attempt Options) (Result, error) {
			return showCustom(
				attempt,
				anchor,
				monitor,
				workArea,
				appearance,
			)
		},
		func() (Result, error) {
			return showNative(options, anchor, workArea)
		},
		func() {
			DiscardCached(options.Owner)
		},
	)
}

type customPopupPresenter func(Options) (Result, error)
type nativePopupPresenter func() (Result, error)

func showWithPolicy(
	options Options,
	highContrast bool,
	custom customPopupPresenter,
	native nativePopupPresenter,
	discard func(),
) (Result, error) {
	showNativeReason := func(reason NativeReason) (Result, error) {
		result, err := native()
		if err == nil {
			result.UsedNativeFallback = true
			result.NativeReason = reason
		}
		return result, err
	}
	switch {
	case options.PreferNative:
		return showNativeReason(NativeReasonExplicit)
	case highContrast:
		return showNativeReason(NativeReasonHighContrast)
	}

	result, initialErr := custom(options)
	if initialErr == nil {
		return result, nil
	}
	if errors.Is(initialErr, errPopupAlreadyActive) {
		// A re-entrant notification must never open a second menu over the
		// active custom one.
		return Result{Cancelled: true}, nil
	}

	// A cached HWND can become unusable after composition, DPI, display, or
	// owner-style changes. Retire it and make exactly one clean custom attempt.
	if discard != nil {
		discard()
	}
	recoveryOptions := options
	recoveryOptions.DisableWindowReuse = true
	recovered, recoveryErr := custom(recoveryOptions)
	if recoveryErr == nil {
		return recovered, nil
	}
	if errors.Is(recoveryErr, errPopupAlreadyActive) {
		return Result{Cancelled: true}, nil
	}
	return Result{}, &CustomPresentationError{
		Initial:    initialErr,
		FreshRetry: recoveryErr,
	}
}

func showCustom(
	options Options,
	anchor Rect,
	monitor win.HMONITOR,
	workArea Rect,
	appearance winfx.ResolvedAppearance,
) (Result, error) {
	if err := registerPopupClass(); err != nil {
		return Result{}, fmt.Errorf("%w: %v", errCustomUnavailable, err)
	}

	dpi := monitorDPI(monitor, options.Owner)
	font, err := newMenuFontForFamily(dpi, options.FontFamily)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", errCustomUnavailable, err)
	}
	defer font.close()

	dimensions := buildDimensionsForDirection(
		options.Items,
		dpi,
		font.measure,
		options.RightToLeft,
	)
	margin := scale(4, dpi)
	if !workArea.validArea() ||
		dimensions.width > workArea.Right-workArea.Left-margin*2 {
		return Result{}, fmt.Errorf("%w: menu does not fit the monitor work area", errCustomUnavailable)
	}
	dimensions, fits := fitDimensionsHeight(
		dimensions,
		workArea.Bottom-workArea.Top-margin*2,
	)
	if !fits {
		return Result{}, fmt.Errorf(
			"%w: menu cannot retain readable density in the monitor work area",
			errCustomUnavailable,
		)
	}
	icons, err := prepareIcons(options.Items, dimensions.iconSize)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", errCustomUnavailable, err)
	}

	dark := appearance.Dark
	x, y := placePopupForDirection(
		anchor,
		workArea,
		dimensions.width,
		dimensions.height,
		margin,
		options.Alignment,
		options.RightToLeft,
	)
	menuPalette := paletteFor(dark)
	window, reused, err := acquirePopupWindow(
		win.HWND(options.Owner),
		dpi,
		options.DisableWindowReuse,
	)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", errCustomUnavailable, err)
	}

	session := &popupSession{
		window:       window,
		hwnd:         window.hwnd,
		overlay:      window.overlay,
		options:      options,
		dimensions:   dimensions,
		font:         font,
		icons:        icons,
		dark:         dark,
		transparency: appearance.TransparencyEnabled,
		motionEnabled: appearance.MotionEnabled &&
			!options.DisableAnimations,
		palette:    menuPalette,
		navigation: newNavigator(options.Items),
		hover:      newHoverAnimator(len(options.Items)),
		finalX:     x,
		finalY:     y,
		result: Result{
			Cancelled:    true,
			BackdropUsed: BackdropSolid,
		},
	}
	if reused {
		session.lifecycle = popupLifecycleHiddenReusable
	}
	popupSessions.Store(session.hwnd, session)
	defer func() {
		popupSessions.Delete(session.hwnd)
		if !session.done {
			session.destroyNow()
		}
	}()

	session.slideOffset = scale(8, dpi)
	if y >= anchor.Bottom {
		session.slideOffset = -session.slideOffset
	}
	if session.motionEnabled {
		session.presenting = 1
		// Commit a visibly meaningful entrance frame. A zero-alpha first frame
		// is initialized, but exposing only the material host for one compositor
		// interval can still be perceived as a white flash.
		session.presentation = popupInitialPresentation
	} else {
		session.presentation = popupPresentationFullyShown
	}
	if err := session.prepareHiddenFirstFrame(); err != nil {
		session.destroyNow()
		return Result{}, fmt.Errorf("%w: %v", errCustomUnavailable, err)
	}
	if !session.lifecycle.activate() {
		session.destroyNow()
		return Result{}, fmt.Errorf(
			"%w: popup first-frame visibility gate was not committed",
			errCustomUnavailable,
		)
	}
	session.activating = true
	if !win.SetWindowPos(
		session.hwnd,
		win.HWND_TOPMOST,
		x,
		y+presentationOffset(
			session.slideOffset,
			session.presentation,
		),
		dimensions.width,
		dimensions.height,
		win.SWP_SHOWWINDOW|win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER,
	) {
		session.activating = false
		session.destroyNow()
		return Result{}, fmt.Errorf("%w: reveal prepared popup failed", errCustomUnavailable)
	}
	win.SetForegroundWindow(session.hwnd)
	win.SetFocus(session.hwnd)
	win.SetCapture(session.hwnd)
	session.shown = true
	session.activating = false
	if session.motionEnabled {
		session.ensureAnimationScheduler()
	}

	var message win.MSG
	var repostQuit bool
	var quitCode int32
	for !session.done {
		status := win.GetMessage(&message, 0, 0, 0)
		if status == -1 {
			session.destroyNow()
			return Result{}, fmt.Errorf("%w: GetMessage failed", errCustomUnavailable)
		}
		if status == 0 {
			repostQuit = true
			quitCode = int32(message.WParam)
			session.destroyNow()
			break
		}
		win.TranslateMessage(&message)
		win.DispatchMessage(&message)
	}
	if repostQuit {
		win.PostQuitMessage(quitCode)
	}
	if session.renderFailed {
		return Result{}, fmt.Errorf("%w: renderer initialization failed", errCustomUnavailable)
	}
	return session.result, nil
}

func acquirePopupWindow(
	owner win.HWND,
	dpi int32,
	disableReuse bool,
) (*popupWindow, bool, error) {
	key := popupWindowKey{owner: owner, threadID: win.GetCurrentThreadId()}

	popupWindowMu.Lock()
	if popupActiveWindow[key] != nil {
		popupWindowMu.Unlock()
		return nil, false, errPopupAlreadyActive
	}
	cached := popupWindowCache[key]
	if cached != nil && (disableReuse || cached.dpi != dpi || cached.retired) {
		delete(popupWindowCache, key)
		cached.retired = true
	}
	if cached != nil && !cached.retired {
		popupActiveWindow[key] = cached
	}
	popupWindowMu.Unlock()

	if cached != nil && cached.retired {
		destroyPopupWindow(cached)
		cached = nil
	}
	if cached != nil {
		if popupWindowIsValid(cached) {
			return cached, true, nil
		}
		destroyPopupWindow(cached)
		cached = nil
	}

	created, err := createPopupWindow(key, dpi)
	if err != nil {
		return nil, false, err
	}
	popupWindowMu.Lock()
	if popupActiveWindow[key] != nil {
		popupWindowMu.Unlock()
		destroyPopupWindow(created)
		return nil, false, errors.New("traymenu: popup became active while creating its window")
	}
	popupActiveWindow[key] = created
	if !disableReuse {
		popupWindowCache[key] = created
	}
	popupWindowMu.Unlock()
	return created, false, nil
}

func createPopupWindow(key popupWindowKey, dpi int32) (*popupWindow, error) {
	windowTitle, err := windows.UTF16FromString("RayanLamp")
	if err != nil {
		return nil, err
	}
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW,
		&popupClassName[0],
		&windowTitle[0],
		win.WS_POPUP,
		0,
		0,
		1,
		1,
		key.owner,
		0,
		win.GetModuleHandle(nil),
		nil,
	)
	runtime.KeepAlive(windowTitle)
	if hwnd == 0 {
		return nil, errors.New("traymenu: CreateWindowEx for popup failed")
	}
	overlay, err := createForegroundOverlay(hwnd)
	if err != nil {
		win.DestroyWindow(hwnd)
		return nil, err
	}
	return &popupWindow{
		key:     key,
		hwnd:    hwnd,
		overlay: overlay,
		dpi:     dpi,
	}, nil
}

func popupWindowIsValid(window *popupWindow) bool {
	if window == nil || window.hwnd == 0 || window.overlay == 0 {
		return false
	}
	return nativeWindowExists(window.hwnd) &&
		nativeWindowExists(window.overlay) &&
		win.GetWindowThreadProcessId(window.hwnd, nil) == window.key.threadID &&
		win.GetWindowThreadProcessId(window.overlay, nil) == window.key.threadID &&
		win.GetWindow(window.hwnd, win.GW_OWNER) == window.key.owner &&
		win.GetParent(window.overlay) == window.hwnd &&
		windowClassMatches(window.hwnd, "RayanLamp.TrayPopup.v2") &&
		windowClassMatches(window.overlay, "RayanLamp.TrayPopup.Overlay.v1")
}

func windowClassMatches(hwnd win.HWND, expected string) bool {
	if hwnd == 0 || expected == "" {
		return false
	}
	var className [128]uint16
	count, err := win.GetClassName(hwnd, &className[0], len(className))
	return err == nil && count > 0 &&
		windows.UTF16ToString(className[:count]) == expected
}

func popupWindowCanBeRetained(window *popupWindow) bool {
	if window == nil {
		return false
	}
	popupWindowMu.Lock()
	defer popupWindowMu.Unlock()
	return !window.retired && popupWindowCache[window.key] == window
}

func (window *popupWindow) invalidateComposition() {
	if window == nil {
		return
	}
	window.composition.backdropValid = false
	window.composition.regionValid = false
}

func invalidatePopupWindowComposition(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	popupWindowMu.Lock()
	defer popupWindowMu.Unlock()
	for _, window := range popupWindowCache {
		if window != nil && window.hwnd == hwnd {
			window.invalidateComposition()
			return
		}
	}
}

func releasePopupWindow(window *popupWindow) {
	if window == nil {
		return
	}
	popupWindowMu.Lock()
	if popupActiveWindow[window.key] == window {
		delete(popupActiveWindow, window.key)
	}
	popupWindowMu.Unlock()
}

func destroyPopupWindow(window *popupWindow) {
	if window == nil {
		return
	}
	popupWindowMu.Lock()
	if popupWindowCache[window.key] == window {
		delete(popupWindowCache, window.key)
	}
	if popupActiveWindow[window.key] == window {
		delete(popupActiveWindow, window.key)
	}
	window.retired = true
	hwnd := window.hwnd
	popupWindowMu.Unlock()

	if hwnd == 0 {
		return
	}
	clearWindowBackdrop(hwnd)
	destroyed := win.DestroyWindow(hwnd)
	if destroyed || !nativeWindowExists(hwnd) {
		popupWindowMu.Lock()
		if window.hwnd == hwnd {
			window.hwnd = 0
			window.overlay = 0
		}
		popupWindowMu.Unlock()
		return
	}

	// Retain the only handles when USER32 rejects destruction (for example,
	// an accidental wrong-thread call) so a later owner-thread cleanup can
	// retry instead of leaking an unreachable orphan.
	popupWindowMu.Lock()
	if popupWindowCache[window.key] == nil {
		popupWindowCache[window.key] = window
	}
	popupWindowMu.Unlock()
}

func purgePopupWindowHandle(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	popupWindowMu.Lock()
	for key, window := range popupWindowCache {
		if window != nil && window.hwnd == hwnd {
			delete(popupWindowCache, key)
			window.retired = true
			window.hwnd = 0
			window.overlay = 0
		}
	}
	for key, window := range popupActiveWindow {
		if window != nil && window.hwnd == hwnd {
			delete(popupActiveWindow, key)
			window.retired = true
			window.hwnd = 0
			window.overlay = 0
		}
	}
	popupWindowMu.Unlock()
}

// DiscardCached releases the reusable popup associated with owner. It must be
// called on the owner's UI thread; Shell does so when reuse is disabled and
// before its main HWND is destroyed.
func DiscardCached(owner uintptr) {
	key := popupWindowKey{
		owner:    win.HWND(owner),
		threadID: win.GetCurrentThreadId(),
	}
	popupWindowMu.Lock()
	window := popupWindowCache[key]
	if window != nil {
		delete(popupWindowCache, key)
		window.retired = true
	}
	active := window != nil && popupActiveWindow[key] == window
	popupWindowMu.Unlock()
	if window == nil {
		return
	}
	if active {
		if win.PostMessage(window.hwnd, win.WM_CLOSE, 0, 0) == 0 {
			destroyPopupWindow(window)
		}
		return
	}
	destroyPopupWindow(window)
}

func registerPopupClass() error {
	popupClassOnce.Do(func() {
		var err error
		popupClassName, err = windows.UTF16FromString("RayanLamp.TrayPopup.v2")
		if err != nil {
			popupClassErr = err
			return
		}
		class := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			Style:         win.CS_HREDRAW | win.CS_VREDRAW,
			LpfnWndProc:   popupCallback,
			HInstance:     win.GetModuleHandle(nil),
			HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
			LpszClassName: &popupClassName[0],
		}
		if win.RegisterClassEx(&class) == 0 {
			popupClassErr = errors.New("RegisterClassEx failed")
			return
		}

		popupOverlayClassName, err = windows.UTF16FromString(
			"RayanLamp.TrayPopup.Overlay.v1",
		)
		if err != nil {
			popupClassErr = err
			return
		}
		overlayClass := win.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
			Style:         win.CS_HREDRAW | win.CS_VREDRAW,
			LpfnWndProc:   popupOverlayCallback,
			HInstance:     win.GetModuleHandle(nil),
			HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
			LpszClassName: &popupOverlayClassName[0],
		}
		if win.RegisterClassEx(&overlayClass) == 0 {
			popupClassErr = errors.New("RegisterClassEx for layered overlay failed")
		}
		runtime.KeepAlive(popupClassName)
		runtime.KeepAlive(popupOverlayClassName)
	})
	return popupClassErr
}

func popupWindowProc(rawHWND uintptr, message uint32, wParam, lParam uintptr) uintptr {
	hwnd := win.HWND(rawHWND)
	value, exists := popupSessions.Load(hwnd)
	if !exists {
		if popupAppearanceMessage(message) {
			invalidatePopupWindowComposition(hwnd)
		}
		switch message {
		case win.WM_PAINT:
			var paint win.PAINTSTRUCT
			if destination := win.BeginPaint(hwnd, &paint); destination != 0 {
				win.EndPaint(hwnd, &paint)
			}
			return 0
		case win.WM_ERASEBKGND:
			return 1
		case win.WM_NCDESTROY:
			purgePopupWindowHandle(hwnd)
		}
		return win.DefWindowProc(hwnd, message, wParam, lParam)
	}
	session := value.(*popupSession)

	if popupAppearanceMessage(message) {
		session.window.invalidateComposition()
		appearance, _ := winfx.ResolveAppearance(
			string(session.options.Theme),
		)
		if !session.applyResolvedAppearance(appearance) {
			// Custom owner-drawn chrome must yield immediately when Windows
			// enters High Contrast. Destroying (rather than hiding) retires
			// the cached HWND; the next invocation is routed to TrackPopupMenu.
			session.result.Action = ""
			session.result.Cancelled = true
			session.destroyNow()
			return 0
		}
		if err := session.commitPopupComposition(
			session.dimensions.width,
			session.dimensions.height,
			applyWindowBackdrop,
			applyRoundedRegion,
		); err != nil {
			session.failRender()
			return 0
		}
		session.releaseForeground()
		win.InvalidateRect(hwnd, nil, false)
		win.UpdateWindow(hwnd)
		return 0
	}

	switch message {
	case win.WM_PAINT:
		session.paint()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_GETDLGCODE:
		return win.DLGC_WANTALLKEYS
	case win.WM_SETCURSOR:
		win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)))
		return 1
	case win.WM_MOUSEMOVE:
		// Mouse capture can suppress WM_SETCURSOR. Reassert during captured
		// navigation too, rather than inheriting the owner's last edge cursor.
		win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)))
		index := session.dimensions.rowAt(win.GET_X_LPARAM(lParam), win.GET_Y_LPARAM(lParam))
		if session.navigation.selectIndex(index) {
			session.selectionChanged()
		}
		return 0
	case win.WM_LBUTTONDOWN:
		index := session.dimensions.rowAt(win.GET_X_LPARAM(lParam), win.GET_Y_LPARAM(lParam))
		if index < 0 {
			session.finish("", true)
			return 0
		}
		if session.navigation.selectIndex(index) {
			session.selectionChanged()
		}
		return 0
	case win.WM_LBUTTONUP:
		index := session.dimensions.rowAt(win.GET_X_LPARAM(lParam), win.GET_Y_LPARAM(lParam))
		if index < 0 || !session.navigation.selectable(index) {
			session.finish("", true)
			return 0
		}
		session.finish(session.options.Items[index].ID, false)
		return 0
	case win.WM_RBUTTONDOWN, win.WM_CANCELMODE:
		session.finish("", true)
		return 0
	case win.WM_CLOSE:
		if session.renderFailed {
			session.destroyNow()
		} else {
			session.finish("", true)
		}
		return 0
	case win.WM_KEYDOWN:
		switch uint32(wParam) {
		case win.VK_UP:
			if session.navigation.move(-1) {
				session.selectionChanged()
			}
		case win.VK_DOWN:
			if session.navigation.move(1) {
				session.selectionChanged()
			}
		case win.VK_HOME:
			if session.navigation.home() {
				session.selectionChanged()
			}
		case win.VK_END:
			if session.navigation.end() {
				session.selectionChanged()
			}
		case win.VK_RETURN, win.VK_SPACE:
			if action, ok := session.navigation.action(); ok {
				session.finish(action, false)
			}
		case win.VK_ESCAPE:
			session.finish("", true)
		}
		return 0
	case win.WM_CHAR:
		if session.navigation.matchRune(rune(wParam)) {
			session.selectionChanged()
		}
		return 0
	case wmPopupAnimationFrame:
		if consumePopupAnimationFrame(
			session.animationRunning,
			session.animationToken,
			uint32(wParam),
			session.animationGate,
		) {
			session.animationTick()
			return 0
		}
		return 0
	case win.WM_ACTIVATE:
		if session.shown && !session.activating &&
			uint16(wParam&0xffff) == win.WA_INACTIVE {
			session.finish("", true)
			return 0
		}
	case win.WM_KILLFOCUS, win.WM_CAPTURECHANGED:
		if session.shown && !session.activating && !session.closing {
			session.finish("", true)
			return 0
		}
	case win.WM_DPICHANGED:
		// The popup is deliberately short-lived. Rebuilding every GDI resource
		// inside a DPI transition is riskier than handing the interaction to the
		// native fallback, which is already per-monitor aware.
		session.failRender()
		return 0
	case win.WM_DESTROY:
		session.stopAnimationScheduler()
		session.done = true
		session.lifecycle.invalidate()
		return 0
	case win.WM_NCDESTROY:
		popupSessions.Delete(hwnd)
		purgePopupWindowHandle(hwnd)
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

func popupAppearanceMessage(message uint32) bool {
	switch message {
	case win.WM_THEMECHANGED,
		win.WM_SETTINGCHANGE,
		wmSysColorChanged,
		wmDWMCompositionChanged,
		wmDWMColorizationChanged:
		return true
	default:
		return false
	}
}

func (s *popupSession) applyResolvedAppearance(
	appearance winfx.ResolvedAppearance,
) bool {
	if s == nil || appearance.HighContrast {
		return false
	}
	s.dark = appearance.Dark
	s.transparency = appearance.TransparencyEnabled
	s.motionEnabled = appearance.MotionEnabled &&
		!s.options.DisableAnimations
	if !s.motionEnabled {
		s.stopAnimationScheduler()
		s.presenting = 0
		s.presentation = popupPresentationFullyShown
		s.presentationMotion.active = false
		s.hover.settle(s.navigation.selected)
		s.updatePresentationPosition()
	}
	s.palette = paletteFor(appearance.Dark)
	return true
}

func popupOverlayWindowProc(
	rawHWND uintptr,
	message uint32,
	wParam, lParam uintptr,
) uintptr {
	hwnd := win.HWND(rawHWND)
	switch message {
	case win.WM_SETCURSOR:
		// The transparent visual overlay must not inherit a resize cursor
		// from the owner's non-client border beneath the popup.
		win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)))
		return 1
	case win.WM_PAINT:
		var paint win.PAINTSTRUCT
		if destination := win.BeginPaint(hwnd, &paint); destination != 0 {
			win.EndPaint(hwnd, &paint)
		}
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_NCHITTEST:
		return ^uintptr(0) // HTTRANSPARENT
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

func (s *popupSession) finish(action ActionID, cancelled bool) {
	if s.closing {
		return
	}
	s.closing = true
	s.result.Action = action
	s.result.Cancelled = cancelled
	s.shown = false
	releasePopupCapture(s.hwnd)
	if !s.lifecycle.beginHiding() {
		s.destroyNow()
		return
	}
	if !s.motionEnabled {
		s.completeNow()
		return
	}
	if s.presentation == 0 {
		s.completeNow()
		return
	}
	s.presenting = -1
	now := time.Now()
	firstFrameLead := time.Duration(popupAnimationInterval) * time.Millisecond
	s.presentationMotion.begin(
		s.presentation,
		0,
		now.Add(-firstFrameLead),
		popupDismissDuration,
	)
	s.lastAnimationTick = now.Add(-firstFrameLead)
	// Native controls react on release in the same input turn. Advance the
	// first dismissal frame now so the popup never sits motionless waiting for
	// a ticker's initial 16 ms interval.
	s.animationTickAt(now)
	if !s.done {
		s.ensureAnimationScheduler()
	}
}

func (s *popupSession) failRender() {
	if s.renderFailed {
		return
	}
	s.renderFailed = true
	win.PostMessage(s.hwnd, win.WM_CLOSE, 0, 0)
}

func createForegroundOverlay(parent win.HWND) (win.HWND, error) {
	windowTitle, _ := windows.UTF16FromString("")
	overlay := win.CreateWindowEx(
		win.WS_EX_LAYERED|win.WS_EX_TRANSPARENT|win.WS_EX_NOACTIVATE,
		&popupOverlayClassName[0],
		&windowTitle[0],
		win.WS_CHILD,
		0,
		0,
		1,
		1,
		parent,
		0,
		win.GetModuleHandle(nil),
		nil,
	)
	runtime.KeepAlive(windowTitle)
	if overlay == 0 {
		return 0, fmt.Errorf(
			"traymenu: create layered foreground failed (Win32 error %d)",
			win.GetLastError(),
		)
	}
	return overlay, nil
}

func (s *popupSession) prepareHiddenFirstFrame() error {
	if s == nil || s.window == nil || s.hwnd == 0 || s.overlay == 0 {
		return errors.New("traymenu: popup window is unavailable")
	}
	if !s.lifecycle.beginPreparation() {
		return errors.New("traymenu: popup lifecycle rejected hidden preparation")
	}

	win.ShowWindow(s.hwnd, win.SW_HIDE)
	win.ShowWindow(s.overlay, win.SW_HIDE)
	width, height := s.dimensions.width, s.dimensions.height
	offset := presentationOffset(s.slideOffset, s.presentation)
	if !win.SetWindowPos(
		s.hwnd,
		win.HWND_TOPMOST,
		s.finalX,
		s.finalY+offset,
		width,
		height,
		win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER,
	) {
		return errors.New("traymenu: size hidden popup failed")
	}
	if !win.SetWindowPos(
		s.overlay,
		win.HWND_TOP,
		0,
		0,
		width,
		height,
		win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER,
	) {
		return errors.New("traymenu: size hidden foreground failed")
	}
	if err := s.commitPopupComposition(
		width,
		height,
		applyWindowBackdrop,
		applyRoundedRegion,
	); err != nil {
		return err
	}

	destination := win.GetDC(s.hwnd)
	if destination == 0 {
		return errors.New("traymenu: GetDC for hidden first frame failed")
	}
	paintErr := s.paintToDC(destination)
	win.ReleaseDC(s.hwnd, destination)
	if paintErr != nil {
		return paintErr
	}
	validateWindow(s.hwnd)
	validateWindow(s.overlay)

	// The child is made logically visible only after UpdateLayeredWindow has
	// supplied its first BGRA surface. Its hidden ancestor prevents exposure.
	if !win.SetWindowPos(
		s.overlay,
		win.HWND_TOP,
		0,
		0,
		width,
		height,
		win.SWP_NOACTIVATE|win.SWP_NOOWNERZORDER|win.SWP_SHOWWINDOW,
	) {
		return errors.New("traymenu: arm prepared foreground failed")
	}
	// UpdateLayeredWindow is the required foreground commit. A DWM barrier is
	// useful only after the material host itself changed; forcing it for an
	// already prepared cached HWND adds at least one compositor interval to
	// every menu opening.
	if s.backdropChanged {
		_ = flushDWM()
	}
	if !s.lifecycle.commitPreparation() {
		return errors.New("traymenu: popup lifecycle rejected prepared frame")
	}
	return nil
}

type popupBackdropApplier func(win.HWND, Backdrop, bool, bool) Backdrop

type popupRegionApplier func(win.HWND, int32, int32, int32) error

func (s *popupSession) commitPopupComposition(
	width, height int32,
	applyBackdrop popupBackdropApplier,
	applyRegion popupRegionApplier,
) error {
	if s == nil || applyBackdrop == nil || applyRegion == nil {
		return errors.New("traymenu: popup composition parameters are invalid")
	}
	s.backdropChanged = false
	cache := (*popupCompositionCache)(nil)
	if s.window != nil {
		cache = &s.window.composition
	}
	if cache != nil && cache.backdropValid &&
		cache.requested == s.options.Backdrop &&
		cache.dark == s.dark &&
		cache.transparency == s.transparency {
		s.result.BackdropUsed = cache.used
	} else {
		s.result.BackdropUsed = applyBackdrop(
			s.hwnd,
			s.options.Backdrop,
			s.dark,
			s.transparency,
		)
		s.backdropChanged = true
		if cache != nil {
			cache.backdropValid = true
			cache.requested = s.options.Backdrop
			cache.dark = s.dark
			cache.transparency = s.transparency
			cache.used = s.result.BackdropUsed
			// DWM/Accent may replace the native visual clip.
			cache.regionValid = false
		}
	}
	if cache != nil && cache.regionValid &&
		cache.width == width &&
		cache.height == height &&
		cache.radius == s.dimensions.cornerRadius {
		return nil
	}
	// DWM/Accent creates or rebuilds the host's compositor visual. Commit the
	// canonical parent and foreground regions after that mutation so the live
	// material visual receives the same final clip as the layered pixels.
	if err := applyRegion(
		s.hwnd,
		width,
		height,
		s.dimensions.cornerRadius,
	); err != nil {
		return err
	}
	if err := applyRegion(
		s.overlay,
		width,
		height,
		s.dimensions.cornerRadius,
	); err != nil {
		return err
	}
	if cache != nil {
		cache.regionValid = true
		cache.width = width
		cache.height = height
		cache.radius = s.dimensions.cornerRadius
	}
	return nil
}

func (s *popupSession) selectionChanged() {
	if s == nil || s.closing {
		return
	}
	if !s.motionEnabled {
		s.hover.settle(s.navigation.selected)
		if err := s.presentForeground(); err != nil {
			s.failRender()
		}
		return
	}
	s.ensureAnimationScheduler()
}

func (s *popupSession) ensureAnimationScheduler() {
	if s == nil || s.hwnd == 0 || !s.motionEnabled ||
		s.animationRunning {
		return
	}
	now := time.Now()
	if s.presenting != 0 && !s.presentationMotion.active {
		target := uint8(0)
		duration := popupDismissDuration
		if s.presenting > 0 {
			target = popupPresentationFullyShown
			duration = popupRevealDuration
		}
		s.presentationMotion.begin(
			s.presentation,
			target,
			now,
			duration,
		)
	}
	if s.lastAnimationTick.IsZero() {
		s.lastAnimationTick = now
	}
	token := atomic.AddUint32(&popupAnimationSequence, 1)
	if token == 0 {
		token = atomic.AddUint32(&popupAnimationSequence, 1)
	}
	stop := make(chan struct{})
	gate := new(popupFrameGate)
	s.animationToken = token
	s.animationStop = stop
	s.animationRunning = true
	s.animationGate = gate
	go runPopupAnimationScheduler(
		s.hwnd,
		token,
		stop,
		gate,
	)
}

func runPopupAnimationScheduler(
	hwnd win.HWND,
	token uint32,
	stop <-chan struct{},
	gate *popupFrameGate,
) {
	ticker := time.NewTicker(
		time.Duration(popupAnimationInterval) * time.Millisecond,
	)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			gate.request(func() bool {
				return win.PostMessage(
					hwnd,
					wmPopupAnimationFrame,
					uintptr(token),
					0,
				) != 0
			})
		}
	}
}

func (s *popupSession) stopAnimationScheduler() {
	if s == nil || !s.animationRunning {
		return
	}
	stop := s.animationStop
	s.animationRunning = false
	s.animationToken = 0
	s.animationStop = nil
	s.animationGate = nil
	if stop != nil {
		close(stop)
	}
}

func (s *popupSession) animationTick() {
	s.animationTickAt(time.Now())
}

func (s *popupSession) animationTickAt(now time.Time) {
	if s == nil || s.done {
		return
	}
	redraw := false
	continuing := false

	elapsed := now.Sub(s.lastAnimationTick)
	if elapsed <= 0 {
		elapsed = time.Duration(popupAnimationInterval) * time.Millisecond
	}
	s.lastAnimationTick = now

	if s.presenting != 0 {
		next, changed, presentationContinuing :=
			s.presentationMotion.advance(now)
		s.presentation = next
		if s.presenting < 0 && next <= popupDismissCutoff {
			// The layered foreground is already effectively transparent here,
			// while the live DWM material host cannot share that per-pixel
			// alpha. Hiding at the perceptual endpoint avoids a stationary
			// glass rectangle lingering through quantized near-zero frames.
			s.presentation = 0
			s.completeNow()
			return
		}
		if changed {
			s.updatePresentationPosition()
			redraw = true
		}
		continuing = continuing || presentationContinuing
		if !presentationContinuing {
			direction := s.presenting
			s.presenting = 0
			if direction < 0 {
				s.completeNow()
				return
			}
		}
	}

	if !s.closing {
		changed, hoverContinuing := s.hover.advance(
			s.navigation.selected,
			elapsed,
		)
		redraw = redraw || changed
		continuing = continuing || hoverContinuing
	}
	if redraw {
		// UpdateLayeredWindow is itself the atomic presentation operation.
		// Calling it directly avoids chaining a posted frame request and
		// low-priority WM_PAINT, which previously let several logical ticks
		// collapse into one visible frame under pointer traffic.
		if err := s.presentForeground(); err != nil {
			s.failRender()
			return
		}
	}
	if !continuing && s.presenting == 0 {
		s.lastAnimationTick = time.Time{}
		s.stopAnimationScheduler()
	}
}

func (s *popupSession) updatePresentationPosition() {
	if s == nil || s.hwnd == 0 {
		return
	}
	offset := presentationOffset(s.slideOffset, s.presentation)
	win.SetWindowPos(
		s.hwnd,
		0,
		s.finalX,
		s.finalY+offset,
		0,
		0,
		win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE,
	)
}

func (s *popupSession) destroyNow() {
	if s == nil || s.done {
		return
	}
	s.stopAnimationScheduler()
	s.done = true
	s.shown = false
	s.activating = false
	s.lifecycle.invalidate()
	releasePopupCapture(s.hwnd)
	s.releaseForeground()
	popupSessions.Delete(s.hwnd)
	destroyPopupWindow(s.window)
}

func (s *popupSession) completeNow() {
	if s == nil || s.done {
		return
	}
	s.stopAnimationScheduler()
	s.shown = false
	s.activating = false
	releasePopupCapture(s.hwnd)
	s.releaseForeground()

	keepReusable := !s.options.DisableWindowReuse &&
		!s.renderFailed &&
		popupWindowCanBeRetained(s.window)
	if keepReusable && hidePopupWindow(s.hwnd, s.overlay) {
		if s.lifecycle.completeHiding(true) != popupCloseHide {
			s.destroyNow()
			return
		}
		popupSessions.Delete(s.hwnd)
		releasePopupWindow(s.window)
		s.done = true
		return
	}
	s.lifecycle.completeHiding(false)
	s.destroyNow()
}

func hidePopupWindow(hwnd, overlay win.HWND) bool {
	hideFlags := uint32(
		win.SWP_HIDEWINDOW |
			win.SWP_NOMOVE |
			win.SWP_NOSIZE |
			win.SWP_NOZORDER |
			win.SWP_NOACTIVATE |
			win.SWP_NOOWNERZORDER,
	)
	if hwnd == 0 || !win.SetWindowPos(hwnd, 0, 0, 0, 0, 0, hideFlags) {
		return false
	}
	if overlay == 0 ||
		!win.SetWindowPos(overlay, 0, 0, 0, 0, 0, hideFlags) {
		return false
	}
	return !win.IsWindowVisible(hwnd)
}
