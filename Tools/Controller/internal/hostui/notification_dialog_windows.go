//go:build windows

package hostui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	taskDialogEnableHyperlinks  = uint32(0x0001)
	taskDialogCallbackTimer     = uint32(0x0800)
	taskDialogCloseButton       = uint32(0x0020)
	taskDialogNotificationTimer = uint32(4)
	taskDialogClickButton       = uintptr(0x0400 + 102)
	taskDialogCustomButtonBase  = int32(1000)
	taskDialogCloseButtonID     = uintptr(8)
	maxFallbackDisplayMS        = uintptr(8000)
)

var (
	comctl32DLL            = windows.NewLazySystemDLL("comctl32.dll")
	user32NotificationDLL  = windows.NewLazySystemDLL("user32.dll")
	shell32NotificationDLL = windows.NewLazySystemDLL("shell32.dll")
	procTaskDialogIndirect = comctl32DLL.NewProc("TaskDialogIndirect")
	procSendMessageW       = user32NotificationDLL.NewProc("SendMessageW")
	procShellExecuteW      = shell32NotificationDLL.NewProc("ShellExecuteW")
)

type taskDialogButton struct {
	ID   int32
	Text *uint16
}

type taskDialogConfig struct {
	Size                 uint32
	Parent               uintptr
	Instance             uintptr
	Flags                uint32
	CommonButtons        uint32
	WindowTitle          *uint16
	MainIcon             uintptr
	MainInstruction      *uint16
	Content              *uint16
	ButtonCount          uint32
	Buttons              *taskDialogButton
	DefaultButton        int32
	RadioButtonCount     uint32
	RadioButtons         *taskDialogButton
	DefaultRadioButton   int32
	VerificationText     *uint16
	ExpandedInformation  *uint16
	ExpandedControlText  *uint16
	CollapsedControlText *uint16
	FooterIcon           uintptr
	Footer               *uint16
	Callback             uintptr
	CallbackData         uintptr
	Width                uint32
}

// showWindowsNotificationDialog is a bounded native fallback for systems
// where unpackaged WinRT notifications are unavailable or disabled. It keeps
// action buttons functional by opening only pre-validated protocol/HTTP URIs,
// and closes automatically after eight seconds so the host bridge cannot be
// held indefinitely by a modal system surface.
func showWindowsNotificationDialog(
	ctx context.Context,
	notification Notification,
	nativeToastError error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireWindowsProcedures(
		"native notification fallback", procTaskDialogIndirect, procSendMessageW,
		procShellExecuteW,
	); err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString(notification.Title)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(notification.Body)
	if body == "" {
		body = notification.Title
	}
	content, err := windows.UTF16PtrFromString(body)
	if err != nil {
		return err
	}

	actions := append([]NotificationAction(nil), notification.Actions...)
	if len(actions) == 0 && notification.LaunchURI != "" {
		actions = append(actions, NotificationAction{Label: "Open", URI: notification.LaunchURI})
	}
	buttons := make([]taskDialogButton, 0, len(actions))
	buttonLabels := make([]*uint16, 0, len(actions))
	for index, action := range actions {
		if err := validateActionURI(action.URI); err != nil {
			return err
		}
		label, err := windows.UTF16PtrFromString(action.Label)
		if err != nil {
			return err
		}
		buttonLabels = append(buttonLabels, label)
		buttons = append(buttons, taskDialogButton{ID: taskDialogCustomButtonBase + int32(index), Text: label})
	}

	callback := syscall.NewCallback(func(window uintptr, message uint32, parameter, _ uintptr, _ uintptr) uintptr {
		if message == taskDialogNotificationTimer && (parameter >= maxFallbackDisplayMS || ctx.Err() != nil) {
			procSendMessageW.Call(window, taskDialogClickButton, taskDialogCloseButtonID, 0)
		}
		return 0
	})
	config := taskDialogConfig{
		Flags:           taskDialogEnableHyperlinks | taskDialogCallbackTimer,
		CommonButtons:   taskDialogCloseButton,
		WindowTitle:     title,
		MainInstruction: title,
		Content:         content,
		ButtonCount:     uint32(len(buttons)),
		Callback:        callback,
		Width:           360,
	}
	if len(buttons) != 0 {
		config.Buttons = &buttons[0]
		config.DefaultButton = buttons[0].ID
	}
	config.MainIcon = taskDialogIcon(notification.Severity)
	config.Size = uint32(unsafe.Sizeof(config))
	var pressed int32
	status, _, callErr := procTaskDialogIndirect.Call(
		uintptr(unsafe.Pointer(&config)), uintptr(unsafe.Pointer(&pressed)), 0, 0,
	)
	runtime.KeepAlive(buttonLabels)
	runtime.KeepAlive(buttons)
	runtime.KeepAlive(title)
	runtime.KeepAlive(content)
	if err := hresult(status).error("show native notification fallback"); err != nil {
		return fmt.Errorf("%w: %v", err, callErr)
	}
	selected := int(pressed - taskDialogCustomButtonBase)
	if selected >= 0 && selected < len(actions) {
		if err := openWindowsURI(actions[selected].URI); err != nil {
			return fmt.Errorf("open notification action: %w", err)
		}
	}
	runtime.KeepAlive(nativeToastError)
	return nil
}

func taskDialogIcon(severity string) uintptr {
	severity = strings.ToLower(strings.TrimSpace(severity))
	switch {
	case severity == "error" || strings.Contains(severity, "hot"):
		return 0xFFFE // TD_ERROR_ICON
	case strings.Contains(severity, "warning") || strings.Contains(severity, "door"):
		return 0xFFFF // TD_WARNING_ICON
	default:
		return 0xFFFD // TD_INFORMATION_ICON
	}
}

func openWindowsURI(value string) error {
	if err := validateActionURI(value); err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	result, _, callErr := procShellExecuteW.Call(
		0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1,
	)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed with code %d: %v", result, callErr)
	}
	return nil
}
