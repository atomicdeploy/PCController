//go:build windows

package hostui

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	coinitApartmentThreaded = uint32(0x2)
	roInitMultithreaded     = uint32(0x1)
)

var (
	ole32DLL                = windows.NewLazySystemDLL("ole32.dll")
	combaseDLL              = windows.NewLazySystemDLL("combase.dll")
	procCoInitializeEx      = ole32DLL.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32DLL.NewProc("CoUninitialize")
	procCoCreateInstance    = ole32DLL.NewProc("CoCreateInstance")
	procRoInitialize        = combaseDLL.NewProc("RoInitialize")
	procRoUninitialize      = combaseDLL.NewProc("RoUninitialize")
	procRoGetActivation     = combaseDLL.NewProc("RoGetActivationFactory")
	procRoActivateInstance  = combaseDLL.NewProc("RoActivateInstance")
	procWindowsCreateString = combaseDLL.NewProc("WindowsCreateString")
	procWindowsDeleteString = combaseDLL.NewProc("WindowsDeleteString")
)

type hresult uint32

func (result hresult) failed() bool { return int32(result) < 0 }

func (result hresult) error(operation string) error {
	if !result.failed() {
		return nil
	}
	return fmt.Errorf("%s failed (HRESULT 0x%08X)", operation, uint32(result))
}

// runWindowsApartment executes native COM/WinRT work on a dedicated locked OS
// thread, so callers never inherit or disturb the apartment used by the TUI.
func runWindowsApartment(
	ctx context.Context,
	initialize func() hresult,
	uninitialize func(),
	work func() error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hr := initialize()
		if hr.failed() {
			done <- hr.error("initialize Windows apartment")
			return
		}
		defer uninitialize()
		if err := ctx.Err(); err != nil {
			done <- err
			return
		}
		done <- work()
	}()
	// Native ABI calls cannot be safely interrupted after dispatch. Waiting for
	// the dedicated apartment to unwind avoids leaving an OS-thread worker that
	// can race caller-owned state after a context deadline. Bounded surfaces
	// such as TaskDialog observe ctx themselves and close before returning.
	return <-done
}

func runCOMApartment(ctx context.Context, work func() error) error {
	if err := requireWindowsProcedures(
		"Windows Shell COM", procCoInitializeEx, procCoUninitialize, procCoCreateInstance,
	); err != nil {
		return err
	}
	return runWindowsApartment(ctx, func() hresult {
		result, _, _ := procCoInitializeEx.Call(0, uintptr(coinitApartmentThreaded))
		return hresult(result)
	}, func() { procCoUninitialize.Call() }, work)
}

func runWinRTApartment(ctx context.Context, work func() error) error {
	if err := requireWindowsProcedures(
		"Windows Runtime notifications", procRoInitialize, procRoUninitialize,
		procRoGetActivation, procRoActivateInstance, procWindowsCreateString,
		procWindowsDeleteString,
	); err != nil {
		return err
	}
	return runWindowsApartment(ctx, func() hresult {
		result, _, _ := procRoInitialize.Call(uintptr(roInitMultithreaded))
		return hresult(result)
	}, func() { procRoUninitialize.Call() }, work)
}

func requireWindowsProcedures(capability string, procedures ...*windows.LazyProc) error {
	for _, procedure := range procedures {
		if err := procedure.Find(); err != nil {
			return fmt.Errorf("%s is unavailable: %w", capability, err)
		}
	}
	return nil
}

func callCOM(object unsafe.Pointer, method int, arguments ...uintptr) hresult {
	if object == nil {
		return hresult(0x80004003) // E_POINTER
	}
	vtable := *(*unsafe.Pointer)(object)
	entry := (*[64]uintptr)(vtable)[method]
	params := make([]uintptr, 0, len(arguments)+1)
	params = append(params, uintptr(object))
	params = append(params, arguments...)
	result, _, _ := syscall.SyscallN(entry, params...)
	runtime.KeepAlive(object)
	return hresult(result)
}

func releaseCOM(object unsafe.Pointer) {
	if object != nil {
		callCOM(object, 2)
	}
}

func queryCOM(object unsafe.Pointer, iid windows.GUID) (unsafe.Pointer, error) {
	var result unsafe.Pointer
	hr := callCOM(object, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&result)))
	if err := hr.error("query COM interface"); err != nil {
		return nil, err
	}
	return result, nil
}

func createCOMInstance(classID, interfaceID windows.GUID) (unsafe.Pointer, error) {
	const clsctxInprocServer = uintptr(0x1)
	var result unsafe.Pointer
	value, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&classID)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&interfaceID)), uintptr(unsafe.Pointer(&result)),
	)
	if err := hresult(value).error("create COM instance"); err != nil {
		return nil, err
	}
	return result, nil
}

type windowsHString uintptr

func newWindowsHString(value string) (windowsHString, error) {
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		return 0, err
	}
	var result windowsHString
	status, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)-1),
		uintptr(unsafe.Pointer(&result)),
	)
	runtime.KeepAlive(encoded)
	if err := hresult(status).error("create Windows Runtime string"); err != nil {
		return 0, err
	}
	return result, nil
}

func (value windowsHString) delete() {
	if value != 0 {
		procWindowsDeleteString.Call(uintptr(value))
	}
}

func winRTActivationFactory(runtimeClass string, iid windows.GUID) (unsafe.Pointer, error) {
	className, err := newWindowsHString(runtimeClass)
	if err != nil {
		return nil, err
	}
	defer className.delete()
	var result unsafe.Pointer
	status, _, _ := procRoGetActivation.Call(
		uintptr(className), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&result)),
	)
	if err := hresult(status).error("get Windows Runtime activation factory"); err != nil {
		return nil, err
	}
	return result, nil
}

func activateWinRTInstance(runtimeClass string) (unsafe.Pointer, error) {
	className, err := newWindowsHString(runtimeClass)
	if err != nil {
		return nil, err
	}
	defer className.delete()
	var result unsafe.Pointer
	status, _, _ := procRoActivateInstance.Call(
		uintptr(className), uintptr(unsafe.Pointer(&result)),
	)
	if err := hresult(status).error("activate Windows Runtime class"); err != nil {
		return nil, err
	}
	return result, nil
}
