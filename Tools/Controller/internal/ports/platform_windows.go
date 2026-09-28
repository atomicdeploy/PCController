//go:build windows

package ports

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var cmLocateDevNodeW = windows.NewLazySystemDLL(
	"cfgmgr32.dll",
).NewProc("CM_Locate_DevNodeW")

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

func listPlatformHardwareProblems(filter Filter) ([]HardwareProblem, error) {
	devices, err := windows.SetupDiGetClassDevsEx(
		nil,
		"",
		0,
		windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES,
		0,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("enumerate Windows device problems: %w", err)
	}
	defer devices.Close()

	related := relatedRegistryInstanceIDs(filter)
	observedAt := time.Now()
	var problems []HardwareProblem
	for index := 0; ; index++ {
		device, enumErr := devices.EnumDeviceInfo(index)
		if enumErr == windows.ERROR_NO_MORE_ITEMS {
			break
		}
		if enumErr != nil {
			return nil, fmt.Errorf("enumerate Windows device problem %d: %w", index, enumErr)
		}
		var status, problemNumber uint32
		if statusErr := windows.CM_Get_DevNode_Status(
			&status,
			&problemNumber,
			device.DevInst,
			0,
		); statusErr != nil || problemNumber == 0 || status&windows.DN_HAS_PROBLEM == 0 {
			continue
		}
		deviceID, idErr := windows.SetupDiGetDeviceInstanceId(devices, device)
		if idErr != nil {
			continue
		}
		hardwareIDs := deviceRegistryStrings(devices, device, windows.SPDRP_HARDWAREID)
		if !hardwareProblemMatches(deviceID, hardwareIDs, filter, related) {
			continue
		}
		classificationIDs := append(append([]string(nil), hardwareIDs...), deviceID)
		code := classifyHardwareProblem(problemNumber, classificationIDs)
		severity := "error"
		if code == HardwareProblemDisabled || code == HardwareProblemRemovalPending {
			severity = "warning"
		}
		description := deviceRegistryString(devices, device, windows.SPDRP_FRIENDLYNAME)
		if description == "" {
			description = deviceRegistryString(devices, device, windows.SPDRP_DEVICEDESC)
		}
		problems = append(problems, HardwareProblem{
			Code:          code,
			Severity:      severity,
			OSProblemCode: problemNumber,
			DeviceID:      deviceID,
			HardwareIDs:   hardwareIDs,
			Description:   description,
			Class:         deviceRegistryString(devices, device, windows.SPDRP_CLASS),
			Location:      deviceRegistryString(devices, device, windows.SPDRP_LOCATION_INFORMATION),
			LocationPaths: deviceRegistryStrings(devices, device, windows.SPDRP_LOCATION_PATHS),
			ObservedAt:    observedAt,
		})
	}
	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].Code != problems[j].Code {
			return problems[i].Code < problems[j].Code
		}
		return problems[i].DeviceID < problems[j].DeviceID
	})
	return problems, nil
}

func deviceRegistryString(
	devices windows.DevInfo,
	device *windows.DevInfoData,
	property windows.SPDRP,
) string {
	value, err := windows.SetupDiGetDeviceRegistryProperty(devices, device, property)
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func deviceRegistryStrings(
	devices windows.DevInfo,
	device *windows.DevInfoData,
	property windows.SPDRP,
) []string {
	value, err := windows.SetupDiGetDeviceRegistryProperty(devices, device, property)
	if err != nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
		return result
	case string:
		if typed = strings.TrimSpace(typed); typed != "" {
			return []string{typed}
		}
	}
	return nil
}

func relatedRegistryInstanceIDs(filter Filter) []string {
	ports := []string{filter.Port, filter.Preferred.Port}
	seen := make(map[string]bool)
	var result []string
	for _, port := range ports {
		port = strings.TrimSpace(port)
		if port == "" {
			continue
		}
		for _, instanceID := range registryInstanceIDsForPort(port) {
			key := strings.ToUpper(instanceID)
			if !seen[key] {
				seen[key] = true
				result = append(result, instanceID)
			}
		}
	}
	return result
}

// registryInstanceIDsForPort deliberately includes phantom Enum entries. A
// failed USB descriptor cannot expose its former COM name, so the physical
// instance suffix of the remembered port is the trustworthy correlation link.
func registryInstanceIDsForPort(port string) []string {
	usb, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\USB`,
		registry.READ,
	)
	if err != nil {
		return nil
	}
	defer usb.Close()
	hardwareIDs, err := usb.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var result []string
	for _, hardwareID := range hardwareIDs {
		device, openErr := registry.OpenKey(usb, hardwareID, registry.READ)
		if openErr != nil {
			continue
		}
		instances, _ := device.ReadSubKeyNames(-1)
		for _, instance := range instances {
			instanceKey, instanceErr := registry.OpenKey(device, instance, registry.READ)
			if instanceErr != nil {
				continue
			}
			parameters, parametersErr := registry.OpenKey(instanceKey, "Device Parameters", registry.READ)
			instanceKey.Close()
			if parametersErr != nil {
				continue
			}
			portName, _, valueErr := parameters.GetStringValue("PortName")
			parameters.Close()
			if valueErr == nil && strings.EqualFold(strings.TrimSpace(portName), port) {
				result = append(result, `USB\`+hardwareID+`\`+instance)
			}
		}
		device.Close()
	}
	return result
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
