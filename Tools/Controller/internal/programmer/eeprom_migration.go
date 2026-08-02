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
	// EEPROMMigrationSchema versions the host-side migration report and policy.
	// It is deliberately unrelated to the unversioned MCU EEPROM record.
	EEPROMMigrationSchema = 1

	EEPROMMigrationLegacyV1 = "legacy-v1/unversioned-19+crc8"
	EEPROMMigrationLegacyV2 = "development-v2/unversioned-29+crc8"
	EEPROMMigrationCurrent  = "current/unversioned-31+crc8"

	eepromLegacyV1ValueBytes   uint32 = 19
	eepromLegacyV2ValueBytes   uint32 = 29
	eepromLegacyMenuPageCount  byte   = 15
	eepromCurrentMenuPageCount byte   = 14
)

type EEPROMMigrationOptions struct {
	BackupManifest     string
	SourceFormat       string
	ExpectedEEPROMHash string
	OutputPath         string
}

// EEPROMMigrationResult is the durable evidence returned by a file-only
// migration. The output is a complete EEPROM restore candidate; applying it is
// intentionally a separate, explicitly authorized programming operation.
type EEPROMMigrationResult struct {
	Schema                   int      `json:"schema"`
	Action                   string   `json:"action"`
	BackupManifest           string   `json:"backup_manifest"`
	BackupManifestSHA256     string   `json:"backup_manifest_sha256"`
	BackupReference          string   `json:"backup_reference"`
	SourceEEPROM             string   `json:"source_eeprom"`
	SourceEEPROMSHA256       string   `json:"source_eeprom_sha256"`
	SourceFormat             string   `json:"source_format"`
	TargetFormat             string   `json:"target_format"`
	OutputPath               string   `json:"output_path"`
	OutputSHA256             string   `json:"output_sha256"`
	ReadbackSHA256           string   `json:"readback_sha256"`
	ReadbackVerified         bool     `json:"readback_verified"`
	OutputDataBytes          uint32   `json:"output_data_bytes"`
	PreservedOutsideSettings uint32   `json:"preserved_outside_settings_bytes"`
	Decisions                []string `json:"decisions"`
}

type eepromMigrationProfile struct {
	format     string
	valueBytes uint32
	hasMenu    bool
}

type eepromMigrationOutputWriter func(path string, content []byte) error

// MigrateLegacyEEPROMBackup migrates one explicitly selected historical
// settings layout inside a complete, hash-validated backup. It never opens a
// serial port, mutates the backup, or writes the board.
func MigrateLegacyEEPROMBackup(
	options EEPROMMigrationOptions,
) (EEPROMMigrationResult, error) {
	return migrateLegacyEEPROMBackup(options, func(path string, content []byte) error {
		return atomicCreateFile(path, content, 0o600)
	})
}

func migrateLegacyEEPROMBackup(
	options EEPROMMigrationOptions,
	writeOutput eepromMigrationOutputWriter,
) (EEPROMMigrationResult, error) {
	var result EEPROMMigrationResult
	if strings.TrimSpace(options.BackupManifest) == "" ||
		strings.TrimSpace(options.SourceFormat) == "" ||
		strings.TrimSpace(options.ExpectedEEPROMHash) == "" ||
		strings.TrimSpace(options.OutputPath) == "" {
		return result, errors.New(
			"EEPROM migration requires backup manifest, source format, expected EEPROM SHA-256, and output path",
		)
	}
	if writeOutput == nil {
		return result, errors.New("EEPROM migration output writer is nil")
	}
	profile, err := resolveEEPROMMigrationProfile(options.SourceFormat)
	if err != nil {
		return result, err
	}
	expectedEEPROMHash, err := normalizeRequiredSHA256(options.ExpectedEEPROMHash)
	if err != nil {
		return result, fmt.Errorf("expected EEPROM SHA-256: %w", err)
	}

	backup, err := ValidateBackupManifest(options.BackupManifest)
	if err != nil {
		return result, fmt.Errorf("validate migration backup: %w", err)
	}
	eeprom := backup.Files["eeprom"]
	if eeprom.SHA256 != expectedEEPROMHash {
		return result, fmt.Errorf(
			"validated backup EEPROM SHA-256 is %s, expected %s",
			eeprom.SHA256,
			expectedEEPROMHash,
		)
	}
	document, err := LoadIntelHex(eeprom.Path)
	if err != nil {
		return result, err
	}
	if err := requireFullEEPROMImage(document.Image); err != nil {
		return result, fmt.Errorf("migration backup EEPROM is incomplete: %w", err)
	}
	if current, readErr := document.Image.BytesAt(
		EEPROMSettingsAddress, EEPROMSettingsRecordBytes,
	); readErr == nil {
		decoded := decodeOfflineSettingsRecord(current)
		if decoded.Supported && decoded.Valid {
			return result, fmt.Errorf(
				"backup already contains %s settings; refusing legacy migration",
				EEPROMMigrationCurrent,
			)
		}
	}

	legacyRecord, err := document.Image.BytesAt(
		EEPROMSettingsAddress, profile.valueBytes+1,
	)
	if err != nil {
		return result, fmt.Errorf("source settings record is absent: %w", err)
	}
	legacyValues, err := validateLegacyEEPROMSettings(profile, legacyRecord)
	if err != nil {
		return result, err
	}
	currentRecord, decisions, err := migrateLegacyEEPROMSettings(profile, legacyValues)
	if err != nil {
		return result, err
	}

	merged := &IntelHexImage{data: make(map[uint32]byte, len(document.Image.data))}
	for address, value := range document.Image.data {
		merged.data[address] = value
	}
	for offset, value := range currentRecord {
		merged.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := merged.Canonical()
	if err != nil {
		return result, fmt.Errorf("encode migrated EEPROM image: %w", err)
	}
	if err := verifyMigratedEEPROMImage(merged, document.Image); err != nil {
		return result, err
	}

	outputAbsolute, err := resolveEEPROMMigrationOutput(options.OutputPath, backup.Directory)
	if err != nil {
		return result, err
	}
	if err := writeOutput(outputAbsolute, content); err != nil {
		if errors.Is(err, os.ErrExist) {
			return result, fmt.Errorf("EEPROM migration output already exists: %s", outputAbsolute)
		}
		return result, fmt.Errorf("write EEPROM migration output: %w", err)
	}
	readback, err := verifyPersistedEEPROMMigration(outputAbsolute, merged, content)
	if err != nil {
		_ = os.Remove(outputAbsolute)
		return result, err
	}

	// Revalidate after output creation so a source backup mutation during the
	// operation cannot produce a successful migration report.
	revalidated, err := ValidateBackupManifest(options.BackupManifest)
	if err != nil {
		_ = os.Remove(outputAbsolute)
		return result, fmt.Errorf("revalidate migration backup: %w", err)
	}
	if revalidated.ManifestSHA256 != backup.ManifestSHA256 ||
		revalidated.Files["eeprom"].SHA256 != eeprom.SHA256 {
		_ = os.Remove(outputAbsolute)
		return result, errors.New("migration backup changed while creating output")
	}

	return EEPROMMigrationResult{
		Schema: EEPROMMigrationSchema, Action: "migrate",
		BackupManifest: backup.ManifestPath, BackupManifestSHA256: backup.ManifestSHA256,
		BackupReference: backup.Manifest.Reference,
		SourceEEPROM:    eeprom.Path, SourceEEPROMSHA256: eeprom.SHA256,
		SourceFormat: profile.format, TargetFormat: EEPROMMigrationCurrent,
		OutputPath: outputAbsolute, OutputSHA256: sha256Hex(content),
		ReadbackSHA256: readback.SourceSHA256, ReadbackVerified: true,
		OutputDataBytes:          PCControllerEEPROMBytes,
		PreservedOutsideSettings: PCControllerEEPROMBytes - EEPROMSettingsRecordBytes,
		Decisions:                decisions,
	}, nil
}

func resolveEEPROMMigrationProfile(value string) (eepromMigrationProfile, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EEPROMMigrationLegacyV1:
		return eepromMigrationProfile{
			format: EEPROMMigrationLegacyV1, valueBytes: eepromLegacyV1ValueBytes,
		}, nil
	case EEPROMMigrationLegacyV2:
		return eepromMigrationProfile{
			format: EEPROMMigrationLegacyV2, valueBytes: eepromLegacyV2ValueBytes, hasMenu: true,
		}, nil
	default:
		return eepromMigrationProfile{}, fmt.Errorf(
			"unsupported EEPROM source format %q; require %q or %q",
			value,
			EEPROMMigrationLegacyV1,
			EEPROMMigrationLegacyV2,
		)
	}
}

func validateLegacyEEPROMSettings(
	profile eepromMigrationProfile,
	record []byte,
) ([]byte, error) {
	if len(record) != int(profile.valueBytes+1) {
		return nil, fmt.Errorf(
			"%s settings record is %d bytes, require %d",
			profile.format,
			len(record),
			profile.valueBytes+1,
		)
	}
	values := append([]byte(nil), record[:len(record)-1]...)
	stored := record[len(record)-1]
	computed := avrCRC8(values)
	if stored != computed {
		return nil, fmt.Errorf(
			"%s settings CRC-8 mismatch: stored 0x%02X computed 0x%02X",
			profile.format,
			stored,
			computed,
		)
	}
	var issues []string
	if values[1] > 2 {
		issues = append(issues, "illumination mode exceeds 2")
	}
	if values[4] > 7 {
		issues = append(issues, "display brightness exceeds 7")
	}
	if values[6] > 2 {
		issues = append(issues, "PWM boot mode exceeds 2")
	}
	period := binary.LittleEndian.Uint16(values[7:9])
	if period != 0 && period < 100 {
		issues = append(issues, "non-zero stream period is below 100 ms")
	}
	if values[17] >= eepromLegacyMenuPageCount {
		issues = append(issues, "default menu page exceeds 14")
	}
	if profile.hasMenu {
		issues = append(issues, validateLegacyV2MenuLayout(values)...)
	}
	if len(issues) != 0 {
		return nil, fmt.Errorf(
			"%s settings fail semantic validation: %s",
			profile.format,
			strings.Join(issues, "; "),
		)
	}
	return values, nil
}

func validateLegacyV2MenuLayout(values []byte) []string {
	const allLegacyPages uint16 = 0x7FFF
	mask := binary.LittleEndian.Uint16(values[19:21])
	var issues []string
	if mask == 0 || mask&^allLegacyPages != 0 {
		issues = append(issues, "visible menu mask is empty or exceeds pages 0..14")
	} else if mask&(uint16(1)<<values[17]) == 0 {
		issues = append(issues, "default menu page is hidden")
	}
	order := values[21:29]
	if order[7]&0xF0 != 0xF0 {
		issues = append(issues, "unused high menu-order nibble is not 0xF")
	}
	var seen uint16
	for rank := byte(0); rank < eepromLegacyMenuPageCount; rank++ {
		page := unpackMenuPage(order, rank)
		if page >= eepromLegacyMenuPageCount {
			issues = append(issues, fmt.Sprintf("menu-order rank %d has invalid page %d", rank, page))
			continue
		}
		bit := uint16(1) << page
		if seen&bit != 0 {
			issues = append(issues, fmt.Sprintf("menu-order page %d is duplicated", page))
			continue
		}
		seen |= bit
	}
	if seen != allLegacyPages {
		issues = append(issues, "menu order is not a permutation of pages 0..14")
	}
	return issues
}

func migrateLegacyEEPROMSettings(
	profile eepromMigrationProfile,
	legacy []byte,
) ([]byte, []string, error) {
	current := make([]byte, EEPROMSettingsRecordBytes)
	copy(current[:19], legacy[:19])

	legacyFlags := legacy[0]
	current[0] = legacyFlags &^ 0x82 // Old reserved bit and break selector are not current flags.
	current[6] = 0                   // Retired PWM boot modes never become output-restore flags.
	current[28] = 0                  // Display-off while closed; 2-second motion exit hold.
	current[29] = 0                  // Never synthesize a remembered live relay mask.
	current[30] = 1
	if legacyFlags&0x80 != 0 {
		current[30] = 100
	}

	decisions := []string{
		"cleared retired/reserved flag bits instead of creating programming mode",
		"retired PWM boot mode mapped to output-persistence off",
		"relay restore mask initialized to zero",
		"display closed brightness initialized off with a two-second motion exit hold",
	}
	if legacyFlags&0x80 != 0 {
		decisions = append(decisions, "legacy extended motion break mapped to 100 ms")
	} else {
		decisions = append(decisions, "legacy compact motion break mapped to 1 ms")
	}

	if !profile.hasMenu {
		binary.LittleEndian.PutUint16(current[19:21], 0x3FFF)
		copy(current[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
		if current[17] >= eepromCurrentMenuPageCount {
			current[17] = 0
			decisions = append(decisions, "removed legacy page 14 default and selected page 0")
		}
		decisions = append(decisions, "initialized all current menu pages visible in identity order")
	} else {
		legacyMask := binary.LittleEndian.Uint16(legacy[19:21])
		currentMask := legacyMask & 0x3FFF
		legacyOrder := legacy[21:29]
		currentOrder := make([]byte, 7)
		currentRank := byte(0)
		for legacyRank := byte(0); legacyRank < eepromLegacyMenuPageCount; legacyRank++ {
			page := unpackMenuPage(legacyOrder, legacyRank)
			if page == 14 {
				continue
			}
			packMenuPage(currentOrder, currentRank, page)
			currentRank++
		}
		if currentRank != eepromCurrentMenuPageCount {
			return nil, nil, errors.New("legacy menu order did not yield fourteen current pages")
		}
		if currentMask == 0 {
			currentMask = 0x3FFF
			decisions = append(decisions, "removed legacy-only page 14 and restored all current pages visible")
		} else {
			decisions = append(decisions, "removed legacy-only page 14 while preserving current menu visibility/order")
		}
		binary.LittleEndian.PutUint16(current[19:21], currentMask)
		copy(current[21:28], currentOrder)
		if current[17] >= eepromCurrentMenuPageCount ||
			currentMask&(uint16(1)<<current[17]) == 0 {
			current[17] = firstVisibleMenuPage(currentMask, currentOrder)
			decisions = append(decisions, "selected the first retained visible page as the current default")
		}
	}
	current[EEPROMSettingsValueBytes] = avrCRC8(current[:EEPROMSettingsValueBytes])
	decoded := decodeOfflineSettingsRecord(current)
	if !decoded.Supported || !decoded.Valid {
		return nil, nil, fmt.Errorf(
			"migrated settings fail current semantic validation: %s",
			decoded.Issue,
		)
	}
	return current, decisions, nil
}

func verifyMigratedEEPROMImage(migrated, source *IntelHexImage) error {
	if err := requireFullEEPROMImage(migrated); err != nil {
		return fmt.Errorf("migrated EEPROM is incomplete: %w", err)
	}
	decoded := decodeOfflineSettings(migrated)
	if !decoded.Supported || !decoded.Valid {
		return fmt.Errorf("migrated EEPROM settings are invalid: %s", decoded.Issue)
	}
	for address := uint32(0); address < PCControllerEEPROMBytes; address++ {
		if address >= EEPROMSettingsAddress &&
			address < EEPROMSettingsAddress+EEPROMSettingsRecordBytes {
			continue
		}
		sourceValue, sourcePresent := source.Byte(address)
		migratedValue, migratedPresent := migrated.Byte(address)
		if !sourcePresent || !migratedPresent || sourceValue != migratedValue {
			return fmt.Errorf("migration changed EEPROM byte outside settings at 0x%04X", address)
		}
	}
	return nil
}

func resolveEEPROMMigrationOutput(path, backupDirectory string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve EEPROM migration output: %w", err)
	}
	if pathWithin(backupDirectory, absolute) {
		return "", errors.New("EEPROM migration output must be outside the immutable backup directory")
	}
	return absolute, nil
}

func verifyPersistedEEPROMMigration(
	path string,
	expected *IntelHexImage,
	expectedContent []byte,
) (*IntelHexDocument, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return nil, fmt.Errorf("read back EEPROM migration output: %w", err)
	}
	expectedHash := sha256Hex(expectedContent)
	if document.SourceSHA256 != expectedHash {
		return nil, fmt.Errorf(
			"EEPROM migration readback SHA-256 mismatch: got %s require %s",
			document.SourceSHA256,
			expectedHash,
		)
	}
	if err := requireFullEEPROMImage(document.Image); err != nil {
		return nil, fmt.Errorf("EEPROM migration readback is incomplete: %w", err)
	}
	for address := uint32(0); address < PCControllerEEPROMBytes; address++ {
		want, wantPresent := expected.Byte(address)
		got, gotPresent := document.Image.Byte(address)
		if !wantPresent || !gotPresent || want != got {
			return nil, fmt.Errorf("EEPROM migration readback differs at 0x%04X", address)
		}
	}
	decoded := decodeOfflineSettings(document.Image)
	if !decoded.Supported || !decoded.Valid {
		return nil, fmt.Errorf("EEPROM migration readback settings are invalid: %s", decoded.Issue)
	}
	return document, nil
}

func unpackMenuPage(order []byte, rank byte) byte {
	packed := order[rank>>1]
	if rank&1 == 0 {
		return packed & 0x0F
	}
	return packed >> 4
}

func packMenuPage(order []byte, rank, page byte) {
	if rank&1 == 0 {
		order[rank>>1] = (order[rank>>1] & 0xF0) | (page & 0x0F)
		return
	}
	order[rank>>1] = (order[rank>>1] & 0x0F) | (page << 4)
}

func firstVisibleMenuPage(mask uint16, order []byte) byte {
	for rank := byte(0); rank < eepromCurrentMenuPageCount; rank++ {
		page := unpackMenuPage(order, rank)
		if mask&(uint16(1)<<page) != 0 {
			return page
		}
	}
	return 0
}
