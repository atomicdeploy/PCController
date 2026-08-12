package appconfig

import "fmt"

const MaxPeripheralNames = 96

// PeripheralDescriptor is the host-owned presentation contract for one board
// peripheral. Names are stored in the PC configuration; this catalog never
// mutates board EEPROM or implies that a system-owned channel is directly
// writable through a generic control.
type PeripheralDescriptor struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	Role        string `json:"role"`
	Index       int    `json:"index"`
	DefaultName string `json:"default_name"`
	Control     string `json:"control"`
}

// ControlDescriptor is the compact, ordered cross-surface contract for the
// operator-controllable board channels.  The underlying key stays canonical
// (pwm.0 and motion.a); Kind supplies the operator vocabulary (MOSFET/Side)
// without duplicating a second channel registry in Web, TUI, CLI, or RPC.
type ControlDescriptor struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Order   int    `json:"order"`
	Name    string `json:"name"`
	Control string `json:"control"`
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

// ControlDescriptors resolves configured host names over the one canonical
// peripheral registry.  It deliberately exposes only relay, MOSFET, and Side
// controls; sensors and system-owned channels remain available in the full
// peripheral catalog but are not misrepresented as generic outputs.
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
