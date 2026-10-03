package appconfig

import (
	"fmt"
	"strings"
)

const MaxPeripheralNames = 96

// PeripheralDescriptor is the host-owned presentation contract for one board
// peripheral. Names are stored in the PC configuration; this catalog never
// mutates board EEPROM. The Control field distinguishes general user outputs
// from role-specific channels so clients can apply a visibility policy without
// losing access to the underlying hardware.
type PeripheralDescriptor struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	Role        string `json:"role"`
	Index       int    `json:"index"`
	DefaultName string `json:"default_name"`
	Name        string `json:"name,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	UpColor     string `json:"up_color,omitempty"`
	DownColor   string `json:"down_color,omitempty"`
	Group       string `json:"group,omitempty"`
	Order       *int   `json:"order,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
	Control     string `json:"control"`
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
	Key       string             `json:"key"`
	Kind      string             `json:"kind"`
	Order     int                `json:"order"`
	Name      string             `json:"name"`
	Icon      string             `json:"icon,omitempty"`
	Color     string             `json:"color,omitempty"`
	UpColor   string             `json:"up_color,omitempty"`
	DownColor string             `json:"down_color,omitempty"`
	Group     string             `json:"group,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	Locked    bool               `json:"locked,omitempty"`
	Control   string             `json:"control"`
	Actions   []ActionDescriptor `json:"actions,omitempty"`
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
			Hidden: strings.HasPrefix(pwmRoles[index], "status-"),
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
// peripheral registry. Every relay, motion side, and PWM channel is included;
// Control tells clients which channels are general user outputs and which are
// role-specific diagnostic/raw controls.
func ControlDescriptors(names map[string]string) []ControlDescriptor {
	controls := make([]ControlDescriptor, 0, 26)
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
		name := descriptor.DefaultName
		if configured := names[descriptor.Key]; configured != "" {
			name = configured
		}
		controls = append(controls, ControlDescriptor{
			Key: descriptor.Key, Kind: kind, Order: descriptor.Index,
			Name: name, Hidden: descriptor.Hidden, Control: descriptor.Control,
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
	controls := make([]ControlDescriptor, 0, 26)
	addControl := func(descriptor PeripheralDescriptor, kind string, actions []ActionDescriptor) {
		resolved := resolvePresentation(descriptor.Key, descriptor.DefaultName, legacyNames, presentation)
		if _, explicitlyConfigured := presentation[descriptor.Key]; !explicitlyConfigured {
			resolved.Hidden = descriptor.Hidden
		}
		descriptor.Name, descriptor.Icon, descriptor.Color, descriptor.UpColor, descriptor.DownColor, descriptor.Group, descriptor.Order = resolved.Name, resolved.Icon, resolved.Color, resolved.UpColor, resolved.DownColor, resolved.Group, resolved.Order
		descriptor.Hidden, descriptor.Locked = resolved.Hidden, resolved.Locked
		order := descriptor.Index
		if resolved.Order != nil {
			order = *resolved.Order
		}
		peripherals = append(peripherals, descriptor)
		controls = append(controls, ControlDescriptor{
			Key: descriptor.Key, Kind: kind, Order: order,
			Name: descriptor.Name, Icon: descriptor.Icon, Color: descriptor.Color, UpColor: descriptor.UpColor, DownColor: descriptor.DownColor, Group: descriptor.Group,
			Hidden: descriptor.Hidden, Locked: descriptor.Locked,
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
				seat.Name, seat.Icon, seat.Color, seat.UpColor, seat.DownColor, seat.Group, seat.Order = resolved.Name, resolved.Icon, resolved.Color, resolved.UpColor, resolved.DownColor, resolved.Group, resolved.Order
				seat.Hidden, seat.Locked = resolved.Hidden, resolved.Locked
				order := descriptor.Index
				if resolved.Order != nil {
					order = *resolved.Order
				}
				peripherals = append(peripherals, seat)
				controls = append(controls, ControlDescriptor{
					Key: key, Kind: "seat", Order: order, Name: seat.Name,
					Icon: seat.Icon, Color: seat.Color, UpColor: seat.UpColor, DownColor: seat.DownColor, Group: seat.Group, Hidden: seat.Hidden, Locked: seat.Locked,
					Control: "seat", Actions: actions,
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
				descriptor.Name, descriptor.Icon, descriptor.Color, descriptor.UpColor, descriptor.DownColor, descriptor.Group, descriptor.Order = resolved.Name, resolved.Icon, resolved.Color, resolved.UpColor, resolved.DownColor, resolved.Group, resolved.Order
				descriptor.Hidden, descriptor.Locked = resolved.Hidden, resolved.Locked
				peripherals = append(peripherals, descriptor)
			}
			continue
		}
		if descriptor.Kind == "pwm" {
			addControl(descriptor, "mosfet", nil)
			continue
		}
		resolved := resolvePresentation(descriptor.Key, descriptor.DefaultName, legacyNames, presentation)
		descriptor.Name, descriptor.Icon, descriptor.Color, descriptor.UpColor, descriptor.DownColor, descriptor.Group, descriptor.Order = resolved.Name, resolved.Icon, resolved.Color, resolved.UpColor, resolved.DownColor, resolved.Group, resolved.Order
		descriptor.Hidden, descriptor.Locked = resolved.Hidden, resolved.Locked
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
