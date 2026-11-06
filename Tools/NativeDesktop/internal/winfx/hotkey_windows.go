//go:build windows

package winfx

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const (
	maxApplicationHotkeyID = 0xBFFF
	vkF12                  = 0x7B
)

var (
	hotkeyUser32           = windows.NewLazySystemDLL("user32.dll")
	hotkeyRegisterHotKey   = hotkeyUser32.NewProc("RegisterHotKey")
	hotkeyUnregisterHotKey = hotkeyUser32.NewProc("UnregisterHotKey")
)

// RegisterHotKey registers a parsed chord against hwnd. It always requests
// MOD_NOREPEAT so holding the chord cannot flood device commands.
func RegisterHotKey(hwnd uintptr, id int32, hotkey Hotkey) error {
	if id < 0 || id > maxApplicationHotkeyID {
		return fmt.Errorf("winfx: hotkey id %d is outside application range 0..%d", id, maxApplicationHotkeyID)
	}
	if hotkey.Key == 0 {
		return fmt.Errorf("%w: missing virtual key", ErrInvalidHotkey)
	}
	if hotkey.Key == vkF12 {
		return fmt.Errorf("%w: F12 is reserved for the debugger", ErrReservedHotkey)
	}
	if err := hotkeyRegisterHotKey.Find(); err != nil {
		return fmt.Errorf("resolve RegisterHotKey: %w", err)
	}
	ok, _, callErr := hotkeyRegisterHotKey.Call(
		hwnd,
		uintptr(id),
		uintptr(hotkey.Modifiers|ModNoRepeat),
		uintptr(hotkey.Key),
	)
	if ok == 0 {
		return boolCallError("RegisterHotKey("+hotkey.String()+")", callErr)
	}
	return nil
}

// RegisterHotkeyChord parses then registers a user-facing chord.
func RegisterHotkeyChord(hwnd uintptr, id int32, chord string) (Hotkey, error) {
	hotkey, err := ParseHotkey(chord)
	if err != nil {
		return Hotkey{}, err
	}
	if err := RegisterHotKey(hwnd, id, hotkey); err != nil {
		return Hotkey{}, err
	}
	return hotkey, nil
}

// UnregisterHotKey unregisters an application hotkey ID from hwnd.
func UnregisterHotKey(hwnd uintptr, id int32) error {
	if id < 0 || id > maxApplicationHotkeyID {
		return fmt.Errorf("winfx: hotkey id %d is outside application range 0..%d", id, maxApplicationHotkeyID)
	}
	if err := hotkeyUnregisterHotKey.Find(); err != nil {
		return fmt.Errorf("resolve UnregisterHotKey: %w", err)
	}
	ok, _, callErr := hotkeyUnregisterHotKey.Call(hwnd, uintptr(id))
	if ok == 0 {
		return boolCallError("UnregisterHotKey", callErr)
	}
	return nil
}
