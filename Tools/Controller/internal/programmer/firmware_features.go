package programmer

import (
	"fmt"
	"sort"
	"strings"
)

// FirmwareFeature is a reviewed, reproducible compile-time firmware option.
// It is deliberately not a raw compiler flag: callers can select only values
// listed below, and every selection is included in the firmware identity.
type FirmwareFeature string

const (
	// FirmwareFeatureEEPROMBootOpcodes enables the board's validated EEPROM
	// boot-opcode executor. The firmware remains byte-for-byte feature-off by
	// default, so existing provisioning and identities keep their behavior.
	FirmwareFeatureEEPROMBootOpcodes FirmwareFeature = "eeprom-boot-opcodes"
	// FirmwareFeatureEEPROMMenuLabels relocates the fixed seven-segment menu
	// labels to the dedicated EEPROM tail region when that firmware feature is
	// compiled into the image.
	FirmwareFeatureEEPROMMenuLabels FirmwareFeature = "eeprom-menu-labels"
)

// NormalizeFirmwareFeatures validates, sorts, and de-duplicates named
// firmware features. A stable order keeps cache paths, identities, manifests,
// and Arduino compiler arguments reproducible.
func NormalizeFirmwareFeatures(values []string) ([]FirmwareFeature, error) {
	selected := make(map[FirmwareFeature]struct{}, len(values))
	for _, value := range values {
		feature := FirmwareFeature(strings.ToLower(strings.TrimSpace(value)))
		switch feature {
		case "":
			return nil, fmt.Errorf("firmware feature must not be empty; supported: %s, %s", FirmwareFeatureEEPROMBootOpcodes, FirmwareFeatureEEPROMMenuLabels)
		case FirmwareFeatureEEPROMBootOpcodes, FirmwareFeatureEEPROMMenuLabels:
			selected[feature] = struct{}{}
		default:
			return nil, fmt.Errorf("unsupported firmware feature %q; supported: %s, %s", value, FirmwareFeatureEEPROMBootOpcodes, FirmwareFeatureEEPROMMenuLabels)
		}
	}
	features := make([]FirmwareFeature, 0, len(selected))
	for feature := range selected {
		features = append(features, feature)
	}
	sort.Slice(features, func(left, right int) bool { return features[left] < features[right] })
	return features, nil
}

func firmwareFeatureNames(features []FirmwareFeature) []string {
	if len(features) == 0 {
		return nil
	}
	names := make([]string, len(features))
	for index, feature := range features {
		names[index] = string(feature)
	}
	return names
}

func firmwareFeatureBuildDefines(features []FirmwareFeature) []string {
	defines := make([]string, 0, len(features))
	for _, feature := range features {
		switch feature {
		case FirmwareFeatureEEPROMBootOpcodes:
			defines = append(defines, "-DPCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES=1")
		case FirmwareFeatureEEPROMMenuLabels:
			defines = append(defines, "-DPCCONTROLLER_ENABLE_EEPROM_MENU_LABELS=1")
		}
	}
	return defines
}
