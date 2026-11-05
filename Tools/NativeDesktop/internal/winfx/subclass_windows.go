//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

const wmNCDestroy = 0x0082

var (
	ErrNilSubclassCallback = errors.New("winfx: window subclass callback is nil")
	ErrSubclassExists      = errors.New("winfx: window subclass id is already installed")

	subclassComctl32              = windows.NewLazySystemDLL("comctl32.dll")
	subclassSetWindowSubclassProc = subclassComctl32.NewProc("SetWindowSubclass")
	subclassRemoveProc            = subclassComctl32.NewProc("RemoveWindowSubclass")
	subclassDefProc               = subclassComctl32.NewProc("DefSubclassProc")

	subclassCallbackPointer = windows.NewCallback(subclassThunk)
	subclassMu              sync.RWMutex
	subclassCallbacks       = make(map[subclassKey]SubclassCallback)
)

type subclassKey struct {
	hwnd uintptr
	id   uintptr
}

// SubclassCallback handles messages for a SetWindowSubclass hook. Returning
// handled=false automatically forwards the message to DefSubclassProc.
type SubclassCallback func(hwnd, msg, wParam, lParam uintptr) (result uintptr, handled bool)

// SetWindowSubclass installs a ComCtl32 subclass on an existing HWND. It must
// be called on the thread that owns hwnd. callback is retained until
// RemoveWindowSubclass or WM_NCDESTROY, so callers do not need an additional
// runtime.KeepAlive. A panic in callback is recovered and treated as unhandled
// to avoid unwinding through a Windows callback frame.
func SetWindowSubclass(hwnd, id uintptr, callback SubclassCallback) error {
	if hwnd == 0 {
		return ErrInvalidWindow
	}
	if callback == nil {
		return ErrNilSubclassCallback
	}
	if err := subclassSetWindowSubclassProc.Find(); err != nil {
		return fmt.Errorf("resolve SetWindowSubclass: %w", err)
	}
	if err := subclassDefProc.Find(); err != nil {
		return fmt.Errorf("resolve DefSubclassProc: %w", err)
	}

	key := subclassKey{hwnd: hwnd, id: id}
	subclassMu.Lock()
	if _, exists := subclassCallbacks[key]; exists {
		subclassMu.Unlock()
		return fmt.Errorf("%w: hwnd=%#x id=%d", ErrSubclassExists, hwnd, id)
	}
	subclassCallbacks[key] = callback
	subclassMu.Unlock()

	ok, _, callErr := subclassSetWindowSubclassProc.Call(
		hwnd,
		subclassCallbackPointer,
		id,
		0,
	)
	if ok == 0 {
		subclassMu.Lock()
		delete(subclassCallbacks, key)
		subclassMu.Unlock()
		return boolCallError("SetWindowSubclass", callErr)
	}
	return nil
}

// RemoveWindowSubclass removes a hook previously installed by this package.
// It is idempotent. Call it on the HWND's owner thread before destroying the
// window; WM_NCDESTROY also releases the retained callback automatically.
func RemoveWindowSubclass(hwnd, id uintptr) error {
	if hwnd == 0 {
		return nil
	}
	key := subclassKey{hwnd: hwnd, id: id}
	subclassMu.RLock()
	_, exists := subclassCallbacks[key]
	subclassMu.RUnlock()
	if !exists {
		return nil
	}
	if err := subclassRemoveProc.Find(); err != nil {
		return fmt.Errorf("resolve RemoveWindowSubclass: %w", err)
	}
	ok, _, callErr := subclassRemoveProc.Call(hwnd, subclassCallbackPointer, id)
	if ok == 0 {
		return boolCallError("RemoveWindowSubclass", callErr)
	}
	subclassMu.Lock()
	delete(subclassCallbacks, key)
	subclassMu.Unlock()
	return nil
}

func subclassThunk(
	hwnd uintptr,
	msg uintptr,
	wParam uintptr,
	lParam uintptr,
	id uintptr,
	_ uintptr,
) uintptr {
	key := subclassKey{hwnd: hwnd, id: id}
	subclassMu.RLock()
	callback := subclassCallbacks[key]
	subclassMu.RUnlock()

	if msg == wmNCDestroy {
		// Destruction must always reach the original chain. Clean our map only
		// after DefSubclassProc has completed.
		if callback != nil {
			_, _ = callSubclassCallback(callback, hwnd, msg, wParam, lParam)
		}
		result := callDefSubclassProc(hwnd, msg, wParam, lParam)
		subclassMu.Lock()
		delete(subclassCallbacks, key)
		subclassMu.Unlock()
		return result
	}

	if callback != nil {
		if result, handled := callSubclassCallback(callback, hwnd, msg, wParam, lParam); handled {
			return result
		}
	}
	return callDefSubclassProc(hwnd, msg, wParam, lParam)
}

func callSubclassCallback(
	callback SubclassCallback,
	hwnd, msg, wParam, lParam uintptr,
) (result uintptr, handled bool) {
	defer func() {
		if recover() != nil {
			result = 0
			handled = false
		}
	}()
	return callback(hwnd, msg, wParam, lParam)
}

func callDefSubclassProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if subclassDefProc.Find() != nil {
		return 0
	}
	result, _, _ := subclassDefProc.Call(hwnd, msg, wParam, lParam)
	return result
}
