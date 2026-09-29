// Package firmwarefeatures owns the finite, reproducible AVR compile-profile
// selector shared by persistent host configuration and programmer tooling.
package firmwarefeatures

import (
	"fmt"
	"sort"
	"strings"
)

// Feature is a reviewed compile-time firmware option, never a raw compiler
// flag.
type Feature string

const (
	EEPROMBootOpcodes Feature = "eeprom-boot-opcodes"
	EEPROMMenuLabels  Feature = "eeprom-menu-labels"
)

// Normalize validates, sorts, and de-duplicates named firmware features.
func Normalize(values []string) ([]Feature, error) {
	selected := make(map[Feature]struct{}, len(values))
	for _, value := range values {
		feature := Feature(strings.ToLower(strings.TrimSpace(value)))
		switch feature {
		case "":
			return nil, fmt.Errorf(
				"firmware feature must not be empty; supported: %s, %s",
				EEPROMBootOpcodes, EEPROMMenuLabels,
			)
		case EEPROMBootOpcodes, EEPROMMenuLabels:
			selected[feature] = struct{}{}
		default:
			return nil, fmt.Errorf(
				"unsupported firmware feature %q; supported: %s, %s",
				value, EEPROMBootOpcodes, EEPROMMenuLabels,
			)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	features := make([]Feature, 0, len(selected))
	for feature := range selected {
		features = append(features, feature)
	}
	sort.Slice(features, func(left, right int) bool {
		return features[left] < features[right]
	})
	return features, nil
}

// Names returns a detached string representation suitable for flags and JSON.
func Names(features []Feature) []string {
	if len(features) == 0 {
		return nil
	}
	names := make([]string, len(features))
	for index, feature := range features {
		names[index] = string(feature)
	}
	return names
}
