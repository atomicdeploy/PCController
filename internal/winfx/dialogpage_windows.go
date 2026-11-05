//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/design"
)

const (
	dialogPageHostSubclassID   = 0x524C5048 // "RLPH"
	dialogPageHeaderSubclassID = 0x524C5054 // "RLPT"
	dialogPageOverlaySubclass  = 0x524C504F // "RLPO"

	dialogPageWMPrint              = 0x0317
	dialogPageWMPrintClient        = 0x0318
	dialogPageWMEraseBkgnd         = 0x0014
	dialogPageWMPaint              = 0x000F
	dialogPageWMNCHitTest          = 0x0084
	dialogPageWMNotify             = 0x004E
	dialogPageWMWindowPos          = 0x0047
	dialogPageWMSize               = 0x0005
	dialogPageWMThemeChange        = 0x031A
	dialogPageWMDPIChanged         = 0x02E0
	dialogPageWMSetting            = 0x001A
	dialogPageWMControlColorStatic = 0x0138
	dialogPageWMAnimate            = 0x8000 + 0x0553

	dialogPageTCMFirst       = 0x1300
	dialogPageTCMSetCurSel   = dialogPageTCMFirst + 12
	dialogPageTCMSelChange   = -551
	dialogPageTCMSelChanging = -552

	dialogPagePRFCheckVisible = 0x00000001
	dialogPagePRFNonClient    = 0x00000002
	dialogPagePRFClient       = 0x00000004
	dialogPagePRFErase        = 0x00000008
	dialogPagePRFChildren     = 0x00000010
	dialogPagePRFOwned        = 0x00000020

	dialogPageWSChild        = 0x40000000
	dialogPageWSClipSiblings = 0x04000000
	dialogPageWSExNoActivate = 0x08000000

	dialogPageSWPHideWindow = 0x0080
	dialogPageSWPShowWindow = 0x0040
	dialogPageSWPNoActivate = 0x0010

	dialogPageRDWInvalidate  = 0x0001
	dialogPageRDWErase       = 0x0004
	dialogPageRDWAllChildren = 0x0080
	dialogPageRDWUpdateNow   = 0x0100

	dialogPageBkTransparent = 1
	dialogPageSRCCopy       = 0x00CC0020
	dialogPageColorWindow   = 5

	dialogPageAnimationFallback = 180 * time.Millisecond
	dialogPageSlideDIP          = 14
)

type dialogPageRect struct {
	Left, Top, Right, Bottom int32
}

type dialogPagePoint struct {
	X, Y int32
}

type dialogPagePaintStruct struct {
	DC          uintptr
	Erase       int32
	Paint       dialogPageRect
	Restore     int32
	Incremental int32
	Reserved    [32]byte
}

type dialogPageNotifyHeader struct {
	Window uintptr
	ID     uintptr
	Code   int32
}

type dialogPageSurface struct {
	memory    uintptr
	bitmap    uintptr
	oldBitmap uintptr
	width     int32
	height    int32
}

type dialogPageFrameScheduler struct {
	token   uint32
	stop    chan struct{}
	pending atomic.Bool
}

type dialogPageTransitionState struct {
	host    uintptr
	header  uintptr
	overlay uintptr
	pages   []uintptr

	selected      int
	preparedFrom  int
	target        int
	direction     int
	prepared      bool
	running       bool
	animate       bool
	redrawBlocked uintptr

	pageRect dialogPageRect
	from     dialogPageSurface
	to       dialogPageSurface
	frame    dialogPageSurface
	started  time.Time
	duration time.Duration

	disposed atomic.Bool
}

// DialogPageTransitions owns the short-lived visual overlay used while a
// native tab widget changes pages. The actual controls remain ordinary HWNDs,
// so focus, mnemonics, keyboard navigation, automation, and accessibility are
// never replaced by a bitmap-only UI.
type DialogPageTransitions struct {
	state *dialogPageTransitionState
}

var (
	dialogPageUser32                 = windows.NewLazySystemDLL("user32.dll")
	dialogPageCreateWindowEx         = dialogPageUser32.NewProc("CreateWindowExW")
	dialogPageDestroyWindow          = dialogPageUser32.NewProc("DestroyWindow")
	dialogPageFindWindowEx           = dialogPageUser32.NewProc("FindWindowExW")
	dialogPageGetClientRect          = dialogPageUser32.NewProc("GetClientRect")
	dialogPageGetWindowRect          = dialogPageUser32.NewProc("GetWindowRect")
	dialogPageMapWindowPoints        = dialogPageUser32.NewProc("MapWindowPoints")
	dialogPageGetDC                  = dialogPageUser32.NewProc("GetDC")
	dialogPageReleaseDC              = dialogPageUser32.NewProc("ReleaseDC")
	dialogPageSendMessage            = dialogPageUser32.NewProc("SendMessageW")
	dialogPageSetWindowPos           = dialogPageUser32.NewProc("SetWindowPos")
	dialogPageRedrawWindow           = dialogPageUser32.NewProc("RedrawWindow")
	dialogPageInvalidateRect         = dialogPageUser32.NewProc("InvalidateRect")
	dialogPageUpdateWindow           = dialogPageUser32.NewProc("UpdateWindow")
	dialogPageBeginPaint             = dialogPageUser32.NewProc("BeginPaint")
	dialogPageEndPaint               = dialogPageUser32.NewProc("EndPaint")
	dialogPagePostMessage            = dialogPageUser32.NewProc("PostMessageW")
	dialogPageGetDPIForWindow        = dialogPageUser32.NewProc("GetDpiForWindow")
	dialogPageFillRect               = dialogPageUser32.NewProc("FillRect")
	dialogPageGetSysColorBrush       = dialogPageUser32.NewProc("GetSysColorBrush")
	dialogPageGDI32                  = windows.NewLazySystemDLL("gdi32.dll")
	dialogPageCreateCompatibleDC     = dialogPageGDI32.NewProc("CreateCompatibleDC")
	dialogPageCreateCompatibleBitmap = dialogPageGDI32.NewProc("CreateCompatibleBitmap")
	dialogPageSelectObject           = dialogPageGDI32.NewProc("SelectObject")
	dialogPageDeleteObject           = dialogPageGDI32.NewProc("DeleteObject")
	dialogPageDeleteDC               = dialogPageGDI32.NewProc("DeleteDC")
	dialogPageBitBlt                 = dialogPageGDI32.NewProc("BitBlt")
	dialogPageSetBkMode              = dialogPageGDI32.NewProc("SetBkMode")
	dialogPageAlphaBlend             = windows.NewLazySystemDLL("msimg32.dll").NewProc("AlphaBlend")
	dialogPageRtlMoveMemory          = windows.NewLazySystemDLL("ntdll.dll").NewProc("RtlMoveMemory")

	dialogPageStatesMu sync.RWMutex
	dialogPageStates   = make(map[uintptr]*dialogPageTransitionState)

	dialogPageSchedulerMu       sync.Mutex
	dialogPageSchedulers        = make(map[uintptr]*dialogPageFrameScheduler)
	dialogPageSchedulerSequence uint32
)

// AttachDialogPageTransitions adds an atomic visual handoff to a native tab
// host. tabHost is the Walk TabWidget HWND and pages contains its TabPage
// children in selection order. The overlay is one non-activating child sized
// strictly to the page client rectangle; it never covers the tab header.
func AttachDialogPageTransitions(
	tabHost uintptr,
	pages []uintptr,
	initialIndex int,
	animate bool,
) (*DialogPageTransitions, error) {
	if tabHost == 0 || !isWindow(tabHost) {
		return nil, ErrInvalidWindow
	}
	if len(pages) == 0 {
		return nil, errors.New("winfx: dialog page list is empty")
	}
	if initialIndex < 0 || initialIndex >= len(pages) {
		return nil, fmt.Errorf(
			"winfx: invalid initial dialog page %d of %d",
			initialIndex,
			len(pages),
		)
	}
	for index, page := range pages {
		if page == 0 || !isWindow(page) {
			return nil, fmt.Errorf(
				"winfx: invalid dialog page window at index %d",
				index,
			)
		}
	}

	tabClass, err := windows.UTF16PtrFromString("SysTabControl32")
	if err != nil {
		return nil, err
	}
	header, _, callErr := dialogPageFindWindowEx.Call(
		tabHost,
		0,
		uintptr(unsafe.Pointer(tabClass)),
		0,
	)
	runtime.KeepAlive(tabClass)
	if header == 0 {
		return nil, boolCallError("FindWindowExW(SysTabControl32)", callErr)
	}

	staticClass, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		return nil, err
	}
	overlay, _, callErr := dialogPageCreateWindowEx.Call(
		dialogPageWSExNoActivate,
		uintptr(unsafe.Pointer(staticClass)),
		0,
		dialogPageWSChild|dialogPageWSClipSiblings,
		0,
		0,
		1,
		1,
		tabHost,
		0,
		0,
		0,
	)
	runtime.KeepAlive(staticClass)
	if overlay == 0 {
		return nil, boolCallError("CreateWindowExW(dialog page overlay)", callErr)
	}

	state := &dialogPageTransitionState{
		host:         tabHost,
		header:       header,
		overlay:      overlay,
		pages:        append([]uintptr(nil), pages...),
		selected:     initialIndex,
		preparedFrom: initialIndex,
		target:       initialIndex,
		animate:      animate,
		duration:     dialogPageAnimationDuration(),
	}
	dialogPageStatesMu.Lock()
	dialogPageStates[overlay] = state
	dialogPageStatesMu.Unlock()

	cleanup := func() {
		dialogPageStatesMu.Lock()
		delete(dialogPageStates, overlay)
		dialogPageStatesMu.Unlock()
		_, _, _ = dialogPageDestroyWindow.Call(overlay)
	}
	if err := SetWindowSubclass(
		overlay,
		dialogPageOverlaySubclass,
		dialogPageOverlayMessage,
	); err != nil {
		cleanup()
		return nil, fmt.Errorf("subclass dialog page overlay: %w", err)
	}
	if err := SetWindowSubclass(
		tabHost,
		dialogPageHostSubclassID,
		func(hwnd, message, wParam, lParam uintptr) (uintptr, bool) {
			return state.hostMessage(hwnd, message, wParam, lParam)
		},
	); err != nil {
		_ = RemoveWindowSubclass(overlay, dialogPageOverlaySubclass)
		cleanup()
		return nil, fmt.Errorf("subclass dialog page host: %w", err)
	}
	if err := SetWindowSubclass(
		header,
		dialogPageHeaderSubclassID,
		func(hwnd, message, wParam, lParam uintptr) (uintptr, bool) {
			return state.headerMessage(hwnd, message, wParam, lParam)
		},
	); err != nil {
		_ = RemoveWindowSubclass(tabHost, dialogPageHostSubclassID)
		_ = RemoveWindowSubclass(overlay, dialogPageOverlaySubclass)
		cleanup()
		return nil, fmt.Errorf("subclass dialog page header: %w", err)
	}

	// Prime the current app-owned page while the dialog is still hidden. The
	// first real selection can therefore be staged before Walk hides it.
	_ = state.captureSelected()
	return &DialogPageTransitions{state: state}, nil
}

// SelectionChanged must be called from the tab widget's native selection
// event after Walk has made the destination page current.
func (transitions *DialogPageTransitions) SelectionChanged(index int) {
	state := transitions.pageState()
	if state == nil {
		return
	}
	state.selectionChanged(index, time.Now())
}

// SetMotion applies the live "Animations" preference. Turning motion off
// during a transition settles the destination in the same UI dispatch.
func (transitions *DialogPageTransitions) SetMotion(animate bool) {
	state := transitions.pageState()
	if state == nil {
		return
	}
	state.animate = animate
	if !animate && state.running {
		state.finish(true)
	} else if !animate && state.prepared {
		state.prepared = false
		state.restorePageRedraw()
		state.hideOverlay()
	}
}

// Invalidate drops page render caches after DPI, theme, or layout changes.
// The live selected page remains visible and is captured lazily next time.
func (transitions *DialogPageTransitions) Invalidate() {
	state := transitions.pageState()
	if state == nil {
		return
	}
	state.invalidateSurfaces()
}

// RefreshAppearance drops every old-theme surface, settles any in-flight tab
// animation, and synchronously repaints the native tab host. The root dialog
// can then commit one complete frame without a stale page overlay briefly
// covering the new palette.
func (transitions *DialogPageTransitions) RefreshAppearance() {
	state := transitions.pageState()
	if state == nil {
		return
	}
	state.invalidateSurfaces()
	_, _, _ = dialogPageRedrawWindow.Call(
		state.host,
		0,
		0,
		dialogPageRDWInvalidate|
			dialogPageRDWErase|
			dialogPageRDWAllChildren|
			dialogPageRDWUpdateNow,
	)
}

// Dispose releases the overlay, its retained DCs/bitmaps, both subclass hooks,
// and any pending scheduler generation. It is safe to call more than once.
func (transitions *DialogPageTransitions) Dispose() {
	if transitions == nil || transitions.state == nil {
		return
	}
	state := transitions.state
	if !state.disposed.CompareAndSwap(false, true) {
		return
	}
	stopDialogPageScheduler(state.overlay)
	state.restorePageRedraw()
	state.hideOverlay()
	_ = RemoveWindowSubclass(state.header, dialogPageHeaderSubclassID)
	_ = RemoveWindowSubclass(state.host, dialogPageHostSubclassID)
	_ = RemoveWindowSubclass(state.overlay, dialogPageOverlaySubclass)
	dialogPageStatesMu.Lock()
	delete(dialogPageStates, state.overlay)
	dialogPageStatesMu.Unlock()
	state.releaseSurfaces()
	if state.overlay != 0 && isWindow(state.overlay) {
		_, _, _ = dialogPageDestroyWindow.Call(state.overlay)
	}
	state.overlay = 0
	transitions.state = nil
}

func (transitions *DialogPageTransitions) pageState() *dialogPageTransitionState {
	if transitions == nil || transitions.state == nil ||
		transitions.state.disposed.Load() {
		return nil
	}
	return transitions.state
}

func (state *dialogPageTransitionState) hostMessage(
	_ uintptr,
	message uintptr,
	_ uintptr,
	lParam uintptr,
) (uintptr, bool) {
	if state == nil || state.disposed.Load() {
		return 0, false
	}
	switch uint32(message) {
	case dialogPageWMNotify:
		if lParam == 0 {
			break
		}
		header := readDialogPageNotifyHeader(lParam)
		if header.Window == state.header &&
			header.Code == dialogPageTCMSelChanging {
			state.prepareCurrent()
		}

	case dialogPageWMSize, dialogPageWMDPIChanged,
		dialogPageWMThemeChange, dialogPageWMSetting:
		if info, _ := GetOSInfo(); info.HighContrast {
			state.animate = false
			if state.running {
				state.finish(true)
			} else if state.prepared {
				state.prepared = false
				state.hideOverlay()
			}
		}
		state.invalidateSurfaces()

	case wmNCDestroy:
		// Dialog disposal normally calls Dispose first. This fallback stops
		// work promptly if an embedder destroys the host directly.
		stopDialogPageScheduler(state.overlay)
		state.restorePageRedraw()
		state.hideOverlay()
		state.releaseSurfaces()
		state.disposed.Store(true)
	}
	return 0, false
}

func (state *dialogPageTransitionState) headerMessage(
	_ uintptr,
	message uintptr,
	wParam uintptr,
	_ uintptr,
) (uintptr, bool) {
	if state == nil || state.disposed.Load() {
		return 0, false
	}
	if uint32(message) == dialogPageTCMSetCurSel {
		next := int(int32(wParam))
		if next >= 0 && next < len(state.pages) && next != state.selected {
			state.prepareCurrent()
		}
	}
	return 0, false
}

func dialogPageOverlayMessage(
	hwnd, message, wParam, _ uintptr,
) (uintptr, bool) {
	state := dialogPageState(hwnd)
	switch uint32(message) {
	case dialogPageWMPaint:
		if paintDialogPageOverlay(hwnd, state, 0) {
			return 0, true
		}

	case dialogPageWMPrintClient:
		if wParam != 0 && paintDialogPageOverlay(hwnd, state, wParam) {
			return 0, true
		}

	case dialogPageWMEraseBkgnd:
		return 1, true

	case dialogPageWMNCHitTest:
		// The transition is visual only. Pointer input keeps reaching the live
		// page controls and the independently interactive tab header.
		return ^uintptr(0), true // HTTRANSPARENT

	case dialogPageWMAnimate:
		if consumeDialogPageFrame(hwnd, uint32(wParam)) && state != nil {
			state.presentFrame(time.Now())
		}
		return 0, true

	case wmNCDestroy:
		stopDialogPageScheduler(hwnd)
		dialogPageStatesMu.Lock()
		delete(dialogPageStates, hwnd)
		dialogPageStatesMu.Unlock()
		if state != nil {
			state.releaseSurfaces()
			state.overlay = 0
		}
	}
	return 0, false
}

func (state *dialogPageTransitionState) prepareCurrent() {
	if state == nil || state.disposed.Load() ||
		state.selected < 0 || state.selected >= len(state.pages) {
		return
	}
	if state.running {
		// A second selection during motion starts exactly at the last
		// presented visual, rather than flashing either logical endpoint.
		state.compose(state.progressAt(time.Now()))
		_ = copyDialogPageSurface(&state.from, &state.frame)
		stopDialogPageScheduler(state.overlay)
		state.restorePageRedraw()
		state.running = false
		state.prepared = true
		state.preparedFrom = state.selected
		state.showOverlay()
		return
	}
	if err := state.capturePage(state.selected, &state.from); err != nil {
		state.prepared = false
		state.hideOverlay()
		return
	}
	state.prepared = true
	state.preparedFrom = state.selected
	state.compose(0)
	state.showOverlay()
}

func (state *dialogPageTransitionState) selectionChanged(
	index int,
	now time.Time,
) {
	if state == nil || state.disposed.Load() ||
		index < 0 || index >= len(state.pages) {
		return
	}
	if index == state.selected && !state.prepared && !state.running {
		return
	}
	previous := state.selected
	if !state.prepared {
		state.prepareCurrent()
	}
	state.target = index
	state.selected = index
	if !state.prepared {
		// A capture failure must degrade to the fully live destination, never
		// to an allocated-but-uninitialized bitmap. Refresh the settled cache
		// opportunistically for the next navigation.
		state.restorePageRedraw()
		state.hideOverlay()
		_ = state.capturePage(index, &state.from)
		return
	}
	// TCN_SELCHANGING already put the complete previous page over this client
	// rectangle. Walk has now selected and laid out the destination, so freeze
	// only that newly visible page before asking it for the offscreen frame.
	// Freezing every TabPage would let DefWindowProc alter hidden-page
	// WS_VISIBLE state and could make several pages overlap when redraw was
	// restored.
	state.blockPageRedraw(state.pages[index])
	if err := state.capturePage(index, &state.to); err != nil {
		state.finish(false)
		return
	}
	if previous <= index {
		state.direction = 1
	} else {
		state.direction = -1
	}
	if !state.animate || state.duration <= 0 {
		state.finish(true)
		return
	}

	state.started = now
	state.running = true
	state.prepared = false
	state.compose(0)
	state.showOverlay()
	startDialogPageScheduler(state.overlay)
}

func (state *dialogPageTransitionState) presentFrame(now time.Time) {
	if state == nil || !state.running || state.disposed.Load() {
		return
	}
	progress := state.progressAt(now)
	if progress >= 1 {
		// finish performs the atomic cached/live handoff. Avoid presenting an
		// extra terminal animation frame immediately before that handoff.
		state.finish(true)
		return
	}
	state.compose(progress)
	_, _, _ = dialogPageInvalidateRect.Call(state.overlay, 0, 0)
	_, _, _ = dialogPageUpdateWindow.Call(state.overlay)
}

func (state *dialogPageTransitionState) progressAt(now time.Time) float64 {
	if state == nil || !state.running || state.duration <= 0 {
		return 1
	}
	return dialogClamp(float64(now.Sub(state.started)) / float64(state.duration))
}

func (state *dialogPageTransitionState) finish(showTarget bool) {
	if state == nil {
		return
	}
	stopDialogPageScheduler(state.overlay)
	state.running = false
	state.prepared = false

	if showTarget && state.target >= 0 && state.target < len(state.pages) {
		state.compose(1)
		_, _, _ = dialogPageInvalidateRect.Call(state.overlay, 0, 0)
		_, _, _ = dialogPageUpdateWindow.Call(state.overlay)
		state.restorePageRedraw()
		page := state.pages[state.target]
		_, _, _ = dialogPageRedrawWindow.Call(
			page,
			0,
			0,
			dialogPageRDWInvalidate|
				dialogPageRDWErase|
				dialogPageRDWAllChildren|
				dialogPageRDWUpdateNow,
		)
		// The live hierarchy is now complete behind the still-visible final
		// overlay frame. Removing that overlay is the only remaining visual
		// commit; repainting it a second time here merely duplicated work and
		// could hold the terminal frame on screen for another dispatch.
		_ = copyDialogPageSurface(&state.from, &state.to)
	} else {
		state.restorePageRedraw()
	}
	state.hideOverlay()
}

func (state *dialogPageTransitionState) captureSelected() error {
	if state == nil || state.selected < 0 || state.selected >= len(state.pages) {
		return ErrInvalidWindow
	}
	return state.capturePage(state.selected, &state.from)
}

func (state *dialogPageTransitionState) capturePage(
	index int,
	target *dialogPageSurface,
) error {
	if state == nil || target == nil ||
		index < 0 || index >= len(state.pages) {
		return ErrInvalidWindow
	}
	page := state.pages[index]
	rect, err := dialogPageBoundsInHost(state.host, page)
	if err != nil {
		return err
	}
	width, height := rect.Right-rect.Left, rect.Bottom-rect.Top
	if width <= 0 || height <= 0 {
		return errors.New("winfx: dialog page has empty bounds")
	}
	if err := ensureDialogPageSurface(target, state.host, width, height); err != nil {
		return err
	}
	if err := ensureDialogPageSurface(&state.frame, state.host, width, height); err != nil {
		return err
	}
	if target != &state.from {
		if err := ensureDialogPageSurface(&state.from, state.host, width, height); err != nil {
			return err
		}
	}
	if target != &state.to {
		if err := ensureDialogPageSurface(&state.to, state.host, width, height); err != nil {
			return err
		}
	}
	state.pageRect = rect

	// NullBrush TabPages inherit the dialog's material. Ask the native parent
	// for the exact brush it would use for this page and DC. Walk also aligns
	// the gradient's brush origin in that WM_CTLCOLORSTATIC dispatch. Filling
	// first guarantees every compatible-bitmap pixel is initialized even if a
	// particular child declines WM_PRINT; the following page erase/print then
	// adds the complete live hierarchy without any desktop snapshot.
	brush, _, _ := dialogPageSendMessage.Call(
		state.host,
		dialogPageWMControlColorStatic,
		target.memory,
		page,
	)
	if brush == 0 {
		brush, _, _ = dialogPageGetSysColorBrush.Call(dialogPageColorWindow)
	}
	if !fillDialogPageSurface(target.memory, width, height, brush) {
		return errors.New("winfx: initialize dialog page surface failed")
	}
	_, _, _ = dialogPageSendMessage.Call(
		page,
		dialogPageWMEraseBkgnd,
		target.memory,
		0,
	)

	// The surface was already initialized from the aligned parent material.
	// Transparent text/background mode lets native labels, check boxes, and
	// radio buttons preserve that material instead of stamping opaque control
	// rectangles into the transition frame.
	_, _, _ = dialogPageSetBkMode.Call(
		target.memory,
		dialogPageBkTransparent,
	)
	_, _, _ = dialogPageSendMessage.Call(
		page,
		dialogPageWMPrint,
		target.memory,
		dialogPagePrintFlags(),
	)
	runtime.KeepAlive(state)
	return nil
}

func (state *dialogPageTransitionState) compose(progress float64) {
	if state == nil || state.frame.memory == 0 || state.from.memory == 0 {
		return
	}
	progress = dialogEase(progress)
	width, height := state.frame.width, state.frame.height
	if width <= 0 || height <= 0 {
		return
	}
	_, _, _ = dialogPageBitBlt.Call(
		state.frame.memory,
		0,
		0,
		uintptr(width),
		uintptr(height),
		state.from.memory,
		0,
		0,
		dialogPageSRCCopy,
	)
	if state.to.memory == 0 || progress <= 0 {
		return
	}
	offset := int32(
		float64(dialogPageScaleDIP(dialogPageSlideDIP, state.overlay)) *
			(1 - progress),
	)
	offset *= int32(state.direction)
	dstX, srcX, blendWidth := offset, int32(0), width
	if offset < 0 {
		dstX = 0
		srcX = -offset
		blendWidth = width + offset
	} else if offset > 0 {
		blendWidth = width - offset
	}
	if blendWidth <= 0 {
		return
	}
	alpha := byte(progress*255 + 0.5)
	blendFunction := uintptr(alpha) << 16 // AC_SRC_OVER, constant alpha.
	_, _, _ = dialogPageAlphaBlend.Call(
		state.frame.memory,
		signedDialogPageArg(dstX),
		0,
		uintptr(blendWidth),
		uintptr(height),
		state.to.memory,
		signedDialogPageArg(srcX),
		0,
		uintptr(blendWidth),
		uintptr(height),
		blendFunction,
	)
}

func (state *dialogPageTransitionState) showOverlay() {
	if state == nil || state.overlay == 0 {
		return
	}
	width := state.pageRect.Right - state.pageRect.Left
	height := state.pageRect.Bottom - state.pageRect.Top
	if width <= 0 || height <= 0 {
		return
	}
	_, _, _ = dialogPageSetWindowPos.Call(
		state.overlay,
		0, // HWND_TOP
		signedDialogPageArg(state.pageRect.Left),
		signedDialogPageArg(state.pageRect.Top),
		uintptr(width),
		uintptr(height),
		dialogPageSWPNoActivate|dialogPageSWPShowWindow,
	)
	_, _, _ = dialogPageInvalidateRect.Call(state.overlay, 0, 0)
	_, _, _ = dialogPageUpdateWindow.Call(state.overlay)
}

func (state *dialogPageTransitionState) hideOverlay() {
	if state == nil || state.overlay == 0 {
		return
	}
	_, _, _ = dialogPageSetWindowPos.Call(
		state.overlay,
		0,
		0,
		0,
		0,
		0,
		dialogPageSWPNoActivate|dialogPageSWPHideWindow,
	)
}

func (state *dialogPageTransitionState) blockPageRedraw(page uintptr) {
	state.restorePageRedraw()
	if page == 0 || !isWindow(page) {
		return
	}
	_, _, _ = dialogPageSendMessage.Call(page, 0x000B, 0, 0) // WM_SETREDRAW
	state.redrawBlocked = page
}

func (state *dialogPageTransitionState) restorePageRedraw() {
	if state == nil || state.redrawBlocked == 0 {
		return
	}
	page := state.redrawBlocked
	state.redrawBlocked = 0
	if isWindow(page) {
		_, _, _ = dialogPageSendMessage.Call(page, 0x000B, 1, 0)
	}
}

func (state *dialogPageTransitionState) invalidateSurfaces() {
	if state == nil || state.disposed.Load() {
		return
	}
	stopDialogPageScheduler(state.overlay)
	state.restorePageRedraw()
	state.running = false
	state.prepared = false
	state.hideOverlay()
	state.releaseSurfaces()
}

func (state *dialogPageTransitionState) releaseSurfaces() {
	if state == nil {
		return
	}
	releaseDialogPageSurface(&state.from)
	releaseDialogPageSurface(&state.to)
	releaseDialogPageSurface(&state.frame)
}

func dialogPageState(hwnd uintptr) *dialogPageTransitionState {
	dialogPageStatesMu.RLock()
	state := dialogPageStates[hwnd]
	dialogPageStatesMu.RUnlock()
	return state
}

func readDialogPageNotifyHeader(address uintptr) dialogPageNotifyHeader {
	var header dialogPageNotifyHeader
	if address == 0 {
		return header
	}
	_, _, _ = dialogPageRtlMoveMemory.Call(
		uintptr(unsafe.Pointer(&header)),
		address,
		unsafe.Sizeof(header),
	)
	runtime.KeepAlive(&header)
	return header
}

func dialogPageBoundsInHost(
	host, page uintptr,
) (dialogPageRect, error) {
	var window dialogPageRect
	ok, _, callErr := dialogPageGetWindowRect.Call(
		page,
		uintptr(unsafe.Pointer(&window)),
	)
	if ok == 0 {
		return dialogPageRect{}, boolCallError("GetWindowRect(dialog page)", callErr)
	}
	points := [2]dialogPagePoint{
		{X: window.Left, Y: window.Top},
		{X: window.Right, Y: window.Bottom},
	}
	_, _, _ = dialogPageMapWindowPoints.Call(
		0,
		host,
		uintptr(unsafe.Pointer(&points[0])),
		2,
	)
	// MapWindowPoints has no unambiguous failure sentinel: zero is also the
	// documented result when no translation is needed, and LazyProc.Call can
	// surface a stale thread last-error. Both HWNDs were validated above, so
	// use the mapped coordinates directly instead of rejecting a valid
	// zero-translation page.
	return dialogPageRect{
		Left:   points[0].X,
		Top:    points[0].Y,
		Right:  points[1].X,
		Bottom: points[1].Y,
	}, nil
}

func ensureDialogPageSurface(
	surface *dialogPageSurface,
	referenceWindow uintptr,
	width, height int32,
) error {
	if surface == nil || width <= 0 || height <= 0 {
		return errors.New("winfx: invalid dialog page surface dimensions")
	}
	if surface.memory != 0 && surface.bitmap != 0 &&
		surface.width == width && surface.height == height {
		return nil
	}
	releaseDialogPageSurface(surface)

	referenceDC, _, callErr := dialogPageGetDC.Call(referenceWindow)
	if referenceDC == 0 {
		return boolCallError("GetDC(dialog page host)", callErr)
	}
	defer dialogPageReleaseDC.Call(referenceWindow, referenceDC)

	memory, _, callErr := dialogPageCreateCompatibleDC.Call(referenceDC)
	if memory == 0 {
		return boolCallError("CreateCompatibleDC(dialog page)", callErr)
	}
	bitmap, _, callErr := dialogPageCreateCompatibleBitmap.Call(
		referenceDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogPageDeleteDC.Call(memory)
		return boolCallError("CreateCompatibleBitmap(dialog page)", callErr)
	}
	oldBitmap, _, _ := dialogPageSelectObject.Call(memory, bitmap)
	if oldBitmap == 0 {
		_, _, _ = dialogPageDeleteObject.Call(bitmap)
		_, _, _ = dialogPageDeleteDC.Call(memory)
		return errors.New("winfx: SelectObject(dialog page bitmap) failed")
	}
	surface.memory = memory
	surface.bitmap = bitmap
	surface.oldBitmap = oldBitmap
	surface.width = width
	surface.height = height
	return nil
}

func releaseDialogPageSurface(surface *dialogPageSurface) {
	if surface == nil {
		return
	}
	if surface.memory != 0 && surface.oldBitmap != 0 {
		_, _, _ = dialogPageSelectObject.Call(
			surface.memory,
			surface.oldBitmap,
		)
	}
	if surface.bitmap != 0 {
		_, _, _ = dialogPageDeleteObject.Call(surface.bitmap)
	}
	if surface.memory != 0 {
		_, _, _ = dialogPageDeleteDC.Call(surface.memory)
	}
	*surface = dialogPageSurface{}
}

func copyDialogPageSurface(
	destination, source *dialogPageSurface,
) error {
	if destination == nil || source == nil || source.memory == 0 ||
		source.width <= 0 || source.height <= 0 {
		return errors.New("winfx: invalid dialog page source surface")
	}
	if destination.memory == 0 ||
		destination.width != source.width ||
		destination.height != source.height {
		return errors.New("winfx: dialog page surfaces do not match")
	}
	ok, _, callErr := dialogPageBitBlt.Call(
		destination.memory,
		0,
		0,
		uintptr(source.width),
		uintptr(source.height),
		source.memory,
		0,
		0,
		dialogPageSRCCopy,
	)
	if ok == 0 {
		return boolCallError("BitBlt(dialog page cache)", callErr)
	}
	return nil
}

func fillDialogPageSurface(
	dc uintptr,
	width, height int32,
	brush uintptr,
) bool {
	if dc == 0 || width <= 0 || height <= 0 || brush == 0 {
		return false
	}
	rect := dialogPageRect{Right: width, Bottom: height}
	ok, _, _ := dialogPageFillRect.Call(
		dc,
		uintptr(unsafe.Pointer(&rect)),
		brush,
	)
	runtime.KeepAlive(&rect)
	return ok != 0
}

func dialogPagePrintFlags() uintptr {
	return dialogPagePRFNonClient |
		dialogPagePRFClient |
		dialogPagePRFErase |
		dialogPagePRFChildren |
		dialogPagePRFOwned
}

func paintDialogPageOverlay(
	hwnd uintptr,
	state *dialogPageTransitionState,
	suppliedDC uintptr,
) bool {
	if state == nil || state.frame.memory == 0 {
		return false
	}
	dc := suppliedDC
	var paint dialogPagePaintStruct
	if dc == 0 {
		result, _, _ := dialogPageBeginPaint.Call(
			hwnd,
			uintptr(unsafe.Pointer(&paint)),
		)
		if result == 0 {
			return false
		}
		dc = result
		defer func() {
			_, _, _ = dialogPageEndPaint.Call(
				hwnd,
				uintptr(unsafe.Pointer(&paint)),
			)
			runtime.KeepAlive(&paint)
		}()
	}
	ok, _, _ := dialogPageBitBlt.Call(
		dc,
		0,
		0,
		uintptr(state.frame.width),
		uintptr(state.frame.height),
		state.frame.memory,
		0,
		0,
		dialogPageSRCCopy,
	)
	return ok != 0
}

func startDialogPageScheduler(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	stopDialogPageScheduler(hwnd)
	dialogPageSchedulerMu.Lock()
	dialogPageSchedulerSequence++
	if dialogPageSchedulerSequence == 0 {
		dialogPageSchedulerSequence++
	}
	scheduler := &dialogPageFrameScheduler{
		token: dialogPageSchedulerSequence,
		stop:  make(chan struct{}),
	}
	dialogPageSchedulers[hwnd] = scheduler
	dialogPageSchedulerMu.Unlock()

	go func() {
		ticker := time.NewTicker(dialogMotionTimerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !scheduler.pending.CompareAndSwap(false, true) {
					continue
				}
				ok, _, _ := dialogPagePostMessage.Call(
					hwnd,
					dialogPageWMAnimate,
					uintptr(scheduler.token),
					0,
				)
				if ok == 0 {
					scheduler.pending.Store(false)
					return
				}
			case <-scheduler.stop:
				return
			}
		}
	}()
}

func stopDialogPageScheduler(hwnd uintptr) {
	dialogPageSchedulerMu.Lock()
	scheduler := dialogPageSchedulers[hwnd]
	if scheduler != nil {
		delete(dialogPageSchedulers, hwnd)
		close(scheduler.stop)
	}
	dialogPageSchedulerMu.Unlock()
}

func consumeDialogPageFrame(hwnd uintptr, token uint32) bool {
	dialogPageSchedulerMu.Lock()
	defer dialogPageSchedulerMu.Unlock()
	scheduler := dialogPageSchedulers[hwnd]
	if scheduler == nil || scheduler.token != token {
		return false
	}
	scheduler.pending.Store(false)
	return true
}

func dialogPageAnimationDuration() time.Duration {
	duration := time.Duration(design.Default.Motion.NormalMS) * time.Millisecond
	if duration <= 0 {
		return dialogPageAnimationFallback
	}
	return duration
}

func dialogPageScaleDIP(value int32, hwnd uintptr) int32 {
	dpi := uintptr(96)
	if hwnd != 0 {
		if got, _, _ := dialogPageGetDPIForWindow.Call(hwnd); got != 0 {
			dpi = got
		}
	}
	return int32((int64(value)*int64(dpi) + 48) / 96)
}

func signedDialogPageArg(value int32) uintptr {
	return uintptr(int64(value))
}
