//go:build windows

package nativeshell

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmQuit          = 0x0012
	wmSettingChange = 0x001A
	wmContextMenu   = 0x007B
	wmSetIcon       = 0x0080
	wmTimer         = 0x0113
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmThemeChanged  = 0x031A
	wmUser          = 0x0400
	wmApp           = 0x8000

	ninSelect    = wmUser
	ninKeySelect = wmUser + 1

	trayCallbackMessage = wmApp + 0x51
	applyThemeMessage   = wmApp + 0x52
	trayIconID          = 1
	trayTimerID         = 1

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifShowTip = 0x00000080

	notifyIconVersion4 = 4

	imageIcon     = 1
	lrDefaultSize = 0x00000040
	lrShared      = 0x00008000
	iconSmall     = 0
	iconBig       = 1

	mfString     = 0x00000000
	mfDisabled   = 0x00000002
	mfGrayed     = 0x00000001
	mfSeparator  = 0x00000800
	mfByPosition = 0x00000400

	tpmRightButton = 0x0002
	tpmNonotify    = 0x0080
	tpmReturnCmd   = 0x0100

	swHide = 0

	spiGetHighContrast                = 0x0042
	hcfHighContrastOn                 = 0x00000001
	dwmUseImmersiveDarkModeBefore20H1 = 19
	dwmUseImmersiveDarkMode           = 20
	smCXMenuCheck                     = 71
	smCYMenuCheck                     = 72
	smCXIcon                          = 11
	smCYIcon                          = 12
	smCXSmallIcon                     = 49
	smCYSmallIcon                     = 50
	smtoAbortIfHung                   = 0x0002
	consoleIconMessageTimeoutMS       = 250
)

type winPoint struct {
	X int32
	Y int32
}

type winMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   winPoint
	Private uint32
}

type windowClassEx struct {
	Size        uint32
	Style       uint32
	WindowProc  uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
	SmallIcon   uintptr
}

type highContrast struct {
	Size          uint32
	Flags         uint32
	DefaultScheme *uint16
}

type iconInfo struct {
	Icon     int32
	HotspotX uint32
	HotspotY uint32
	Mask     uintptr
	Color    uintptr
}

// notifyIconData matches NOTIFYICONDATAW through hBalloonIcon. uintptr fields
// preserve the SDK's architecture-specific alignment on both 32- and 64-bit
// Windows builds.
type notifyIconData struct {
	Size            uint32
	Window          uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     uintptr
}

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	shell32                   = windows.NewLazySystemDLL("shell32.dll")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	uxtheme                   = windows.NewLazySystemDLL("uxtheme.dll")
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32                     = windows.NewLazySystemDLL("gdi32.dll")
	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procUnregisterClass       = user32.NewProc("UnregisterClassW")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procGetMessage            = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procPostMessage           = user32.NewProc("PostMessageW")
	procPostThreadMessage     = user32.NewProc("PostThreadMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procLoadImage             = user32.NewProc("LoadImageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu            = user32.NewProc("AppendMenuW")
	procDeleteMenu            = user32.NewProc("DeleteMenu")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procGetMenuItemCount      = user32.NewProc("GetMenuItemCount")
	procSetMenuDefaultItem    = user32.NewProc("SetMenuDefaultItem")
	procSetMenuItemBitmaps    = user32.NewProc("SetMenuItemBitmaps")
	procTrackPopupMenuEx      = user32.NewProc("TrackPopupMenuEx")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetIconInfo           = user32.NewProc("GetIconInfo")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfo  = user32.NewProc("SystemParametersInfoW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID    = kernel32.NewProc("GetCurrentThreadId")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procSendMessageTimeout    = user32.NewProc("SendMessageTimeoutW")
	procSetWindowTheme        = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	procDeleteObject          = gdi32.NewProc("DeleteObject")

	windowProcedure = syscall.NewCallback(nativeWindowProcedure)
	windowsShells   sync.Map
)

// nativeThemeState keeps theme application out of re-entrant Win32 message
// dispatch. SetWindowTheme can synchronously send WM_THEMECHANGED to its target
// window, so the active flag must be set before entering uxtheme.dll. The
// applied-mode cache also turns a delayed self-notification into a no-op.
type nativeThemeState struct {
	applying    atomic.Bool
	posted      atomic.Bool
	initialized atomic.Bool
	dark        atomic.Bool
}

func (state *nativeThemeState) begin(dark bool) bool {
	if !state.applying.CompareAndSwap(false, true) {
		return false
	}
	if state.initialized.Load() && state.dark.Load() == dark {
		state.applying.Store(false)
		return false
	}
	return true
}

func (state *nativeThemeState) complete(dark bool) {
	state.dark.Store(dark)
	state.initialized.Store(true)
	state.applying.Store(false)
}

func (state *nativeThemeState) requestPost() bool {
	return !state.applying.Load() && state.posted.CompareAndSwap(false, true)
}

func (state *nativeThemeState) consumePost() {
	state.posted.Store(false)
}

type windowsShell struct {
	options Options

	actionContext        context.Context
	cancelActions        context.CancelFunc
	window               atomic.Uintptr
	threadID             atomic.Uint32
	instance             uintptr
	className            *uint16
	icon                 uintptr
	icons                map[IconState]uintptr
	activeIcon           uintptr
	lastIconState        IconState
	menu                 uintptr
	menuBitmap           uintptr
	menuItems            []MenuItem
	taskbarCreated       uint32
	lastTooltip          string
	iconAdded            bool
	theme                nativeThemeState
	systemEvents         systemEventEmitter
	sessionNotifications bool
	networkMonitorDone   chan struct{}
	closeOnce            sync.Once
	done                 chan struct{}
	initialization       chan error
}

func Supported() bool { return true }

// ApplyConsoleIcon assigns the packaged APP resource to an attached classic
// console window. Windows does not consistently replace conhost's inherited
// icon when the executable icon uses a named RT_GROUP_ICON resource, especially
// when Controller is launched from a build shell. The tray keeps using the same
// resource independently. Windows Terminal/pseudoconsole and resource-free
// development builds have no applicable HWND/icon and therefore remain a safe
// no-op.
func ApplyConsoleIcon() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	instance, _, _ := procGetModuleHandle.Call(0)
	resourceName, err := windows.UTF16PtrFromString("APP")
	if err != nil {
		return
	}
	load := func(small bool) uintptr {
		xMetric, yMetric := uintptr(smCXIcon), uintptr(smCYIcon)
		if small {
			xMetric, yMetric = smCXSmallIcon, smCYSmallIcon
		}
		width, _, _ := procGetSystemMetrics.Call(xMetric)
		height, _, _ := procGetSystemMetrics.Call(yMetric)
		icon, _, _ := procLoadImage.Call(
			instance,
			uintptr(unsafe.Pointer(resourceName)),
			imageIcon,
			width,
			height,
			lrShared,
		)
		return icon
	}
	applyConsoleIcons(hwnd, instance, load, func(kind, icon uintptr) {
		var result uintptr
		_, _, _ = procSendMessageTimeout.Call(
			hwnd, wmSetIcon, kind, icon,
			smtoAbortIfHung, consoleIconMessageTimeoutMS,
			uintptr(unsafe.Pointer(&result)),
		)
	})
}

func applyConsoleIcons(
	hwnd, instance uintptr,
	load func(small bool) uintptr,
	set func(kind, icon uintptr),
) {
	if hwnd == 0 || instance == 0 || load == nil || set == nil {
		return
	}
	if icon := load(false); icon != 0 {
		set(iconBig, icon)
	}
	if icon := load(true); icon != 0 {
		set(iconSmall, icon)
	}
}

func startPlatform(ctx context.Context, options Options) (Shell, error) {
	actionContext, cancelActions := context.WithCancel(ctx)
	shell := &windowsShell{
		options:        options,
		actionContext:  actionContext,
		cancelActions:  cancelActions,
		systemEvents:   systemEventEmitter{callback: options.HandleSystemEvent},
		done:           make(chan struct{}),
		initialization: make(chan error, 1),
	}
	go shell.loop()
	if err := <-shell.initialization; err != nil {
		<-shell.done
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			shell.requestClose()
		case <-shell.done:
		}
	}()
	return shell, nil
}

func (shell *windowsShell) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(shell.done)
	threadID, _, _ := procGetCurrentThreadID.Call()
	shell.threadID.Store(uint32(threadID))
	defer shell.threadID.Store(0)
	if err := shell.initialize(); err != nil {
		shell.cleanup()
		shell.initialization <- err
		return
	}
	shell.initialization <- nil

	var message winMessage
	for {
		result, _, callErr := procGetMessage.Call(
			uintptr(unsafe.Pointer(&message)), 0, 0, 0,
		)
		if int32(result) == -1 {
			report(shell.options, windowsCallError("read native shell message", callErr))
			break
		}
		if result == 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	shell.cleanup()
}

func (shell *windowsShell) initialize() error {
	instance, _, callErr := procGetModuleHandle.Call(0)
	if instance == 0 {
		return windowsCallError("resolve executable module", callErr)
	}
	shell.instance = instance
	className, err := windows.UTF16PtrFromString(fmt.Sprintf(
		"Controller.NativeWebShell.%d.%d", windows.GetCurrentProcessId(), time.Now().UnixNano(),
	))
	if err != nil {
		return fmt.Errorf("encode native shell window class: %w", err)
	}
	shell.className = className
	class := windowClassEx{
		Size:       uint32(unsafe.Sizeof(windowClassEx{})),
		WindowProc: windowProcedure,
		Instance:   instance,
		ClassName:  className,
	}
	if result, _, registerErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); result == 0 {
		return windowsCallError("register native shell window class", registerErr)
	}

	windowName, _ := windows.UTF16PtrFromString(
		normalizeTitle(shell.options.Snapshot().Title) + " native web shell",
	)
	hwnd, _, createErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0,
		0,
		instance,
		0,
	)
	if hwnd == 0 {
		return windowsCallError("create native shell message window", createErr)
	}
	shell.window.Store(hwnd)
	windowsShells.Store(hwnd, shell)
	_, _, _ = procShowWindow.Call(hwnd, swHide)
	shell.applyNativeMenuTheme(hwnd)

	resourceName, _ := windows.UTF16PtrFromString("APP")
	icon, _, iconErr := procLoadImage.Call(
		instance,
		uintptr(unsafe.Pointer(resourceName)),
		imageIcon,
		0,
		0,
		lrDefaultSize|lrShared,
	)
	if icon == 0 {
		return windowsCallError("load APP icon from executable resources", iconErr)
	}
	shell.icon = icon
	shell.icons = make(map[IconState]uintptr, 4)
	for _, state := range []IconState{IconConnected, IconReconnecting, IconPaused, IconOffline} {
		name, _ := windows.UTF16PtrFromString(iconResourceName(state))
		stateIcon, _, _ := procLoadImage.Call(
			instance,
			uintptr(unsafe.Pointer(name)),
			imageIcon,
			0,
			0,
			lrDefaultSize|lrShared,
		)
		if stateIcon != 0 {
			shell.icons[state] = stateIcon
		}
	}
	shell.menuBitmap = loadMenuBitmap(instance, resourceName)
	if err := shell.createMenu(); err != nil {
		return err
	}
	taskbarCreatedName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbarCreated, _, taskbarErr := procRegisterWindowMessage.Call(
		uintptr(unsafe.Pointer(taskbarCreatedName)),
	)
	if taskbarCreated == 0 {
		return windowsCallError("register taskbar recovery message", taskbarErr)
	}
	shell.taskbarCreated = uint32(taskbarCreated)
	if err := shell.addIcon(); err != nil {
		return err
	}
	if timer, _, timerErr := procSetTimer.Call(hwnd, trayTimerID, 1000, 0); timer == 0 {
		return windowsCallError("start native shell state timer", timerErr)
	}
	shell.initializeSystemEvents(hwnd)
	return nil
}

func nativeWindowProcedure(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	value, found := windowsShells.Load(hwnd)
	if !found {
		result, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	shell := value.(*windowsShell)
	if message == shell.taskbarCreated && shell.taskbarCreated != 0 {
		shell.iconAdded = false
		if err := shell.addIcon(); err != nil {
			report(shell.options, err)
		}
		return 0
	}
	switch message {
	case wmTimer:
		if wParam == trayTimerID {
			if err := shell.refreshTooltip(); err != nil {
				report(shell.options, err)
			}
			if err := shell.refreshMenu(false); err != nil {
				report(shell.options, err)
			}
			return 0
		}
	case trayCallbackMessage:
		shell.handleTrayNotification(wParam, lParam)
		return 0
	case wmWTSSessionChange:
		if event, ok := decodeNativeSystemEvent(message, wParam); ok {
			shell.systemEvents.emit(event)
		}
		return 0
	case wmPowerBroadcast:
		if event, ok := decodeNativeSystemEvent(message, wParam); ok {
			shell.systemEvents.emit(event)
		}
		// WM_POWERBROADCAST expects a non-zero result when the notification
		// was accepted. No suspend veto is attempted by this status-only shell.
		return 1
	case wmSettingChange, wmThemeChanged:
		shell.deferNativeMenuTheme(hwnd)
		return 0
	case applyThemeMessage:
		shell.theme.consumePost()
		shell.applyNativeMenuTheme(hwnd)
		return 0
	case wmContextMenu:
		shell.showMenu(pointFromPacked(lParam))
		return 0
	case wmClose:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		_, _, _ = procKillTimer.Call(hwnd, trayTimerID)
		shell.unregisterSystemEvents(hwnd)
		shell.removeIcon()
		windowsShells.Delete(hwnd)
		shell.window.Store(0)
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (shell *windowsShell) handleTrayNotification(wParam, lParam uintptr) {
	notification := uint32(lParam & 0xffff)
	switch notification {
	case wmContextMenu:
		shell.showMenu(pointFromPacked(wParam))
	case ninSelect, ninKeySelect:
		shell.showMenu(pointFromPacked(wParam))
	case wmLButtonUp, wmRButtonUp:
		shell.showMenu(cursorPoint())
	}
}

func (shell *windowsShell) showMenu(point winPoint) {
	hwnd := shell.window.Load()
	if hwnd == 0 || shell.menu == 0 {
		return
	}
	if point.X == -1 && point.Y == -1 || point.X == 0 && point.Y == 0 {
		point = cursorPoint()
	}
	if err := shell.refreshMenu(false); err != nil {
		report(shell.options, err)
		return
	}
	menu := shell.menu
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	command, _, _ := procTrackPopupMenuEx.Call(
		menu,
		tpmRightButton|tpmNonotify|tpmReturnCmd,
		uintptr(int64(point.X)),
		uintptr(int64(point.Y)),
		hwnd,
		0,
	)
	// This documented WM_NULL nudge makes a native tray menu dismiss correctly
	// when focus changes to another process.
	_, _, _ = procPostMessage.Call(hwnd, wmNull, 0, 0)
	if command == 0 {
		return
	}
	selected := Command(command)
	go func() {
		actionContext, cancel := context.WithTimeout(shell.actionContext, 15*time.Second)
		defer cancel()
		dispatch(actionContext, shell.options, selected)
	}()
}

func (shell *windowsShell) createMenu() error {
	menu, _, callErr := procCreatePopupMenu.Call()
	if menu == 0 {
		return windowsCallError("create native shell menu", callErr)
	}
	shell.menu = menu
	if err := shell.refreshMenu(true); err != nil {
		_, _, _ = procDestroyMenu.Call(menu)
		shell.menu = 0
		return err
	}
	return nil
}

func (shell *windowsShell) refreshMenu(force bool) error {
	if shell.menu == 0 {
		return errors.New("native shell menu is unavailable")
	}
	items := BuildMenu(shell.options.Snapshot())
	if !force && menuItemsEqual(items, shell.menuItems) {
		return nil
	}
	count, _, countErr := procGetMenuItemCount.Call(shell.menu)
	if int32(count) == -1 {
		return windowsCallError("count native shell menu items", countErr)
	}
	for position := int(count) - 1; position >= 0; position-- {
		if result, _, deleteErr := procDeleteMenu.Call(
			shell.menu, uintptr(position), mfByPosition,
		); result == 0 {
			return windowsCallError("clear native shell menu", deleteErr)
		}
	}
	defaultCommand := CommandNone
	for _, item := range items {
		flags := uintptr(mfString)
		identifier := uintptr(item.Command)
		var label uintptr
		var labelText *uint16
		switch item.Kind {
		case ItemSeparator:
			flags = mfSeparator
			identifier = 0
		case ItemStatus:
			flags |= mfDisabled | mfGrayed
			identifier = 0
			labelText, _ = windows.UTF16PtrFromString(safeMenuText(item.Label))
			label = uintptr(unsafe.Pointer(labelText))
		case ItemAction:
			if !item.Enabled {
				flags |= mfDisabled | mfGrayed
			}
			if item.Default {
				defaultCommand = item.Command
			}
			labelText, _ = windows.UTF16PtrFromString(safeMenuText(item.Label))
			label = uintptr(unsafe.Pointer(labelText))
		}
		if result, _, appendErr := procAppendMenu.Call(shell.menu, flags, identifier, label); result == 0 {
			return windowsCallError("append native shell menu item", appendErr)
		}
		if item.Kind == ItemAction && shell.menuBitmap != 0 && (item.Default || item.Command == CommandReconnect) {
			// The bitmap remains owned by windowsShell and is reused by the small
			// number of primary app actions. Native menu drawing preserves system
			// focus, disabled, high-contrast, and keyboard behavior.
			_, _, _ = procSetMenuItemBitmaps.Call(
				shell.menu, identifier, 0, shell.menuBitmap, 0,
			)
		}
		runtime.KeepAlive(labelText)
	}
	if defaultCommand != CommandNone {
		_, _, _ = procSetMenuDefaultItem.Call(shell.menu, uintptr(defaultCommand), 0)
	}
	shell.menuItems = append(shell.menuItems[:0], items...)
	return nil
}

func (shell *windowsShell) addIcon() error {
	shell.lastIconState = StateIcon(shell.options.Snapshot())
	shell.activeIcon = shell.iconForState(shell.lastIconState)
	data := shell.iconData(nifMessage | nifIcon | nifTip | nifShowTip)
	if result, _, callErr := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data))); result == 0 {
		return windowsCallError("add native shell icon", callErr)
	}
	version := shell.iconData(0)
	version.Version = notifyIconVersion4
	if result, _, callErr := procShellNotifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&version))); result == 0 {
		deleteData := shell.iconData(0)
		_, _, _ = procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&deleteData)))
		return windowsCallError("set native shell icon behavior", callErr)
	}
	shell.iconAdded = true
	shell.lastTooltip = Tooltip(shell.options.Snapshot())
	return nil
}

func (shell *windowsShell) refreshTooltip() error {
	if !shell.iconAdded {
		return nil
	}
	state := shell.options.Snapshot()
	tooltip := Tooltip(state)
	iconState := StateIcon(state)
	if tooltip == shell.lastTooltip && iconState == shell.lastIconState {
		return nil
	}
	flags := uint32(nifTip | nifShowTip)
	previousIcon := shell.activeIcon
	if iconState != shell.lastIconState {
		flags |= nifIcon
		shell.activeIcon = shell.iconForState(iconState)
	}
	data := shell.iconData(flags)
	if result, _, callErr := procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&data))); result == 0 {
		shell.activeIcon = previousIcon
		return windowsCallError("update native shell status", callErr)
	}
	shell.lastTooltip = tooltip
	shell.lastIconState = iconState
	return nil
}

func (shell *windowsShell) iconData(flags uint32) notifyIconData {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Window:          shell.window.Load(),
		ID:              trayIconID,
		Flags:           flags,
		CallbackMessage: trayCallbackMessage,
		Icon:            shell.activeIcon,
	}
	if data.Icon == 0 {
		data.Icon = shell.icon
	}
	copyUTF16(data.Tip[:], Tooltip(shell.options.Snapshot()))
	return data
}

func (shell *windowsShell) iconForState(state IconState) uintptr {
	if shell.icons != nil {
		if icon := shell.icons[state]; icon != 0 {
			return icon
		}
	}
	return shell.icon
}

func iconResourceName(state IconState) string {
	switch state {
	case IconConnected:
		return "TRAY_CONNECTED"
	case IconReconnecting:
		return "TRAY_RECONNECTING"
	case IconPaused:
		return "TRAY_PAUSED"
	default:
		return "TRAY_OFFLINE"
	}
}

func (shell *windowsShell) removeIcon() {
	if !shell.iconAdded {
		return
	}
	data := shell.iconData(0)
	_, _, _ = procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	shell.iconAdded = false
}

func (shell *windowsShell) cleanup() {
	shell.cancelActions()
	shell.waitForNetworkMonitor()
	hwnd := shell.window.Load()
	if hwnd != 0 {
		_, _, _ = procKillTimer.Call(hwnd, trayTimerID)
		shell.unregisterSystemEvents(hwnd)
		shell.removeIcon()
		_, _, _ = procDestroyWindow.Call(hwnd)
		windowsShells.Delete(hwnd)
		shell.window.Store(0)
	}
	if shell.menu != 0 {
		_, _, _ = procDestroyMenu.Call(shell.menu)
		shell.menu = 0
		shell.menuItems = nil
	}
	if shell.menuBitmap != 0 {
		_, _, _ = procDeleteObject.Call(shell.menuBitmap)
		shell.menuBitmap = 0
	}
	if shell.className != nil && shell.instance != 0 {
		_, _, _ = procUnregisterClass.Call(
			uintptr(unsafe.Pointer(shell.className)), shell.instance,
		)
	}
}

func menuItemsEqual(left, right []MenuItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func loadMenuBitmap(instance uintptr, resourceName *uint16) uintptr {
	width, _, _ := procGetSystemMetrics.Call(smCXMenuCheck)
	height, _, _ := procGetSystemMetrics.Call(smCYMenuCheck)
	if int32(width) <= 0 {
		width = 16
	}
	if int32(height) <= 0 {
		height = 16
	}
	icon, _, _ := procLoadImage.Call(
		instance,
		uintptr(unsafe.Pointer(resourceName)),
		imageIcon,
		width,
		height,
		lrShared,
	)
	if icon == 0 {
		return 0
	}
	var info iconInfo
	if result, _, _ := procGetIconInfo.Call(
		icon, uintptr(unsafe.Pointer(&info)),
	); result == 0 {
		return 0
	}
	if info.Mask != 0 {
		_, _, _ = procDeleteObject.Call(info.Mask)
	}
	// GetIconInfo creates this color bitmap for the caller. The shell owns it
	// until after DestroyMenu, then releases it with DeleteObject.
	return info.Color
}

func (shell *windowsShell) deferNativeMenuTheme(hwnd uintptr) {
	if hwnd == 0 || !shell.theme.requestPost() {
		return
	}
	if result, _, callErr := procPostMessage.Call(hwnd, applyThemeMessage, 0, 0); result == 0 {
		shell.theme.consumePost()
		report(shell.options, windowsCallError("defer native shell theme update", callErr))
	}
}

func (shell *windowsShell) applyNativeMenuTheme(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dark := windowsAppsUseDarkTheme()
	if !shell.theme.begin(dark) {
		return
	}
	defer shell.theme.complete(dark)
	if err := procSetWindowTheme.Find(); err == nil {
		themeName := "Explorer"
		if dark {
			themeName = "DarkMode_Explorer"
		}
		theme, themeErr := windows.UTF16PtrFromString(themeName)
		if themeErr == nil {
			_, _, _ = procSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(theme)), 0)
		}
	}
	if err := procDwmSetWindowAttribute.Find(); err != nil {
		return
	}
	enabled := int32(0)
	if dark {
		enabled = 1
	}
	for _, attribute := range []uintptr{dwmUseImmersiveDarkMode, dwmUseImmersiveDarkModeBefore20H1} {
		result, _, _ := procDwmSetWindowAttribute.Call(
			hwnd,
			attribute,
			uintptr(unsafe.Pointer(&enabled)),
			unsafe.Sizeof(enabled),
		)
		if int32(result) >= 0 {
			break
		}
	}
}

func windowsAppsUseDarkTheme() bool {
	var contrast highContrast
	contrast.Size = uint32(unsafe.Sizeof(contrast))
	result, _, _ := procSystemParametersInfo.Call(
		spiGetHighContrast,
		uintptr(contrast.Size),
		uintptr(unsafe.Pointer(&contrast)),
		0,
	)
	// If accessibility state cannot be read, retain the native default rather
	// than risking a low-contrast forced theme.
	if result == 0 || contrast.Flags&hcfHighContrastOn != 0 {
		return false
	}
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return false
	}
	defer key.Close()
	appsUseLightTheme, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return err == nil && appsUseLightTheme == 0
}

func (shell *windowsShell) requestClose() {
	hwnd := shell.window.Load()
	if hwnd != 0 {
		if result, _, _ := procPostMessage.Call(hwnd, wmClose, 0, 0); result != 0 {
			return
		}
	}
	if threadID := shell.threadID.Load(); threadID != 0 {
		// The loop cleanup path still destroys the window and icon after WM_QUIT,
		// so a stale/failed HWND post cannot strand native handles.
		_, _, _ = procPostThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
	}
}

func (shell *windowsShell) Close() error {
	if shell == nil {
		return nil
	}
	shell.closeOnce.Do(shell.requestClose)
	select {
	case <-shell.done:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("native shell message loop did not stop within two seconds")
	}
}

func pointFromPacked(value uintptr) winPoint {
	return winPoint{
		X: int32(int16(value & 0xffff)),
		Y: int32(int16((value >> 16) & 0xffff)),
	}
}

func cursorPoint() winPoint {
	var point winPoint
	if result, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&point))); result == 0 {
		return winPoint{}
	}
	return point
}

func copyUTF16(target []uint16, value string) {
	if len(target) == 0 {
		return
	}
	value = strings.ReplaceAll(value, "\x00", "")
	units := utf16.Encode([]rune(value))
	if len(units) >= len(target) {
		units = units[:len(target)-1]
		// Avoid leaving an unmatched high surrogate at the truncation edge.
		if len(units) > 0 && units[len(units)-1] >= 0xD800 && units[len(units)-1] <= 0xDBFF {
			units = units[:len(units)-1]
		}
	}
	copy(target, units)
	target[len(units)] = 0
}

func safeMenuText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(value, "&", "&&")
	return strings.TrimSpace(value)
}

func windowsCallError(operation string, err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s: %w", operation, err)
}
