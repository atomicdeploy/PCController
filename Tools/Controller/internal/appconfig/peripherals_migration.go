package appconfig

// presentationDefaults records values emitted by an older registry. It is
// migration-only data: rendering always uses corePeripheralDescriptors.
type presentationDefaults struct {
	name        string
	description string
}

var legacyCopiedControlDefaults = map[string]presentationDefaults{
	"relay.1":  {"Side A Direction", "Protected relay output R1 (motion direction)"},
	"relay.2":  {"Side A Output", "Protected relay output R2 (motion enable)"},
	"relay.3":  {"Side B Direction", "Protected relay output R3 (motion direction)"},
	"relay.4":  {"Side B Output", "Protected relay output R4 (motion enable)"},
	"relay.5":  {"User Relay 5", "Protected relay output R5 (user output)"},
	"relay.6":  {"User Relay 6", "Protected relay output R6 (user output)"},
	"relay.7":  {"User Relay 7", "Protected relay output R7 (user output)"},
	"relay.8":  {"User Relay 8", "Protected relay output R8 (user output)"},
	"motion.a": {"Side A motion", "Interlocked direction and output control for Side A"},
	"motion.b": {"Side B motion", "Interlocked direction and output control for Side B"},
	"pwm.0":    {"MOSFET 1", "12-bit PWM channel 0 (user output)"},
	"pwm.1":    {"MOSFET 2", "12-bit PWM channel 1 (user output)"},
	"pwm.2":    {"MOSFET 3", "12-bit PWM channel 2 (user output)"},
	"pwm.3":    {"MOSFET 4", "12-bit PWM channel 3 (user output)"},
	"pwm.4":    {"MOSFET 5", "12-bit PWM channel 4 (user output)"},
	"pwm.5":    {"MOSFET 6", "12-bit PWM channel 5 (user output)"},
	"pwm.6":    {"MOSFET 7", "12-bit PWM channel 6 (user output)"},
	"pwm.7":    {"MOSFET 8", "12-bit PWM channel 7 (user output)"},
	"pwm.8":    {"User PWM 9", "12-bit PWM channel 8 (user output)"},
	"pwm.9":    {"User PWM 10", "12-bit PWM channel 9 (user output)"},
	"pwm.10":   {"User PWM 11", "12-bit PWM channel 10 (user output)"},
	"pwm.11":   {"Enclosure light", "12-bit PWM channel 11 (illumination)"},
	"pwm.12":   {"Power indicator", "12-bit PWM channel 12 (power indicator)"},
	"pwm.13":   {"Status red", "12-bit PWM channel 13 (status red)"},
	"pwm.14":   {"Status green", "12-bit PWM channel 14 (status green)"},
	"pwm.15":   {"Status blue", "12-bit PWM channel 15 (status blue)"},
}

// normalizePeripheralPresentationDefaults removes registry values that older
// WebUI clients copied back as if they were operator overrides. Custom labels,
// descriptions, and ordering remain intact. Returning a copy keeps Write from
// mutating its caller through shared map storage.
func normalizePeripheralPresentationDefaults(ui UI) UI {
	current := make(map[string]presentationDefaults, len(corePeripheralDescriptors))
	for _, descriptor := range corePeripheralDescriptors {
		current[descriptor.Key] = presentationDefaults{
			name: descriptor.DefaultName, description: descriptor.DefaultDescription,
		}
	}

	if ui.PeripheralNames != nil {
		names := make(map[string]string, len(ui.PeripheralNames))
		for key, name := range ui.PeripheralNames {
			active, known := current[key]
			legacy := legacyCopiedControlDefaults[key]
			if (known && name == active.name) || (legacy.name != "" && name == legacy.name) {
				continue
			}
			names[key] = name
		}
		if len(names) == 0 {
			ui.PeripheralNames = nil
		} else {
			ui.PeripheralNames = names
		}
	}

	if ui.PeripheralPresentation != nil {
		presentation := make(map[string]PeripheralPresentation, len(ui.PeripheralPresentation))
		for key, value := range ui.PeripheralPresentation {
			active, known := current[key]
			legacy := legacyCopiedControlDefaults[key]
			if (known && value.Name == active.name) || (legacy.name != "" && value.Name == legacy.name) {
				value.Name = ""
			}
			if (known && value.Description == active.description) ||
				(legacy.description != "" && value.Description == legacy.description) {
				value.Description = ""
			}
			if value.Name == "" && value.Description == "" && value.Order == nil {
				continue
			}
			if value.Order != nil {
				order := *value.Order
				value.Order = &order
			}
			presentation[key] = value
		}
		if len(presentation) == 0 {
			ui.PeripheralPresentation = nil
		} else {
			ui.PeripheralPresentation = presentation
		}
	}
	return ui
}
