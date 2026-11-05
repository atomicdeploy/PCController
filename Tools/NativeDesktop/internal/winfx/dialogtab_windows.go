//go:build windows

package winfx

import (
	"errors"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/design"
)

const (
	dialogTabSubclassID = 0x524C5442 // "RLTB"

	dialogTabWMPaint       = 0x000F
	dialogTabWMEraseBkgnd  = 0x0014
	dialogTabWMSetFocus    = 0x0007
	dialogTabWMKillFocus   = 0x0008
	dialogTabWMSize        = 0x0005
	dialogTabWMEnable      = 0x000A
	dialogTabWMTimer       = 0x0113
	dialogTabWMKeyDown     = 0x0100
	dialogTabWMLButtonDown = 0x0201
	dialogTabWMLButtonUp   = 0x0202
	dialogTabWMMouseMove   = 0x0200
	dialogTabWMMouseLeave  = 0x02A3
	dialogTabWMPrintClient = 0x0318
	dialogTabWMThemeChange = 0x031A
	dialogTabWMDPIChanged  = 0x02E0
	dialogTabWMAnimate     = 0x8000 + 0x0541

	dialogTabTCMFirst        = 0x1300
	dialogTabTCMGetItemCount = dialogTabTCMFirst + 4
	dialogTabTCMGetItemRect  = dialogTabTCMFirst + 10
	dialogTabTCMGetCurSel    = dialogTabTCMFirst + 11
	dialogTabTCMSetCurSel    = dialogTabTCMFirst + 12
	dialogTabTCMHitTest      = dialogTabTCMFirst + 13
	dialogTabTCMSetPadding   = dialogTabTCMFirst + 43
	dialogTabTCMGetItemW     = dialogTabTCMFirst + 60
	dialogTabTCIFText        = 0x0001

	dialogTabWMGetFont      = 0x0031
	dialogTabDefaultGUIFont = 17

	dialogTabBkTransparent = 1
	dialogTabDTCenter      = 0x00000001
	dialogTabDTVCenter     = 0x00000004
	dialogTabDTSingleLine  = 0x00000020
	dialogTabDTNoPrefix    = 0x00000800
	dialogTabDTRTLReading  = 0x00020000
	dialogTabDTEndEllipsis = 0x00008000
	dialogTabDTCalcRect    = 0x00000400

	dialogTabTMELeave = 0x00000002
	dialogTabNullPen  = 8
	dialogTabSRCCopy  = 0x00CC0020

	dialogTabDefaultCharset   = 1
	dialogTabClearTypeQuality = 5

	dialogTabTextHorizontalPadding = 6
	dialogTabIconHorizontalPadding = 20
	dialogTabVerticalPadding       = 3
)

type dialogTabRect struct {
	Left, Top, Right, Bottom int32
}

type dialogTabPaintStruct struct {
	DC          uintptr
	Erase       int32
	Paint       dialogTabRect
	Restore     int32
	Incremental int32
	Reserved    [32]byte
}

type dialogTabItemW struct {
	Mask      uint32
	State     uint32
	StateMask uint32
	Text      uintptr
	TextMax   int32
	Image     int32
	Param     uintptr
}

type dialogTabHitTest struct {
	X, Y  int32
	Flags uint32
}

type dialogTabTrackMouse struct {
	Size      uint32
	Flags     uint32
	Window    uintptr
	HoverTime uint32
}

type dialogTabThemeState struct {
	Dark              bool
	Material          bool
	Hot               int32
	Selected          int32
	Tracking          bool
	Animate           bool
	HoverLevels       []float64
	SelectionLevels   []float64
	IndicatorFrom     float64
	IndicatorTo       float64
	IndicatorProgress float64
	LastTick          time.Time
}

type dialogTabPalette struct {
	Background uint32
	Inactive   uint32
	Hot        uint32
	Selected   uint32
	Text       uint32
	MutedText  uint32
	Disabled   uint32
	Accent     uint32
}

type dialogTabAnimationScheduler struct {
	token   uint32
	stop    chan struct{}
	pending atomic.Bool
}

type dialogTabPaintBuffer struct {
	width, height int32
	dpi           int32
	memory        uintptr
	bitmap        uintptr
	oldBitmap     uintptr
	iconFont      uintptr
}

var (
	dialogTabUser32                 = windows.NewLazySystemDLL("user32.dll")
	dialogTabBeginPaint             = dialogTabUser32.NewProc("BeginPaint")
	dialogTabEndPaint               = dialogTabUser32.NewProc("EndPaint")
	dialogTabFillRect               = dialogTabUser32.NewProc("FillRect")
	dialogTabGetClientRect          = dialogTabUser32.NewProc("GetClientRect")
	dialogTabDrawText               = dialogTabUser32.NewProc("DrawTextW")
	dialogTabGetDPIForWindow        = dialogTabUser32.NewProc("GetDpiForWindow")
	dialogTabInvalidateRect         = dialogTabUser32.NewProc("InvalidateRect")
	dialogTabPostMessage            = dialogTabUser32.NewProc("PostMessageW")
	dialogTabIsWindowEnabled        = dialogTabUser32.NewProc("IsWindowEnabled")
	dialogTabTrackMouseEvent        = dialogTabUser32.NewProc("TrackMouseEvent")
	dialogTabGDI32                  = windows.NewLazySystemDLL("gdi32.dll")
	dialogTabCreateBrush            = dialogTabGDI32.NewProc("CreateSolidBrush")
	dialogTabDeleteObject           = dialogTabGDI32.NewProc("DeleteObject")
	dialogTabRoundRect              = dialogTabGDI32.NewProc("RoundRect")
	dialogTabSelectObject           = dialogTabGDI32.NewProc("SelectObject")
	dialogTabSetBkMode              = dialogTabGDI32.NewProc("SetBkMode")
	dialogTabSetTextColor           = dialogTabGDI32.NewProc("SetTextColor")
	dialogTabGetStockObject         = dialogTabGDI32.NewProc("GetStockObject")
	dialogTabCreateFontW            = dialogTabGDI32.NewProc("CreateFontW")
	dialogTabGetTextFaceW           = dialogTabGDI32.NewProc("GetTextFaceW")
	dialogTabCreateCompatibleDC     = dialogTabGDI32.NewProc("CreateCompatibleDC")
	dialogTabCreateCompatibleBitmap = dialogTabGDI32.NewProc("CreateCompatibleBitmap")
	dialogTabBitBlt                 = dialogTabGDI32.NewProc("BitBlt")
	dialogTabDeleteDC               = dialogTabGDI32.NewProc("DeleteDC")

	dialogTabMu     sync.RWMutex
	dialogTabThemes = make(map[uintptr]dialogTabThemeState)

	dialogTabSchedulerMu       sync.Mutex
	dialogTabSchedulers        = make(map[uintptr]*dialogTabAnimationScheduler)
	dialogTabSchedulerSequence uint32

	dialogTabBufferMu sync.Mutex
	dialogTabBuffers  = make(map[uintptr]*dialogTabPaintBuffer)
)

func setDialogTabTheme(
	hwnd uintptr,
	dark, material, animate, enabled bool,
) {
	if hwnd == 0 {
		return
	}
	if !enabled {
		setDialogTabPadding(hwnd, dialogTabDPI(hwnd), false)
		stopDialogTabAnimationScheduler(hwnd)
		releaseDialogTabPaintBuffer(hwnd)
		_ = RemoveWindowSubclass(hwnd, dialogTabSubclassID)
		dialogTabMu.Lock()
		delete(dialogTabThemes, hwnd)
		dialogTabMu.Unlock()
		invalidateDialogTab(hwnd)
		return
	}

	setDialogTabPadding(
		hwnd,
		dialogTabDPI(hwnd),
		dialogTabUsesGlyphs(hwnd),
	)
	count := dialogTabItemCount(hwnd)
	selected := dialogTabCurrentSelection(hwnd)
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	state.Dark = dark
	state.Material = material
	state.Animate = animate
	resizeDialogTabLevels(&state, count)
	if !exists {
		state.Hot = -1
		state.Selected = selected
		settleDialogTabSelection(&state)
		state.IndicatorFrom = float64(selected)
		state.IndicatorTo = float64(selected)
		state.IndicatorProgress = 1
	} else if !animate {
		state.Selected = selected
		settleDialogTabMotion(&state)
	}
	dialogTabThemes[hwnd] = state
	dialogTabMu.Unlock()
	if !animate {
		stopDialogTabAnimationScheduler(hwnd)
	}

	if err := SetWindowSubclass(
		hwnd,
		dialogTabSubclassID,
		dialogTabWindowMessage,
	); err != nil && !errors.Is(err, ErrSubclassExists) {
		dialogTabMu.Lock()
		delete(dialogTabThemes, hwnd)
		dialogTabMu.Unlock()
		return
	}
	invalidateDialogTab(hwnd)
}

func setDialogTabMotion(hwnd uintptr, animate bool) {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	if exists {
		state.Animate = animate
		if !animate {
			settleDialogTabMotion(&state)
		}
		dialogTabThemes[hwnd] = state
	}
	dialogTabMu.Unlock()
	if !exists {
		return
	}
	if !animate {
		stopDialogTabAnimationScheduler(hwnd)
	}
	invalidateDialogTab(hwnd)
}

func dialogTabWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	switch uint32(message) {
	case wmNCDestroy:
		stopDialogTabAnimationScheduler(hwnd)
		releaseDialogTabPaintBuffer(hwnd)
		dialogTabMu.Lock()
		delete(dialogTabThemes, hwnd)
		dialogTabMu.Unlock()
		return 0, false

	case dialogTabWMPaint:
		if paintDialogTab(hwnd, 0) {
			return 0, true
		}

	case dialogTabWMPrintClient:
		if wParam != 0 && paintDialogTab(hwnd, wParam) {
			return 0, true
		}

	case dialogTabWMEraseBkgnd:
		return 1, true

	case dialogTabWMAnimate:
		if consumeDialogTabAnimationFrame(hwnd, uint32(wParam)) {
			if !advanceDialogTabAnimation(hwnd, time.Now()) {
				stopDialogTabAnimationScheduler(hwnd)
			}
			return 0, true
		}
		return 0, true

	case dialogTabTCMSetCurSel, dialogTabWMLButtonDown,
		dialogTabWMLButtonUp, dialogTabWMKeyDown,
		dialogTabWMSetFocus, dialogTabWMKillFocus:
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		syncDialogTabSelection(hwnd)
		invalidateDialogTab(hwnd)
		return result, true

	case dialogTabWMMouseMove:
		// Custom hit tracking owns the complete hover state. Letting the native
		// tab proc run as well causes an independent hot-track invalidation,
		// exposing an intermediate background between our frames.
		updateDialogTabHotItem(hwnd, lParam)
		if beginDialogTabMouseTracking(hwnd) {
			trackDialogTabMouseLeave(hwnd)
		}
		return 0, true

	case dialogTabWMMouseLeave:
		endDialogTabMouseTracking(hwnd)
		setDialogTabHotItem(hwnd, -1)
		return 0, true

	case dialogTabWMSize, dialogTabWMEnable, dialogTabWMThemeChange,
		dialogTabWMDPIChanged:
		if uint32(message) == dialogTabWMSize ||
			uint32(message) == dialogTabWMDPIChanged {
			releaseDialogTabPaintBuffer(hwnd)
		}
		if uint32(message) == dialogTabWMDPIChanged {
			setDialogTabPadding(
				hwnd,
				dialogTabDPI(hwnd),
				dialogTabUsesGlyphs(hwnd),
			)
		}
		invalidateDialogTab(hwnd)
	}
	return 0, false
}

func paintDialogTab(hwnd, suppliedDC uintptr) bool {
	syncDialogTabSelection(hwnd)
	state, exists := dialogTabTheme(hwnd)
	if !exists {
		return false
	}

	dc := suppliedDC
	var paint dialogTabPaintStruct
	if dc == 0 {
		result, _, _ := dialogTabBeginPaint.Call(
			hwnd,
			uintptr(unsafe.Pointer(&paint)),
		)
		if result == 0 {
			return false
		}
		dc = result
		defer func() {
			_, _, _ = dialogTabEndPaint.Call(
				hwnd,
				uintptr(unsafe.Pointer(&paint)),
			)
			runtime.KeepAlive(&paint)
		}()
	}

	var client dialogTabRect
	ok, _, _ := dialogTabGetClientRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&client)),
	)
	if ok == 0 {
		return false
	}
	dpi := dialogTabDPI(hwnd)
	iconFont := uintptr(0)
	iconFontManaged := false
	if bufferedDC, finish, cachedFont, buffered :=
		beginCachedDialogTabBuffer(hwnd, dc, client, dpi); buffered {
		dc = bufferedDC
		iconFont = cachedFont
		iconFontManaged = true
		defer finish()
	} else if bufferedDC, finish, buffered :=
		beginDialogTabBuffer(dc, client); buffered {
		dc = bufferedDC
		defer finish()
	}
	palette := dialogTabColors(state.Dark, state.Material)
	rtl := windowRTLReading(hwnd)
	fillDialogTabRect(dc, client, palette.Background)

	font, _, _ := dialogThemeSendMessage.Call(hwnd, dialogTabWMGetFont, 0, 0)
	if font == 0 {
		font, _, _ = dialogTabGetStockObject.Call(dialogTabDefaultGUIFont)
	}
	var oldFont uintptr
	if font != 0 {
		oldFont, _, _ = dialogTabSelectObject.Call(dc, font)
		defer func() {
			if oldFont != 0 {
				_, _, _ = dialogTabSelectObject.Call(dc, oldFont)
			}
		}()
	}
	_, _, _ = dialogTabSetBkMode.Call(dc, dialogTabBkTransparent)
	if iconFont == 0 && !iconFontManaged {
		iconFont = createDialogTabIconFont(dc, dpi)
	}
	if iconFont != 0 && !iconFontManaged {
		defer func() {
			_, _, _ = dialogTabDeleteObject.Call(iconFont)
		}()
	}

	count := dialogTabItemCount(hwnd)
	if count < 0 || count > 64 {
		return false
	}
	enabled, _, _ := dialogTabIsWindowEnabled.Call(hwnd)
	itemRects := make([]dialogTabRect, count)
	itemRectValid := make([]bool, count)
	for index := 0; index < count; index++ {
		var itemRect dialogTabRect
		gotRect, _, _ := dialogThemeSendMessage.Call(
			hwnd,
			dialogTabTCMGetItemRect,
			uintptr(index),
			uintptr(unsafe.Pointer(&itemRect)),
		)
		if gotRect == 0 {
			continue
		}
		itemRects[index] = itemRect
		itemRectValid[index] = true
		hover := dialogEase(dialogTabLevel(state.HoverLevels, index))
		selection := dialogEase(dialogTabLevel(state.SelectionLevels, index))
		itemBackground := dialogMixColor(
			palette.Inactive,
			palette.Hot,
			hover,
		)
		itemBackground = dialogMixColor(
			itemBackground,
			palette.Selected,
			selection,
		)
		emphasis := math.Max(hover, selection)
		itemText := dialogMixColor(
			palette.MutedText,
			palette.Text,
			emphasis,
		)
		if enabled == 0 {
			itemText = palette.Disabled
		}
		if hover > 0 || selection > 0 || !state.Material {
			itemBackgroundRect := itemRect
			inset := scaleDialogTabMetric(2, dpi)
			itemBackgroundRect.Left += inset
			itemBackgroundRect.Top += inset
			itemBackgroundRect.Right -= inset
			itemBackgroundRect.Bottom -= inset
			fillDialogTabRoundRect(
				dc,
				itemBackgroundRect,
				itemBackground,
				scaleDialogTabMetric(7, dpi),
			)
		}

		var text [256]uint16
		item := dialogTabItemW{
			Mask:    dialogTabTCIFText,
			Text:    uintptr(unsafe.Pointer(&text[0])),
			TextMax: int32(len(text)),
		}
		_, _, _ = dialogThemeSendMessage.Call(
			hwnd,
			dialogTabTCMGetItemW,
			uintptr(index),
			uintptr(unsafe.Pointer(&item)),
		)
		textRect := itemRect
		// The native tab already allocates text padding when it computes each
		// item width. Adding another fixed 8 px on both sides made otherwise
		// valid labels ellipsize at 125–175% DPI.
		textInset := scaleDialogTabMetric(2, dpi)
		textRect.Left += textInset
		textRect.Right -= textInset
		_, _, _ = dialogTabSetTextColor.Call(dc, uintptr(itemText))
		title := windows.UTF16ToString(text[:])
		glyph, hasGlyph := dialogTabGlyph(title)
		if iconFont != 0 && hasGlyph {
			textWidth := measureDialogTabText(dc, &text[0])
			iconRect, labelRect, laidOut := dialogTabIconTextLayout(
				itemRect,
				textWidth,
				dpi,
			)
			if laidOut {
				if rtl {
					iconRect.Left, iconRect.Right = mirrorHorizontalEdges(
						itemRect.Left,
						itemRect.Right,
						iconRect.Left,
						iconRect.Right,
					)
					labelRect.Left, labelRect.Right = mirrorHorizontalEdges(
						itemRect.Left,
						itemRect.Right,
						labelRect.Left,
						labelRect.Right,
					)
				}
				iconText := [2]uint16{glyph, 0}
				previousFont, _, _ := dialogTabSelectObject.Call(dc, iconFont)
				if previousFont != 0 && previousFont != ^uintptr(0) {
					iconColor := dialogMixColor(
						itemText,
						palette.Accent,
						0.18*hover+0.32*selection,
					)
					_, _, _ = dialogTabSetTextColor.Call(
						dc,
						uintptr(iconColor),
					)
					_, _, _ = dialogTabDrawText.Call(
						dc,
						uintptr(unsafe.Pointer(&iconText[0])),
						1,
						uintptr(unsafe.Pointer(&iconRect)),
						dialogTabDTCenter|
							dialogTabDTVCenter|
							dialogTabDTSingleLine|
							dialogTabDTNoPrefix,
					)
					_, _, _ = dialogTabSelectObject.Call(dc, previousFont)
					_, _, _ = dialogTabSetTextColor.Call(dc, uintptr(itemText))
					labelFormat := uintptr(
						dialogTabDTVCenter |
							dialogTabDTSingleLine |
							dialogTabDTNoPrefix |
							dialogTabDTEndEllipsis,
					)
					if rtl {
						labelFormat |= dialogTabDTRTLReading
					}
					_, _, _ = dialogTabDrawText.Call(
						dc,
						uintptr(unsafe.Pointer(&text[0])),
						uintptr(^uint32(0)),
						uintptr(unsafe.Pointer(&labelRect)),
						labelFormat,
					)
					runtime.KeepAlive(&iconText)
					runtime.KeepAlive(&iconRect)
					runtime.KeepAlive(&labelRect)
				} else {
					hasGlyph = false
				}
			} else {
				hasGlyph = false
			}
		}
		if iconFont == 0 || !hasGlyph {
			_, _, _ = dialogTabSetTextColor.Call(dc, uintptr(itemText))
			textFormat := uintptr(
				dialogTabDTCenter |
					dialogTabDTVCenter |
					dialogTabDTSingleLine |
					dialogTabDTNoPrefix |
					dialogTabDTEndEllipsis,
			)
			if rtl {
				textFormat |= dialogTabDTRTLReading
			}
			_, _, _ = dialogTabDrawText.Call(
				dc,
				uintptr(unsafe.Pointer(&text[0])),
				uintptr(^uint32(0)),
				uintptr(unsafe.Pointer(&textRect)),
				textFormat,
			)
		}
		runtime.KeepAlive(&item)
		runtime.KeepAlive(&text)
	}
	if indicator, ok := dialogTabIndicatorRect(
		itemRects,
		itemRectValid,
		state,
		dpi,
	); ok {
		fillDialogTabRoundRect(
			dc,
			indicator,
			palette.Accent,
			scaleDialogTabMetric(2, dpi),
		)
	}
	runtime.KeepAlive(&client)
	return true
}

// dialogTabGlyph maps the native Settings page names to the matching Fluent
// system glyph. The code points are shared by Segoe Fluent Icons and Segoe
// MDL2 Assets, so Windows 10 and 11 retain the same visual vocabulary.
func dialogTabGlyph(title string) (uint16, bool) {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "lamp", "چراغ":
		return 0xEA80, true // Lightbulb
	case "appearance", "ظاهر":
		return 0xE790, true // Color
	case "notifications", "اعلان‌ها":
		return 0xEA8F, true // Ringer
	case "hotkeys", "میان‌برها":
		return 0xE765, true // KeyboardClassic
	case "automation", "خودکارسازی":
		return 0xE945, true // LightningBolt
	case "integration", "یکپارچه‌سازی":
		return 0xE703, true // Connect
	default:
		return 0, false
	}
}

func setDialogTabPadding(hwnd uintptr, dpi int32, withIcons bool) {
	if hwnd == 0 {
		return
	}
	horizontal := scaleDialogTabMetric(dialogTabTextHorizontalPadding, dpi)
	if withIcons {
		horizontal = scaleDialogTabMetric(dialogTabIconHorizontalPadding, dpi)
	}
	vertical := scaleDialogTabMetric(dialogTabVerticalPadding, dpi)
	_, _, _ = dialogThemeSendMessage.Call(
		hwnd,
		dialogTabTCMSetPadding,
		0,
		packDialogTabPadding(horizontal, vertical),
	)
}

func packDialogTabPadding(horizontal, vertical int32) uintptr {
	return uintptr(uint16(horizontal)) |
		uintptr(uint16(vertical))<<16
}

func dialogTabUsesGlyphs(hwnd uintptr) bool {
	count := dialogTabItemCount(hwnd)
	if count < 1 || count > 64 {
		return false
	}
	for index := 0; index < count; index++ {
		var text [256]uint16
		item := dialogTabItemW{
			Mask:    dialogTabTCIFText,
			Text:    uintptr(unsafe.Pointer(&text[0])),
			TextMax: int32(len(text)),
		}
		_, _, _ = dialogThemeSendMessage.Call(
			hwnd,
			dialogTabTCMGetItemW,
			uintptr(index),
			uintptr(unsafe.Pointer(&item)),
		)
		_, ok := dialogTabGlyph(windows.UTF16ToString(text[:]))
		runtime.KeepAlive(&item)
		runtime.KeepAlive(&text)
		if ok {
			return true
		}
	}
	return false
}

func createDialogTabIconFont(dc uintptr, dpi int32) uintptr {
	if dc == 0 {
		return 0
	}
	for _, family := range []string{
		"Segoe Fluent Icons",
		"Segoe MDL2 Assets",
	} {
		face, err := windows.UTF16PtrFromString(family)
		if err != nil {
			continue
		}
		height := -scaleDialogTabMetric(16, dpi)
		font, _, _ := dialogTabCreateFontW.Call(
			uintptr(uint32(height)),
			0,
			0,
			0,
			400,
			0,
			0,
			0,
			dialogTabDefaultCharset,
			0,
			0,
			dialogTabClearTypeQuality,
			0,
			uintptr(unsafe.Pointer(face)),
		)
		runtime.KeepAlive(face)
		if font == 0 {
			continue
		}
		if dialogTabFontFaceMatches(dc, font, family) {
			return font
		}
		_, _, _ = dialogTabDeleteObject.Call(font)
	}
	return 0
}

func dialogTabFontFaceMatches(dc, font uintptr, family string) bool {
	if dc == 0 || font == 0 {
		return false
	}
	previous, _, _ := dialogTabSelectObject.Call(dc, font)
	if previous == 0 || previous == ^uintptr(0) {
		return false
	}
	defer func() {
		_, _, _ = dialogTabSelectObject.Call(dc, previous)
	}()
	var face [64]uint16
	length, _, _ := dialogTabGetTextFaceW.Call(
		dc,
		uintptr(len(face)),
		uintptr(unsafe.Pointer(&face[0])),
	)
	if length == 0 {
		return false
	}
	return strings.EqualFold(windows.UTF16ToString(face[:]), family)
}

func measureDialogTabText(dc uintptr, text *uint16) int32 {
	if dc == 0 || text == nil {
		return 0
	}
	var rect dialogTabRect
	_, _, _ = dialogTabDrawText.Call(
		dc,
		uintptr(unsafe.Pointer(text)),
		uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(&rect)),
		dialogTabDTSingleLine|
			dialogTabDTNoPrefix|
			dialogTabDTCalcRect,
	)
	runtime.KeepAlive(text)
	runtime.KeepAlive(&rect)
	return max(int32(0), rect.Right-rect.Left)
}

func dialogTabIconTextLayout(
	item dialogTabRect,
	textWidth, dpi int32,
) (icon, text dialogTabRect, ok bool) {
	inset := scaleDialogTabMetric(4, dpi)
	iconSize := scaleDialogTabMetric(16, dpi)
	gap := scaleDialogTabMetric(6, dpi)
	available := item.Right - item.Left - inset*2
	maxTextWidth := available - iconSize - gap
	if item.Bottom <= item.Top || maxTextWidth < 1 {
		return dialogTabRect{}, dialogTabRect{}, false
	}
	textWidth = max(int32(1), min(textWidth, maxTextWidth))
	groupWidth := iconSize + gap + textWidth
	left := item.Left + (item.Right-item.Left-groupWidth)/2
	left = max(left, item.Left+inset)
	top := item.Top + (item.Bottom-item.Top-iconSize)/2
	icon = dialogTabRect{
		Left:   left,
		Top:    top,
		Right:  left + iconSize,
		Bottom: top + iconSize,
	}
	text = dialogTabRect{
		Left:   icon.Right + gap,
		Top:    item.Top,
		Right:  min(item.Right-inset, icon.Right+gap+maxTextWidth),
		Bottom: item.Bottom,
	}
	return icon, text, text.Right > text.Left
}

// beginDialogTabBuffer renders the whole control into one compatible bitmap
// and transfers it to the paint DC with a single BitBlt. Hover transitions no
// longer expose the cleared background between rounded fills and label draws.
// If a GDI allocation fails, the caller safely falls back to direct painting.
func beginDialogTabBuffer(
	targetDC uintptr,
	client dialogTabRect,
) (dc uintptr, finish func(), ok bool) {
	width := client.Right - client.Left
	height := client.Bottom - client.Top
	if targetDC == 0 || width < 1 || height < 1 {
		return 0, nil, false
	}

	memoryDC, _, _ := dialogTabCreateCompatibleDC.Call(targetDC)
	if memoryDC == 0 {
		return 0, nil, false
	}
	bitmap, _, _ := dialogTabCreateCompatibleBitmap.Call(
		targetDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogTabDeleteDC.Call(memoryDC)
		return 0, nil, false
	}
	previous, _, _ := dialogTabSelectObject.Call(memoryDC, bitmap)
	if previous == 0 {
		_, _, _ = dialogTabDeleteObject.Call(bitmap)
		_, _, _ = dialogTabDeleteDC.Call(memoryDC)
		return 0, nil, false
	}

	finish = func() {
		_, _, _ = dialogTabBitBlt.Call(
			targetDC,
			uintptr(client.Left),
			uintptr(client.Top),
			uintptr(width),
			uintptr(height),
			memoryDC,
			0,
			0,
			dialogTabSRCCopy,
		)
		_, _, _ = dialogTabSelectObject.Call(memoryDC, previous)
		_, _, _ = dialogTabDeleteObject.Call(bitmap)
		_, _, _ = dialogTabDeleteDC.Call(memoryDC)
	}
	return memoryDC, finish, true
}

func beginCachedDialogTabBuffer(
	hwnd, targetDC uintptr,
	client dialogTabRect,
	dpi int32,
) (dc uintptr, finish func(), iconFont uintptr, ok bool) {
	width := client.Right - client.Left
	height := client.Bottom - client.Top
	if hwnd == 0 || targetDC == 0 || width < 1 || height < 1 {
		return 0, nil, 0, false
	}
	if dpi <= 0 {
		dpi = 96
	}

	dialogTabBufferMu.Lock()
	buffer := dialogTabBuffers[hwnd]
	if buffer != nil &&
		(buffer.width != width ||
			buffer.height != height ||
			buffer.dpi != dpi) {
		delete(dialogTabBuffers, hwnd)
	}
	dialogTabBufferMu.Unlock()
	if buffer != nil &&
		(buffer.width != width ||
			buffer.height != height ||
			buffer.dpi != dpi) {
		closeDialogTabPaintBuffer(buffer)
		buffer = nil
	}
	if buffer == nil {
		buffer = newDialogTabPaintBuffer(targetDC, width, height, dpi)
		if buffer == nil {
			return 0, nil, 0, false
		}
		dialogTabBufferMu.Lock()
		if existing := dialogTabBuffers[hwnd]; existing != nil {
			dialogTabBufferMu.Unlock()
			closeDialogTabPaintBuffer(buffer)
			buffer = existing
		} else {
			dialogTabBuffers[hwnd] = buffer
			dialogTabBufferMu.Unlock()
		}
	}

	finish = func() {
		_, _, _ = dialogTabBitBlt.Call(
			targetDC,
			uintptr(client.Left),
			uintptr(client.Top),
			uintptr(width),
			uintptr(height),
			buffer.memory,
			0,
			0,
			dialogTabSRCCopy,
		)
	}
	return buffer.memory, finish, buffer.iconFont, true
}

func newDialogTabPaintBuffer(
	targetDC uintptr,
	width, height, dpi int32,
) *dialogTabPaintBuffer {
	memory, _, _ := dialogTabCreateCompatibleDC.Call(targetDC)
	if memory == 0 {
		return nil
	}
	bitmap, _, _ := dialogTabCreateCompatibleBitmap.Call(
		targetDC,
		uintptr(width),
		uintptr(height),
	)
	if bitmap == 0 {
		_, _, _ = dialogTabDeleteDC.Call(memory)
		return nil
	}
	oldBitmap, _, _ := dialogTabSelectObject.Call(memory, bitmap)
	if oldBitmap == 0 || oldBitmap == ^uintptr(0) {
		_, _, _ = dialogTabDeleteObject.Call(bitmap)
		_, _, _ = dialogTabDeleteDC.Call(memory)
		return nil
	}
	return &dialogTabPaintBuffer{
		width:     width,
		height:    height,
		dpi:       dpi,
		memory:    memory,
		bitmap:    bitmap,
		oldBitmap: oldBitmap,
		iconFont:  createDialogTabIconFont(memory, dpi),
	}
}

func releaseDialogTabPaintBuffer(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogTabBufferMu.Lock()
	buffer := dialogTabBuffers[hwnd]
	delete(dialogTabBuffers, hwnd)
	dialogTabBufferMu.Unlock()
	closeDialogTabPaintBuffer(buffer)
}

func closeDialogTabPaintBuffer(buffer *dialogTabPaintBuffer) {
	if buffer == nil {
		return
	}
	if buffer.iconFont != 0 {
		_, _, _ = dialogTabDeleteObject.Call(buffer.iconFont)
	}
	if buffer.memory != 0 && buffer.oldBitmap != 0 &&
		buffer.oldBitmap != ^uintptr(0) {
		_, _, _ = dialogTabSelectObject.Call(
			buffer.memory,
			buffer.oldBitmap,
		)
	}
	if buffer.bitmap != 0 {
		_, _, _ = dialogTabDeleteObject.Call(buffer.bitmap)
	}
	if buffer.memory != 0 {
		_, _, _ = dialogTabDeleteDC.Call(buffer.memory)
	}
	buffer.iconFont = 0
	buffer.bitmap = 0
	buffer.memory = 0
}

func dialogTabTheme(hwnd uintptr) (dialogTabThemeState, bool) {
	dialogTabMu.RLock()
	state, exists := dialogTabThemes[hwnd]
	dialogTabMu.RUnlock()
	return state, exists
}

func dialogTabItemCount(hwnd uintptr) int {
	count, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogTabTCMGetItemCount,
		0,
		0,
	)
	if count == ^uintptr(0) || count > 64 {
		return -1
	}
	return int(count)
}

func dialogTabCurrentSelection(hwnd uintptr) int32 {
	selected, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogTabTCMGetCurSel,
		0,
		0,
	)
	if selected == ^uintptr(0) || selected > math.MaxInt32 {
		return -1
	}
	return int32(selected)
}

func resizeDialogTabLevels(state *dialogTabThemeState, count int) {
	if state == nil || count < 0 || count > 64 {
		return
	}
	resize := func(values []float64) []float64 {
		if len(values) == count {
			return values
		}
		next := make([]float64, count)
		copy(next, values)
		return next
	}
	state.HoverLevels = resize(state.HoverLevels)
	state.SelectionLevels = resize(state.SelectionLevels)
}

func dialogTabLevel(levels []float64, index int) float64 {
	if index < 0 || index >= len(levels) {
		return 0
	}
	return dialogClamp(levels[index])
}

func syncDialogTabSelection(hwnd uintptr) {
	setDialogTabSelectedItem(
		hwnd,
		dialogTabCurrentSelection(hwnd),
		dialogTabItemCount(hwnd),
	)
}

func setDialogTabSelectedItem(hwnd uintptr, selected int32, count int) {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	if !exists {
		dialogTabMu.Unlock()
		return
	}
	resizeDialogTabLevels(&state, count)
	changed := state.Selected != selected
	animate := false
	if changed {
		currentIndicator := dialogTabIndicatorPosition(state)
		state.Selected = selected
		if state.Animate {
			state.IndicatorFrom = currentIndicator
			if state.IndicatorFrom < 0 {
				state.IndicatorFrom = float64(selected)
			}
			state.IndicatorTo = float64(selected)
			state.IndicatorProgress = 0
			state.LastTick = time.Now()
			animate = true
		} else {
			settleDialogTabSelection(&state)
			state.IndicatorFrom = float64(selected)
			state.IndicatorTo = float64(selected)
			state.IndicatorProgress = 1
			state.LastTick = time.Time{}
		}
		dialogTabThemes[hwnd] = state
	}
	dialogTabMu.Unlock()
	if !changed {
		return
	}
	if animate && !startDialogTabAnimationScheduler(hwnd) {
		settleDialogTabAnimation(hwnd)
	}
	invalidateDialogTab(hwnd)
}

func dialogTabIndicatorPosition(state dialogTabThemeState) float64 {
	if state.IndicatorTo < 0 {
		return -1
	}
	progress := dialogEase(state.IndicatorProgress)
	return state.IndicatorFrom +
		(state.IndicatorTo-state.IndicatorFrom)*progress
}

func advanceDialogTabAnimation(hwnd uintptr, now time.Time) bool {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	if !exists {
		dialogTabMu.Unlock()
		return false
	}
	if !state.Animate {
		settleDialogTabMotion(&state)
		dialogTabThemes[hwnd] = state
		dialogTabMu.Unlock()
		invalidateDialogTab(hwnd)
		return false
	}

	elapsed := now.Sub(state.LastTick)
	active := false
	for index := range state.HoverLevels {
		target := 0.0
		if int32(index) == state.Hot {
			target = 1
		}
		var continuing bool
		state.HoverLevels[index], continuing = advanceDialogMotion(
			state.HoverLevels[index],
			target,
			elapsed,
			design.Default.Motion.FastMS,
		)
		active = active || continuing
	}
	for index := range state.SelectionLevels {
		target := 0.0
		if int32(index) == state.Selected {
			target = 1
		}
		var continuing bool
		state.SelectionLevels[index], continuing = advanceDialogMotion(
			state.SelectionLevels[index],
			target,
			elapsed,
			design.Default.Motion.NormalMS,
		)
		active = active || continuing
	}
	if state.IndicatorProgress < 1 {
		var continuing bool
		state.IndicatorProgress, continuing = advanceDialogMotion(
			state.IndicatorProgress,
			1,
			elapsed,
			design.Default.Motion.NormalMS,
		)
		active = active || continuing
	}
	if active {
		state.LastTick = now
	} else {
		state.IndicatorFrom = state.IndicatorTo
		state.IndicatorProgress = 1
		state.LastTick = time.Time{}
	}
	dialogTabThemes[hwnd] = state
	dialogTabMu.Unlock()
	invalidateDialogTab(hwnd)
	presentDialogMotionFrame(hwnd)
	return active
}

func settleDialogTabSelection(state *dialogTabThemeState) {
	if state == nil {
		return
	}
	for index := range state.SelectionLevels {
		state.SelectionLevels[index] = 0
		if int32(index) == state.Selected {
			state.SelectionLevels[index] = 1
		}
	}
}

func settleDialogTabMotion(state *dialogTabThemeState) {
	if state == nil {
		return
	}
	for index := range state.HoverLevels {
		state.HoverLevels[index] = 0
		if int32(index) == state.Hot {
			state.HoverLevels[index] = 1
		}
	}
	settleDialogTabSelection(state)
	state.IndicatorFrom = float64(state.Selected)
	state.IndicatorTo = float64(state.Selected)
	state.IndicatorProgress = 1
	state.LastTick = time.Time{}
}

func settleDialogTabAnimation(hwnd uintptr) {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	if exists {
		settleDialogTabMotion(&state)
		dialogTabThemes[hwnd] = state
	}
	dialogTabMu.Unlock()
}

func dialogTabIndicatorRect(
	rects []dialogTabRect,
	valid []bool,
	state dialogTabThemeState,
	dpi int32,
) (dialogTabRect, bool) {
	position := dialogTabIndicatorPosition(state)
	if position < 0 || len(rects) == 0 || len(valid) != len(rects) {
		return dialogTabRect{}, false
	}
	maximum := float64(len(rects) - 1)
	if position > maximum {
		position = maximum
	}
	leftIndex := int(math.Floor(position))
	rightIndex := int(math.Ceil(position))
	if leftIndex < 0 || rightIndex >= len(rects) ||
		!valid[leftIndex] || !valid[rightIndex] {
		return dialogTabRect{}, false
	}
	amount := position - float64(leftIndex)
	from := rects[leftIndex]
	to := rects[rightIndex]
	indicator := dialogTabRect{
		Left:   dialogLerpInt32(from.Left, to.Left, amount),
		Top:    dialogLerpInt32(from.Top, to.Top, amount),
		Right:  dialogLerpInt32(from.Right, to.Right, amount),
		Bottom: dialogLerpInt32(from.Bottom, to.Bottom, amount),
	}
	indicator.Top = indicator.Bottom - scaleDialogTabMetric(3, dpi)
	indicator.Left += scaleDialogTabMetric(8, dpi)
	indicator.Right -= scaleDialogTabMetric(8, dpi)
	return indicator, indicator.Right > indicator.Left
}

func dialogLerpInt32(first, second int32, amount float64) int32 {
	amount = dialogClamp(amount)
	return int32(math.Round(
		float64(first)*(1-amount) + float64(second)*amount,
	))
}

func dialogTabColors(dark, material bool) dialogTabPalette {
	if dark {
		tokens := design.Default.Colors.Dark
		background := dialogTabToken(tokens.Surface)
		inactive := dialogTabToken(tokens.CanvasAlt)
		if material {
			inactive = background
		}
		return dialogTabPalette{
			Background: background,
			Inactive:   inactive,
			Hot:        dialogTabToken(tokens.SurfaceHigh),
			Selected:   dialogTabToken(tokens.SurfaceHigh),
			Text:       dialogTabToken(tokens.Text),
			MutedText:  dialogTabToken(tokens.TextMuted),
			Disabled:   dialogTabToken(tokens.TextSubtle),
			Accent:     dialogTabToken(tokens.Accent),
		}
	}
	tokens := design.Default.Colors.Light
	background := dialogTabToken(tokens.CanvasAlt)
	inactive := dialogTabToken(tokens.Canvas)
	if material {
		inactive = background
	}
	return dialogTabPalette{
		Background: background,
		Inactive:   inactive,
		Hot:        dialogTabToken(tokens.SurfaceHigh),
		Selected:   dialogTabToken(tokens.Surface),
		Text:       dialogTabToken(tokens.Text),
		MutedText:  dialogTabToken(tokens.TextMuted),
		Disabled:   dialogTabToken(tokens.TextSubtle),
		Accent:     dialogTabToken(tokens.Accent),
	}
}

func dialogTabToken(value string) uint32 {
	color := design.MustColor(value)
	return dialogTabRGB(color.R, color.G, color.B)
}

func dialogTabDPI(hwnd uintptr) int32 {
	dpi, _, _ := dialogTabGetDPIForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return int32(dpi)
}

func scaleDialogTabMetric(value, dpi int32) int32 {
	if value <= 0 {
		return 0
	}
	if dpi <= 0 {
		dpi = 96
	}
	return (value*dpi + 48) / 96
}

func dialogTabRGB(red, green, blue byte) uint32 {
	return uint32(red) | uint32(green)<<8 | uint32(blue)<<16
}

func fillDialogTabRect(dc uintptr, rect dialogTabRect, color uint32) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	brush, _, _ := dialogTabCreateBrush.Call(uintptr(color))
	if brush == 0 {
		return
	}
	_, _, _ = dialogTabFillRect.Call(
		dc,
		uintptr(unsafe.Pointer(&rect)),
		brush,
	)
	_, _, _ = dialogTabDeleteObject.Call(brush)
	runtime.KeepAlive(&rect)
}

func fillDialogTabRoundRect(
	dc uintptr,
	rect dialogTabRect,
	color uint32,
	radius int32,
) {
	if dc == 0 || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return
	}
	if radius <= 0 {
		fillDialogTabRect(dc, rect, color)
		return
	}
	brush, _, _ := dialogTabCreateBrush.Call(uintptr(color))
	if brush == 0 {
		return
	}
	oldBrush, _, _ := dialogTabSelectObject.Call(dc, brush)
	nullPen, _, _ := dialogTabGetStockObject.Call(dialogTabNullPen)
	oldPen, _, _ := dialogTabSelectObject.Call(dc, nullPen)
	diameter := uintptr(radius * 2)
	_, _, _ = dialogTabRoundRect.Call(
		dc,
		uintptr(rect.Left),
		uintptr(rect.Top),
		uintptr(rect.Right),
		uintptr(rect.Bottom),
		diameter,
		diameter,
	)
	if oldPen != 0 {
		_, _, _ = dialogTabSelectObject.Call(dc, oldPen)
	}
	if oldBrush != 0 {
		_, _, _ = dialogTabSelectObject.Call(dc, oldBrush)
	}
	_, _, _ = dialogTabDeleteObject.Call(brush)
}

func updateDialogTabHotItem(hwnd, packedPoint uintptr) {
	hit := dialogTabHitTest{
		X: int32(int16(uint16(packedPoint))),
		Y: int32(int16(uint16(packedPoint >> 16))),
	}
	index, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogTabTCMHitTest,
		0,
		uintptr(unsafe.Pointer(&hit)),
	)
	hot := int32(index)
	if index == ^uintptr(0) {
		hot = -1
	}
	setDialogTabHotItem(hwnd, hot)
	runtime.KeepAlive(&hit)
}

func setDialogTabHotItem(hwnd uintptr, hot int32) {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	changed := exists && state.Hot != hot
	animate := false
	if changed {
		state.Hot = hot
		if state.Animate {
			state.LastTick = time.Now()
			animate = true
		} else {
			for index := range state.HoverLevels {
				state.HoverLevels[index] = 0
				if int32(index) == hot {
					state.HoverLevels[index] = 1
				}
			}
			state.LastTick = time.Time{}
		}
		dialogTabThemes[hwnd] = state
	}
	dialogTabMu.Unlock()
	if changed {
		if animate && !startDialogTabAnimationScheduler(hwnd) {
			settleDialogTabAnimation(hwnd)
		}
		invalidateDialogTab(hwnd)
	}
}

func beginDialogTabMouseTracking(hwnd uintptr) bool {
	dialogTabMu.Lock()
	defer dialogTabMu.Unlock()
	state, exists := dialogTabThemes[hwnd]
	if !exists || state.Tracking {
		return false
	}
	state.Tracking = true
	dialogTabThemes[hwnd] = state
	return true
}

func endDialogTabMouseTracking(hwnd uintptr) {
	dialogTabMu.Lock()
	state, exists := dialogTabThemes[hwnd]
	if exists && state.Tracking {
		state.Tracking = false
		dialogTabThemes[hwnd] = state
	}
	dialogTabMu.Unlock()
}

func trackDialogTabMouseLeave(hwnd uintptr) {
	tracking := dialogTabTrackMouse{
		Size:   uint32(unsafe.Sizeof(dialogTabTrackMouse{})),
		Flags:  dialogTabTMELeave,
		Window: hwnd,
	}
	tracked, _, _ := dialogTabTrackMouseEvent.Call(
		uintptr(unsafe.Pointer(&tracking)),
	)
	if tracked == 0 {
		endDialogTabMouseTracking(hwnd)
	}
	runtime.KeepAlive(&tracking)
}

func startDialogTabAnimationScheduler(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	dialogTabSchedulerMu.Lock()
	if dialogTabSchedulers[hwnd] != nil {
		dialogTabSchedulerMu.Unlock()
		return true
	}
	token := atomic.AddUint32(&dialogTabSchedulerSequence, 1)
	if token == 0 {
		token = atomic.AddUint32(&dialogTabSchedulerSequence, 1)
	}
	scheduler := &dialogTabAnimationScheduler{
		token: token,
		stop:  make(chan struct{}),
	}
	dialogTabSchedulers[hwnd] = scheduler
	dialogTabSchedulerMu.Unlock()

	go runDialogTabAnimationScheduler(hwnd, scheduler)
	return true
}

func runDialogTabAnimationScheduler(
	hwnd uintptr,
	scheduler *dialogTabAnimationScheduler,
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
			posted, _, _ := dialogTabPostMessage.Call(
				hwnd,
				dialogTabWMAnimate,
				uintptr(scheduler.token),
				0,
			)
			if posted == 0 {
				scheduler.pending.Store(false)
			}
		}
	}
}

func consumeDialogTabAnimationFrame(hwnd uintptr, token uint32) bool {
	dialogTabSchedulerMu.Lock()
	scheduler := dialogTabSchedulers[hwnd]
	valid := scheduler != nil && scheduler.token == token
	if valid {
		scheduler.pending.Store(false)
	}
	dialogTabSchedulerMu.Unlock()
	return valid
}

func stopDialogTabAnimationScheduler(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dialogTabSchedulerMu.Lock()
	scheduler := dialogTabSchedulers[hwnd]
	delete(dialogTabSchedulers, hwnd)
	dialogTabSchedulerMu.Unlock()
	if scheduler != nil {
		close(scheduler.stop)
		scheduler.pending.Store(false)
	}
}

func invalidateDialogTab(hwnd uintptr) {
	_, _, _ = dialogTabInvalidateRect.Call(hwnd, 0, 0)
}

func invalidateDialogTabItem(hwnd uintptr, index int32) {
	if index < 0 {
		return
	}
	var rect dialogTabRect
	ok, _, _ := dialogThemeSendMessage.Call(
		hwnd,
		dialogTabTCMGetItemRect,
		uintptr(index),
		uintptr(unsafe.Pointer(&rect)),
	)
	if ok == 0 {
		invalidateDialogTab(hwnd)
		return
	}
	// Include the rounded fill and selection indicator edge without erasing
	// the entire tab surface between hover frames.
	rect.Left--
	rect.Top--
	rect.Right++
	rect.Bottom++
	_, _, _ = dialogTabInvalidateRect.Call(
		hwnd,
		uintptr(unsafe.Pointer(&rect)),
		0,
	)
	runtime.KeepAlive(&rect)
}
