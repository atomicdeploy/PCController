package appconfig

import (
	"fmt"
	"strings"
)

const MaxPeripheralNames = 96

// PeripheralDescriptor is the host-owned presentation contract for one board
// peripheral. Names are stored in the PC configuration; this catalog never
// mutates board EEPROM or implies that a system-owned channel is directly
// writable through a generic control.
type PeripheralDescriptor struct {
	Key         string  `json:"key"`
	Kind        string  `json:"kind"`
	Role        string  `json:"role"`
	Index       int     `json:"index"`
	DefaultName string  `json:"default_name"`
	Name        string  `json:"name,omitempty"`
	Icon        string  `json:"icon,omitempty"`
	Group       string  `json:"group,omitempty"`
	Control     string  `json:"control"`
	OutputType  string  `json:"output_type,omitempty"`
	Curve       string  `json:"curve,omitempty"`
	Gamma       float64 `json:"gamma,omitempty"`
}

type ActionDescriptor struct {
	ID   string `json:"id"`
	Verb string `json:"verb"`
	Name string `json:"name"`
}

// ControlDescriptor is the compact, ordered cross-surface contract for one
// operator-controllable board channel. The canonical key remains suitable for
// commands and persisted names while Kind supplies the operator vocabulary.
type ControlDescriptor struct {
	Key        string             `json:"key"`
	Kind       string             `json:"kind"`
	Order      int                `json:"order"`
	Name       string             `json:"name"`
	Icon       string             `json:"icon,omitempty"`
	Group      string             `json:"group,omitempty"`
	Control    string             `json:"control"`
	OutputType string             `json:"output_type,omitempty"`
	Curve      string             `json:"curve,omitempty"`
	Gamma      float64            `json:"gamma,omitempty"`
	Actions    []ActionDescriptor `json:"actions,omitempty"`
}

var corePeripheralDescriptors = buildPeripheralDescriptors()

func buildPeripheralDescriptors() []PeripheralDescriptor {
	descriptors := make([]PeripheralDescriptor, 0, 34)
	relayNames := []string{
		"Side A Direction", "Side A Output", "Side B Direction", "Side B Output",
		"User Relay 5", "User Relay 6", "User Relay 7", "User Relay 8",
	}
	relayRoles := []string{
		"motion-direction", "motion-enable", "motion-direction", "motion-enable",
		"user-output", "user-output", "user-output", "user-output",
	}
	for index, name := range relayNames {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: fmt.Sprintf("relay.%d", index+1), Kind: "relay",
			Role: relayRoles[index], Index: index + 1, DefaultName: name,
			Control: "relay",
		})
	}
	for index, side := range []string{"a", "b"} {
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: "motion." + side, Kind: "motion", Role: "motion-side",
			Index: index + 1, DefaultName: fmt.Sprintf("Side %s motion", []string{"A", "B"}[index]),
			Control: "motion",
		})
	}
	pwmNames := []string{
		"MOSFET 1", "MOSFET 2", "MOSFET 3", "MOSFET 4",
		"MOSFET 5", "MOSFET 6", "MOSFET 7", "MOSFET 8",
		"User PWM 9", "User PWM 10", "User PWM 11", "Enclosure light",
		"Power indicator", "Status red", "Status green", "Status blue",
	}
	pwmRoles := []string{
		"user-output", "user-output", "user-output", "user-output",
		"user-output", "user-output", "user-output", "user-output",
		"user-output", "user-output", "user-output", "illumination",
		"power-indicator", "status-red", "status-green", "status-blue",
	}
	for index, name := range pwmNames {
		control := "role-specific"
		if index <= 10 {
			control = "pwm-user"
		}
		descriptors = append(descriptors, PeripheralDescriptor{
			Key: fmt.Sprintf("pwm.%d", index), Kind: "pwm", Role: pwmRoles[index],
			Index: index, DefaultName: name, Control: control,
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
		{"sensor.temperature-audio", "temperature-audio", "BT Amplifier temperature"},
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

// ControlDescriptors resolves configured host names over the canonical
// peripheral registry. Only directly operator-controlled relay, Side, and
// MOSFET channels are included; sensors and system-owned PWM channels remain
// available through the full peripheral catalog.
func ControlDescriptors(names map[string]string) []ControlDescriptor {
	controls := make([]ControlDescriptor, 0, 21)
	for _, descriptor := range corePeripheralDescriptors {
		kind := ""
		switch descriptor.Kind {
		case "relay":
			kind = "relay"
		case "motion":
			kind = "side"
		case "pwm":
			if descriptor.Index <= 10 {
				kind = "mosfet"
			}
		}
		if kind == "" {
			continue
		}
		name := descriptor.DefaultName
		if configured := names[descriptor.Key]; configured != "" {
			name = configured
		}
		controls = append(controls, ControlDescriptor{
			Key: descriptor.Key, Kind: kind, Order: descriptor.Index,
			Name: name, Control: descriptor.Control,
		})
	}
	return controls
}

// ProfileDescriptors resolves the active board wiring. Cinema profiles expose
// semantic seat controls and may also expose their underlying R1..R4 channels
// when an operator explicitly enables diagnostic/raw control for that board.
func ProfileDescriptors(mode string, exposeRawRelays bool, legacyNames map[string]string, presentation map[string]PeripheralPresentation) ([]PeripheralDescriptor, []ControlDescriptor) {
	mode = NormalizeBoardMode(mode)
	peripherals := make([]PeripheralDescriptor, 0, len(corePeripheralDescriptors))
	controls := make([]ControlDescriptor, 0, 21)
	addControl := func(descriptor PeripheralDescriptor, kind string, actions []ActionDescriptor) {
		resolved := resolvePresentation(descriptor.Key, descriptor.DefaultName, legacyNames, presentation)
		descriptor.Name, descriptor.Icon, descriptor.Group = resolved.Name, resolved.Icon, resolved.Group
		peripherals = append(peripherals, descriptor)
		controls = append(controls, ControlDescriptor{
			Key: descriptor.Key, Kind: kind, Order: descriptor.Index,
			Name: descriptor.Name, Icon: descriptor.Icon, Group: descriptor.Group,
			Control: descriptor.Control, Actions: actions,
		})
	}
	for _, descriptor := range corePeripheralDescriptors {
		if descriptor.Kind == "motion" {
			if mode == BoardModeCinemaSeatMotion {
				side := []string{"a", "b"}[descriptor.Index-1]
				key := "seat." + side
				seat := PeripheralDescriptor{
					Key: key, Kind: "seat", Role: "cinema-seat-motion", Index: descriptor.Index,
					DefaultName: fmt.Sprintf("Seat %s", strings.ToUpper(side)), Control: "seat",
				}
				legacy := make(map[string]string, len(legacyNames)+1)
				for itemKey, value := range legacyNames {
					legacy[itemKey] = value
				}
				if _, exists := legacy[key]; !exists {
					legacy[key] = legacyNames["motion."+side]
				}
				actions := []ActionDescriptor{
					{ID: key + ".up", Verb: "up", Name: "Up"},
					{ID: key + ".down", Verb: "down", Name: "Down"},
					{ID: key + ".stop", Verb: "stop", Name: "Stop"},
				}
				resolved := resolvePresentation(key, seat.DefaultName, legacy, presentation)
				seat.Name, seat.Icon, seat.Group = resolved.Name, resolved.Icon, resolved.Group
				peripherals = append(peripherals, seat)
				controls = append(controls, ControlDescriptor{
					Key: key, Kind: "seat", Order: descriptor.Index, Name: seat.Name,
					Icon: seat.Icon, Group: seat.Group, Control: "seat", Actions: actions,
				})
			}
			continue
		}
		if descriptor.Kind == "relay" {
			if descriptor.Index <= 4 {
				switch mode {
				case BoardModeOrdinaryRelays:
					descriptor.Role, descriptor.Control = "user-output", "relay"
				case BoardModeCinemaSeatMotion:
					descriptor.Role, descriptor.Control = "seat-output", "seat-internal"
				default:
					descriptor.Control = "unavailable"
				}
			}
			if descriptor.Index > 4 || mode == BoardModeOrdinaryRelays || mode == BoardModeCinemaSeatMotion && exposeRawRelays {
				actions := []ActionDescriptor{
					{ID: fmt.Sprintf("relay.%d.on", descriptor.Index), Verb: "on", Name: "On"},
					{ID: fmt.Sprintf("relay.%d.off", descriptor.Index), Verb: "off", Name: "Off"},
				}
				addControl(descriptor, "relay", actions)
			} else {
				resolved := resolvePresentation(descriptor.Key, descriptor.DefaultName, legacyNames, presentation)
				descriptor.Name, descriptor.Icon, descriptor.Group = resolved.Name, resolved.Icon, resolved.Group
				peripherals = append(peripherals, descriptor)
			}
			continue
		}
		if descriptor.Kind == "pwm" && descriptor.Index <= 10 {
			addControl(descriptor, "mosfet", nil)
			continue
		}
		resolved := resolvePresentation(descriptor.Key, descriptor.DefaultName, legacyNames, presentation)
		descriptor.Name, descriptor.Icon, descriptor.Group = resolved.Name, resolved.Icon, resolved.Group
		peripherals = append(peripherals, descriptor)
	}
	return peripherals, controls
}

func resolvePresentation(key, defaultName string, legacyNames map[string]string, presentation map[string]PeripheralPresentation) PeripheralPresentation {
	result := presentation[key]
	if result.Name == "" {
		result.Name = legacyNames[key]
	}
	if result.Name == "" {
		result.Name = defaultName
	}
	return result
}
