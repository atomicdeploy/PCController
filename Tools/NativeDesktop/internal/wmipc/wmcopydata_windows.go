//go:build windows

package wmipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"rayanlamp.local/app/internal/ipc"
)

const (
	copyDataMagic = 0x524C414D // "RLAM"
	wmCopyData    = 0x004A
	maxPayload    = 64 << 10

	smtoBlock       = 0x0001
	smtoAbortIfHung = 0x0002
)

const WindowProperty = "RayanLamp.PrimaryWindow.{66F0C709-6E91-45BE-9013-5A96219E6D3D}"

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procEnumWindows        = user32.NewProc("EnumWindows")
	procGetPropW           = user32.NewProc("GetPropW")
	procSetPropW           = user32.NewProc("SetPropW")
	procRemovePropW        = user32.NewProc("RemovePropW")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
	procCopyMemory         = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlMoveMemory")
	propertyName, _        = syscall.UTF16PtrFromString(WindowProperty)
)

type copyDataStruct struct {
	Data   uintptr
	Length uint32
	Bytes  unsafe.Pointer
}

func RegisterPrimary(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("WM IPC: invalid primary window")
	}
	result, _, callErr := procSetPropW.Call(
		hwnd,
		uintptr(unsafe.Pointer(propertyName)),
		1,
	)
	if result == 0 {
		return windowsCallError(callErr, "SetPropW failed")
	}
	return nil
}

func UnregisterPrimary(hwnd uintptr) {
	if hwnd != 0 {
		procRemovePropW.Call(hwnd, uintptr(unsafe.Pointer(propertyName)))
	}
}

func FindPrimary() uintptr {
	var found uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		value, _, _ := procGetPropW.Call(
			hwnd,
			uintptr(unsafe.Pointer(propertyName)),
		)
		if value != 0 {
			found = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(callback, 0)
	return found
}

// Send delivers a command to the primary window. WM_COPYDATA is intentionally
// an acknowledgement-only handoff; callers that need structured state use the
// same request over the named-pipe or loopback HTTP APIs.
func Send(ctx context.Context, hwnd uintptr, request ipc.Request) error {
	if hwnd == 0 {
		return errors.New("WM IPC: primary window not found")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("WM IPC: encode command: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxPayload {
		return errors.New("WM IPC: command exceeds payload limit")
	}

	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			return context.DeadlineExceeded
		}
	}
	timeoutMS := timeout.Milliseconds()
	if timeoutMS < 1 {
		timeoutMS = 1
	}
	if timeoutMS > 30_000 {
		timeoutMS = 30_000
	}

	data := copyDataStruct{
		Data:   copyDataMagic,
		Length: uint32(len(payload)),
		Bytes:  unsafe.Pointer(&payload[0]),
	}
	var acknowledged uintptr
	result, _, callErr := procSendMessageTimeout.Call(
		hwnd,
		wmCopyData,
		0,
		uintptr(unsafe.Pointer(&data)),
		smtoBlock|smtoAbortIfHung,
		uintptr(timeoutMS),
		uintptr(unsafe.Pointer(&acknowledged)),
	)
	if result == 0 {
		return windowsCallError(callErr, "SendMessageTimeoutW failed")
	}
	if acknowledged == 0 {
		return errors.New("WM IPC: primary instance rejected the command")
	}
	return nil
}

// Decode validates an incoming WM_COPYDATA lParam and returns its command.
func Decode(lParam uintptr) (ipc.Request, error) {
	if lParam == 0 {
		return ipc.Request{}, errors.New("WM IPC: missing COPYDATASTRUCT")
	}
	var data copyDataStruct
	procCopyMemory.Call(
		uintptr(unsafe.Pointer(&data)),
		lParam,
		unsafe.Sizeof(data),
	)
	if data.Data != copyDataMagic {
		return ipc.Request{}, errors.New("WM IPC: unknown payload type")
	}
	if data.Length == 0 || data.Length > maxPayload || data.Bytes == nil {
		return ipc.Request{}, errors.New("WM IPC: invalid payload length")
	}

	payload := unsafe.Slice((*byte)(data.Bytes), int(data.Length))
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var request ipc.Request
	if err := decoder.Decode(&request); err != nil {
		return ipc.Request{}, fmt.Errorf("WM IPC: invalid JSON: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ipc.Request{}, errors.New("WM IPC: multiple JSON values")
		}
		return ipc.Request{}, fmt.Errorf("WM IPC: invalid trailing JSON data: %w", err)
	}
	if request.ProtocolVersion != ipc.ProtocolVersion {
		return ipc.Request{}, errors.New("WM IPC: protocol mismatch")
	}
	request.Method = strings.ToLower(strings.TrimSpace(request.Method))
	if request.Method == "" || len(request.Method) > 64 {
		return ipc.Request{}, errors.New("WM IPC: invalid method")
	}
	return request, nil
}

func MessageID() uint32 {
	return wmCopyData
}

func windowsCallError(err error, fallback string) error {
	if err != nil && !errors.Is(err, syscall.Errno(0)) {
		return err
	}
	return errors.New(fallback)
}
