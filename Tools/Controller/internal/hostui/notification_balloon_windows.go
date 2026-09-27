//go:build windows

package hostui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	balloonWMClose         = 0x0010
	balloonWMDestroy       = 0x0002
	balloonWMUser          = 0x0400
	balloonWMApp           = 0x8000
	balloonCallbackMessage = balloonWMApp + 0x73
	balloonClicked         = balloonWMUser + 5
	balloonIconID          = 0x50434354
	balloonNIMAdd          = 0
	balloonNIMModify       = 1
	balloonNIMDelete       = 2
	balloonNIFMessage      = 0x00000001
	balloonNIFIcon         = 0x00000002
	balloonNIFTip          = 0x00000004
	balloonNIFInfo         = 0x00000010
	balloonNIIFUser        = 0x00000004
	balloonImageIcon       = 1
	balloonLRDefaultSize   = 0x00000040
	balloonLRShared        = 0x00008000
	balloonDisplayTime     = 8 * time.Second
)

type balloonWindowClass struct {
	Size, Style                        uint32
	WindowProc                         uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}

type balloonMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          struct{ X, Y int32 }
	Private        uint32
}

type balloonNotifyIconData struct {
	Size                       uint32
	Window                     uintptr
	ID, Flags, CallbackMessage uint32
	Icon                       uintptr
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Version                    uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GUIDItem                   windows.GUID
	BalloonIcon                uintptr
}

var (
	balloonUser32           = windows.NewLazySystemDLL("user32.dll")
	balloonShell32          = windows.NewLazySystemDLL("shell32.dll")
	balloonKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	balloonRegisterClass    = balloonUser32.NewProc("RegisterClassExW")
	balloonCreateWindow     = balloonUser32.NewProc("CreateWindowExW")
	balloonDefWindowProc    = balloonUser32.NewProc("DefWindowProcW")
	balloonDestroyWindow    = balloonUser32.NewProc("DestroyWindow")
	balloonGetMessage       = balloonUser32.NewProc("GetMessageW")
	balloonTranslateMessage = balloonUser32.NewProc("TranslateMessage")
	balloonDispatchMessage  = balloonUser32.NewProc("DispatchMessageW")
	balloonPostMessage      = balloonUser32.NewProc("PostMessageW")
	balloonPostQuitMessage  = balloonUser32.NewProc("PostQuitMessage")
	balloonLoadImage        = balloonUser32.NewProc("LoadImageW")
	balloonShellNotifyIcon  = balloonShell32.NewProc("Shell_NotifyIconW")
	balloonGetModuleHandle  = balloonKernel32.NewProc("GetModuleHandleW")
	balloonClassOnce        sync.Once
	balloonClassErr         error
	balloonClassName        = windows.StringToUTF16Ptr("PCController.NotificationBalloon")
	balloonStates           sync.Map
	balloonWindowProcedure  = syscall.NewCallback(balloonWndProc)
)

type balloonState struct{ launchURI string }

// showWindowsBalloon provides the notification-area balloon supported by
// legacy Windows shells. It is selected only after the branded WinRT toast
// fails and keeps a product-icon tray item alive for the bounded display.
func showWindowsBalloon(ctx context.Context, notification Notification, _ error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireWindowsProcedures("legacy notification balloon",
		balloonRegisterClass, balloonCreateWindow, balloonDefWindowProc,
		balloonDestroyWindow, balloonGetMessage, balloonTranslateMessage,
		balloonDispatchMessage, balloonPostMessage, balloonPostQuitMessage,
		balloonLoadImage, balloonShellNotifyIcon,
		balloonGetModuleHandle,
	); err != nil {
		return err
	}
	if notification.LaunchURI != "" {
		if err := validateActionURI(notification.LaunchURI); err != nil {
			return err
		}
	}
	balloonClassOnce.Do(registerBalloonClass)
	if balloonClassErr != nil {
		return balloonClassErr
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	instance, _, _ := balloonGetModuleHandle.Call(0)
	hwndMessage := ^uintptr(2)
	hwnd, _, callErr := balloonCreateWindow.Call(0, uintptr(unsafe.Pointer(balloonClassName)), 0,
		0, 0, 0, 0, 0, hwndMessage, 0, instance, 0)
	if hwnd == 0 {
		return balloonWindowsCallError("create legacy notification window", callErr)
	}
	balloonStates.Store(hwnd, balloonState{launchURI: notification.LaunchURI})
	defer balloonStates.Delete(hwnd)

	icon, err := loadBalloonIcon(instance)
	if err != nil {
		_, _, _ = balloonDestroyWindow.Call(hwnd)
		drainBalloonQuitMessage()
		return err
	}
	data := balloonNotifyIconData{Size: uint32(unsafe.Sizeof(balloonNotifyIconData{})), Window: hwnd,
		ID: balloonIconID, Flags: balloonNIFMessage | balloonNIFIcon | balloonNIFTip,
		CallbackMessage: balloonCallbackMessage, Icon: icon}
	copyBalloonUTF16(data.Tip[:], notification.Title)
	if result, _, err := balloonShellNotifyIcon.Call(balloonNIMAdd, uintptr(unsafe.Pointer(&data))); result == 0 {
		_, _, _ = balloonDestroyWindow.Call(hwnd)
		drainBalloonQuitMessage()
		return balloonWindowsCallError("add legacy notification icon", err)
	}
	defer func() {
		remove := data
		remove.Flags = 0
		_, _, _ = balloonShellNotifyIcon.Call(balloonNIMDelete, uintptr(unsafe.Pointer(&remove)))
	}()

	data.Flags = balloonNIFInfo
	data.InfoFlags = balloonNIIFUser
	data.BalloonIcon = icon
	copyBalloonUTF16(data.InfoTitle[:], notification.Title)
	body := strings.TrimSpace(notification.Body)
	if body == "" {
		body = notification.Title
	}
	copyBalloonUTF16(data.Info[:], body)
	if result, _, err := balloonShellNotifyIcon.Call(balloonNIMModify, uintptr(unsafe.Pointer(&data))); result == 0 {
		_, _, _ = balloonDestroyWindow.Call(hwnd)
		drainBalloonQuitMessage()
		return balloonWindowsCallError("show legacy notification balloon", err)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(balloonDisplayTime):
		case <-done:
			return
		}
		_, _, _ = balloonPostMessage.Call(hwnd, balloonWMClose, 0, 0)
	}()
	var message balloonMessage
	for {
		result, _, err := balloonGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			close(done)
			return balloonWindowsCallError("read legacy notification messages", err)
		}
		if result == 0 {
			break
		}
		_, _, _ = balloonTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = balloonDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	close(done)
	return nil
}

func registerBalloonClass() {
	instance, _, err := balloonGetModuleHandle.Call(0)
	if instance == 0 {
		balloonClassErr = balloonWindowsCallError("resolve legacy notification module", err)
		return
	}
	class := balloonWindowClass{Size: uint32(unsafe.Sizeof(balloonWindowClass{})), WindowProc: balloonWindowProcedure,
		Instance: instance, ClassName: balloonClassName}
	if atom, _, err := balloonRegisterClass.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		balloonClassErr = balloonWindowsCallError("register legacy notification window", err)
	}
}

func balloonWndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case balloonCallbackMessage:
		if uint32(lparam) == balloonClicked {
			if value, ok := balloonStates.Load(hwnd); ok {
				if launch := value.(balloonState).launchURI; launch != "" {
					_ = openWindowsURI(launch)
				}
			}
			_, _, _ = balloonPostMessage.Call(hwnd, balloonWMClose, 0, 0)
		}
		return 0
	case balloonWMClose:
		_, _, _ = balloonDestroyWindow.Call(hwnd)
		return 0
	case balloonWMDestroy:
		_, _, _ = balloonPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := balloonDefWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
	return result
}

func loadBalloonIcon(instance uintptr) (uintptr, error) {
	name := windows.StringToUTF16Ptr("APP")
	icon, _, err := balloonLoadImage.Call(instance, uintptr(unsafe.Pointer(name)), balloonImageIcon, 0, 0,
		balloonLRDefaultSize|balloonLRShared)
	if icon == 0 {
		return 0, balloonWindowsCallError("load APP icon for legacy notification balloon", err)
	}
	return icon, nil
}

func drainBalloonQuitMessage() {
	var message balloonMessage
	_, _, _ = balloonGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
}

func copyBalloonUTF16(destination []uint16, value string) {
	if len(destination) == 0 {
		return
	}
	encoded := utf16.Encode([]rune(strings.ReplaceAll(value, "\x00", "")))
	if len(encoded) >= len(destination) {
		encoded = encoded[:len(destination)-1]
	}
	copy(destination, encoded)
}

func balloonWindowsCallError(operation string, err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return errors.New(operation + " failed")
	}
	return fmt.Errorf("%s: %w", operation, err)
}
