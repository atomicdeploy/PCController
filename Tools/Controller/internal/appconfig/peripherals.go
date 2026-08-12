package appconfig

import (
	"fmt"
	"sort"
	"strings"
)

const MaxPeripheralNames = 96
const MaxPeripheralDescriptionRunes = 160
const MaxPresentedControls = 26

// PeripheralPresentation is the persisted host-owned presentation override for
// one relay, motion side, or PWM channel. A nil order retains the registry
// order; blank name/description fields retain their registry defaults.
type PeripheralPresentation struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Order       *int   `json:"order,omitempty"`
}

// PeripheralDescriptor is the host-owned presentation contract for one board
// peripheral. Names are stored in the PC configuration; this catalog never
// mutates board EEPROM or implies that a system-owned channel is directly
// writable through a generic control. Its fallback names derive only from
// stable hardware IDs, and fallback descriptions are intentionally empty.
type PeripheralDescriptor struct {
	Key                string `json:"key"`
	Kind               string `json:"kind"`
	Role               string `json:"role"`
	Index              int    `json:"index"`
	DefaultName        string `json:"default_name"`
	DefaultDescription string `json:"default_description"`
	Control            string `json:"control"`
}

// ControlDescriptor is the compact, ordered cross-surface contract for the
// operator-controllable board channels.  The underlying key stays canonical
// (pwm.0 and motion.a); Kind supplies the operator vocabulary (MOSFET/Side)
// without duplicating a second channel registry in Web, TUI, CLI, or RPC.
type ControlDescriptor struct {
	Key                string `json:"key"`
	Kind               string `json:"kind"`
	Role               string `json:"role"`
	Index              int    `json:"index"`
	Order              int    `json:"order"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	DefaultName        string `json:"default_name"`
	DefaultDescription string `json:"default_description"`
	Control            string `json:"control"`
}

var corePeripheralDescriptors = buildPeripheralDescriptors()

func buildPeripheralDescriptors() []PeripheralDescriptor {
	descriptors := make([]PeripheralDescriptor, 0, 34)
	relayRoles := []string{
		"motion-direction", "motion-enable", "motion-direction", "motion-enable",
		"user-output", "user-output", "user-output", "user-output",
	}
	for index, role := range relayRoles {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: fmt.Sprintf("relay.%d", index+1), Kind: "relay",
			Role: role, Index: index + 1, DefaultName: fmt.Sprintf("Relay %d", index+1),
			Control: "relay",
		})
	}
	for index, side := range []string{"a", "b"} {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: "motion." + side, Kind: "motion", Role: "motion-side",
			Index: index + 1, DefaultName: fmt.Sprintf("Motion %s", strings.ToUpper(side)),
			Control: "motion",
		})
	}
	pwmRoles := []string{
		"user-output", "user-output", "user-output", "user-output",
		"user-output", "user-output", "user-output", "user-output",
		"user-output", "user-output", "user-output", "illumination",
		"power-indicator", "status-red", "status-green", "status-blue",
	}
	for index, role := range pwmRoles {
		control := "role-specific"
		if index <= 10 {
			control = "pwm-user"
		}
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: fmt.Sprintf("pwm.%d", index), Kind: "pwm", Role: role,
			Index: index, DefaultName: fmt.Sprintf("PWM %d", index), Control: control,
		})
	}
	for index, display := range []struct{ key, role, name string }{
		{"display.segment", "front-panel", "Four-digit display"},
		{"display.lcd", "character-display", "LCD display"},
	} {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: display.key, Kind: "display", Role: display.role, Index: index,
			DefaultName: display.name, Control: "read-only",
		})
	}
	for index, sensor := range []struct{ key, role, name string }{
		{"sensor.supply-voltage", "supply-voltage", "Supply voltage"},
		{"sensor.bus-voltage", "bus-voltage", "Bus voltage"},
		{"sensor.current", "current", "Load current"},
		{"sensor.power", "power", "Load power"},
		{"sensor.temperature-led", "temperature-led", "Lighting temperature"},
		{"sensor.temperature-audio", "temperature-audio", "Audio temperature"},
	} {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: sensor.key, Kind: "sensor", Role: sensor.role, Index: index,
			DefaultName: sensor.name, Control: "read-only",
		})
	}
	return descriptors
}

// PeripheralDescriptors returns a copy so RPC and UI callers cannot mutate the
// canonical registry shared by validation and every host surface.
func PeripheralDescriptors() []PeripheralDescriptor {
	return append([]PeripheralDescriptor(nil), corePeripheralDescriptors...)
}

func PeripheralDefaultName(key string) (string, bool) {
	for _, descriptor := range corePeripheralDescriptors {
		if descriptor.Key == key {
			return descriptor.DefaultName, true
		}
	}
	return "", false
}

func IsPresentedControlKey(key string) bool {
	for _, descriptor := range corePeripheralDescriptors {
		if descriptor.Key == key && (descriptor.Kind == "relay" || descriptor.Kind == "motion" || descriptor.Kind == "pwm") {
			return true
		}
	}
	return false
}

// ControlDescriptors resolves configured host names over the one canonical
// peripheral registry.  It deliberately exposes only relay, MOSFET, and Side
// controls; sensors and system-owned channels remain available in the full
// peripheral catalog but are not misrepresented as generic outputs.
func ControlDescriptors(ui UI) []ControlDescriptor {
	ui = normalizePeripheralPresentationDefaults(ui)
	controls := make([]ControlDescriptor, 0, MaxPresentedControls)
	type orderedOverride struct {
		key         string
		order, base int
	}
	overrides := make([]orderedOverride, 0, MaxPresentedControls)
	for _, descriptor := range corePeripheralDescriptors {
		kind := ""
		switch descriptor.Kind {
		case "relay":
			kind = "relay"
		case "motion":
			kind = "side"
		case "pwm":
			kind = "mosfet"
		}
		if kind == "" {
			continue
		}
		defaultOrder := len(controls)
		name, description, order := descriptor.DefaultName, descriptor.DefaultDescription, defaultOrder
		if configured := strings.TrimSpace(ui.PeripheralNames[descriptor.Key]); configured != "" {
			name = configured
		}
		if presentation, ok := ui.PeripheralPresentation[descriptor.Key]; ok {
			if presentation.Name != "" {
				name = presentation.Name
			}
			if presentation.Description != "" {
				description = presentation.Description
			}
			if presentation.Order != nil {
				order = *presentation.Order
				overrides = append(overrides, orderedOverride{descriptor.Key, order, defaultOrder})
			}
		}
		controls = append(controls, ControlDescriptor{
			Key: descriptor.Key, Kind: kind, Role: descriptor.Role, Index: descriptor.Index,
			Order: order, Name: name, Description: description,
			DefaultName: descriptor.DefaultName, DefaultDescription: descriptor.DefaultDescription,
			Control: descriptor.Control,
		})
	}
	sort.SliceStable(overrides, func(left, right int) bool {
		if overrides[left].order != overrides[right].order {
			return overrides[left].order < overrides[right].order
		}
		return overrides[left].base < overrides[right].base
	})
	for _, override := range overrides {
		index := -1
		for candidate := range controls {
			if controls[candidate].Key == override.key {
				index = candidate
				break
			}
		}
		if index < 0 {
			continue
		}
		control := controls[index]
		controls = append(controls[:index], controls[index+1:]...)
		order := override.order
		if order > len(controls) {
			order = len(controls)
		}
		controls = append(controls, ControlDescriptor{})
		copy(controls[order+1:], controls[order:])
		controls[order] = control
	}
	for order := range controls {
		controls[order].Order = order
	}
	return controls
}

// ResolvedPeripheralPresentation returns the complete normalized settings
// projection consumed by RPC/REST/Web clients. It is built from the same
// ordered descriptors used by local CLI and TUI surfaces.
func ResolvedPeripheralPresentation(ui UI) map[string]PeripheralPresentation {
	controls := ControlDescriptors(ui)
	result := make(map[string]PeripheralPresentation, len(controls))
	for _, control := range controls {
		order := control.Order
		result[control.Key] = PeripheralPresentation{
			Name: control.Name, Description: control.Description, Order: &order,
		}
	}
	return result
}
