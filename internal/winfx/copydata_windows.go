//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	WM_COPYDATA      = 0x004A
	MaxCopyDataBytes = 1 << 20

	smtoBlock       = 0x0001
	smtoAbortIfHung = 0x0002
	smtoErrorOnExit = 0x0020
)

var (
	ErrCopyDataTooLarge = errors.New("winfx: WM_COPYDATA payload is too large")
	ErrInvalidCopyData  = errors.New("winfx: invalid WM_COPYDATA packet")

	copyDataUser32             = windows.NewLazySystemDLL("user32.dll")
	copyDataSendMessageTimeout = copyDataUser32.NewProc("SendMessageTimeoutW")
)

// CopyDataPacket matches COPYDATASTRUCT. A WM_COPYDATA handler may cast its
// lParam to *CopyDataPacket for the duration of the callback, then immediately
// pass it to ParseCopyData.
type CopyDataPacket struct {
	Tag  uintptr
	Size uint32
	Data unsafe.Pointer
}

// SendCopyData sends a bounded byte payload synchronously without allowing a
// hung receiver to block forever. accepted is the LRESULT returned by the
// receiver. An empty payload is valid.
func SendCopyData(
	hwnd uintptr,
	sender uintptr,
	tag uintptr,
	payload []byte,
	timeout time.Duration,
) (accepted bool, err error) {
	if hwnd == 0 {
		return false, ErrInvalidWindow
	}
	if len(payload) > MaxCopyDataBytes {
		return false, fmt.Errorf("%w: %d > %d", ErrCopyDataTooLarge, len(payload), MaxCopyDataBytes)
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	timeoutMillis := timeout.Milliseconds()
	if timeoutMillis < 1 {
		timeoutMillis = 1
	}
	if timeoutMillis > int64(^uint32(0)) {
		timeoutMillis = int64(^uint32(0))
	}
	if err := copyDataSendMessageTimeout.Find(); err != nil {
		return false, fmt.Errorf("resolve SendMessageTimeoutW: %w", err)
	}

	packet := CopyDataPacket{Tag: tag, Size: uint32(len(payload))}
	if len(payload) != 0 {
		packet.Data = unsafe.Pointer(&payload[0])
	}
	var result uintptr
	ok, _, callErr := copyDataSendMessageTimeout.Call(
		hwnd,
		WM_COPYDATA,
		sender,
		uintptr(unsafe.Pointer(&packet)),
		smtoBlock|smtoAbortIfHung|smtoErrorOnExit,
		uintptr(uint32(timeoutMillis)),
		uintptr(unsafe.Pointer(&result)),
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(&packet)
	if ok == 0 {
		return false, boolCallError("SendMessageTimeoutW(WM_COPYDATA)", callErr)
	}
	return result != 0, nil
}

// ParseCopyData validates and copies a packet while its WM_COPYDATA memory is
// still valid. maxBytes <= 0 selects MaxCopyDataBytes. The returned payload is
// owned by the caller and remains valid after the window procedure returns.
func ParseCopyData(packet *CopyDataPacket, maxBytes int) (tag uintptr, payload []byte, err error) {
	if packet == nil {
		return 0, nil, ErrInvalidCopyData
	}
	if maxBytes <= 0 || maxBytes > MaxCopyDataBytes {
		maxBytes = MaxCopyDataBytes
	}
	if uint64(packet.Size) > uint64(maxBytes) {
		return 0, nil, fmt.Errorf("%w: %d > %d", ErrCopyDataTooLarge, packet.Size, maxBytes)
	}
	if packet.Size == 0 {
		return packet.Tag, []byte{}, nil
	}
	if packet.Data == nil {
		return 0, nil, fmt.Errorf("%w: non-empty packet has nil data", ErrInvalidCopyData)
	}
	source := unsafe.Slice((*byte)(packet.Data), int(packet.Size))
	payload = make([]byte, len(source))
	copy(payload, source)
	return packet.Tag, payload, nil
}
