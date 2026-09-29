package programmer

import (
	"pccontroller.local/controller/internal/firmwarefeatures"
)

// FirmwareFeature is a reviewed, reproducible compile-time firmware option.
// It is deliberately not a raw compiler flag: callers can select only values
// listed below, and every selection is included in the firmware identity.
type FirmwareFeature = firmwarefeatures.Feature

const (
	// FirmwareFeatureEEPROMBootOpcodes enables the board's validated EEPROM
	// boot-opcode executor. The firmware remains byte-for-byte feature-off by
	// default, so existing provisioning and identities keep their behavior.
	FirmwareFeatureEEPROMBootOpcodes = firmwarefeatures.EEPROMBootOpcodes
	// FirmwareFeatureEEPROMMenuLabels relocates the fixed seven-segment menu
	// labels to the dedicated EEPROM tail region when that firmware feature is
	// compiled into the image.
	FirmwareFeatureEEPROMMenuLabels = firmwarefeatures.EEPROMMenuLabels
)

// NormalizeFirmwareFeatures validates, sorts, and de-duplicates named
// firmware features. A stable order keeps cache paths, identities, manifests,
// and Arduino compiler arguments reproducible.
func NormalizeFirmwareFeatures(values []string) ([]FirmwareFeature, error) {
	return firmwarefeatures.Normalize(values)
}

func firmwareFeatureNames(features []FirmwareFeature) []string {
	return firmwarefeatures.Names(features)
}

// FirmwareFeatureNames returns a detached representation for configuration,
// public API, and command-plan surfaces.
func FirmwareFeatureNames(features []FirmwareFeature) []string {
	return firmwarefeatures.Names(features)
}

func firmwareFeatureBuildDefines(features []FirmwareFeature) []string {
	// Do not multiply an untrusted slice length to guess capacity: the
	// multiplication can overflow before make sees it. The feature set is tiny,
	// and append's checked growth is both safe and immaterial here.
	defines := make([]string, 0, len(features))
	for _, feature := range features {
		switch feature {
		case FirmwareFeatureEEPROMBootOpcodes:
			defines = append(defines,
				"-DPCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES=1",
				"-DPCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES=0",
			)
		case FirmwareFeatureEEPROMMenuLabels:
			defines = append(defines, "-DPCCONTROLLER_ENABLE_EEPROM_MENU_LABELS=1")
		}
	}
	return defines
}
