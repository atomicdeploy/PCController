//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	// WMSystemNetworkChange is posted to the registered HWND after an IP
	// interface change. The native callback intentionally carries no row
	// pointer across the callback boundary.
	WMSystemNetworkChange = uint32(win.WM_APP + 0x0527)

	WMWTSSessionChange = 0x02B1
	WTSSessionLock     = 0x0007
	WTSSessionUnlock   = 0x0008

	PBTAPMSuspend         = 0x0004
	PBTAPMResumeSuspend   = 0x0007
	PBTAPMResumeAutomatic = 0x0012

	notifyForThisSession = 0
	addressFamilyUnspec  = 0
)

var (
	systemEventsWTSAPI32 = windows.NewLazySystemDLL("wtsapi32.dll")
	systemEventsIPHLPAPI = windows.NewLazySystemDLL("iphlpapi.dll")

	systemEventsWTSRegister = systemEventsWTSAPI32.NewProc(
		"WTSRegisterSessionNotification",
	)
	systemEventsWTSUnregister = systemEventsWTSAPI32.NewProc(
		"WTSUnRegisterSessionNotification",
	)
	systemEventsNotifyIPInterface = systemEventsIPHLPAPI.NewProc(
		"NotifyIpInterfaceChange",
	)
	systemEventsCancelMIBChange = systemEventsIPHLPAPI.NewProc(
		"CancelMibChangeNotify2",
	)

	systemEventsNetworkCallback = windows.NewCallback(
		systemEventsNetworkChange,
	)
	systemEventsPostMessage = win.PostMessage
)

// SystemEvents owns WTS and IP Helper registrations associated with one HWND.
// Close must run before that HWND is destroyed.
type SystemEvents struct {
	hwnd          win.HWND
	wtsRegistered bool
	networkHandle windows.Handle
	closeOnce     sync.Once
}

// RegisterSystemEvents registers every available Windows lifecycle source.
// Partial success is retained and returned alongside a joined non-fatal error.
func RegisterSystemEvents(hwnd uintptr) (*SystemEvents, error) {
	if hwnd == 0 {
		return nil, ErrInvalidWindow
	}
	registration := &SystemEvents{hwnd: win.HWND(hwnd)}
	var failures []error

	if err := systemEventsWTSRegister.Find(); err != nil {
		failures = append(failures, fmt.Errorf(
			"system events: resolve WTSRegisterSessionNotification: %w",
			err,
		))
	} else {
		ok, _, callErr := systemEventsWTSRegister.Call(
			hwnd,
			notifyForThisSession,
		)
		if ok == 0 {
			failures = append(failures, boolCallError(
				"WTSRegisterSessionNotification",
				callErr,
			))
		} else {
			registration.wtsRegistered = true
		}
	}

	if err := systemEventsNotifyIPInterface.Find(); err != nil {
		failures = append(failures, fmt.Errorf(
			"system events: resolve NotifyIpInterfaceChange: %w",
			err,
		))
	} else {
		var handle windows.Handle
		result, _, _ := systemEventsNotifyIPInterface.Call(
			addressFamilyUnspec,
			systemEventsNetworkCallback,
			hwnd,
			0,
			uintptr(unsafe.Pointer(&handle)),
		)
		if result != 0 {
			failures = append(failures, windowsCodeError(
				"NotifyIpInterfaceChange",
				uint32(result),
			))
		} else if handle == 0 {
			failures = append(
				failures,
				errors.New("NotifyIpInterfaceChange returned a null handle"),
			)
		} else {
			registration.networkHandle = handle
		}
	}

	return registration, errors.Join(failures...)
}

// Close deregisters callbacks before their target HWND disappears.
func (s *SystemEvents) Close() error {
	if s == nil {
		return nil
	}
	var result error
	s.closeOnce.Do(func() {
		var failures []error
		if s.networkHandle != 0 {
			if err := systemEventsCancelMIBChange.Find(); err != nil {
				failures = append(failures, fmt.Errorf(
					"system events: resolve CancelMibChangeNotify2: %w",
					err,
				))
			} else {
				code, _, _ := systemEventsCancelMIBChange.Call(
					uintptr(s.networkHandle),
				)
				if code != 0 {
					failures = append(failures, windowsCodeError(
						"CancelMibChangeNotify2",
						uint32(code),
					))
				}
			}
			s.networkHandle = 0
		}
		if s.wtsRegistered {
			if err := systemEventsWTSUnregister.Find(); err != nil {
				failures = append(failures, fmt.Errorf(
					"system events: resolve WTSUnRegisterSessionNotification: %w",
					err,
				))
			} else {
				ok, _, callErr := systemEventsWTSUnregister.Call(
					uintptr(s.hwnd),
				)
				if ok == 0 {
					failures = append(failures, boolCallError(
						"WTSUnRegisterSessionNotification",
						callErr,
					))
				}
			}
			s.wtsRegistered = false
		}
		result = errors.Join(failures...)
	})
	return result
}

func systemEventsNetworkChange(
	callerContext uintptr,
	_ uintptr,
	notificationType uintptr,
) uintptr {
	if callerContext != 0 {
		systemEventsPostMessage(
			win.HWND(callerContext),
			WMSystemNetworkChange,
			notificationType,
			0,
		)
	}
	return 0
}

func windowsCodeError(operation string, code uint32) error {
	return fmt.Errorf("%s: %w", operation, syscallErrno(code))
}

// syscallErrno is isolated for deterministic formatting and tests without
// exposing a syscall package type in the SystemEvents API.
func syscallErrno(code uint32) error {
	return windows.Errno(code)
}
