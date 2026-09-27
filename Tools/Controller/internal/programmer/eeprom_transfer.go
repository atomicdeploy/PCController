package programmer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EEPROMSettingsEndExclusive = EEPROMSettingsAddress + EEPROMSettingsRecordBytes

// EEPROMTransferResult describes a file-only current-layout settings export,
// import, or full EEPROM restore artifact prepared from a validated backup.
type EEPROMTransferResult struct {
	Action             string `json:"action"`
	BackupManifest     string `json:"backup_manifest"`
	BackupManifestHash string `json:"backup_manifest_sha256"`
	BackupReference    string `json:"backup_reference"`
	SettingsPath       string `json:"settings_path,omitempty"`
	SettingsSHA256     string `json:"settings_sha256,omitempty"`
	OutputPath         string `json:"output_path"`
	OutputSHA256       string `json:"output_sha256"`
	OutputBytes        int    `json:"output_bytes"`
	SettingsFormat     string `json:"settings_format"`
}

// ExportCurrentEEPROMSettings extracts only the current settings record from
// a complete validated programming backup. It never opens a device.
func ExportCurrentEEPROMSettings(
	manifestPath, outputPath string,
) (EEPROMTransferResult, error) {
	backup, document, decoded, err := loadCurrentBackupEEPROM(manifestPath)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	_ = document
	record, err := encodeCurrentEEPROMSettingsRecord(decoded.Settings.Values)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	image := &IntelHexImage{data: make(map[uint32]byte, len(record))}
	for offset, value := range record {
		image.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := image.Canonical()
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	output, err := createEEPROMTransferOutput(outputPath, content)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	return newEEPROMTransferResult(
		"export", backup, "schema1/core22+name9+crc8", "", "", output, content,
	), nil
}

// ImportCurrentEEPROMSettings overlays one validated sparse current settings
// artifact onto the complete EEPROM image from a validated backup. Every byte
// outside the settings record is preserved in the generated restore image.
func ImportCurrentEEPROMSettings(
	manifestPath, settingsPath, outputPath string,
) (EEPROMTransferResult, error) {
	backup, base, _, err := loadCurrentBackupEEPROM(manifestPath)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	settingsDocument, settingsDecoded, err := loadCurrentSettingsArtifact(settingsPath)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	record, err := settingsDocument.Image.BytesAt(
		EEPROMSettingsAddress, EEPROMSettingsRecordBytes,
	)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	merged := &IntelHexImage{data: make(map[uint32]byte, len(base.Image.data))}
	for address, value := range base.Image.data {
		merged.data[address] = value
	}
	for offset := uint32(0); offset < EEPROMMenuLayoutRecordBytes; offset++ {
		merged.data[EEPROMSettingsAddress+offset] = 0xFF
	}
	for offset, value := range record {
		merged.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := merged.Canonical()
	if err != nil {
		return EEPROMTransferResult{}, fmt.Errorf("encode imported EEPROM image: %w", err)
	}
	verification, err := ParseIntelHex(strings.NewReader(string(content)))
	if err != nil {
		return EEPROMTransferResult{}, fmt.Errorf("verify imported EEPROM image: %w", err)
	}
	if err := requireFullEEPROMImage(verification); err != nil {
		return EEPROMTransferResult{}, err
	}
	if decoded := decodeOfflineSettings(verification); !decoded.Supported || !decoded.Valid {
		return EEPROMTransferResult{}, fmt.Errorf(
			"imported EEPROM settings failed current semantic validation: %s", decoded.Issue,
		)
	}
	output, err := createEEPROMTransferOutput(outputPath, content)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	settingsAbsolute, err := filepath.Abs(settingsPath)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	return newEEPROMTransferResult(
		"import", backup, settingsDecoded.Settings.Format,
		settingsAbsolute, settingsDocument.SourceSHA256, output, content,
	), nil
}

// PrepareCurrentEEPROMRestore copies the complete EEPROM artifact from a
// validated backup to a no-overwrite output path for an explicit later write.
func PrepareCurrentEEPROMRestore(
	manifestPath, outputPath string,
) (EEPROMTransferResult, error) {
	backup, document, decoded, err := loadCurrentBackupEEPROM(manifestPath)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	migrated := &IntelHexImage{data: make(map[uint32]byte, len(document.Image.data))}
	for address, value := range document.Image.data {
		migrated.data[address] = value
	}
	for offset := uint32(0); offset < EEPROMMenuLayoutRecordBytes; offset++ {
		migrated.data[EEPROMSettingsAddress+offset] = 0xFF
	}
	record, err := encodeCurrentEEPROMSettingsRecord(decoded.Settings.Values)
	if err != nil {
		return EEPROMTransferResult{}, fmt.Errorf("semantically migrate backup settings: %w", err)
	}
	for offset, value := range record {
		migrated.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := migrated.Canonical()
	if err != nil {
		return EEPROMTransferResult{}, fmt.Errorf("encode restore EEPROM image: %w", err)
	}
	output, err := createEEPROMTransferOutput(outputPath, content)
	if err != nil {
		return EEPROMTransferResult{}, err
	}
	return newEEPROMTransferResult(
		"restore", backup, "schema1/core22+name9+crc8", "", "", output, content,
	), nil
}

func loadCurrentBackupEEPROM(
	manifestPath string,
) (ValidatedBackup, *IntelHexDocument, OfflineEEPROMDecode, error) {
	backup, err := ValidateBackupManifest(manifestPath)
	if err != nil {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, err
	}
	eeprom := backup.Files["eeprom"]
	document, err := LoadIntelHex(eeprom.Path)
	if err != nil {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, err
	}
	if err := requireFullEEPROMImage(document.Image); err != nil {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, fmt.Errorf(
			"validated backup EEPROM is not a complete restore base: %w", err,
		)
	}
	decoded, err := DecodeOfflineEEPROMHex(eeprom.Path)
	if err != nil {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, err
	}
	if !decoded.Settings.Supported {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, fmt.Errorf(
			"unsupported backup settings layout: %s", decoded.Settings.Issue,
		)
	}
	if !decoded.Settings.Valid {
		return ValidatedBackup{}, nil, OfflineEEPROMDecode{}, fmt.Errorf(
			"backup settings fail current semantic validation: %s", decoded.Settings.Issue,
		)
	}
	return backup, document, decoded, nil
}

func loadCurrentSettingsArtifact(
	path string,
) (*IntelHexDocument, OfflineEEPROMDecode, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return nil, OfflineEEPROMDecode{}, err
	}
	inspection, err := document.Image.Inspect()
	if err != nil {
		return nil, OfflineEEPROMDecode{}, err
	}
	if inspection.DataBytes != EEPROMSettingsRecordBytes ||
		inspection.MinimumAddress != EEPROMSettingsAddress ||
		inspection.MaximumAddress+1 != EEPROMSettingsEndExclusive ||
		len(inspection.Segments) != 1 {
		return nil, OfflineEEPROMDecode{}, fmt.Errorf(
			"unsupported settings artifact: require exactly %d bytes at EEPROM 0x%04X..0x%04X",
			EEPROMSettingsRecordBytes,
			EEPROMSettingsAddress,
			EEPROMSettingsEndExclusive-1,
		)
	}
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		return nil, OfflineEEPROMDecode{}, err
	}
	if !decoded.Settings.Supported || !decoded.Settings.Valid {
		return nil, OfflineEEPROMDecode{}, fmt.Errorf(
			"settings artifact is not the current semantic layout: %s", decoded.Settings.Issue,
		)
	}
	return document, decoded, nil
}

func requireFullEEPROMImage(image *IntelHexImage) error {
	inspection, err := image.Inspect()
	if err != nil {
		return err
	}
	if inspection.DataBytes != PCControllerEEPROMBytes ||
		inspection.MinimumAddress != 0 || inspection.MaximumAddress+1 != PCControllerEEPROMBytes {
		return fmt.Errorf(
			"require all %d EEPROM bytes at 0x0000..0x%04X; image has %d bytes at 0x%04X..0x%04X",
			PCControllerEEPROMBytes,
			PCControllerEEPROMBytes-1,
			inspection.DataBytes,
			inspection.MinimumAddress,
			inspection.MaximumAddress,
		)
	}
	return nil
}

func createEEPROMTransferOutput(path string, content []byte) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("EEPROM transfer requires an output path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := atomicCreateFile(absolute, content, 0o600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("EEPROM transfer output already exists: %s", absolute)
		}
		return "", fmt.Errorf("write EEPROM transfer output: %w", err)
	}
	return absolute, nil
}

func newEEPROMTransferResult(
	action string,
	backup ValidatedBackup,
	settingsFormat, settingsPath, settingsHash, outputPath string,
	content []byte,
) EEPROMTransferResult {
	return EEPROMTransferResult{
		Action:         action,
		BackupManifest: backup.ManifestPath, BackupManifestHash: backup.ManifestSHA256,
		BackupReference: backup.Manifest.Reference, SettingsPath: settingsPath,
		SettingsSHA256: settingsHash, OutputPath: outputPath,
		OutputSHA256: sha256Hex(content), OutputBytes: len(content),
		SettingsFormat: settingsFormat,
	}
}
