//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL           = 13
	hcAction               = 0
	errorInvalidHookHandle = windows.Errno(1404)
)

var (
	ErrNilKeyboardCaptureHandler = errors.New("winfx: keyboard capture handler is nil")
	ErrKeyboardCaptureActive     = errors.New("winfx: another keyboard capture is already active")

	keyboardCaptureUser32         = windows.NewLazySystemDLL("user32.dll")
	keyboardCaptureKernel32       = windows.NewLazySystemDLL("kernel32.dll")
	keyboardSetWindowsHookExProc  = keyboardCaptureUser32.NewProc("SetWindowsHookExW")
	keyboardUnhookWindowsHookProc = keyboardCaptureUser32.NewProc("UnhookWindowsHookEx")
	keyboardCallNextHookProc      = keyboardCaptureUser32.NewProc("CallNextHookEx")
	keyboardGetAsyncKeyStateProc  = keyboardCaptureUser32.NewProc("GetAsyncKeyState")
	keyboardGetModuleHandleProc   = keyboardCaptureKernel32.NewProc("GetModuleHandleW")

	keyboardCaptureCallbackPointer = windows.NewCallback(keyboardCaptureThunk)
	keyboardCaptureGlobalMu        sync.RWMutex
	activeKeyboardCapture          *KeyboardCapture
)

// SampleKeyboardModifiers samples the physical/logical async keyboard state.
// Call it immediately after StartKeyboardCapture, then feed each value from
// VirtualKeys into ChordRecorder.KeyDown. This preserves modifiers that were
// already held before the Record control received its click.
func SampleKeyboardModifiers() KeyboardModifierState {
	if keyboardGetAsyncKeyStateProc.Find() != nil {
		return KeyboardModifierState{}
	}
	return KeyboardModifierState{
		LeftControl:  asyncKeyIsDown(0xA2),
		RightControl: asyncKeyIsDown(0xA3),
		LeftAlt:      asyncKeyIsDown(0xA4),
		RightAlt:     asyncKeyIsDown(0xA5),
		LeftShift:    asyncKeyIsDown(0xA0),
		RightShift:   asyncKeyIsDown(0xA1),
		LeftWin:      asyncKeyIsDown(0x5B),
		RightWin:     asyncKeyIsDown(0x5C),
	}
}

func asyncKeyIsDown(virtualKey uint32) bool {
	state, _, _ := keyboardGetAsyncKeyStateProc.Call(uintptr(virtualKey))
	return uint16(state)&0x8000 != 0
}

// KeyboardCapture is a short-lived, process-owned WH_KEYBOARD_LL recording
// scope. Start it on the GUI thread while the recorder is active and Close it
// as soon as recording ends. That thread must continue pumping messages.
//
// Captured events are suppressed before Windows accelerators and shell
// shortcuts see them, so Alt+Tab and Win+letter combinations remain inside the
// recorder. Only one capture can be active process-wide.
type KeyboardCapture struct {
	mu            sync.Mutex
	hook          windows.Handle
	handler       KeyboardCaptureHandler
	options       KeyboardCaptureOptions
	ownerThreadID uint32
	held          map[uint32]bool
	accepting     bool
	closing       bool
	closeDone     chan struct{}
	closeErr      error
}

// StartKeyboardCapture installs a recording-scoped low-level keyboard hook.
// It must be called on the GUI thread that will pump messages for the capture.
// The handler is retained until Close succeeds.
func StartKeyboardCapture(
	options KeyboardCaptureOptions,
	handler KeyboardCaptureHandler,
) (*KeyboardCapture, error) {
	if handler == nil {
		return nil, ErrNilKeyboardCaptureHandler
	}
	for name, proc := range map[string]*windows.LazyProc{
		"SetWindowsHookExW":   keyboardSetWindowsHookExProc,
		"UnhookWindowsHookEx": keyboardUnhookWindowsHookProc,
		"CallNextHookEx":      keyboardCallNextHookProc,
		"GetModuleHandleW":    keyboardGetModuleHandleProc,
	} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("resolve %s: %w", name, err)
		}
	}

	capture := &KeyboardCapture{
		handler:       handler,
		options:       options,
		ownerThreadID: windows.GetCurrentThreadId(),
		held:          make(map[uint32]bool),
	}

	// Reserve the single process-wide callback before installing the hook.
	// The callback simply forwards events while accepting remains false.
	keyboardCaptureGlobalMu.Lock()
	if activeKeyboardCapture != nil {
		keyboardCaptureGlobalMu.Unlock()
		return nil, ErrKeyboardCaptureActive
	}
	activeKeyboardCapture = capture
	keyboardCaptureGlobalMu.Unlock()

	module, _, moduleErr := keyboardGetModuleHandleProc.Call(0)
	if module == 0 {
		keyboardCaptureGlobalMu.Lock()
		if activeKeyboardCapture == capture {
			activeKeyboardCapture = nil
		}
		keyboardCaptureGlobalMu.Unlock()
		return nil, boolCallError("GetModuleHandleW", moduleErr)
	}

	hook, _, callErr := keyboardSetWindowsHookExProc.Call(
		whKeyboardLL,
		keyboardCaptureCallbackPointer,
		module,
		0,
	)
	if hook == 0 {
		keyboardCaptureGlobalMu.Lock()
		if activeKeyboardCapture == capture {
			activeKeyboardCapture = nil
		}
		keyboardCaptureGlobalMu.Unlock()
		return nil, boolCallError("SetWindowsHookExW(WH_KEYBOARD_LL)", callErr)
	}

	capture.mu.Lock()
	capture.hook = windows.Handle(hook)
	capture.accepting = true
	capture.mu.Unlock()
	runtime.KeepAlive(handler)
	return capture, nil
}

// Active reports whether this capture is installed and accepting events.
func (c *KeyboardCapture) Active() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hook != 0 && c.accepting && !c.closing
}

// OwnerThreadID returns the Windows thread that installed this capture.
func (c *KeyboardCapture) OwnerThreadID() uint32 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ownerThreadID
}

// Close stops suppression, unhooks the capture, and releases its handler. It
// is safe to call from the handler and is idempotent. Concurrent callers wait
// for an in-progress unhook and receive the same result.
func (c *KeyboardCapture) Close() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if c.hook == 0 {
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	if c.closing {
		done := c.closeDone
		ownerThreadID := c.ownerThreadID
		c.mu.Unlock()
		// UnhookWindowsHookEx may be waiting for a callback already in
		// progress on the owner thread. Never make that callback wait back
		// on the concurrent unhook operation.
		if windows.GetCurrentThreadId() == ownerThreadID {
			return nil
		}
		<-done
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.closing = true
	c.accepting = false
	c.closeDone = make(chan struct{})
	done := c.closeDone
	hook := c.hook
	c.mu.Unlock()

	ok, _, callErr := keyboardUnhookWindowsHookProc.Call(uintptr(hook))
	var closeErr error
	if ok == 0 && !errors.Is(callErr, errorInvalidHookHandle) {
		closeErr = boolCallError("UnhookWindowsHookEx(WH_KEYBOARD_LL)", callErr)
	}

	if closeErr == nil {
		keyboardCaptureGlobalMu.Lock()
		if activeKeyboardCapture == c {
			activeKeyboardCapture = nil
		}
		keyboardCaptureGlobalMu.Unlock()
	}

	c.mu.Lock()
	c.closeErr = closeErr
	c.closing = false
	if closeErr == nil {
		c.hook = 0
		c.handler = nil
		c.held = nil
	} else {
		// Permit a later Close to retry, while keeping the callback safely
		// retained and the process-wide slot reserved. Never resume accepting:
		// if the owner has already ended recording, a still-installed hook must
		// fail open through CallNextHookEx rather than suppress every key.
		c.accepting = false
	}
	close(done)
	c.mu.Unlock()
	runtime.KeepAlive(c)
	return closeErr
}

func keyboardCaptureThunk(
	nCode uintptr,
	wParam uintptr,
	input *lowLevelKeyboardInput,
) uintptr {
	lParam := uintptr(unsafe.Pointer(input))
	if int32(nCode) < hcAction || input == nil {
		return callNextKeyboardCapture(nCode, wParam, lParam)
	}

	keyboardCaptureGlobalMu.RLock()
	capture := activeKeyboardCapture
	keyboardCaptureGlobalMu.RUnlock()
	if capture == nil {
		return callNextKeyboardCapture(nCode, wParam, lParam)
	}
	if capture.dispatch(uint32(wParam), input) {
		return 1
	}
	return callNextKeyboardCapture(nCode, wParam, lParam)
}

func (c *KeyboardCapture) dispatch(
	message uint32,
	input *lowLevelKeyboardInput,
) bool {
	if c == nil || input == nil {
		return false
	}

	c.mu.Lock()
	if !c.accepting || c.handler == nil {
		c.mu.Unlock()
		return false
	}

	normalizedKey := normalizeCapturedVirtualKey(
		input.VirtualKey,
		input.ScanCode,
		input.Flags,
	)
	keyDown := message == keyboardMessageKeyDown ||
		message == keyboardMessageSysKeyDown
	repeat := keyDown && c.held[normalizedKey]
	event, valid := keyboardCaptureEvent(message, *input, repeat)
	if !valid || c.options.IgnoreInjected && event.Injected {
		c.mu.Unlock()
		return false
	}
	if event.KeyDown {
		c.held[event.VirtualKey] = true
	} else {
		delete(c.held, event.VirtualKey)
	}
	handler := c.handler
	c.mu.Unlock()

	// Panics must never unwind through a Windows callback frame. The key is
	// still suppressed because it belongs to the active recording scope.
	invokeKeyboardCaptureHandler(handler, event)
	return true
}

func invokeKeyboardCaptureHandler(
	handler KeyboardCaptureHandler,
	event KeyboardCaptureEvent,
) {
	defer func() {
		_ = recover()
	}()
	handler(event)
}

func callNextKeyboardCapture(nCode, wParam, lParam uintptr) uintptr {
	if keyboardCallNextHookProc.Find() != nil {
		return 0
	}
	result, _, _ := keyboardCallNextHookProc.Call(0, nCode, wParam, lParam)
	return result
}
