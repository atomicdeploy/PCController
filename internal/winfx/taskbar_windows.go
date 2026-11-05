//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	thumbnailButtonMaskIcon    = 0x00000002
	thumbnailButtonMaskTooltip = 0x00000004
	thumbnailButtonMaskFlags   = 0x00000008

	thumbnailButtonDisabled = 0x00000001
	thumbnailButtonHidden   = 0x00000008

	maxThumbnailButtons = 7
)

// TaskbarProgressState is one of the mutually exclusive progress states
// supported by ITaskbarList3.
type TaskbarProgressState int

const (
	TaskbarProgressNone          TaskbarProgressState = win.TBPF_NOPROGRESS
	TaskbarProgressIndeterminate TaskbarProgressState = win.TBPF_INDETERMINATE
	TaskbarProgressNormal        TaskbarProgressState = win.TBPF_NORMAL
	TaskbarProgressError         TaskbarProgressState = win.TBPF_ERROR
	TaskbarProgressPaused        TaskbarProgressState = win.TBPF_PAUSED
)

// ThumbnailButton describes one permanent slot in a taskbar thumbnail
// toolbar. Windows fixes the number and order of slots after the first add;
// callers may subsequently change icons, tooltips, and flags.
type ThumbnailButton struct {
	ID       uint32
	Icon     uintptr
	Tooltip  string
	Disabled bool
	Hidden   bool
}

// thumbnailButton mirrors THUMBBUTTON from shobjidl_core.h. uintptr gives
// HICON the correct natural alignment on both 32- and 64-bit Windows.
type thumbnailButton struct {
	Mask    uint32
	ID      uint32
	Bitmap  uint32
	Icon    uintptr
	Tooltip [260]uint16
	Flags   uint32
}

// Taskbar owns one ITaskbarList3 instance. Every method must be called from
// the apartment/UI thread that created it.
type Taskbar struct {
	hwnd           win.HWND
	object         *win.ITaskbarList3
	createdMessage uint32
	ready          bool
	buttonsAdded   bool
	closed         bool
}

// NewTaskbar creates the COM controller and registers the Explorer readiness
// message. The owning window must not call any other method until Explorer
// sends CreatedMessage to that window; SetButtons, SetProgress, and
// SetThumbnailTooltip therefore harmlessly cache nothing and no-op until then.
func NewTaskbar(hwnd uintptr) (*Taskbar, error) {
	if hwnd == 0 {
		return nil, ErrInvalidWindow
	}
	messageName, err := windows.UTF16PtrFromString("TaskbarButtonCreated")
	if err != nil {
		return nil, fmt.Errorf("taskbar: encode readiness message: %w", err)
	}
	createdMessage := win.RegisterWindowMessage(messageName)
	if createdMessage == 0 {
		return nil, errors.New("taskbar: RegisterWindowMessageW failed")
	}

	var object *win.ITaskbarList3
	result := win.CoCreateInstance(
		&win.CLSID_TaskbarList,
		nil,
		win.CLSCTX_INPROC_SERVER,
		&win.IID_ITaskbarList3,
		(*unsafe.Pointer)(unsafe.Pointer(&object)),
	)
	if win.FAILED(result) {
		return nil, taskbarHRESULT("CoCreateInstance", result)
	}
	if object == nil {
		return nil, errors.New("taskbar: CoCreateInstance returned a nil object")
	}
	if result = callTaskbarHRESULT(
		object.LpVtbl.HrInit,
		uintptr(unsafe.Pointer(object)),
	); win.FAILED(result) {
		releaseTaskbarObject(object)
		return nil, taskbarHRESULT("ITaskbarList3.HrInit", result)
	}
	return &Taskbar{
		hwnd:           win.HWND(hwnd),
		object:         object,
		createdMessage: createdMessage,
	}, nil
}

// CreatedMessage returns the process-local message value registered for
// "TaskbarButtonCreated".
func (t *Taskbar) CreatedMessage() uint32 {
	if t == nil {
		return 0
	}
	return t.createdMessage
}

// NotifyButtonCreated marks the Explorer taskbar button ready. Explorer sends
// this message again after it restarts, so the permanent thumbnail slots must
// be added again on every call.
func (t *Taskbar) NotifyButtonCreated(buttons []ThumbnailButton) error {
	if t == nil || t.closed || t.object == nil {
		return nil
	}
	t.ready = true
	t.buttonsAdded = false
	return t.SetButtons(buttons)
}

// NotifyButtonRemoved stops issuing calls for a taskbar button that has been
// hidden or removed. A later TaskbarButtonCreated message re-enables it.
func (t *Taskbar) NotifyButtonRemoved() {
	if t == nil || t.closed {
		return
	}
	t.ready = false
	t.buttonsAdded = false
}

// SetButtons adds or updates the fixed thumbnail toolbar slots.
func (t *Taskbar) SetButtons(buttons []ThumbnailButton) error {
	if t == nil || t.closed || t.object == nil || !t.ready {
		return nil
	}
	native, err := makeThumbnailButtons(buttons)
	if err != nil {
		return err
	}
	address := t.object.LpVtbl.ThumbBarUpdateButtons
	operation := "ITaskbarList3.ThumbBarUpdateButtons"
	if !t.buttonsAdded {
		address = t.object.LpVtbl.ThumbBarAddButtons
		operation = "ITaskbarList3.ThumbBarAddButtons"
	}
	result := callTaskbarHRESULT(
		address,
		uintptr(unsafe.Pointer(t.object)),
		uintptr(t.hwnd),
		uintptr(len(native)),
		uintptr(unsafe.Pointer(&native[0])),
	)
	runtime.KeepAlive(native)
	if win.FAILED(result) {
		return taskbarHRESULT(operation, result)
	}
	t.buttonsAdded = true
	return nil
}

// SetProgress updates the taskbar progress indicator. current and total are
// used for determinate, paused, and error presentations and ignored for none
// or indeterminate.
func (t *Taskbar) SetProgress(
	state TaskbarProgressState,
	current uint32,
	total uint32,
) error {
	if t == nil || t.closed || t.object == nil || !t.ready {
		return nil
	}
	switch state {
	case TaskbarProgressNone, TaskbarProgressIndeterminate:
	case TaskbarProgressNormal, TaskbarProgressError, TaskbarProgressPaused:
		if total == 0 {
			total = 100
		}
		if current > total {
			current = total
		}
		if result := t.object.SetProgressValue(
			t.hwnd,
			current,
			total,
		); win.FAILED(result) {
			return taskbarHRESULT("ITaskbarList3.SetProgressValue", result)
		}
	default:
		return fmt.Errorf("taskbar: invalid progress state %d", state)
	}
	if result := t.object.SetProgressState(
		t.hwnd,
		int(state),
	); win.FAILED(result) {
		return taskbarHRESULT("ITaskbarList3.SetProgressState", result)
	}
	return nil
}

// SetThumbnailTooltip changes the text shown for the main window's preview.
func (t *Taskbar) SetThumbnailTooltip(text string) error {
	if t == nil || t.closed || t.object == nil || !t.ready {
		return nil
	}
	pointer, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return fmt.Errorf("taskbar: encode thumbnail tooltip: %w", err)
	}
	result := callTaskbarHRESULT(
		t.object.LpVtbl.SetThumbnailTooltip,
		uintptr(unsafe.Pointer(t.object)),
		uintptr(t.hwnd),
		uintptr(unsafe.Pointer(pointer)),
	)
	runtime.KeepAlive(pointer)
	if win.FAILED(result) {
		return taskbarHRESULT("ITaskbarList3.SetThumbnailTooltip", result)
	}
	return nil
}

// Close clears progress and releases the apartment-bound COM interface.
func (t *Taskbar) Close() {
	if t == nil || t.closed {
		return
	}
	t.closed = true
	if t.object != nil {
		if t.ready {
			_ = t.object.SetProgressState(t.hwnd, win.TBPF_NOPROGRESS)
		}
		releaseTaskbarObject(t.object)
		t.object = nil
	}
	t.ready = false
	t.buttonsAdded = false
}

func makeThumbnailButtons(
	buttons []ThumbnailButton,
) ([]thumbnailButton, error) {
	if len(buttons) < 1 || len(buttons) > maxThumbnailButtons {
		return nil, fmt.Errorf(
			"taskbar: thumbnail button count %d is outside 1..%d",
			len(buttons),
			maxThumbnailButtons,
		)
	}
	seen := make(map[uint32]struct{}, len(buttons))
	native := make([]thumbnailButton, len(buttons))
	for index, button := range buttons {
		if button.ID == 0 {
			return nil, fmt.Errorf("taskbar: thumbnail button %d has zero ID", index)
		}
		if _, exists := seen[button.ID]; exists {
			return nil, fmt.Errorf("taskbar: duplicate thumbnail button ID %d", button.ID)
		}
		seen[button.ID] = struct{}{}
		if button.Icon == 0 {
			return nil, fmt.Errorf(
				"taskbar: thumbnail button %d has no icon",
				button.ID,
			)
		}
		encoded, err := windows.UTF16FromString(button.Tooltip)
		if err != nil {
			return nil, fmt.Errorf(
				"taskbar: encode thumbnail button %d tooltip: %w",
				button.ID,
				err,
			)
		}
		encoded = truncateTaskbarTooltip(encoded, len(native[index].Tooltip))
		native[index].Mask = thumbnailButtonMaskIcon |
			thumbnailButtonMaskTooltip |
			thumbnailButtonMaskFlags
		native[index].ID = button.ID
		native[index].Icon = button.Icon
		copy(native[index].Tooltip[:], encoded)
		if button.Disabled {
			native[index].Flags |= thumbnailButtonDisabled
		}
		if button.Hidden {
			native[index].Flags |= thumbnailButtonHidden
		}
	}
	return native, nil
}

func truncateTaskbarTooltip(encoded []uint16, capacity int) []uint16 {
	if capacity < 1 {
		return nil
	}
	if len(encoded) <= capacity {
		return encoded
	}
	encoded = encoded[:capacity]
	encoded[capacity-1] = 0
	// Avoid leaving an unmatched UTF-16 high surrogate before the terminator.
	if capacity >= 2 &&
		encoded[capacity-2] >= 0xD800 &&
		encoded[capacity-2] <= 0xDBFF {
		encoded[capacity-2] = 0
	}
	return encoded
}

func callTaskbarHRESULT(address uintptr, arguments ...uintptr) win.HRESULT {
	result, _, _ := syscall.SyscallN(address, arguments...)
	return win.HRESULT(result)
}

func releaseTaskbarObject(object *win.ITaskbarList3) {
	if object == nil || object.LpVtbl == nil || object.LpVtbl.Release == 0 {
		return
	}
	_, _, _ = syscall.SyscallN(
		object.LpVtbl.Release,
		uintptr(unsafe.Pointer(object)),
	)
}

func taskbarHRESULT(operation string, result win.HRESULT) error {
	return fmt.Errorf(
		"taskbar: %s failed with HRESULT 0x%08X",
		operation,
		uint32(result),
	)
}
