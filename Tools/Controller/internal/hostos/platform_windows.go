//go:build windows

package hostos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"pccontroller.local/controller/internal/productidentity"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	powrprof            = syscall.NewLazyDLL("powrprof.dll")
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	dxva2               = syscall.NewLazyDLL("dxva2.dll")
	procSendInput       = user32.NewProc("SendInput")
	procLockWorkStation = user32.NewProc("LockWorkStation")
	procMonitorFromWnd  = user32.NewProc("MonitorFromWindow")
	procSetSuspend      = powrprof.NewProc("SetSuspendState")
	procGetTickCount64  = kernel32.NewProc("GetTickCount64")
	procPhysicalCount   = dxva2.NewProc("GetNumberOfPhysicalMonitorsFromHMONITOR")
	procPhysicalList    = dxva2.NewProc("GetPhysicalMonitorsFromHMONITOR")
	procPhysicalDestroy = dxva2.NewProc("DestroyPhysicalMonitors")
	procBrightnessGet   = dxva2.NewProc("GetMonitorBrightness")
	procBrightnessSet   = dxva2.NewProc("SetMonitorBrightness")
)

const monitorDefaultToPrimary = 1

type keyboardInput struct {
	VirtualKey uint16
	ScanCode   uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

// mouseInput gives the INPUT union its native maximum size and alignment on
// both 32-bit and 64-bit Windows; its storage is reused as KEYBDINPUT below.
type mouseInput struct {
	DX, DY    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winInput struct {
	Type  uint32
	Mouse mouseInput
}

// physicalMonitor matches Win32 PHYSICAL_MONITOR. HANDLE width supplies the
// correct native alignment on both 32-bit and 64-bit builds.
type physicalMonitor struct {
	Handle      syscall.Handle
	Description [128]uint16
}

func sendKeyboardInput(code uint16, keyUp bool) error {
	input := winInput{Type: 1}
	keyboard := (*keyboardInput)(unsafe.Pointer(&input.Mouse))
	keyboard.VirtualKey = code
	if keyUp {
		keyboard.Flags = 0x0002
	}
	result, _, callErr := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&input)),
		unsafe.Sizeof(input),
	)
	if result != 1 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return fmt.Errorf("SendInput: %w", callErr)
		}
		return errors.New("SendInput accepted zero keyboard events")
	}
	return nil
}

func platformKeyDown(code uint16) error { return sendKeyboardInput(code, false) }
func platformKeyUp(code uint16) error   { return sendKeyboardInput(code, true) }

func platformPowerAction(ctx context.Context, action string) error {
	switch action {
	case "lock":
		result, _, callErr := procLockWorkStation.Call()
		if result == 0 {
			if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
				return fmt.Errorf("LockWorkStation: %w", callErr)
			}
			return errors.New("LockWorkStation was rejected")
		}
		return nil
	case "sleep", "hibernate":
		hibernate := uintptr(0)
		if action == "hibernate" {
			hibernate = 1
		}
		result, _, callErr := procSetSuspend.Call(hibernate, 0, 0)
		if result == 0 {
			if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
				return fmt.Errorf("SetSuspendState: %w", callErr)
			}
			return errors.New("SetSuspendState was rejected")
		}
		return nil
	case "shutdown", "restart", "logoff":
		argument := map[string]string{
			"shutdown": "/s", "restart": "/r", "logoff": "/l",
		}[action]
		arguments := []string{argument}
		if action != "logoff" {
			arguments = append(arguments, "/t", "0", "/d", "p:0:0", "/c", productidentity.DefaultTitle+" requested action")
		}
		output, err := exec.CommandContext(ctx, "shutdown.exe", arguments...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("shutdown.exe %s: %w (%s)", action, err, string(output))
		}
		return nil
	default:
		return fmt.Errorf("unsupported Windows power action %q", action)
	}
}

func platformMonitorBrightness(ctx context.Context) (BrightnessStatus, error) {
	if err := ctx.Err(); err != nil {
		return BrightnessStatus{}, err
	}
	monitors, err := primaryPhysicalMonitors()
	if err != nil {
		return BrightnessStatus{}, err
	}
	defer destroyPhysicalMonitors(monitors)
	var failures []error
	for index := range monitors {
		status, readErr := readPhysicalBrightness(&monitors[index])
		if readErr == nil {
			return status, nil
		}
		failures = append(failures, readErr)
	}
	return BrightnessStatus{}, fmt.Errorf(
		"DDC/CI brightness is unsupported by the primary display: %w",
		errors.Join(failures...),
	)
}

func platformSetMonitorBrightness(ctx context.Context, percent int) (BrightnessStatus, error) {
	if percent < 0 || percent > 100 {
		return BrightnessStatus{}, fmt.Errorf("monitor brightness %d is outside 0..100", percent)
	}
	if err := ctx.Err(); err != nil {
		return BrightnessStatus{}, err
	}
	monitors, err := primaryPhysicalMonitors()
	if err != nil {
		return BrightnessStatus{}, err
	}
	defer destroyPhysicalMonitors(monitors)
	var failures []error
	for index := range monitors {
		status, readErr := readPhysicalBrightness(&monitors[index])
		if readErr != nil {
			failures = append(failures, readErr)
			continue
		}
		rangeValue := status.RawMaximum - status.RawMinimum
		raw := status.RawMinimum
		if rangeValue != 0 {
			raw += uint32((uint64(rangeValue)*uint64(percent) + 50) / 100)
		}
		result, _, callErr := procBrightnessSet.Call(uintptr(monitors[index].Handle), uintptr(raw))
		if result == 0 {
			failures = append(failures, win32MonitorError("SetMonitorBrightness", status.Display, callErr))
			continue
		}
		status.RawCurrent = raw
		status.Percent = percent
		if refreshed, refreshErr := readPhysicalBrightness(&monitors[index]); refreshErr == nil {
			status = refreshed
		}
		return status, nil
	}
	return BrightnessStatus{}, fmt.Errorf(
		"DDC/CI brightness is unsupported by the primary display: %w",
		errors.Join(failures...),
	)
}

func primaryPhysicalMonitors() ([]physicalMonitor, error) {
	monitor, _, callErr := procMonitorFromWnd.Call(0, monitorDefaultToPrimary)
	if monitor == 0 {
		return nil, win32MonitorError("MonitorFromWindow", "primary display", callErr)
	}
	var count uint32
	result, _, callErr := procPhysicalCount.Call(monitor, uintptr(unsafe.Pointer(&count)))
	if result == 0 {
		return nil, win32MonitorError("GetNumberOfPhysicalMonitorsFromHMONITOR", "primary display", callErr)
	}
	if count == 0 || count > 32 {
		return nil, fmt.Errorf("primary display reported invalid physical-monitor count %d", count)
	}
	monitors := make([]physicalMonitor, count)
	result, _, callErr = procPhysicalList.Call(
		monitor,
		uintptr(count),
		uintptr(unsafe.Pointer(&monitors[0])),
	)
	if result == 0 {
		return nil, win32MonitorError("GetPhysicalMonitorsFromHMONITOR", "primary display", callErr)
	}
	return monitors, nil
}

func destroyPhysicalMonitors(monitors []physicalMonitor) {
	if len(monitors) == 0 {
		return
	}
	_, _, _ = procPhysicalDestroy.Call(
		uintptr(len(monitors)),
		uintptr(unsafe.Pointer(&monitors[0])),
	)
}

func readPhysicalBrightness(monitor *physicalMonitor) (BrightnessStatus, error) {
	name := syscall.UTF16ToString(monitor.Description[:])
	if name == "" {
		name = "primary display"
	}
	var minimum, current, maximum uint32
	result, _, callErr := procBrightnessGet.Call(
		uintptr(monitor.Handle),
		uintptr(unsafe.Pointer(&minimum)),
		uintptr(unsafe.Pointer(&current)),
		uintptr(unsafe.Pointer(&maximum)),
	)
	if result == 0 {
		return BrightnessStatus{}, win32MonitorError("GetMonitorBrightness", name, callErr)
	}
	if maximum <= minimum || current < minimum || current > maximum {
		return BrightnessStatus{}, fmt.Errorf(
			"GetMonitorBrightness returned invalid range %d..%d current=%d for %s",
			minimum, maximum, current, name,
		)
	}
	percent := int((uint64(current-minimum)*100 + uint64(maximum-minimum)/2) /
		uint64(maximum-minimum))
	return BrightnessStatus{
		Percent: percent, RawMinimum: minimum, RawCurrent: current,
		RawMaximum: maximum, Display: name,
	}, nil
}

func win32MonitorError(operation, display string, callErr error) error {
	if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
		return fmt.Errorf("%s(%s): %w", operation, display, callErr)
	}
	return fmt.Errorf("%s(%s) returned false", operation, display)
}

func platformUptimeMS() uint64 {
	value, _, _ := procGetTickCount64.Call()
	return uint64(value)
}
