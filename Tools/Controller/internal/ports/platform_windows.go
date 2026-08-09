//go:build windows

package ports

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var cmLocateDevNodeW = windows.NewLazySystemDLL(
	"cfgmgr32.dll",
).NewProc("CM_Locate_DevNodeW")

func platformEnumerationSource() string {
	return "go.bug.st/serial detailed enumerator; Windows SetupAPI Ports class with DIGCF_PRESENT"
}

type registryDevice struct {
	FriendlyName string
	InstanceID   string
	SerialNumber string
	Present      bool
}

func enrichPlatform(values []Info) []Info {
	byPort := make(map[string]int, len(values))
	for index := range values {
		byPort[strings.ToUpper(values[index].Name)] = index
	}
	candidates := make(map[string][]registryDevice)
	usb, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\USB`,
		registry.READ,
	)
	if err != nil {
		return values
	}
	defer usb.Close()
	hardwareIDs, err := usb.ReadSubKeyNames(-1)
	if err != nil {
		return values
	}
	for _, hardwareID := range hardwareIDs {
		device, openErr := registry.OpenKey(usb, hardwareID, registry.READ)
		if openErr != nil {
			continue
		}
		instances, readErr := device.ReadSubKeyNames(-1)
		for _, instance := range instances {
			instanceKey, instanceErr := registry.OpenKey(
				device,
				instance,
				registry.READ,
			)
			if instanceErr != nil {
				continue
			}
			parameters, parametersErr := registry.OpenKey(
				instanceKey,
				"Device Parameters",
				registry.READ,
			)
			if parametersErr != nil {
				instanceKey.Close()
				continue
			}
			portName, _, portErr := parameters.GetStringValue("PortName")
			parameters.Close()
			if portErr == nil {
				portKey := strings.ToUpper(portName)
				if _, ok := byPort[portKey]; ok {
					instanceID := `USB\` + hardwareID + `\` + instance
					candidate := registryDevice{
						InstanceID: instanceID,
						Present:    deviceInstancePresent(instanceID),
					}
					if friendly, _, valueErr :=
						instanceKey.GetStringValue("FriendlyName"); valueErr == nil {
						candidate.FriendlyName = stableFriendlyName(friendly)
					}
					if !strings.Contains(instance, "&") {
						candidate.SerialNumber = instance
					}
					candidates[portKey] = append(
						candidates[portKey],
						candidate,
					)
				}
			}
			instanceKey.Close()
		}
		device.Close()
		if readErr != nil {
			continue
		}
	}
	for portKey, devices := range candidates {
		index := byPort[portKey]
		selected := selectRegistryDevice(devices)
		if selected.FriendlyName != "" {
			values[index].FriendlyName = selected.FriendlyName
		}
		if selected.Present {
			values[index].InstanceID = selected.InstanceID
			if values[index].SerialNumber == "" {
				values[index].SerialNumber = selected.SerialNumber
			}
		}
	}
	return values
}

// deviceInstancePresent asks Configuration Manager for the devnode using
// CM_LOCATE_DEVNODE_NORMAL. Unlike Enum registry keys, that mode returns a
// handle only for a device currently configured in the live device tree; a
// historical/phantom instance is deliberately not accepted.
func deviceInstancePresent(instanceID string) bool {
	if err := cmLocateDevNodeW.Find(); err != nil {
		return false
	}
	identifier, err := windows.UTF16PtrFromString(instanceID)
	if err != nil {
		return false
	}
	var deviceInstance uint32
	result, _, _ := cmLocateDevNodeW.Call(
		uintptr(unsafe.Pointer(&deviceInstance)),
		uintptr(unsafe.Pointer(identifier)),
		0, // CM_LOCATE_DEVNODE_NORMAL
	)
	return uint32(result) == 0 // CR_SUCCESS
}

// selectRegistryDevice returns a stable instance only when Configuration
// Manager confirms exactly one present devnode for the COM name. If zero or
// multiple current instances claim it, retaining no instance is safer than
// persisting an arbitrary historical registry entry.
func selectRegistryDevice(devices []registryDevice) registryDevice {
	var present []registryDevice
	for _, device := range devices {
		if device.Present {
			present = append(present, device)
		}
	}
	if len(present) == 1 {
		return present[0]
	}

	// A friendly name is descriptive rather than a stable identifier. Keep it
	// only when every non-empty registry value agrees.
	var result registryDevice
	for _, device := range devices {
		name := strings.TrimSpace(device.FriendlyName)
		if name == "" {
			continue
		}
		if result.FriendlyName == "" {
			result.FriendlyName = name
			continue
		}
		if !strings.EqualFold(result.FriendlyName, name) {
			result.FriendlyName = ""
			break
		}
	}
	return result
}

func watchPlatformChanges(ctx context.Context) (<-chan Change, error) {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DEVICEMAP\SERIALCOMM`,
		registry.NOTIFY,
	)
	if err != nil {
		return nil, fmt.Errorf("open Windows serial device map: %w", err)
	}
	deviceEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		key.Close()
		return nil, fmt.Errorf("create serial notification event: %w", err)
	}
	stopEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		windows.CloseHandle(deviceEvent)
		key.Close()
		return nil, fmt.Errorf("create serial notification stop event: %w", err)
	}
	changes := make(chan Change, 1)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = windows.SetEvent(stopEvent)
		case <-done:
		}
	}()
	go func() {
		defer close(done)
		defer close(changes)
		defer key.Close()
		defer windows.CloseHandle(deviceEvent)
		defer windows.CloseHandle(stopEvent)
		for {
			err := windows.RegNotifyChangeKeyValue(
				windows.Handle(key),
				false,
				windows.REG_NOTIFY_CHANGE_NAME|
					windows.REG_NOTIFY_CHANGE_LAST_SET|
					windows.REG_NOTIFY_THREAD_AGNOSTIC,
				deviceEvent,
				true,
			)
			if err != nil {
				return
			}
			signaled, waitErr := windows.WaitForMultipleObjects(
				[]windows.Handle{deviceEvent, stopEvent},
				false,
				windows.INFINITE,
			)
			if waitErr != nil || signaled == windows.WAIT_OBJECT_0+1 {
				return
			}
			if signaled != windows.WAIT_OBJECT_0 {
				return
			}
			select {
			case changes <- Change{
				At:     time.Now(),
				Reason: "Windows Plug-and-Play serial map changed",
			}:
			default:
			}
		}
	}()
	return changes, nil
}
