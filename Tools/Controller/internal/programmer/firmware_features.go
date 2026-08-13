package programmer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const firmwareFeatureMatrixFormat = "pccontroller-firmware-feature-matrix/v1"

const (
	firmwareFeatureDocs   = "docs/Firmware-Features-and-Profiles.md"
	firmwareConfigSource  = "ProjectConfig.h"
	firmwareRuntimeSource = "Project/Firmware/ProtocolRuntime.inc.h"
)

// FirmwareFeatureState is one resolved preprocessor gate. Runtime lists the
// HELLO build-flag/capability evidence controlled by that gate.
type FirmwareFeatureState struct {
	ID          string   `json:"id"`
	Macro       string   `json:"macro"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Runtime     []string `json:"runtime,omitempty"`
	Docs        string   `json:"docs"`
	Source      string   `json:"source"`
}

type FirmwareCapabilityState struct {
	Bit         uint8  `json:"bit"`
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Docs        string `json:"docs"`
	Source      string `json:"source"`
}

type FirmwareProfileState struct {
	ID          string `json:"id"`
	Value       uint8  `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Docs        string `json:"docs"`
	Source      string `json:"source"`
}

// FirmwareFeatureMatrix is embedded in every successful compile manifest and
// is also rendered directly by controller firmware features.
type FirmwareFeatureMatrix struct {
	Format          string                    `json:"format"`
	Profile         FirmwareProfileState      `json:"profile"`
	BuildFlags      uint8                     `json:"buildFlags"`
	BuildFlagsHex   string                    `json:"buildFlagsHex"`
	CapabilityMask  uint32                    `json:"capabilityMask"`
	CapabilitiesHex string                    `json:"capabilitiesHex"`
	Features        []FirmwareFeatureState    `json:"features"`
	Capabilities    []FirmwareCapabilityState `json:"capabilities"`
}

type firmwareFeatureDefinition struct {
	id, macro, label, description string
	defaultEnabled                bool
	capabilityBits                []uint8
	buildFlagBit                  int
}

var firmwareFeatureDefinitions = []firmwareFeatureDefinition{
	{"i2c-lcd", "PCCONTROLLER_ENABLE_I2C_LCD", "MCU LCD renderer", "Render a PCF8574/HD44780 LCD on the MCU.", false, []uint8{6}, -1},
	{"ina219", "PCCONTROLLER_ENABLE_INA219", "INA219 telemetry", "Measure supply voltage and current.", true, []uint8{0}, -1},
	{"ds18b20", "PCCONTROLLER_ENABLE_DS18B20", "DS18B20 temperatures", "Read and identify the two temperature probes.", true, []uint8{1, 10}, -1},
	{"pca9685", "PCCONTROLLER_ENABLE_PCA9685", "PCA9685 PWM", "Drive the 16-channel PWM peripheral.", true, []uint8{2}, -1},
	{"status-led-engine", "PCCONTROLLER_ENABLE_STATUS_LED_ENGINE", "Status RGB effects", "Render smooth procedural RGB effects on the MCU.", true, []uint8{28}, 6},
	{"status-led-profiles", "PCCONTROLLER_ENABLE_STATUS_LED_PROFILES", "EEPROM status profiles", "Select EEPROM-resident condition/effect profiles.", false, []uint8{30}, -1},
	{"illumination-automation", "PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION", "Illumination automation", "Run door-aware enclosure illumination locally.", true, nil, 7},
	{"local-pca-pages", "PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES", "Local PCA pages", "Expose board-local PWM editor pages.", false, nil, 5},
	{"local-rf-learning-ui", "PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI", "Local RF learning UI", "Expose RF learning on the front panel.", true, nil, -1},
	{"bt-led-detection", "PCCONTROLLER_ENABLE_BT_LED_DETECTION", "BT LED detection", "Interpret the commissioned active-high BT audio LED input.", false, nil, -1},
	{"async-presentation-events", "PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS", "Buzzer/RGB push", "Publish unsolicited buzzer and rendered RGB frames.", false, []uint8{27, 29}, -1},
	{"async-segment-events", "PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS", "Seven-segment push", "Publish changed-only TM1637 frames.", true, []uint8{26}, -1},
	{"scheduled-segments", "PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS", "Scheduled segments", "Run scheduled once/loop/interval segment messages.", false, []uint8{25}, -1},
	{"local-audio-cues", "PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES", "Autonomous audio cues", "Keep door and motion feedback tones on the board.", true, nil, 4},
	{"local-settings-editor", "PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR", "Local settings editor", "Expose the extended seven-segment settings editor.", false, nil, -1},
	{"task-scheduler", "PCCONTROLLER_ENABLE_TASK_SCHEDULER", "Task scheduler", "Compile the optional cooperative task scheduler.", false, nil, -1},
	{"key-diagnostic-page", "PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS", "Key diagnostic page", "Use the unified motion/key page as a key identifier.", false, nil, 1},
	{"macro-capture", "PCCONTROLLER_ENABLE_MACRO_CAPTURE", "MCU macro capture", "Capture, replay, and export timed board actions.", true, []uint8{11}, 0},
	{"force-silent", "PCCONTROLLER_FORCE_SILENT", "Force silent", "Compile an image that cannot produce local audio.", false, nil, 2},
	{"blank-eeprom-silent", "PCCONTROLLER_BLANK_EEPROM_SILENT", "Blank EEPROM starts silent", "Default uninitialized settings to silent mode.", true, nil, 3},
	{"menu-directory", "PCCONTROLLER_ENABLE_MENU_DIRECTORY", "MCU menu directory", "Publish the board-authoritative paged menu catalog.", false, []uint8{17}, -1},
	{"menu-layout-storage", "PCCONTROLLER_MENU_LAYOUT_STORAGE", "Menu layout storage", "Persist the extended local menu-layout record.", false, nil, -1},
	{"menu-visibility", "PCCONTROLLER_MENU_VISIBILITY", "Menu visibility", "Persist board-local page visibility.", false, nil, -1},
	{"menu-ordering", "PCCONTROLLER_MENU_ORDERING", "Menu ordering", "Persist board-local page ranks.", false, nil, -1},
	{"menu-hierarchy", "PCCONTROLLER_MENU_HIERARCHY", "Menu hierarchy", "Compile nested board-local menus.", false, nil, -1},
	{"menu-layout-protocol", "PCCONTROLLER_MENU_LAYOUT_PROTOCOL", "Menu layout protocol", "Expose persistent menu-layout wire operations.", false, []uint8{23}, -1},
}

type firmwareCapabilityDefinition struct {
	id, label, description string
	enabled                func(map[string]bool) bool
}

func alwaysFirmwareCapability(map[string]bool) bool { return true }
func gate(name string) func(map[string]bool) bool {
	return func(values map[string]bool) bool { return values[name] }
}

var firmwareCapabilityDefinitions = []firmwareCapabilityDefinition{
	{"ina219", "INA219", "Supply-voltage and current telemetry.", gate("PCCONTROLLER_ENABLE_INA219")},
	{"ds18b20", "DS18B20", "Two temperature probes.", gate("PCCONTROLLER_ENABLE_DS18B20")},
	{"pca9685", "PCA9685", "Sixteen PWM channels.", gate("PCCONTROLLER_ENABLE_PCA9685")},
	{"relay-safety", "Relay safety", "Relay and motion interlock controller.", alwaysFirmwareCapability},
	{"rf-control", "RF control", "RF receive, learning, mapping, and transmit path.", alwaysFirmwareCapability},
	{"tm1637", "TM1637", "Four-cell seven-segment display.", alwaysFirmwareCapability},
	{"mcu-lcd", "MCU LCD", "MCU-rendered I2C LCD.", gate("PCCONTROLLER_ENABLE_I2C_LCD")},
	{"addressable-leds", "Addressable LEDs", "Addressable strip output.", alwaysFirmwareCapability},
	{"persistent-settings", "Persistent settings", "Checksum-backed EEPROM settings.", alwaysFirmwareCapability},
	{"menu-remote", "Menu remote", "Host menu navigation.", alwaysFirmwareCapability},
	{"temperature-identities", "Temperature identities", "Named DS18B20 identities.", gate("PCCONTROLLER_ENABLE_DS18B20")},
	{"macro-capture", "Macro capture", "Board-local capture/replay/export.", gate("PCCONTROLLER_ENABLE_MACRO_CAPTURE")},
	{"display-events", "Display/events", "Host display text and asynchronous events.", alwaysFirmwareCapability},
	{"front-panel-snapshot", "Front-panel snapshot", "Exact physical front-panel snapshot.", alwaysFirmwareCapability},
	{"key-injection", "Key injection", "Host-injected key lifecycle.", alwaysFirmwareCapability},
	{"rf-learning", "RF learning", "Multi/indefinite RF learning.", alwaysFirmwareCapability},
	{"i2c-lease", "I2C lease", "Bounded generic I2C transactions.", alwaysFirmwareCapability},
	{"menu-directory", "Menu directory", "Paged MCU menu catalog.", gate("PCCONTROLLER_ENABLE_MENU_DIRECTORY")},
	{"rf-replace", "RF replacement", "Host-staged learned RF replacement.", alwaysFirmwareCapability},
	{"front-panel-capture", "Front-panel capture", "Host-captured front-panel session.", alwaysFirmwareCapability},
	{"buzzer-state", "Buzzer state", "Buzzer queue/voice busy state.", alwaysFirmwareCapability},
	{"motion-break", "Motion break", "EEPROM-selectable motion dead time.", alwaysFirmwareCapability},
	{"timed-events", "Timed events", "MCU-timed events and macro schema 3.", alwaysFirmwareCapability},
	{"menu-layout", "Menu layout", "Persistent visibility/rank protocol.", gate("PCCONTROLLER_MENU_LAYOUT_PROTOCOL")},
	{"program-state", "Program state", "Host-owned Idle/Running state.", alwaysFirmwareCapability},
	{"scheduled-segments", "Scheduled segments", "Scheduled TM1637 messages.", gate("PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS")},
	{"segment-push", "Segment push", "Changed-only TM1637 frames.", gate("PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS")},
	{"presentation-push", "Presentation push", "Unsolicited buzzer frames.", gate("PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS")},
	{"status-effects", "Status effects", "MCU-owned procedural status LED effects.", func(v map[string]bool) bool {
		return v["PCCONTROLLER_ENABLE_PCA9685"] && v["PCCONTROLLER_ENABLE_STATUS_LED_ENGINE"]
	}},
	{"status-push", "Status push", "Rendered status LED frames.", func(v map[string]bool) bool {
		return v["PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS"] && v["PCCONTROLLER_ENABLE_PCA9685"] && v["PCCONTROLLER_ENABLE_STATUS_LED_ENGINE"]
	}},
	{"status-profiles", "Status profiles", "EEPROM condition/effect profiles.", gate("PCCONTROLLER_ENABLE_STATUS_LED_PROFILES")},
	{"board-name", "Board name", "Checksum-backed operator board name.", alwaysFirmwareCapability},
}

var firmwareDefinePattern = regexp.MustCompile(`(?m)^\s*#\s*define\s+(PCCONTROLLER_[A-Z0-9_]+)\s+([01])(?:\s|$)`)

func ResolveFirmwareFeatureMatrix(sourceRoot string) (FirmwareFeatureMatrix, error) {
	return resolveFirmwareFeatureMatrix(sourceRoot, nil)
}

func ResolveFirmwareFeatureMatrixWithDefines(sourceRoot string, defines []string) (FirmwareFeatureMatrix, error) {
	return resolveFirmwareFeatureMatrix(sourceRoot, defines)
}

func resolveFirmwareFeatureMatrix(sourceRoot string, defines []string) (FirmwareFeatureMatrix, error) {
	content, err := os.ReadFile(filepath.Join(sourceRoot, firmwareConfigSource))
	if err != nil {
		return FirmwareFeatureMatrix{}, fmt.Errorf("read firmware feature gates: %w", err)
	}
	values := make(map[string]bool)
	for _, match := range firmwareDefinePattern.FindAllSubmatch(content, -1) {
		values[string(match[1])] = string(match[2]) == "1"
	}
	for _, definition := range firmwareFeatureDefinitions {
		if _, ok := values[definition.macro]; !ok {
			return FirmwareFeatureMatrix{}, fmt.Errorf("firmware feature gate %s is not a literal 0/1 default in %s", definition.macro, firmwareConfigSource)
		}
	}
	for _, define := range defines {
		parts := strings.SplitN(define, "=", 2)
		if len(parts) != 2 || (parts[1] != "0" && parts[1] != "1") {
			return FirmwareFeatureMatrix{}, fmt.Errorf("invalid resolved firmware define %q", define)
		}
		values[parts[0]] = parts[1] == "1"
	}

	profile := resolvedFirmwareProfile(values)
	buildFlags := uint8(0)
	features := make([]FirmwareFeatureState, 0, len(firmwareFeatureDefinitions))
	for _, definition := range firmwareFeatureDefinitions {
		runtime := make([]string, 0, len(definition.capabilityBits)+1)
		for _, bit := range definition.capabilityBits {
			runtime = append(runtime, fmt.Sprintf("cap b%d", bit))
		}
		if definition.buildFlagBit >= 0 {
			runtime = append(runtime, fmt.Sprintf("HELLO b%d", definition.buildFlagBit))
			if values[definition.macro] {
				buildFlags |= 1 << definition.buildFlagBit
			}
		}
		features = append(features, FirmwareFeatureState{
			ID: definition.id, Macro: definition.macro, Label: definition.label,
			Description: definition.description, Enabled: values[definition.macro], Runtime: runtime,
			Docs: firmwareFeatureDocs + "#" + definition.id, Source: firmwareConfigSource,
		})
	}

	capabilities := make([]FirmwareCapabilityState, 0, len(firmwareCapabilityDefinitions))
	mask := uint32(0)
	for bit, definition := range firmwareCapabilityDefinitions {
		enabled := definition.enabled(values)
		if enabled {
			mask |= uint32(1) << bit
		}
		capabilities = append(capabilities, FirmwareCapabilityState{
			Bit: uint8(bit), ID: definition.id, Label: definition.label,
			Description: definition.description, Enabled: enabled,
			Docs: firmwareFeatureDocs + "#runtime-capabilities", Source: firmwareRuntimeSource,
		})
	}
	return FirmwareFeatureMatrix{
		Format: firmwareFeatureMatrixFormat, Profile: profile,
		BuildFlags: buildFlags, BuildFlagsHex: fmt.Sprintf("0x%02X", buildFlags),
		CapabilityMask: mask, CapabilitiesHex: fmt.Sprintf("0x%08X", mask),
		Features: features, Capabilities: capabilities,
	}, nil
}

// ResolveFirmwareCompileSelection turns public profile/feature names into a
// stable, sorted list of preprocessor overrides. ProjectConfig.h remains the
// final dependency/incompatibility gate during the actual compile.
func ResolveFirmwareCompileSelection(profile string, features []string) ([]string, error) {
	values := map[string]bool{}
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "source", "default", "custom":
	case "full-peripheral":
		values["PCCONTROLLER_ENABLE_INA219"] = true
		values["PCCONTROLLER_ENABLE_DS18B20"] = true
		values["PCCONTROLLER_ENABLE_PCA9685"] = true
		values["PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS"] = false
	case "motion-macro":
		values["PCCONTROLLER_ENABLE_INA219"] = false
		values["PCCONTROLLER_ENABLE_DS18B20"] = false
		values["PCCONTROLLER_ENABLE_PCA9685"] = false
		values["PCCONTROLLER_ENABLE_I2C_LCD"] = false
		values["PCCONTROLLER_ENABLE_STATUS_LED_ENGINE"] = false
		values["PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION"] = false
		values["PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES"] = false
		values["PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS"] = false
	case "key-diagnostic":
		values["PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS"] = true
	default:
		return nil, errors.New("firmware profile must be source, full-peripheral, motion-macro, key-diagnostic, or custom")
	}
	byID := make(map[string]string, len(firmwareFeatureDefinitions))
	knownMacro := make(map[string]bool, len(firmwareFeatureDefinitions))
	for _, definition := range firmwareFeatureDefinitions {
		byID[definition.id] = definition.macro
		knownMacro[definition.macro] = true
	}
	for _, feature := range features {
		parts := strings.SplitN(strings.TrimSpace(feature), "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("firmware feature %q must be NAME=on|off", feature)
		}
		name := strings.TrimSpace(parts[0])
		macro := byID[strings.ToLower(name)]
		if macro == "" && knownMacro[name] {
			macro = name
		}
		if macro == "" {
			return nil, fmt.Errorf("unknown firmware feature %q", name)
		}
		switch strings.ToLower(strings.TrimSpace(parts[1])) {
		case "1", "on", "true", "include", "included", "enable", "enabled":
			values[macro] = true
		case "0", "off", "false", "exclude", "excluded", "disable", "disabled":
			values[macro] = false
		default:
			return nil, fmt.Errorf("firmware feature %q value must be on or off", name)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	defines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := 0
		if values[key] {
			value = 1
		}
		defines = append(defines, fmt.Sprintf("%s=%d", key, value))
	}
	return defines, nil
}

func resolvedFirmwareProfile(values map[string]bool) FirmwareProfileState {
	profile := FirmwareProfileState{ID: "custom", Value: 3, Label: "Custom", Description: "A source-selected combination outside the named profiles."}
	switch {
	case values["PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS"]:
		profile = FirmwareProfileState{ID: "key-diagnostic", Value: 2, Label: "Key diagnostic", Description: "The unified front-panel page identifies physical keys."}
	case values["PCCONTROLLER_ENABLE_INA219"] && values["PCCONTROLLER_ENABLE_DS18B20"] && values["PCCONTROLLER_ENABLE_PCA9685"]:
		profile = FirmwareProfileState{ID: "full-peripheral", Value: 0, Label: "Full peripheral", Description: "INA219, DS18B20, and PCA9685 peripheral engines are compiled."}
	case !values["PCCONTROLLER_ENABLE_INA219"] && !values["PCCONTROLLER_ENABLE_DS18B20"] && !values["PCCONTROLLER_ENABLE_PCA9685"] && !values["PCCONTROLLER_ENABLE_I2C_LCD"]:
		profile = FirmwareProfileState{ID: "motion-macro", Value: 1, Label: "Motion macro", Description: "Constrained motion/macro image without local sensor/PWM/LCD engines."}
	}
	profile.Docs = firmwareFeatureDocs + "#profiles"
	profile.Source = firmwareRuntimeSource
	return profile
}

func LoadFirmwareFeatureMatrixManifest(path string) (FirmwareFeatureMatrix, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return FirmwareFeatureMatrix{}, err
	}
	var document struct {
		Build FirmwareFeatureMatrix `json:"build"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return FirmwareFeatureMatrix{}, err
	}
	if document.Build.Format != firmwareFeatureMatrixFormat {
		return FirmwareFeatureMatrix{}, fmt.Errorf("manifest has no %s build matrix", firmwareFeatureMatrixFormat)
	}
	return document.Build, nil
}

type FirmwareFeatureRenderOptions struct {
	Format        string
	RepositoryURL string
	Revision      string
}

func RenderFirmwareFeatureMatrix(output io.Writer, matrix FirmwareFeatureMatrix, options FirmwareFeatureRenderOptions) error {
	switch strings.ToLower(strings.TrimSpace(options.Format)) {
	case "", "unicode", "text":
		return renderFirmwareFeatureUnicode(output, matrix)
	case "markdown", "md", "github":
		return renderFirmwareFeatureMarkdown(output, matrix, options.RepositoryURL, options.Revision)
	case "json":
		encoded, err := json.MarshalIndent(matrix, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, string(encoded))
		return err
	default:
		return errors.New("feature format must be unicode, markdown, or json")
	}
}

func renderFirmwareFeatureUnicode(output io.Writer, matrix FirmwareFeatureMatrix) error {
	head := fmt.Sprintf("Firmware profile: %s (%d) · HELLO %s · capabilities %s", matrix.Profile.ID, matrix.Profile.Value, matrix.BuildFlagsHex, matrix.CapabilitiesHex)
	rows := [][]string{{"State", "Compile gate", "Runtime", "Feature"}}
	for _, feature := range matrix.Features {
		state := "— excluded"
		if feature.Enabled {
			state = "✓ included"
		}
		runtime := strings.Join(feature.Runtime, ", ")
		if runtime == "" {
			runtime = "—"
		}
		rows = append(rows, []string{state, feature.Macro, runtime, feature.Label})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, value := range row {
			if width := utf8.RuneCountInString(value); width > widths[column] {
				widths[column] = width
			}
		}
	}
	border := func(left, middle, right, fill string) string {
		parts := make([]string, len(widths))
		for index, width := range widths {
			parts[index] = strings.Repeat(fill, width+2)
		}
		return left + strings.Join(parts, middle) + right
	}
	fmt.Fprintln(output, head)
	fmt.Fprintln(output, border("┌", "┬", "┐", "─"))
	for rowIndex, row := range rows {
		fmt.Fprint(output, "│")
		for column, value := range row {
			padding := widths[column] - utf8.RuneCountInString(value)
			fmt.Fprintf(output, " %s%s │", value, strings.Repeat(" ", padding))
		}
		fmt.Fprintln(output)
		if rowIndex == 0 {
			fmt.Fprintln(output, border("├", "┼", "┤", "─"))
		}
	}
	fmt.Fprintln(output, border("└", "┴", "┘", "─"))
	return nil
}

func renderFirmwareFeatureMarkdown(output io.Writer, matrix FirmwareFeatureMatrix, repositoryURL, revision string) error {
	link := func(path string) string {
		if strings.TrimSpace(repositoryURL) == "" {
			return path
		}
		base := strings.TrimRight(repositoryURL, "/")
		ref := strings.TrimSpace(revision)
		if ref == "" {
			ref = "main"
		}
		parts := strings.SplitN(path, "#", 2)
		value := base + "/blob/" + ref + "/" + parts[0]
		if len(parts) == 2 {
			value += "#" + parts[1]
		}
		return value
	}
	fmt.Fprintf(output, "## 🧩 Firmware profile, gates, and capabilities\n\n")
	fmt.Fprintf(output, "| Profile | HELLO build flags | Capability mask |\n|---|---:|---:|\n")
	fmt.Fprintf(output, "| [%s](%s) (%d) | `%s` | `%s` |\n\n", matrix.Profile.Label, link(matrix.Profile.Docs), matrix.Profile.Value, matrix.BuildFlagsHex, matrix.CapabilitiesHex)
	fmt.Fprintln(output, "| State | Compile gate | Runtime evidence | Feature | References |")
	fmt.Fprintln(output, "|---|---|---|---|---|")
	for _, feature := range matrix.Features {
		state := "❌ excluded"
		if feature.Enabled {
			state = "✅ included"
		}
		runtime := strings.Join(feature.Runtime, ", ")
		if runtime == "" {
			runtime = "—"
		}
		fmt.Fprintf(output, "| %s | `%s` | %s | **%s** — %s | [docs](%s) · [source](%s) |\n",
			state, feature.Macro, runtime, feature.Label, feature.Description, link(feature.Docs), link(feature.Source))
	}
	fmt.Fprint(output, "\n<details>\n<summary><strong>Runtime capability bits</strong></summary>\n\n")
	fmt.Fprintln(output, "| State | Bit | Capability | References |")
	fmt.Fprintln(output, "|---|---:|---|---|")
	for _, capability := range matrix.Capabilities {
		state := "❌"
		if capability.Enabled {
			state = "✅"
		}
		fmt.Fprintf(output, "| %s | %d | **%s** — %s | [docs](%s) · [source](%s) |\n",
			state, capability.Bit, capability.Label, capability.Description, link(capability.Docs), link(capability.Source))
	}
	fmt.Fprintln(output, "\n</details>")
	return nil
}

// FirmwareFeatureDefaults is used by source/manifest contract tests.
func FirmwareFeatureDefaults() map[string]bool {
	values := make(map[string]bool, len(firmwareFeatureDefinitions))
	for _, definition := range firmwareFeatureDefinitions {
		values[definition.macro] = definition.defaultEnabled
	}
	return values
}

func WriteFirmwareFeatureTestConfig(output io.Writer) error {
	values := FirmwareFeatureDefaults()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writer := bufio.NewWriter(output)
	for _, key := range keys {
		value := 0
		if values[key] {
			value = 1
		}
		if _, err := fmt.Fprintf(writer, "#define %s %d\n", key, value); err != nil {
			return err
		}
	}
	return writer.Flush()
}
