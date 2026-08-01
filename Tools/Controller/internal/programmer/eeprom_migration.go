package programmer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EEPROMDevelopmentV2VisibleMenuMask uint16 = 0x7FFF
	EEPROMSettingsEndExclusive         uint32 = EEPROMSettingsAddress + EEPROMSettingsRecordBytes
)

var eepromDevelopmentV2IdentityMenuOrder = [8]byte{
	0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC, 0xFE,
}

// EEPROMSettingsMigrationResult describes one host-only legacy settings
// conversion. The generated HEX contains no bytes outside the settings record.
type EEPROMSettingsMigrationResult struct {
	SourcePath          string `json:"source_path"`
	OutputPath          string `json:"output_path"`
	SourceSHA256        string `json:"source_sha256"`
	OutputSHA256        string `json:"output_sha256"`
	SourceFormat        string `json:"source_format"`
	OutputFormat        string `json:"output_format"`
	PreservedValueBytes uint32 `json:"preserved_value_bytes"`
	OutputStart         uint32 `json:"output_start"`
	OutputEndExclusive  uint32 `json:"output_end_exclusive"`
	OutputBytes         uint32 `json:"output_bytes"`
	OutputChecksum      byte   `json:"output_checksum"`
}

// MigrateLegacyEEPROMSettings converts a validated, unversioned 19-byte
// settings value plus CRC-8 into the development-v2 29-byte value plus CRC-8.
// It is strictly off-device and emits a sparse HEX for addresses 32..61 only.
func MigrateLegacyEEPROMSettings(
	sourcePath, outputPath string,
) (EEPROMSettingsMigrationResult, error) {
	var result EEPROMSettingsMigrationResult
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(outputPath) == "" {
		return result, errors.New("EEPROM migration requires input and output paths")
	}
	sourceAbsolute, err := filepath.Abs(sourcePath)
	if err != nil {
		return result, fmt.Errorf("resolve EEPROM migration input: %w", err)
	}
	outputAbsolute, err := filepath.Abs(outputPath)
	if err != nil {
		return result, fmt.Errorf("resolve EEPROM migration output: %w", err)
	}
	if strings.EqualFold(filepath.Clean(sourceAbsolute), filepath.Clean(outputAbsolute)) {
		return result, errors.New("EEPROM migration output must differ from input")
	}
	if _, err := os.Stat(outputAbsolute); err == nil {
		return result, fmt.Errorf("EEPROM migration output already exists: %s", outputAbsolute)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("inspect EEPROM migration output: %w", err)
	}

	document, err := LoadIntelHex(sourceAbsolute)
	if err != nil {
		return result, err
	}
	for address := range document.Image.data {
		if address >= PCControllerEEPROMBytes {
			return result, fmt.Errorf(
				"EEPROM HEX address 0x%X exceeds ATmega328P EEPROM", address,
			)
		}
	}
	legacyRecord, err := document.Image.BytesAt(
		EEPROMSettingsAddress,
		EEPROMSettingsLegacyRecord,
	)
	if err != nil {
		return result, fmt.Errorf("legacy EEPROM settings record is absent: %w", err)
	}
	// The layouts are intentionally unversioned, and an older firmware ignores
	// bytes after its CRC. Those bytes can therefore contain stale data from an
	// earlier development image. Prefer a fully valid current record when one
	// exists; otherwise authenticate the legacy prefix by its own CRC/fields.
	if currentRecord, currentErr := document.Image.BytesAt(
		EEPROMSettingsAddress,
		EEPROMSettingsRecordBytes,
	); currentErr == nil {
		current := decodeOfflineSettingsRecord(currentRecord, false)
		if current.Valid {
			return result, errors.New(
				"EEPROM settings are already development-v2; require legacy/unversioned-19+crc8",
			)
		}
	}
	decoded := decodeOfflineSettingsRecord(legacyRecord, true)
	if !decoded.Valid {
		return result, fmt.Errorf("legacy EEPROM settings are invalid: %s", decoded.Issue)
	}

	currentRecord := make([]byte, EEPROMSettingsRecordBytes)
	copy(currentRecord[:EEPROMSettingsLegacyBytes], legacyRecord[:EEPROMSettingsLegacyBytes])
	binary.LittleEndian.PutUint16(
		currentRecord[EEPROMSettingsLegacyBytes:EEPROMSettingsLegacyBytes+2],
		EEPROMDevelopmentV2VisibleMenuMask,
	)
	copy(
		currentRecord[EEPROMSettingsLegacyBytes+2:EEPROMSettingsValueBytes],
		eepromDevelopmentV2IdentityMenuOrder[:],
	)
	currentRecord[EEPROMSettingsValueBytes] = avrCRC8(
		currentRecord[:EEPROMSettingsValueBytes],
	)

	outputImage := &IntelHexImage{data: make(map[uint32]byte, len(currentRecord))}
	for offset, value := range currentRecord {
		outputImage.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	canonical, err := outputImage.Canonical()
	if err != nil {
		return result, fmt.Errorf("encode migrated EEPROM settings: %w", err)
	}
	if err := verifyMigratedEEPROMSettings(canonical, currentRecord); err != nil {
		return result, err
	}
	if err := atomicCreateFile(outputAbsolute, canonical, 0o600); err != nil {
		return result, fmt.Errorf("write migrated EEPROM HEX: %w", err)
	}

	return EEPROMSettingsMigrationResult{
		SourcePath: sourceAbsolute, OutputPath: outputAbsolute,
		SourceSHA256: document.SourceSHA256, OutputSHA256: sha256Hex(canonical),
		SourceFormat:        decoded.Format,
		OutputFormat:        "development-v2/unversioned-29+crc8",
		PreservedValueBytes: EEPROMSettingsLegacyBytes,
		OutputStart:         EEPROMSettingsAddress, OutputEndExclusive: EEPROMSettingsEndExclusive,
		OutputBytes:    EEPROMSettingsRecordBytes,
		OutputChecksum: currentRecord[EEPROMSettingsValueBytes],
	}, nil
}

func verifyMigratedEEPROMSettings(canonical, expected []byte) error {
	image, err := ParseIntelHex(strings.NewReader(string(canonical)))
	if err != nil {
		return fmt.Errorf("verify migrated EEPROM HEX: %w", err)
	}
	inspection, err := image.Inspect()
	if err != nil {
		return fmt.Errorf("inspect migrated EEPROM HEX: %w", err)
	}
	if inspection.DataBytes != EEPROMSettingsRecordBytes ||
		inspection.MinimumAddress != EEPROMSettingsAddress ||
		inspection.MaximumAddress+1 != EEPROMSettingsEndExclusive ||
		len(inspection.Segments) != 1 {
		return fmt.Errorf(
			"migrated EEPROM HEX escaped settings range: bytes=%d range=0x%X..0x%X segments=%d",
			inspection.DataBytes,
			inspection.MinimumAddress,
			inspection.MaximumAddress,
			len(inspection.Segments),
		)
	}
	actual, err := image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
	if err != nil {
		return fmt.Errorf("verify migrated EEPROM settings bytes: %w", err)
	}
	if len(actual) != len(expected) {
		return errors.New("migrated EEPROM settings length changed during encoding")
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("migrated EEPROM settings byte %d changed during encoding", index)
		}
	}
	decoded := decodeOfflineSettings(image)
	if !decoded.Valid || decoded.Legacy {
		return fmt.Errorf("migrated EEPROM settings failed development-v2 validation: %s", decoded.Issue)
	}
	return nil
}
