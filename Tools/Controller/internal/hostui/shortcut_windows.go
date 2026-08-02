//go:build windows

package hostui

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32ShortcutDLL     = windows.NewLazySystemDLL("shell32.dll")
	procSHGetPropertyStore = shell32ShortcutDLL.NewProc("SHGetPropertyStoreFromParsingName")
	procPropVariantClear   = ole32DLL.NewProc("PropVariantClear")
	classShellLink         = windows.GUID{Data1: 0x00021401, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidShellLinkW          = windows.GUID{Data1: 0x000214F9, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPersistFile         = windows.GUID{Data1: 0x0000010B, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPropertyStore       = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446, Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}
	propertyAppUserModelID = propertyKey{
		FormatID:   windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}},
		PropertyID: 5,
	}
)

type propertyKey struct {
	FormatID   windows.GUID
	PropertyID uint32
}

// propVariant models the pointer-bearing VT_LPWSTR case on both 32-bit and
// 64-bit Windows. The two-word value area matches the native union size.
type propVariant struct {
	ValueType uint16
	Reserved  [3]uint16
	Value     [2]unsafe.Pointer
}

type windowsShortcut struct {
	Target    string
	Arguments string
}

func createWindowsShortcut(executable, shortcut, appID, displayName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return runCOMApartment(ctx, func() error {
		link, err := createCOMInstance(classShellLink, iidShellLinkW)
		if err != nil {
			return fmt.Errorf("create Shell link: %w", err)
		}
		defer releaseCOM(link)

		if err := setShellLinkString(link, 20, executable, "set shortcut target"); err != nil {
			return err
		}
		if err := setShellLinkString(link, 11, "web", "set shortcut arguments"); err != nil {
			return err
		}
		if err := setShellLinkString(link, 9, filepath.Dir(executable), "set shortcut working directory"); err != nil {
			return err
		}
		if err := setShellLinkString(link, 7, displayName, "set shortcut description"); err != nil {
			return err
		}
		icon, err := windows.UTF16PtrFromString(executable)
		if err != nil {
			return err
		}
		if err := callCOM(link, 17, uintptr(unsafe.Pointer(icon)), 0).error("set shortcut icon"); err != nil {
			return err
		}
		runtime.KeepAlive(icon)

		persist, err := queryCOM(link, iidPersistFile)
		if err != nil {
			return fmt.Errorf("open shortcut persistence interface: %w", err)
		}
		defer releaseCOM(persist)
		path, err := windows.UTF16PtrFromString(shortcut)
		if err != nil {
			return err
		}
		saveErr := callCOM(persist, 6, uintptr(unsafe.Pointer(path)), 1).error("save Start-menu shortcut")
		runtime.KeepAlive(path)
		if saveErr != nil {
			return saveErr
		}

		// Some Shell builds do not expose IPropertyStore directly from
		// IShellLink. The documented property-system entry point works against
		// the saved .lnk on all supported desktop Windows versions.
		store, err := shortcutPropertyStore(shortcut)
		if err != nil {
			return err
		}
		defer releaseCOM(store)
		appIDPointer, err := windows.UTF16PtrFromString(appID)
		if err != nil {
			return err
		}
		value := propVariant{ValueType: 31} // VT_LPWSTR
		value.Value[0] = unsafe.Pointer(appIDPointer)
		setErr := callCOM(
			store, 6, uintptr(unsafe.Pointer(&propertyAppUserModelID)), uintptr(unsafe.Pointer(&value)),
		).error("set shortcut AppUserModelID")
		if setErr == nil {
			setErr = callCOM(store, 7).error("commit shortcut properties")
		}
		runtime.KeepAlive(appIDPointer)
		return setErr
	})
}

func shortcutPropertyStore(shortcut string) (unsafe.Pointer, error) {
	if err := requireWindowsProcedures(
		"Windows shortcut property store", procSHGetPropertyStore, procPropVariantClear,
	); err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(shortcut)
	if err != nil {
		return nil, err
	}
	var store unsafe.Pointer
	status, _, _ := procSHGetPropertyStore.Call(
		uintptr(unsafe.Pointer(path)), 0, 2, // GPS_READWRITE
		uintptr(unsafe.Pointer(&iidPropertyStore)), uintptr(unsafe.Pointer(&store)),
	)
	runtime.KeepAlive(path)
	if err := hresult(status).error("open shortcut property store"); err != nil {
		return nil, err
	}
	return store, nil
}

func shortcutAppUserModelID(shortcut string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var appID string
	err := runCOMApartment(ctx, func() error {
		store, err := shortcutPropertyStore(shortcut)
		if err != nil {
			return err
		}
		defer releaseCOM(store)
		var value propVariant
		if err := callCOM(
			store, 5, uintptr(unsafe.Pointer(&propertyAppUserModelID)), uintptr(unsafe.Pointer(&value)),
		).error("read shortcut AppUserModelID"); err != nil {
			return err
		}
		defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&value)))
		if value.ValueType != 31 || value.Value[0] == nil {
			return fmt.Errorf("shortcut AppUserModelID has unexpected property type %d", value.ValueType)
		}
		appID = windows.UTF16PtrToString((*uint16)(value.Value[0]))
		return nil
	})
	return appID, err
}

func inspectWindowsShortcut(shortcut string) (windowsShortcut, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var result windowsShortcut
	err := runCOMApartment(ctx, func() error {
		link, err := createCOMInstance(classShellLink, iidShellLinkW)
		if err != nil {
			return fmt.Errorf("create Shell link reader: %w", err)
		}
		defer releaseCOM(link)
		persist, err := queryCOM(link, iidPersistFile)
		if err != nil {
			return fmt.Errorf("open shortcut persistence interface: %w", err)
		}
		path, err := windows.UTF16PtrFromString(shortcut)
		if err != nil {
			releaseCOM(persist)
			return err
		}
		loadErr := callCOM(persist, 5, uintptr(unsafe.Pointer(path)), 0).error("load Start-menu shortcut")
		runtime.KeepAlive(path)
		releaseCOM(persist)
		if loadErr != nil {
			return loadErr
		}

		target := make([]uint16, windows.MAX_PATH)
		if err := callCOM(
			link, 3, uintptr(unsafe.Pointer(&target[0])), uintptr(len(target)), 0, 4,
		).error("read shortcut target"); err != nil {
			return err
		}
		arguments := make([]uint16, 4096)
		if err := callCOM(
			link, 10, uintptr(unsafe.Pointer(&arguments[0])), uintptr(len(arguments)),
		).error("read shortcut arguments"); err != nil {
			return err
		}
		result.Target = windows.UTF16ToString(target)
		result.Arguments = windows.UTF16ToString(arguments)
		return nil
	})
	return result, err
}

func setShellLinkString(link unsafe.Pointer, method int, value, operation string) error {
	pointer, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	callErr := callCOM(link, method, uintptr(unsafe.Pointer(pointer))).error(operation)
	runtime.KeepAlive(pointer)
	return callErr
}

func shortcutOwnedBy(executable string, shortcut windowsShortcut) bool {
	if !sameWindowsPath(shortcut.Target, executable) {
		return false
	}
	arguments := strings.TrimSpace(shortcut.Arguments)
	return strings.EqualFold(arguments, "web") || strings.EqualFold(arguments, "tui")
}
