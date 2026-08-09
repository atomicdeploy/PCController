//go:build windows

package hostui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmTimer       = 0x0113
	llkhfInjected = 0x10
	vkControl     = 0x11
	vkLControl    = 0xA2
	vkRControl    = 0xA3
)

var (
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procRtlMoveMemory       = kernel32.NewProc("RtlMoveMemory")
)

type lowLevelKeyboardInput struct {
	VirtualKey uint32
	ScanCode   uint32
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

type keyboardDispatch struct {
	event   KeyboardEvent
	done    chan error
	barrier bool
}

type windowsKeyboardRegistrar struct {
	mu           sync.RWMutex
	running      bool
	stopping     bool
	threadID     uint32
	owner        uintptr
	focused      bool
	bindings     []KeyboardBinding
	state        *keyboardState
	callback     func(KeyboardEvent) error
	lastError    string
	lastEvent    *KeyboardEvent
	events       chan keyboardDispatch
	dispatchStop chan struct{}
	dispatchDone chan struct{}
	done         chan struct{}
}

func newPlatformKeyboardRegistrar() KeyboardRegistrar { return &windowsKeyboardRegistrar{} }

func (registrar *windowsKeyboardRegistrar) Start(
	ctx context.Context,
	bindings []KeyboardBinding,
	callback func(KeyboardEvent) error,
) error {
	keys, err := validateKeyboardBindings(bindings)
	if err != nil {
		return err
	}
	if callback == nil {
		return errors.New("keyboard control callback is required")
	}
	owner, _, _ := procGetForegroundWindow.Call()
	if owner == 0 {
		return errors.New("keyboard control requires a foreground host window")
	}
	state := newKeyboardState(bindings, keys)
	for _, key := range keys {
		pressed, _, _ := procGetAsyncKeyState.Call(uintptr(key))
		if uint16(pressed)&0x8000 != 0 {
			state.blockUntilUp(key)
		}
	}

	registrar.mu.Lock()
	if registrar.running {
		registrar.mu.Unlock()
		return errors.New("keyboard-control hook is already running")
	}
	registrar.running = true
	registrar.stopping = false
	registrar.owner = owner
	registrar.focused = true
	registrar.bindings = append([]KeyboardBinding(nil), bindings...)
	registrar.state = state
	registrar.callback = callback
	registrar.lastError = ""
	registrar.lastEvent = nil
	registrar.events = make(chan keyboardDispatch, 128)
	registrar.dispatchStop = make(chan struct{})
	registrar.dispatchDone = make(chan struct{})
	registrar.done = make(chan struct{})
	started := make(chan error, 1)
	registrar.mu.Unlock()
	go registrar.dispatchLoop()
	go registrar.hookLoop(started)
	if err := <-started; err != nil {
		return err
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = registrar.Stop("context-cancelled")
		case <-registrar.done:
		}
	}()
	return nil
}

func (registrar *windowsKeyboardRegistrar) dispatchLoop() {
	defer close(registrar.dispatchDone)
	for {
		select {
		case item := <-registrar.events:
			registrar.mu.RLock()
			callback := registrar.callback
			registrar.mu.RUnlock()
			var err error
			if callback != nil && !item.barrier {
				err = callback(item.event)
			}
			if !item.barrier {
				registrar.mu.Lock()
				event := item.event
				registrar.lastEvent = &event
				if err != nil {
					registrar.lastError = err.Error()
				}
				registrar.mu.Unlock()
			}
			if item.done != nil {
				item.done <- err
				close(item.done)
			}
		case <-registrar.dispatchStop:
			return
		}
	}
}

func (registrar *windowsKeyboardRegistrar) hookLoop(started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, _, _ := procGetCurrentThreadID.Call()
	var message winMessage
	_, _, _ = procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmNoRemove)
	registrar.mu.Lock()
	registrar.threadID = uint32(thread)
	registrar.mu.Unlock()

	callback := syscall.NewCallback(func(code int, wParam, lParam uintptr) uintptr {
		if code >= 0 {
			// Copy the hook payload while it is valid. Passing its uintptr directly
			// to RtlMoveMemory avoids retaining a Windows-owned pointer.
			var input lowLevelKeyboardInput
			_, _, _ = procRtlMoveMemory.Call(
				uintptr(unsafe.Pointer(&input)), lParam, unsafe.Sizeof(input),
			)
			if input.Flags&llkhfInjected == 0 {
				down := wParam == wmKeyDown || wParam == wmSysKeyDown
				up := wParam == wmKeyUp || wParam == wmSysKeyUp
				if down || up {
					control := registrar.controlPressed(input.VirtualKey, down)
					events := registrar.state.handle(
						input.VirtualKey, down, control,
						registrar.hasFocus(), time.Now(),
					)
					registrar.enqueue(events, false)
				}
			}
		}
		result, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
		return result
	})
	hook, _, callErr := procSetWindowsHookEx.Call(whKeyboardLL, callback, 0, 0)
	if hook == 0 {
		err := fmt.Errorf("install low-level keyboard hook: %w", callErr)
		started <- err
		registrar.finish(err)
		return
	}
	timer, _, timerErr := procSetTimer.Call(0, 0, 50, 0)
	if timer == 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(hook)
		err := fmt.Errorf("start keyboard focus monitor: %w", timerErr)
		started <- err
		registrar.finish(err)
		return
	}
	defer procKillTimer.Call(0, timer)
	defer procUnhookWindowsHookEx.Call(hook)
	started <- nil
	for {
		result, _, messageErr := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			registrar.finish(fmt.Errorf("read keyboard-hook message: %w", messageErr))
			return
		}
		if result == 0 || message.Message == wmQuit {
			registrar.finish(nil)
			return
		}
		if message.Message == wmTimer {
			focused := registrar.hasFocus()
			if registrar.focusChanged(focused) && !focused {
				now := time.Now()
				events := registrar.state.releaseAll("focus-lost", now)
				events = append(events, keyboardFailSafeEvent("focus-lost", now))
				registrar.enqueue(events, false)
			}
		}
	}
}

func (registrar *windowsKeyboardRegistrar) controlPressed(key uint32, down bool) bool {
	if key == vkControl || key == vkLControl || key == vkRControl {
		return down
	}
	pressed, _, _ := procGetAsyncKeyState.Call(vkControl)
	return uint16(pressed)&0x8000 != 0
}

func (registrar *windowsKeyboardRegistrar) hasFocus() bool {
	foreground, _, _ := procGetForegroundWindow.Call()
	registrar.mu.RLock()
	owner := registrar.owner
	registrar.mu.RUnlock()
	return owner != 0 && foreground == owner
}

func (registrar *windowsKeyboardRegistrar) focusChanged(focused bool) bool {
	registrar.mu.Lock()
	changed := registrar.focused != focused
	registrar.focused = focused
	registrar.mu.Unlock()
	return changed
}

func (registrar *windowsKeyboardRegistrar) enqueue(events []KeyboardEvent, wait bool) error {
	var result error
	for _, event := range events {
		item := keyboardDispatch{event: event}
		if wait {
			item.done = make(chan error, 1)
		}
		registrar.mu.RLock()
		running := registrar.running
		queue, stopped := registrar.events, registrar.dispatchDone
		registrar.mu.RUnlock()
		if !running || queue == nil {
			return errors.Join(result, errors.New("keyboard-control hook is stopped"))
		}
		select {
		case queue <- item:
		case <-stopped:
			return errors.Join(result, errors.New("keyboard-control dispatcher stopped"))
		}
		if item.done != nil {
			select {
			case err := <-item.done:
				result = errors.Join(result, err)
			case <-stopped:
				result = errors.Join(result, errors.New("keyboard-control dispatcher stopped before release"))
			}
		}
	}
	return result
}

func (registrar *windowsKeyboardRegistrar) flush() {
	barrier := keyboardDispatch{done: make(chan error, 1), barrier: true}
	registrar.mu.RLock()
	queue, stopped := registrar.events, registrar.dispatchDone
	registrar.mu.RUnlock()
	select {
	case queue <- barrier:
		select {
		case <-barrier.done:
		case <-stopped:
		}
	case <-stopped:
	}
}

func (registrar *windowsKeyboardRegistrar) ReleaseAll(reason string) error {
	registrar.mu.RLock()
	running, state := registrar.running, registrar.state
	registrar.mu.RUnlock()
	if !running || state == nil {
		return nil
	}
	now := time.Now()
	events := state.releaseAll(reason, now)
	events = append(events, keyboardFailSafeEvent(reason, now))
	return registrar.enqueue(events, true)
}

func (registrar *windowsKeyboardRegistrar) Stop(reason string) error {
	registrar.mu.Lock()
	if !registrar.running {
		registrar.mu.Unlock()
		return nil
	}
	if registrar.stopping {
		done := registrar.done
		registrar.mu.Unlock()
		<-done
		return nil
	}
	registrar.stopping = true
	threadID, done := registrar.threadID, registrar.done
	registrar.mu.Unlock()
	releaseErr := registrar.ReleaseAll(reason)
	if threadID == 0 {
		return errors.Join(releaseErr, errors.New("keyboard-control message thread is not ready"))
	}
	result, _, callErr := procPostThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
	if result == 0 {
		registrar.mu.Lock()
		registrar.stopping = false
		registrar.mu.Unlock()
		return errors.Join(releaseErr, fmt.Errorf("stop keyboard-control thread: %w", callErr))
	}
	<-done
	return releaseErr
}

func (registrar *windowsKeyboardRegistrar) finish(err error) {
	// Flush events already queued by the hook before shutting down the dispatcher.
	registrar.flush()
	registrar.mu.RLock()
	stop, dispatchDone := registrar.dispatchStop, registrar.dispatchDone
	registrar.mu.RUnlock()
	close(stop)
	<-dispatchDone
	registrar.mu.Lock()
	if err != nil {
		registrar.lastError = err.Error()
	}
	registrar.running = false
	registrar.threadID = 0
	done := registrar.done
	registrar.mu.Unlock()
	close(done)
}

func (registrar *windowsKeyboardRegistrar) Status() KeyboardStatus {
	registrar.mu.RLock()
	defer registrar.mu.RUnlock()
	status := KeyboardStatus{
		Supported: true, Running: registrar.running,
		Bindings:  append([]KeyboardBinding(nil), registrar.bindings...),
		LastError: registrar.lastError,
	}
	if registrar.state != nil {
		status.ActiveKeys = registrar.state.active()
	}
	if registrar.lastEvent != nil {
		event := *registrar.lastEvent
		status.LastEvent = &event
	}
	return status
}
