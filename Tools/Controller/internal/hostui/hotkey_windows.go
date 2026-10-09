//go:build windows

package hostui

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmQuit       = 0x0012
	wmHotkey     = 0x0312
	pmNoRemove   = 0x0000
	hotkeyIDBase = 0x4C00
)

type winPoint struct{ X, Y int32 }
type winMessage struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   winPoint
	Private uint32
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessage         = user32.NewProc("GetMessageW")
	procPeekMessage        = user32.NewProc("PeekMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

type windowsHotkeyRegistrar struct {
	mu        sync.RWMutex
	running   bool
	threadID  uint32
	bindings  []HotkeyBinding
	callback  func(HotkeyEvent)
	lastError string
	lastEvent *HotkeyEvent
	done      chan struct{}
}

func newPlatformHotkeyRegistrar() HotkeyRegistrar { return &windowsHotkeyRegistrar{} }

func (registrar *windowsHotkeyRegistrar) Start(ctx context.Context, bindings []HotkeyBinding, callback func(HotkeyEvent)) error {
	accelerators, err := validateBindings(bindings)
	if err != nil {
		return err
	}
	registrar.mu.Lock()
	if registrar.running {
		registrar.mu.Unlock()
		return fmt.Errorf("global hotkey registrar is already running")
	}
	registrar.running = true
	registrar.bindings = append([]HotkeyBinding(nil), bindings...)
	registrar.callback = callback
	registrar.lastError = ""
	registrar.done = make(chan struct{})
	started := make(chan error, 1)
	registrar.mu.Unlock()
	go registrar.loop(bindings, accelerators, started)
	if err := <-started; err != nil {
		return err
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = registrar.Stop()
		case <-registrar.done:
		}
	}()
	return nil
}

func (registrar *windowsHotkeyRegistrar) loop(bindings []HotkeyBinding, accelerators []Accelerator, started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, _, _ := procGetCurrentThreadID.Call()
	var message winMessage
	_, _, _ = procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmNoRemove)
	registrar.mu.Lock()
	registrar.threadID = uint32(thread)
	registrar.mu.Unlock()
	registered := 0
	for index, accelerator := range accelerators {
		result, _, callErr := procRegisterHotKey.Call(0, uintptr(hotkeyIDBase+index), uintptr(accelerator.Modifiers), uintptr(accelerator.VirtualKey))
		if result == 0 {
			for undo := 0; undo < registered; undo++ {
				_, _, _ = procUnregisterHotKey.Call(0, uintptr(hotkeyIDBase+undo))
			}
			err := fmt.Errorf("register %s: %w", accelerator.Canonical, callErr)
			registrar.finish(err)
			started <- err
			return
		}
		registered++
	}
	defer func() {
		for index := 0; index < registered; index++ {
			_, _, _ = procUnregisterHotKey.Call(0, uintptr(hotkeyIDBase+index))
		}
		registrar.finish(nil)
	}()
	started <- nil
	for {
		result, _, callErr := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			registrar.setError(fmt.Errorf("read hotkey message: %w", callErr))
			return
		}
		if result == 0 || message.Message == wmQuit {
			return
		}
		if message.Message != wmHotkey {
			continue
		}
		index := int(message.WParam) - hotkeyIDBase
		if index < 0 || index >= len(bindings) {
			continue
		}
		event := HotkeyEvent{Binding: bindings[index], At: time.Now()}
		registrar.mu.Lock()
		copyEvent := event
		registrar.lastEvent = &copyEvent
		callback := registrar.callback
		registrar.mu.Unlock()
		if callback != nil {
			go callback(event)
		}
	}
}

func (registrar *windowsHotkeyRegistrar) Stop() error {
	registrar.mu.RLock()
	if !registrar.running {
		registrar.mu.RUnlock()
		return nil
	}
	threadID, done := registrar.threadID, registrar.done
	registrar.mu.RUnlock()
	if threadID == 0 {
		return fmt.Errorf("global hotkey message thread is not ready")
	}
	result, _, callErr := procPostThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
	if result == 0 {
		return fmt.Errorf("stop global hotkey thread: %w", callErr)
	}
	<-done
	return nil
}

func (registrar *windowsHotkeyRegistrar) Status() HotkeyStatus {
	registrar.mu.RLock()
	defer registrar.mu.RUnlock()
	status := HotkeyStatus{Supported: true, Running: registrar.running, Bindings: append([]HotkeyBinding(nil), registrar.bindings...), LastError: registrar.lastError}
	if registrar.lastEvent != nil {
		event := *registrar.lastEvent
		status.LastEvent = &event
	}
	return status
}

func (registrar *windowsHotkeyRegistrar) finish(err error) {
	registrar.mu.Lock()
	if !registrar.running {
		registrar.mu.Unlock()
		return
	}
	registrar.running = false
	registrar.threadID = 0
	if err != nil {
		registrar.lastError = err.Error()
	}
	close(registrar.done)
	registrar.mu.Unlock()
}

func (registrar *windowsHotkeyRegistrar) setError(err error) {
	registrar.mu.Lock()
	registrar.lastError = err.Error()
	registrar.mu.Unlock()
}
