//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	dialogDWMCornerRound = 2

	dialogThemeObserverSubclassID = 0x524C4454 // "RLDT"

	dialogWMSysColorChange              = 0x0015
	dialogWMSettingChange               = 0x001A
	dialogWMThemeChanged                = 0x031A
	dialogWMDWMCompositionChanged       = 0x031E
	dialogWMDWMColorizationColorChanged = 0x0320

	dialogRDWInvalidate  = 0x0001
	dialogRDWErase       = 0x0004
	dialogRDWNoErase     = 0x0020
	dialogRDWNoChildren  = 0x0040
	dialogRDWUpdateNow   = 0x0100
	dialogRDWFrame       = 0x0400
	dialogRDWNoFrame     = 0x0800
	dialogRDWAllChildren = 0x0080
	// AllowDarkModeForWindow is an undocumented but stable UxTheme export on
	// Windows 10 1809 and newer. Calling it is best-effort; documented DWM and
	// SetWindowTheme APIs still provide the fallback path.
	dialogAllowDarkModeForWindowOrdinal = 133
)

var (
	dialogThemeUser32           = windows.NewLazySystemDLL("user32.dll")
	dialogThemeEnumChildWindows = dialogThemeUser32.NewProc("EnumChildWindows")
	dialogThemeGetClassName     = dialogThemeUser32.NewProc("GetClassNameW")
	dialogThemeSendMessage      = dialogThemeUser32.NewProc("SendMessageW")
	dialogThemeRedrawWindow     = dialogThemeUser32.NewProc("RedrawWindow")
	dialogThemeKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	dialogThemeGetProcAddress   = dialogThemeKernel32.NewProc("GetProcAddress")
	dialogThemeUXTheme          = windows.NewLazySystemDLL("uxtheme.dll")
	dialogThemeSetWindowTheme   = dialogThemeUXTheme.NewProc("SetWindowTheme")

	dialogThemeObserverMu     sync.Mutex
	dialogThemeObserverStates = make(map[uintptr]*dialogThemeObserverState)
)

type dialogThemeObserverState struct {
	dark                 bool
	animate              bool
	highContrast         bool
	applying             bool
	accessibilityChanged func() bool
}

// DialogThemeResult reports the accessibility mode and whether a native DWM
// client material was applied. Walk/GDI dialogs deliberately keep Glass false:
// full-client frame extension changes the alpha interpretation of child HWND
// surfaces and can wash colors out or fringe antialiased text.
type DialogThemeResult struct {
	Glass          bool
	HighContrast   bool
	ControlsThemed bool
}

// ApplyDialogTheme applies Windows' native modern dialog treatment to a
// top-level modal and all of its current child controls. It is safe to call
// repeatedly when the user changes the light/dark setting.
func ApplyDialogTheme(hwnd uintptr, dark bool) error {
	_, err := ApplyDialogThemeWithMotionResult(hwnd, dark, true)
	return err
}

// ApplyDialogThemeWithResult is ApplyDialogTheme plus an explicit material
// result so GDI callers can choose a safe opaque fallback when DWM rejects a
// backdrop at runtime.
func ApplyDialogThemeWithResult(
	hwnd uintptr,
	dark bool,
) (DialogThemeResult, error) {
	return ApplyDialogThemeWithMotionResult(hwnd, dark, true)
}

// ApplyDialogThemeWithMotionResult additionally controls custom interaction
// motion. It lets the settings window apply its "animations" preference
// immediately without giving up native dark-mode and accessibility theming.
func ApplyDialogThemeWithMotionResult(
	hwnd uintptr,
	dark, animate bool,
) (DialogThemeResult, error) {
	return applyDialogThemeWithMotionResult(hwnd, dark, animate, true)
}

// ApplyDialogClientThemeWithMotionResult stages the native client controls but
// deliberately leaves the DWM caption attribute unchanged. Callers that also
// own Walk brushes or retained custom surfaces can finish those first, then
// publish the complete client and caption with CommitDialogThemeFrame.
func ApplyDialogClientThemeWithMotionResult(
	hwnd uintptr,
	dark, animate bool,
) (DialogThemeResult, error) {
	return applyDialogThemeWithMotionResult(hwnd, dark, animate, false)
}

func applyDialogThemeWithMotionResult(
	hwnd uintptr,
	dark, animate, commitCaption bool,
) (DialogThemeResult, error) {
	if hwnd == 0 || !isWindow(hwnd) {
		return DialogThemeResult{}, ErrInvalidWindow
	}

	info, infoErr := GetOSInfo()
	result := DialogThemeResult{HighContrast: info.HighContrast}
	rememberDialogThemeObserver(
		hwnd,
		dark,
		animate,
		info.HighContrast,
	)
	// SetWindowTheme normally delivers WM_THEMECHANGED synchronously (with a
	// compatibility fallback when it fails). Mark every direct application,
	// not only observer-initiated ones, so an explicit settings change cannot
	// recursively enter the higher dialog layer while its brushes and custom
	// surfaces are only half rebuilt.
	finishApply := beginDialogThemeApply(hwnd)
	defer finishApply()
	if info.HighContrast {
		dark = false
	}

	_ = setDWMInt32(hwnd, dwmwaWindowCornerPref, dialogDWMCornerRound)
	var failures []error
	if infoErr != nil {
		failures = append(failures, infoErr)
	}
	// Keep DWM in charge of the real caption, dark mode, and rounded frame,
	// but do not extend that frame into Walk's GDI client. Full-client DWM
	// alpha changes the composition of child HWNDs and caused the gray slab,
	// black-void, and ClearType-fringe regressions this path is meant to avoid.
	if err := clearDialogBackdrop(hwnd); err != nil {
		failures = append(failures, err)
	}

	themeDialogWindow(hwnd, dark, info.HighContrast, animate)
	callback := syscall.NewCallback(func(child, _ uintptr) uintptr {
		themeDialogWindow(child, dark, info.HighContrast, animate)
		return 1
	})
	enumResult, _, enumErr := dialogThemeEnumChildWindows.Call(
		hwnd,
		callback,
		0,
	)
	runtime.KeepAlive(callback)

	_, _, _ = dialogThemeRedrawWindow.Call(
		hwnd,
		0,
		0,
		dialogClientThemeRedrawFlags(false),
	)

	if enumErr := dialogEnumerationError(enumResult, enumErr); enumErr != nil {
		failures = append(failures, enumErr)
	} else {
		result.ControlsThemed = true
	}
	if commitCaption {
		if err := CommitDialogThemeFrame(hwnd, dark); err != nil {
			failures = append(failures, err)
		}
	}
	return result, errors.Join(failures...)
}

// CommitDialogThemeFrame synchronously paints the final native client first,
// then publishes and repaints the DWM caption. Consequently an observer that
// sees the new immersive-caption attribute can rely on all child HWNDs already
// containing the same appearance, rather than catching a half-painted dialog.
func CommitDialogThemeFrame(hwnd uintptr, dark bool) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	clientResult, _, clientCallErr := dialogThemeRedrawWindow.Call(
		hwnd,
		0,
		0,
		dialogClientThemeRedrawFlags(true),
	)
	if clientResult == 0 {
		return boolCallError(
			"RedrawWindow(dialog client theme commit)",
			clientCallErr,
		)
	}
	// RDW_UPDATENOW dispatches arbitrary window procedures. The dialog may
	// close reentrantly while painting, so never pass the original handle to
	// DWM without validating it again.
	if !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	applyDarkMode(hwnd, dark, CurrentVersion())
	frameResult, _, frameCallErr := dialogThemeRedrawWindow.Call(
		hwnd,
		0,
		0,
		dialogCaptionThemeRedrawFlags(),
	)
	if frameResult == 0 {
		return boolCallError(
			"RedrawWindow(dialog caption theme commit)",
			frameCallErr,
		)
	}
	return nil
}

func dialogClientThemeRedrawFlags(synchronous bool) uintptr {
	flags := uintptr(
		dialogRDWInvalidate |
			dialogRDWErase |
			dialogRDWNoFrame |
			dialogRDWAllChildren,
	)
	if synchronous {
		flags |= dialogRDWUpdateNow
	}
	return flags
}

func dialogCaptionThemeRedrawFlags() uintptr {
	return dialogRDWInvalidate |
		dialogRDWNoErase |
		dialogRDWNoChildren |
		dialogRDWUpdateNow |
		dialogRDWFrame
}

// rememberDialogThemeObserver keeps custom chrome synchronized with live
// appearance and accessibility changes. Windows can change app color mode or
// enable High Contrast while a modeless Settings window is already open;
// without a top-level observer, fields that intentionally remove their native
// edge would remain stale until the dialog was reopened.
func rememberDialogThemeObserver(
	hwnd uintptr,
	dark, animate, highContrast bool,
) {
	dialogThemeObserverMu.Lock()
	state, exists := dialogThemeObserverStates[hwnd]
	if !exists {
		state = &dialogThemeObserverState{}
		dialogThemeObserverStates[hwnd] = state
	}
	state.dark = dark
	state.animate = animate
	state.highContrast = highContrast
	dialogThemeObserverMu.Unlock()

	if exists {
		return
	}
	if err := SetWindowSubclass(
		hwnd,
		dialogThemeObserverSubclassID,
		dialogThemeObserverWindowMessage,
	); err != nil && !errors.Is(err, ErrSubclassExists) {
		dialogThemeObserverMu.Lock()
		delete(dialogThemeObserverStates, hwnd)
		dialogThemeObserverMu.Unlock()
	}
}

func beginDialogThemeApply(hwnd uintptr) func() {
	dialogThemeObserverMu.Lock()
	state := dialogThemeObserverStates[hwnd]
	if state == nil {
		dialogThemeObserverMu.Unlock()
		return func() {}
	}
	previous := state.applying
	state.applying = true
	dialogThemeObserverMu.Unlock()

	return func() {
		dialogThemeObserverMu.Lock()
		if current := dialogThemeObserverStates[hwnd]; current == state {
			current.applying = previous
		}
		dialogThemeObserverMu.Unlock()
	}
}

func dialogThemeObserverWindowMessage(
	hwnd, message, wParam, lParam uintptr,
) (uintptr, bool) {
	if dialogThemeObserverRefreshMessage(uint32(message)) {
		// Let the native dialog consume the system notification before custom
		// chrome is reconciled. Internal WM_THEMECHANGED messages sent while
		// applying a theme are guarded by state.applying.
		result := callDefSubclassProc(hwnd, message, wParam, lParam)
		reconcileDialogThemeAppearance(hwnd, true)
		return result, true
	}

	switch uint32(message) {
	case wmNCDestroy:
		dialogThemeObserverMu.Lock()
		delete(dialogThemeObserverStates, hwnd)
		dialogThemeObserverMu.Unlock()
	}
	return 0, false
}

func dialogThemeObserverRefreshMessage(message uint32) bool {
	switch message {
	case dialogWMSysColorChange,
		dialogWMSettingChange,
		dialogWMThemeChanged,
		dialogWMDWMCompositionChanged,
		dialogWMDWMColorizationColorChanged:
		return true
	default:
		return false
	}
}

// SetDialogThemeAccessibilityHandler connects native appearance and
// accessibility broadcasts to a higher dialog layer that also owns brushes or
// custom child surfaces.
// The handler runs synchronously on the dialog's UI thread and reports whether
// it applied the new system policy. ApplyDialogThemeWithMotionResult must have
// initialized the HWND before a non-nil handler is installed.
func SetDialogThemeAccessibilityHandler(
	hwnd uintptr,
	handler func() bool,
) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	dialogThemeObserverMu.Lock()
	defer dialogThemeObserverMu.Unlock()
	state := dialogThemeObserverStates[hwnd]
	if state == nil {
		if handler == nil {
			return nil
		}
		return errors.New("winfx: dialog theme observer is not initialized")
	}
	state.accessibilityChanged = handler
	return nil
}

func reconcileDialogThemeAppearance(hwnd uintptr, appearanceChanged bool) {
	// GetOSInfo returns useful partial policy even if an unrelated capability
	// probe fails. An inaccessible transparency registry value, for example,
	// must not prevent a system light/dark broadcast from reaching the dialog.
	info, _ := GetOSInfo()
	dialogThemeObserverMu.Lock()
	state := dialogThemeObserverStates[hwnd]
	if state == nil || !dialogThemeReconcileRequired(
		state.applying,
		appearanceChanged,
		state.highContrast != info.HighContrast,
	) {
		dialogThemeObserverMu.Unlock()
		return
	}
	state.applying = true
	previousHighContrast := state.highContrast
	dark, animate := state.dark, state.animate
	changed := state.accessibilityChanged
	dialogThemeObserverMu.Unlock()

	applied := false
	defer func() {
		dialogThemeObserverMu.Lock()
		if current := dialogThemeObserverStates[hwnd]; current == state {
			if applied {
				current.highContrast = info.HighContrast
			} else {
				// Applying a theme calls rememberDialogThemeObserver before
				// its native work. Roll that optimistic value back so the
				// next broadcast can retry a failed or panicking transition.
				current.highContrast = previousHighContrast
			}
			current.applying = false
		}
		dialogThemeObserverMu.Unlock()
	}()
	if changed != nil {
		applied = changed()
	} else {
		result, _ := ApplyDialogThemeWithMotionResult(hwnd, dark, animate)
		applied = result.ControlsThemed &&
			result.HighContrast == info.HighContrast
	}
}

func dialogThemeReconcileRequired(
	applying, appearanceChanged, highContrastChanged bool,
) bool {
	return !applying && (appearanceChanged || highContrastChanged)
}

// ApplyDialogMotion updates only the custom control transition policy.
// It avoids rebuilding brushes and reapplying DWM/UxTheme when the user
// toggles motion while a settings window is already open.
func ApplyDialogMotion(hwnd uintptr, animate bool) error {
	if hwnd == 0 || !isWindow(hwnd) {
		return ErrInvalidWindow
	}
	callback := syscall.NewCallback(func(child, _ uintptr) uintptr {
		className := classNameForDialogControl(child)
		switch {
		case strings.EqualFold(
			className,
			"SysTabControl32",
		):
			setDialogTabMotion(child, animate)
		case strings.EqualFold(
			className,
			"Button",
		):
			if isDialogPushButtonStyle(dialogWindowStyle(child)) {
				setDialogButtonMotion(child, animate)
			}
		case isDialogFieldClass(className):
			if !isDialogComboEditChild(child) {
				setDialogFieldMotion(child, animate)
			}
		case isDialogComboClass(className):
			setDialogComboMotion(child, animate)
		}
		return 1
	})
	result, _, callErr := dialogThemeEnumChildWindows.Call(hwnd, callback, 0)
	runtime.KeepAlive(callback)
	return dialogEnumerationError(result, callErr)
}

func clearDialogBackdrop(hwnd uintptr) error {
	var failures []error
	if err := setDWMInt32(
		hwnd,
		dwmwaNCRenderingPolicy,
		dwmNCRenderingEnabled,
	); err != nil {
		failures = append(failures, fmt.Errorf("enable dialog non-client rendering: %w", err))
	}
	if err := extendFrame(hwnd, false); err != nil {
		failures = append(failures, fmt.Errorf("clear dialog client frame: %w", err))
	}
	if err := setDWMInt32(
		hwnd,
		dwmwaSystemBackdropType,
		dwmSystemBackdropNone,
	); err != nil {
		failures = append(failures, fmt.Errorf("clear dialog client backdrop: %w", err))
	}
	return errors.Join(failures...)
}

func dialogEnumerationError(result uintptr, callErr error) error {
	// LazyProc.Call exposes the thread's last-error value even when the API
	// succeeded. Theme calls made inside the EnumChildWindows callback may
	// leave a stale non-zero value, so it is meaningful only when enumeration
	// itself returned FALSE.
	if result != 0 || callErr == nil || errors.Is(callErr, windows.ERROR_SUCCESS) {
		return nil
	}
	return callErr
}

func themeDialogWindow(hwnd uintptr, dark, highContrast, animate bool) {
	allowDialogDarkMode(hwnd, dark)
	className := classNameForDialogControl(hwnd)
	themeName := dialogControlTheme(className, dark)
	theme, err := windows.UTF16PtrFromString(themeName)
	themeChangeDelivered := false
	if err == nil && dialogThemeSetWindowTheme.Find() == nil {
		result, _, _ := dialogThemeSetWindowTheme.Call(
			hwnd,
			uintptr(unsafe.Pointer(theme)),
			0,
		)
		// Successful SetWindowTheme synchronously delivers WM_THEMECHANGED.
		// Sending it again below made every subclassed field/dropdown paint
		// twice, with a third paint triggered while its custom state was
		// updated.
		themeChangeDelivered = dialogThemeHRESULTSucceeded(result)
		runtime.KeepAlive(theme)
	}
	if strings.EqualFold(className, "SysTabControl32") {
		customChrome := dialogCustomControlChromeEnabled(highContrast)
		setDialogTabTheme(
			hwnd,
			dark,
			customChrome,
			animate,
			customChrome,
		)
	}
	if strings.EqualFold(className, "Button") {
		style := dialogWindowStyle(hwnd)
		if isDialogPushButtonStyle(style) {
			setDialogButtonTheme(
				hwnd,
				dark,
				animate,
				dialogCustomControlChromeEnabled(highContrast),
			)
		}
	}
	if isDialogFieldClass(className) && !isDialogComboEditChild(hwnd) {
		setDialogFieldTheme(
			hwnd,
			dark,
			animate,
			dialogCustomControlChromeEnabled(highContrast),
		)
	}
	if isDialogComboClass(className) {
		setDialogComboTheme(
			hwnd,
			dark,
			animate,
			dialogCustomControlChromeEnabled(highContrast),
		)
	}
	if !themeChangeDelivered {
		_, _, _ = dialogThemeSendMessage.Call(
			hwnd,
			dialogWMThemeChanged,
			0,
			0,
		)
	}
}

func dialogThemeHRESULTSucceeded(result uintptr) bool {
	return int32(uint32(result)) >= 0
}

func allowDialogDarkMode(hwnd uintptr, dark bool) {
	version := CurrentVersion()
	if version.Major < 10 || version.Build < 17763 {
		return
	}
	if err := dialogThemeUXTheme.Load(); err != nil {
		return
	}
	address, _, _ := dialogThemeGetProcAddress.Call(
		dialogThemeUXTheme.Handle(),
		dialogAllowDarkModeForWindowOrdinal,
	)
	if address == 0 {
		return
	}
	value := uintptr(0)
	if dark {
		value = 1
	}
	_, _, _ = syscall.SyscallN(address, hwnd, value)
}

func classNameForDialogControl(hwnd uintptr) string {
	var buffer [128]uint16
	count, _, _ := dialogThemeGetClassName.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if count == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer[:count])
}

func dialogControlTheme(className string, dark bool) string {
	if !dark {
		return "Explorer"
	}
	switch strings.ToLower(strings.TrimSpace(className)) {
	case "edit", "richedit", "richedit20w", "richedit50w",
		"combobox", "comboboxex32":
		return "DarkMode_CFD"
	default:
		return "DarkMode_Explorer"
	}
}

func dialogCustomControlChromeEnabled(highContrast bool) bool {
	return !highContrast
}
