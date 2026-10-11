package control

import (
	"fmt"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"slices"
	"sort"
	"strings"
)

// Empty means none. All, semantic control keys and raw opcodes are OR-selected.
// A raw opcode deliberately selects every target addressed by that opcode.
type CaptureSelection struct {
	All         bool     `json:"all"`
	ControlKeys []string `json:"control_keys"`
	Opcodes     []uint16 `json:"opcodes"`
}
type CaptureOpcode struct {
	Opcode       byte     `json:"opcode"`
	Name         string   `json:"name"`
	CaptureModes []string `json:"capture_modes"`
}
type RecordingCapabilities struct {
	Controls []appconfig.ControlDescriptor `json:"controls"`
	Opcodes  []CaptureOpcode               `json:"opcodes"`
}

func (capabilities RecordingCapabilities) clone() RecordingCapabilities {
	capabilities.Controls = append([]appconfig.ControlDescriptor{}, capabilities.Controls...)
	for i := range capabilities.Controls {
		capabilities.Controls[i].Actions = append([]appconfig.ActionDescriptor{}, capabilities.Controls[i].Actions...)
	}
	capabilities.Opcodes = append([]CaptureOpcode{}, capabilities.Opcodes...)
	for i := range capabilities.Opcodes {
		capabilities.Opcodes[i].CaptureModes = append([]string{}, capabilities.Opcodes[i].CaptureModes...)
	}
	return capabilities
}

func (s CaptureSelection) key(key string) bool {
	for _, v := range s.ControlKeys {
		if v == key {
			return true
		}
	}
	return false
}
func (s CaptureSelection) opcode(op byte) bool {
	if s.All {
		return true
	}
	for _, v := range s.Opcodes {
		if v == uint16(op) {
			return true
		}
	}
	return false
}
func (s CaptureSelection) clone() CaptureSelection {
	s.ControlKeys = append([]string{}, s.ControlKeys...)
	s.Opcodes = append([]uint16{}, s.Opcodes...)
	return s
}
func firstCaptureSelection(values []CaptureSelection) CaptureSelection {
	if len(values) > 0 {
		return values[0].clone()
	}
	return CaptureSelection{ControlKeys: []string{}, Opcodes: []uint16{}}
}
func relayCaptureOpcode(op byte) bool {
	return op == native.OpRelaySet || op == native.OpRelaySide || op == native.OpRelayAllOff
}

// The presentation catalog may grow independently of evidence capture. Only
// advertise keys that the targeted recorder can actually map to operations.
func selectableCaptureControls(controls []appconfig.ControlDescriptor) []appconfig.ControlDescriptor {
	result := make([]appconfig.ControlDescriptor, 0, len(controls))
	for _, descriptor := range controls {
		supported := false
		switch descriptor.Kind {
		case "seat":
			supported = descriptor.Key == "seat.a" || descriptor.Key == "seat.b"
		case "relay":
			for n := 1; n <= 8; n++ {
				supported = supported || descriptor.Key == fmt.Sprintf("relay.%d", n)
			}
		case "mosfet":
			for n := 0; n < 16; n++ {
				supported = supported || descriptor.Key == fmt.Sprintf("pwm.%d", n)
			}
		}
		if supported {
			result = append(result, descriptor)
		}
	}
	return result
}

func (runner *MacroRunner) recordingCapabilities() RecordingCapabilities {
	config := appconfig.Defaults()
	if runner.hostConfig != nil {
		config = runner.hostConfig()
	}
	runner.runtime.mu.RLock()
	port := runner.runtime.port
	runner.runtime.mu.RUnlock()
	if port.SerialNumber == "" && port.InstanceID == "" && port.Name == "" && config.Connection.LastDevice != nil {
		port.SerialNumber, port.InstanceID, port.Name = config.Connection.LastDevice.SerialNumber, config.Connection.LastDevice.InstanceID, config.Connection.LastDevice.Port
	}
	identity := appconfig.ResolveBoardIdentity(port.SerialNumber, port.InstanceID, port.Name)
	profile := config.BoardProfiles[identity.Value]
	_, controls := appconfig.ProfileDescriptors(profile.Mode, profile.ExposeRawRelays, config.UI.PeripheralNames, profile.Presentation)
	result := RecordingCapabilities{Controls: selectableCaptureControls(controls), Opcodes: []CaptureOpcode{}}
	for n := 0; n < 256; n++ {
		op := byte(n)
		if op == native.OpRelaySide && appconfig.NormalizeBoardMode(profile.Mode) != appconfig.BoardModeCinemaSeatMotion {
			continue
		}
		modes := []string{}
		if hostRecordableOpcode(op) {
			modes = append(modes, "automatic")
		}
		if macroQueueableOpcode(op) {
			modes = append(modes, "device-clock")
		}
		if relayCaptureOpcode(op) {
			modes = append(modes, "board-retained")
		}
		if len(modes) > 0 {
			result.Opcodes = append(result.Opcodes, CaptureOpcode{op, native.OpcodeName(op), modes})
		}
	}
	return result
}
func (runner *MacroRunner) validateCaptureSelection(selection CaptureSelection, mode string, retained bool) (CaptureSelection, error) {
	if len(selection.ControlKeys) > appconfig.MaxPeripheralNames || len(selection.Opcodes) > 256 {
		return selection, fmt.Errorf("capture selection exceeds advertised selection capacity")
	}
	selection = selection.clone()
	available := map[string]bool{}
	capabilities := runner.recordingCapabilities()
	for _, control := range capabilities.Controls {
		available[control.Key] = true
	}
	for _, key := range selection.ControlKeys {
		if !available[key] {
			return selection, fmt.Errorf("capture control %q is not advertised by the active board profile", key)
		}
		if retained && !(strings.HasPrefix(key, "relay.") || strings.HasPrefix(key, "seat.")) {
			return selection, fmt.Errorf("board-retained capture cannot select %s", key)
		}
	}
	for _, value := range selection.Opcodes {
		if value > 255 {
			return selection, fmt.Errorf("capture opcode must be 0..255")
		}
		op := byte(value)
		advertised := false
		for _, descriptor := range capabilities.Opcodes {
			if descriptor.Opcode == op {
				advertised = true
				break
			}
		}
		if !advertised {
			return selection, fmt.Errorf("capture opcode %s is not advertised by the active board profile", native.OpcodeName(op))
		}
		allowed := hostRecordableOpcode(op)
		if mode == macroModeMCU {
			allowed = macroQueueableOpcode(op)
		}
		if retained {
			allowed = relayCaptureOpcode(op)
		}
		if !allowed {
			return selection, fmt.Errorf("opcode %s is not recordable in this capture mode", native.OpcodeName(op))
		}
	}
	sort.Strings(selection.ControlKeys)
	sort.Slice(selection.Opcodes, func(i, j int) bool { return selection.Opcodes[i] < selection.Opcodes[j] })
	selection.ControlKeys = slices.Compact(selection.ControlKeys)
	selection.Opcodes = slices.Compact(selection.Opcodes)
	return selection, nil
}

// Partial masks become targeted operations, never a full mask with excluded
// outputs zeroed. An explicitly broad raw relay opcode retains exact masks.
func selectedRelaySteps(at uint32, previous, mask byte, initial bool, profile string, selection CaptureSelection) []appconfig.MacroStep {
	if selection.opcode(native.OpRelaySet) || selection.opcode(native.OpRelayAllOff) && mask == 0 {
		return relayMaskSteps(at, previous, mask, initial, profile)
	}
	var result []appconfig.MacroStep
	changed := previous ^ mask
	if initial {
		changed = 0xff
	}
	covered := byte(0)
	if profile == appconfig.BoardModeCinemaSeatMotion {
		for side, key := range []string{"seat.a", "seat.b"} {
			bits := byte(3 << (2 * side))
			if !(selection.key(key) || selection.opcode(native.OpRelaySide)) {
				continue
			}
			covered |= bits
			if changed&bits == 0 {
				continue
			}
			value := uint16(0)
			verb := "stop"
			if mask&byte(2<<(2*side)) != 0 {
				value = 1
				verb = "up"
				if mask&byte(1<<(2*side)) != 0 {
					value = 2
					verb = "down"
				}
			}
			result = append(result, appconfig.MacroStep{AtUS: at, Kind: "motion", Target: byte(side), Value: value, ActionIDs: []string{key + "." + verb}})
		}
	}
	for n := 0; n < 8; n++ {
		bit := byte(1 << n)
		key := fmt.Sprintf("relay.%d", n+1)
		if changed&bit == 0 || covered&bit != 0 || !selection.key(key) {
			continue
		}
		value := uint16(0)
		verb := "off"
		if mask&bit != 0 {
			value = 1
			verb = "on"
		}
		result = append(result, appconfig.MacroStep{AtUS: at, Kind: "relay", Target: byte(n), Value: value, ActionIDs: []string{key + "." + verb}})
	}
	return result
}

func selectedCaptureEvidence(e CommandEvidence, selection CaptureSelection) []CommandEvidence {
	if selection.opcode(e.Opcode) {
		return []CommandEvidence{e}
	}
	if e.Opcode == native.OpPWMSet && len(e.Payload) == 3 && selection.key(fmt.Sprintf("pwm.%d", e.Payload[0])) {
		return []CommandEvidence{e}
	}
	if e.Opcode == native.OpPWMAllOff {
		var result []CommandEvidence
		for n := 0; n < 16; n++ {
			if selection.key(fmt.Sprintf("pwm.%d", n)) {
				copy := e
				copy.Opcode = native.OpPWMSet
				copy.Payload = []byte{byte(n), 0, 0}
				result = append(result, copy)
			}
		}
		return result
	}
	return nil
}
