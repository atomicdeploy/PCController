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

type DialogIcon string

const (
	DialogIconNone        DialogIcon = ""
	DialogIconInformation DialogIcon = "information"
	DialogIconWarning     DialogIcon = "warning"
	DialogIconError       DialogIcon = "error"
	DialogIconShield      DialogIcon = "shield"
	DialogIconQuestion    DialogIcon = "question"
)

type DialogButtons uint32

const (
	DialogButtonOK     DialogButtons = 0x0001
	DialogButtonYes    DialogButtons = 0x0002
	DialogButtonNo     DialogButtons = 0x0004
	DialogButtonCancel DialogButtons = 0x0008
	DialogButtonRetry  DialogButtons = 0x0010
	DialogButtonClose  DialogButtons = 0x0020
)

type DialogButton int32

const (
	DialogResultNone     DialogButton = 0
	DialogResultOK       DialogButton = 1
	DialogResultCancel   DialogButton = 2
	DialogResultAbort    DialogButton = 3
	DialogResultRetry    DialogButton = 4
	DialogResultIgnore   DialogButton = 5
	DialogResultYes      DialogButton = 6
	DialogResultNo       DialogButton = 7
	DialogResultClose    DialogButton = 8
	DialogResultHelp     DialogButton = 9
	DialogResultTryAgain DialogButton = 10
	DialogResultContinue DialogButton = 11
)

// TaskDialogOptions describes a TaskDialogIndirect using only common buttons,
// which keeps the wrapper compact and translation-friendly.
type TaskDialogOptions struct {
	Parent              uintptr
	Title               string
	MainInstruction     string
	Content             string
	Details             string
	Icon                DialogIcon
	Buttons             DialogButtons
	DefaultButton       DialogButton
	AllowCancel         bool
	VerificationText    string
	VerificationChecked bool
	RightToLeft         bool
}

type TaskDialogResult struct {
	Button              DialogButton
	VerificationChecked bool
	UsedFallback        bool
}

const (
	tdfAllowDialogCancellation  = 0x0008
	tdfExpandedByDefault        = 0x0080
	tdfVerificationFlagChecked  = 0x0100
	tdfPositionRelativeToWindow = 0x1000
	tdfRTLLayout                = 0x2000
	tdfSizeToContent            = 0x01000000

	tdnCreated           = 0
	tdnDialogConstructed = 7

	taskDialogThemeDataLight = 1
	taskDialogThemeDataDark  = 2

	taskDialogPreferredDefault    = 0
	taskDialogPreferredAllowDark  = 1
	taskDialogPreferredForceDark  = 2
	taskDialogPreferredForceLight = 3
	taskDialogPreferredMax        = 4

	taskDialogRefreshImmersiveColorPolicyOrdinal = 104
	taskDialogSetPreferredAppModeOrdinal         = 135
	taskDialogSetPreferredAppModeFirstBuild      = 18362

	mbOK              = 0x00000000
	mbOKCancel        = 0x00000001
	mbYesNoCancel     = 0x00000003
	mbYesNo           = 0x00000004
	mbRetryCancel     = 0x00000005
	mbIconError       = 0x00000010
	mbIconQuestion    = 0x00000020
	mbIconWarning     = 0x00000030
	mbIconInformation = 0x00000040
	mbDefaultButton2  = 0x00000100
	mbDefaultButton3  = 0x00000200
	mbTaskModal       = 0x00002000
	mbRight           = 0x00080000
	mbRTLReading      = 0x00100000
)

var (
	// TaskDialogIndirect exists in Common Controls v6. NewLazySystemDLL uses
	// LOAD_LIBRARY_SEARCH_SYSTEM32, which bypasses the manifest's side-by-side
	// activation context and pins the legacy DLL that lacks this export.
	// A normal load is intentional for this Windows KnownDLL/SxS assembly.
	taskDialogComctl32     = windows.NewLazyDLL("comctl32.dll")
	taskDialogIndirectProc = taskDialogComctl32.NewProc("TaskDialogIndirect")
	taskDialogDwmGetAttr   = backdropDWMAPI.NewProc("DwmGetWindowAttribute")
	taskDialogUser32       = windows.NewLazySystemDLL("user32.dll")
	taskDialogMessageBoxW  = taskDialogUser32.NewProc("MessageBoxW")

	// TaskDialogIndirect is synchronous, but the callback can run several
	// times while the native modal is alive. Keeping a process-lifetime
	// callback pointer avoids placing a Go pointer in TASKDIALOGCONFIG and
	// makes concurrent dialogs independent through CallbackData.
	taskDialogThemeCallbackPointer = windows.NewCallback(
		taskDialogThemeCallback,
	)

	// SetPreferredAppMode is process-wide. Keep its requested mode stable for
	// the complete lifetime of each native modal and prevent two UI threads
	// from selecting conflicting ComCtl32 palettes.
	taskDialogPreferredModeMu sync.Mutex
)

type taskDialogThemeSelection struct {
	Dark         bool
	HighContrast bool
}

// taskDialogConfig matches TASKDIALOGCONFIG on both 32-bit and 64-bit
// Windows. Pointer-valued fields use uintptr so the object passed to the
// syscall does not itself contain Go pointers.
type taskDialogConfig struct {
	Size                 uint32
	Parent               uintptr
	Instance             uintptr
	Flags                uint32
	CommonButtons        uint32
	WindowTitle          uintptr
	MainIcon             uintptr
	MainInstruction      uintptr
	Content              uintptr
	ButtonCount          uint32
	Buttons              uintptr
	DefaultButton        int32
	RadioButtonCount     uint32
	RadioButtons         uintptr
	DefaultRadioButton   int32
	VerificationText     uintptr
	ExpandedInformation  uintptr
	ExpandedControlText  uintptr
	CollapsedControlText uintptr
	FooterIcon           uintptr
	Footer               uintptr
	Callback             uintptr
	CallbackData         uintptr
	Width                uint32
}

type taskDialogStrings struct {
	values [][]uint16
}

func (s *taskDialogStrings) pointer(value string) (uintptr, error) {
	if value == "" {
		return 0, nil
	}
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		return 0, err
	}
	s.values = append(s.values, encoded)
	return uintptr(unsafe.Pointer(&encoded[0])), nil
}

// TaskDialog displays a modal native task dialog on the caller's UI thread.
// If TaskDialogIndirect is unavailable or rejects the configuration, it
// gracefully uses MessageBoxW and marks UsedFallback in the result.
func TaskDialog(options TaskDialogOptions) (TaskDialogResult, error) {
	if options.Buttons == 0 {
		options.Buttons = DialogButtonOK
	}
	if options.Title == "" {
		options.Title = "RayanLamp"
	}
	if !options.RightToLeft {
		options.RightToLeft = taskDialogContainsRTL(
			options.Title,
			options.MainInstruction,
			options.Content,
			options.Details,
			options.VerificationText,
		)
	}
	if err := validateDialogIcon(options.Icon); err != nil {
		return TaskDialogResult{}, err
	}

	taskDialogPreferredModeMu.Lock()
	defer taskDialogPreferredModeMu.Unlock()

	theme := resolveTaskDialogTheme(options.Parent)
	restorePreferredMode := beginTaskDialogPreferredAppMode(theme)
	defer restorePreferredMode()

	result, err := taskDialogIndirect(options, theme.Dark)
	if err == nil {
		return result, nil
	}
	fallback, fallbackErr := taskDialogMessageBox(options)
	if fallbackErr != nil {
		return TaskDialogResult{}, errors.Join(err, fallbackErr)
	}
	fallback.UsedFallback = true
	return fallback, nil
}

// Confirm displays a Yes/No confirmation and reports true only for Yes.
func Confirm(parent uintptr, title, mainInstruction, content string) (bool, error) {
	result, err := TaskDialog(TaskDialogOptions{
		Parent:          parent,
		Title:           title,
		MainInstruction: mainInstruction,
		Content:         content,
		Icon:            DialogIconQuestion,
		Buttons:         DialogButtonYes | DialogButtonNo,
		DefaultButton:   DialogResultNo,
		AllowCancel:     true,
	})
	if err != nil {
		return false, err
	}
	return result.Button == DialogResultYes, nil
}

func taskDialogIndirect(
	options TaskDialogOptions,
	dark bool,
) (TaskDialogResult, error) {
	if err := taskDialogIndirectProc.Find(); err != nil {
		return TaskDialogResult{}, fmt.Errorf("resolve TaskDialogIndirect: %w", err)
	}

	var strings taskDialogStrings
	title, err := strings.pointer(options.Title)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode task dialog title: %w", err)
	}
	mainInstruction, err := strings.pointer(options.MainInstruction)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode task dialog instruction: %w", err)
	}
	content, err := strings.pointer(options.Content)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode task dialog content: %w", err)
	}
	details, err := strings.pointer(options.Details)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode task dialog details: %w", err)
	}
	verification, err := strings.pointer(options.VerificationText)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode task dialog verification text: %w", err)
	}

	flags := uint32(tdfSizeToContent)
	if options.Parent != 0 {
		flags |= tdfPositionRelativeToWindow
	}
	if options.AllowCancel {
		flags |= tdfAllowDialogCancellation
	}
	if options.VerificationChecked {
		flags |= tdfVerificationFlagChecked
	}
	if options.Details != "" {
		// Details remain collapsed by default. The explicit zero here documents
		// that callers can safely pass sensitive diagnostics without flashing
		// them open before the user chooses to expand.
		flags &^= tdfExpandedByDefault
	}
	if options.RightToLeft {
		flags |= tdfRTLLayout
	}

	config := taskDialogConfig{
		Size:                uint32(unsafe.Sizeof(taskDialogConfig{})),
		Parent:              options.Parent,
		Flags:               flags,
		CommonButtons:       uint32(options.Buttons),
		WindowTitle:         title,
		MainIcon:            taskDialogIconResource(options.Icon),
		MainInstruction:     mainInstruction,
		Content:             content,
		DefaultButton:       int32(options.DefaultButton),
		VerificationText:    verification,
		ExpandedInformation: details,
	}
	configureTaskDialogTheme(
		&config,
		dark,
	)
	var button int32
	var radio int32
	var verified int32
	hresult, _, _ := taskDialogIndirectProc.Call(
		uintptr(unsafe.Pointer(&config)),
		uintptr(unsafe.Pointer(&button)),
		uintptr(unsafe.Pointer(&radio)),
		uintptr(unsafe.Pointer(&verified)),
	)
	runtime.KeepAlive(&strings)
	runtime.KeepAlive(&config)
	if err := hresultError("TaskDialogIndirect", hresult); err != nil {
		return TaskDialogResult{}, err
	}
	return TaskDialogResult{
		Button:              DialogButton(button),
		VerificationChecked: verified != 0,
	}, nil
}

func configureTaskDialogTheme(config *taskDialogConfig, dark bool) {
	if config == nil {
		return
	}
	config.Callback = taskDialogThemeCallbackPointer
	config.CallbackData = taskDialogThemeDataLight
	if dark {
		config.CallbackData = taskDialogThemeDataDark
	}
}

func taskDialogThemeCallback(
	hwnd, notification, _, _, callbackData uintptr,
) (result uintptr) {
	// A panic must never unwind through ComCtl32's native callback frame.
	// The callback is best-effort visual polish, so S_OK remains the correct
	// result even when a platform API is unavailable.
	defer func() {
		if recover() != nil {
			result = 0
		}
	}()
	if !isTaskDialogThemeNotification(notification) {
		return 0
	}
	_ = ApplyDialogTheme(
		hwnd,
		callbackData == taskDialogThemeDataDark,
	)
	return 0
}

func isTaskDialogThemeNotification(notification uintptr) bool {
	return notification == tdnCreated ||
		notification == tdnDialogConstructed
}

func resolveTaskDialogTheme(parent uintptr) taskDialogThemeSelection {
	selection := taskDialogThemeSelection{}
	// High contrast owns the palette. ApplyDialogTheme repeats this check at
	// callback time so a setting change while the dialog is being created is
	// also respected.
	if highContrast, err := HighContrast(); err == nil && highContrast {
		selection.HighContrast = true
		return selection
	}
	if dark, ok := taskDialogParentDarkMode(parent); ok {
		selection.Dark = dark
		return selection
	}
	appearance, _ := ResolveAppearance(AppearanceThemeSystem)
	selection.Dark = appearance.Dark
	return selection
}

func preferredAppModeForTaskDialog(
	selection taskDialogThemeSelection,
) uintptr {
	if selection.HighContrast {
		return taskDialogPreferredDefault
	}
	if selection.Dark {
		// TaskDialog chooses several background resources before its HWND
		// callback runs. ForceDark is required here even on a dark system;
		// AllowDark leaves that early ComCtl32 resource selection light on
		// current Windows 11 builds.
		return taskDialogPreferredForceDark
	}
	return taskDialogPreferredForceLight
}

// beginTaskDialogPreferredAppMode sets UxTheme's process-wide palette policy
// before ComCtl32 selects TaskDialog resources. SetPreferredAppMode is not a
// documented API, so every step is best-effort and the per-window callback
// remains the supported DWM/SetWindowTheme fallback.
func beginTaskDialogPreferredAppMode(
	selection taskDialogThemeSelection,
) func() {
	version := CurrentVersion()
	if version.Major < 10 || version.Build < 17763 {
		return func() {}
	}
	if err := dialogThemeUXTheme.Load(); err != nil {
		return func() {}
	}
	address := taskDialogUXThemeOrdinal(
		taskDialogSetPreferredAppModeOrdinal,
	)
	if address == 0 {
		return func() {}
	}

	requested := preferredAppModeForTaskDialog(selection)
	if version.Build < taskDialogSetPreferredAppModeFirstBuild {
		// Windows 10 1809 exposed AllowDarkModeForApp at the same ordinal.
		// Its BOOL ABI is compatible with AllowDark (1) and Default (0), but
		// it has no restorable PreferredAppMode return value.
		allowed := uintptr(0)
		if requested == taskDialogPreferredAllowDark ||
			requested == taskDialogPreferredForceDark {
			allowed = 1
		}
		_, _, _ = syscall.SyscallN(address, allowed)
		refreshTaskDialogImmersiveColorPolicy()
		return func() {}
	}

	previous, _, _ := syscall.SyscallN(address, requested)
	refreshTaskDialogImmersiveColorPolicy()
	if previous >= taskDialogPreferredMax {
		return func() {}
	}
	return func() {
		_, _, _ = syscall.SyscallN(address, previous)
		refreshTaskDialogImmersiveColorPolicy()
	}
}

func taskDialogUXThemeOrdinal(ordinal uintptr) uintptr {
	address, _, _ := dialogThemeGetProcAddress.Call(
		dialogThemeUXTheme.Handle(),
		ordinal,
	)
	return address
}

func refreshTaskDialogImmersiveColorPolicy() {
	address := taskDialogUXThemeOrdinal(
		taskDialogRefreshImmersiveColorPolicyOrdinal,
	)
	if address != 0 {
		_, _, _ = syscall.SyscallN(address)
	}
}

func taskDialogParentDarkMode(parent uintptr) (dark, known bool) {
	if parent == 0 || !isWindow(parent) {
		return false, false
	}
	version := CurrentVersion()
	if version.Major < 10 || version.Build < 17763 {
		return false, false
	}

	value, err := getTaskDialogDWMInt32(
		parent,
		dwmwaUseImmersiveDarkMode,
	)
	if err == nil {
		return value != 0, true
	}
	// Builds before Windows 10 20H1 used attribute 19 for this setting.
	if version.Build < 19041 {
		value, err = getTaskDialogDWMInt32(
			parent,
			dwmwaUseImmersiveDarkOld,
		)
		if err == nil {
			return value != 0, true
		}
	}
	return false, false
}

func getTaskDialogDWMInt32(hwnd uintptr, attribute uint32) (int32, error) {
	if err := taskDialogDwmGetAttr.Find(); err != nil {
		return 0, err
	}
	var value int32
	result, _, _ := taskDialogDwmGetAttr.Call(
		hwnd,
		uintptr(attribute),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
	runtime.KeepAlive(&value)
	if err := hresultError("DwmGetWindowAttribute", result); err != nil {
		return 0, err
	}
	return value, nil
}

func taskDialogMessageBox(options TaskDialogOptions) (TaskDialogResult, error) {
	if err := taskDialogMessageBoxW.Find(); err != nil {
		return TaskDialogResult{}, fmt.Errorf("resolve MessageBoxW: %w", err)
	}
	title, err := windows.UTF16PtrFromString(options.Title)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode MessageBox title: %w", err)
	}
	message := fallbackDialogText(options)
	messagePtr, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return TaskDialogResult{}, fmt.Errorf("encode MessageBox content: %w", err)
	}
	style := fallbackButtonStyle(options.Buttons) | fallbackIconStyle(options.Icon)
	if options.RightToLeft {
		style |= mbRight | mbRTLReading
	}
	if options.Parent == 0 {
		style |= mbTaskModal
	}
	style |= fallbackDefaultButtonStyle(options)

	button, _, callErr := taskDialogMessageBoxW.Call(
		options.Parent,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(title)),
		uintptr(style),
	)
	runtime.KeepAlive(messagePtr)
	runtime.KeepAlive(title)
	if button == 0 {
		return TaskDialogResult{}, boolCallError("MessageBoxW", callErr)
	}
	return TaskDialogResult{
		Button:              normalizeFallbackButton(DialogButton(button), options.Buttons),
		VerificationChecked: options.VerificationChecked,
	}, nil
}

func taskDialogContainsRTL(values ...string) bool {
	for _, value := range values {
		for _, character := range value {
			switch {
			case character >= 0x0590 && character <= 0x08ff,
				character >= 0xfb1d && character <= 0xfdff,
				character >= 0xfe70 && character <= 0xfefc:
				return true
			}
		}
	}
	return false
}

func validateDialogIcon(icon DialogIcon) error {
	switch icon {
	case DialogIconNone, DialogIconInformation, DialogIconWarning,
		DialogIconError, DialogIconShield, DialogIconQuestion:
		return nil
	default:
		return fmt.Errorf("winfx: unknown task dialog icon %q", icon)
	}
}

func taskDialogIconResource(icon DialogIcon) uintptr {
	// TD_*_ICON values are MAKEINTRESOURCEW(-1 ... -4), which preserves the
	// low WORD rather than sign-extending it on 64-bit Windows.
	switch icon {
	case DialogIconWarning:
		return uintptr(uint16(0xFFFF))
	case DialogIconError:
		return uintptr(uint16(0xFFFE))
	case DialogIconInformation, DialogIconQuestion:
		return uintptr(uint16(0xFFFD))
	case DialogIconShield:
		return uintptr(uint16(0xFFFC))
	default:
		return 0
	}
}

func fallbackDialogText(options TaskDialogOptions) string {
	parts := make([]string, 0, 3)
	if value := strings.TrimSpace(options.MainInstruction); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(options.Content); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(options.Details); value != "" {
		parts = append(parts, "Details:\r\n"+value)
	}
	if len(parts) == 0 {
		return options.Title
	}
	return strings.Join(parts, "\r\n\r\n")
}

func fallbackButtonStyle(buttons DialogButtons) uint32 {
	switch {
	case buttons&DialogButtonYes != 0 &&
		buttons&DialogButtonNo != 0 &&
		buttons&DialogButtonCancel != 0:
		return mbYesNoCancel
	case buttons&DialogButtonYes != 0 && buttons&DialogButtonNo != 0:
		return mbYesNo
	case buttons&DialogButtonRetry != 0 && buttons&DialogButtonCancel != 0:
		return mbRetryCancel
	case buttons&DialogButtonOK != 0 && buttons&DialogButtonCancel != 0:
		return mbOKCancel
	default:
		return mbOK
	}
}

func fallbackIconStyle(icon DialogIcon) uint32 {
	switch icon {
	case DialogIconError:
		return mbIconError
	case DialogIconWarning:
		return mbIconWarning
	case DialogIconQuestion:
		return mbIconQuestion
	case DialogIconInformation, DialogIconShield:
		return mbIconInformation
	default:
		return 0
	}
}

func fallbackDefaultButtonStyle(options TaskDialogOptions) uint32 {
	switch fallbackButtonStyle(options.Buttons) {
	case mbYesNo:
		if options.DefaultButton == DialogResultNo {
			return mbDefaultButton2
		}
	case mbYesNoCancel:
		switch options.DefaultButton {
		case DialogResultNo:
			return mbDefaultButton2
		case DialogResultCancel:
			return mbDefaultButton3
		}
	case mbOKCancel, mbRetryCancel:
		if options.DefaultButton == DialogResultCancel {
			return mbDefaultButton2
		}
	}
	return 0
}

func normalizeFallbackButton(button DialogButton, requested DialogButtons) DialogButton {
	// MessageBox cannot represent Close-only or other unusual common-button
	// subsets. Preserve the caller's semantic button when its OK fallback is
	// the sole choice.
	if button == DialogResultOK {
		switch requested {
		case DialogButtonClose:
			return DialogResultClose
		case DialogButtonRetry:
			return DialogResultRetry
		}
	}
	return button
}
