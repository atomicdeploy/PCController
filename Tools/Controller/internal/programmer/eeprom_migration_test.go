package programmer

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyV1BackupProducesVerifiedFullCurrentImage(t *testing.T) {
	manifest, sourceHash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV1, nil)
	validatedBefore, err := ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "migrated-current.hex")
	result, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
		BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
		ExpectedEEPROMHash: sourceHash, OutputPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema != EEPROMMigrationSchema || result.Action != "migrate" ||
		result.SourceFormat != EEPROMMigrationLegacyV1 ||
		result.TargetFormat != EEPROMMigrationCurrent ||
		result.SourceEEPROMSHA256 != sourceHash ||
		result.OutputSHA256 == "" || result.ReadbackSHA256 != result.OutputSHA256 ||
		!result.ReadbackVerified || result.OutputDataBytes != PCControllerEEPROMBytes ||
		result.PreservedOutsideSettings != PCControllerEEPROMBytes-EEPROMSettingsRecordBytes {
		t.Fatalf("unexpected migration result: %+v", result)
	}

	document, err := LoadIntelHex(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireFullEEPROMImage(document.Image); err != nil {
		t.Fatal(err)
	}
	decoded := decodeOfflineSettings(document.Image)
	if !decoded.Valid || decoded.Format != EEPROMMigrationCurrent ||
		!decoded.Values.Silent || decoded.Values.ProgrammingMode ||
		decoded.Values.OutputPersistence != 0 || decoded.Values.RelayRestoreMask != 0 ||
		decoded.Values.MotionBreakMS != 100 || decoded.Values.DefaultMenuPage != 0 ||
		decoded.Values.VisibleMenuMask != 0x3FFF ||
		decoded.Values.MenuOrder != [7]byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC} {
		t.Fatalf("unexpected migrated settings: %+v", decoded)
	}
	sentinel, err := document.Image.BytesAt(500, 1)
	if err != nil || sentinel[0] != 0xA5 {
		t.Fatalf("byte outside settings changed: % X err=%v", sentinel, err)
	}
	validatedAfter, err := ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if validatedAfter.ManifestSHA256 != validatedBefore.ManifestSHA256 ||
		validatedAfter.Files["eeprom"].SHA256 != sourceHash {
		t.Fatal("source backup changed during migration")
	}
}

func TestMigrateLegacyV2PreservesRetainedMenuOrderAndVisibility(t *testing.T) {
	manifest, sourceHash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV2, nil)
	output := filepath.Join(t.TempDir(), "migrated-current.hex")
	result, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
		BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV2,
		ExpectedEEPROMHash: sourceHash, OutputPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeOfflineEEPROMHex(output)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := [7]byte{0x03, 0x21, 0x54, 0x76, 0x98, 0xBA, 0xDC}
	if !decoded.Settings.Valid || decoded.Settings.Values.DefaultMenuPage != 3 ||
		decoded.Settings.Values.VisibleMenuMask != 1<<3 ||
		decoded.Settings.Values.MenuOrder != wantOrder ||
		decoded.Settings.Values.MotionBreakMS != 1 {
		t.Fatalf("legacy-v2 menu semantics were not retained: %+v", decoded.Settings)
	}
	if !containsMigrationDecision(result.Decisions, "legacy-only page 14") ||
		!containsMigrationDecision(result.Decisions, "first retained visible page") {
		t.Fatalf("migration decisions do not explain lossy mapping: %q", result.Decisions)
	}
}

func TestMigrateLegacyEEPROMBackupRejectsWrongIdentityDamageAndCurrentLayout(t *testing.T) {
	manifest, sourceHash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV1, nil)
	base := EEPROMMigrationOptions{
		BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
		ExpectedEEPROMHash: sourceHash,
	}

	t.Run("wrong expected hash", func(t *testing.T) {
		options := base
		options.ExpectedEEPROMHash = strings.Repeat("0", 64)
		options.OutputPath = filepath.Join(t.TempDir(), "must-not-exist.hex")
		_, err := MigrateLegacyEEPROMBackup(options)
		if err == nil || !strings.Contains(err.Error(), "expected") {
			t.Fatalf("wrong hash was accepted: %v", err)
		}
		if _, statErr := os.Stat(options.OutputPath); !os.IsNotExist(statErr) {
			t.Fatalf("wrong hash created output: %v", statErr)
		}
	})

	t.Run("unsupported explicit version", func(t *testing.T) {
		options := base
		options.SourceFormat = "guess"
		options.OutputPath = filepath.Join(t.TempDir(), "must-not-exist.hex")
		_, err := MigrateLegacyEEPROMBackup(options)
		if err == nil || !strings.Contains(err.Error(), "unsupported EEPROM source format") {
			t.Fatalf("unsupported version was accepted: %v", err)
		}
	})

	t.Run("wrong declared legacy profile", func(t *testing.T) {
		v2Manifest, v2Hash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV2, nil)
		_, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: v2Manifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: v2Hash,
			OutputPath:         filepath.Join(t.TempDir(), "must-not-exist.hex"),
		})
		if err == nil || !strings.Contains(err.Error(), "CRC-8 mismatch") {
			t.Fatalf("wrong explicit profile was accepted: %v", err)
		}
	})

	t.Run("damaged legacy crc", func(t *testing.T) {
		damagedManifest, damagedHash := legacyEEPROMBackupManifest(
			t,
			EEPROMMigrationLegacyV1,
			func(data []byte) { data[EEPROMSettingsAddress+eepromLegacyV1ValueBytes] ^= 0x5A },
		)
		output := filepath.Join(t.TempDir(), "must-not-exist.hex")
		_, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: damagedManifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: damagedHash, OutputPath: output,
		})
		if err == nil || !strings.Contains(err.Error(), "CRC-8 mismatch") {
			t.Fatalf("damaged source was accepted: %v", err)
		}
	})

	t.Run("already current", func(t *testing.T) {
		currentManifest := currentEEPROMBackupManifest(t)
		validated, err := ValidateBackupManifest(currentManifest)
		if err != nil {
			t.Fatal(err)
		}
		_, err = MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: currentManifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: validated.Files["eeprom"].SHA256,
			OutputPath:         filepath.Join(t.TempDir(), "must-not-exist.hex"),
		})
		if err == nil || !strings.Contains(err.Error(), "already contains") {
			t.Fatalf("current settings were migrated as legacy: %v", err)
		}
	})

	t.Run("invalid legacy-v2 menu schema", func(t *testing.T) {
		badManifest, badHash := legacyEEPROMBackupManifest(
			t,
			EEPROMMigrationLegacyV2,
			func(data []byte) {
				values := data[EEPROMSettingsAddress : EEPROMSettingsAddress+eepromLegacyV2ValueBytes]
				values[21] = 0x33
				data[EEPROMSettingsAddress+eepromLegacyV2ValueBytes] = avrCRC8(values)
			},
		)
		_, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: badManifest, SourceFormat: EEPROMMigrationLegacyV2,
			ExpectedEEPROMHash: badHash,
			OutputPath:         filepath.Join(t.TempDir(), "must-not-exist.hex"),
		})
		if err == nil || !strings.Contains(err.Error(), "menu-order page 3 is duplicated") {
			t.Fatalf("invalid legacy-v2 menu schema was accepted: %v", err)
		}
	})
}

func TestMigrateLegacyEEPROMBackupRejectsMutatedBackupAndProtectedOutputs(t *testing.T) {
	manifest, sourceHash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV1, nil)
	validated, err := ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("mutated backup artifact", func(t *testing.T) {
		content, err := os.ReadFile(validated.Files["eeprom"].Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(validated.Files["eeprom"].Path, append(content, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: sourceHash,
			OutputPath:         filepath.Join(t.TempDir(), "must-not-exist.hex"),
		})
		if err == nil || !strings.Contains(err.Error(), "validate migration backup") ||
			(!strings.Contains(err.Error(), "SHA-256 mismatch") &&
				!strings.Contains(err.Error(), "type or size mismatch")) {
			t.Fatalf("mutated backup was accepted: %v", err)
		}
	})

	manifest, sourceHash = legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV1, nil)
	validated, err = ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("output inside backup", func(t *testing.T) {
		_, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: sourceHash,
			OutputPath:         filepath.Join(validated.Directory, "migration.hex"),
		})
		if err == nil || !strings.Contains(err.Error(), "outside the immutable backup") {
			t.Fatalf("backup directory output was accepted: %v", err)
		}
	})

	t.Run("existing output", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "existing.hex")
		if err := os.WriteFile(output, []byte("retain"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := MigrateLegacyEEPROMBackup(EEPROMMigrationOptions{
			BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: sourceHash, OutputPath: output,
		})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("existing output was overwritten: %v", err)
		}
		content, _ := os.ReadFile(output)
		if string(content) != "retain" {
			t.Fatal("existing output content changed")
		}
	})
}

func TestMigrateLegacyEEPROMBackupEnforcesPersistedReadback(t *testing.T) {
	manifest, sourceHash := legacyEEPROMBackupManifest(t, EEPROMMigrationLegacyV1, nil)
	output := filepath.Join(t.TempDir(), "corrupt-readback.hex")
	_, err := migrateLegacyEEPROMBackup(
		EEPROMMigrationOptions{
			BackupManifest: manifest, SourceFormat: EEPROMMigrationLegacyV1,
			ExpectedEEPROMHash: sourceHash, OutputPath: output,
		},
		func(path string, content []byte) error {
			image, err := ParseIntelHex(strings.NewReader(string(content)))
			if err != nil {
				return err
			}
			image.data[500] ^= 0x01
			corrupt, err := image.Canonical()
			if err != nil {
				return err
			}
			return atomicCreateFile(path, corrupt, 0o600)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "readback SHA-256 mismatch") {
		t.Fatalf("corrupt persisted output passed readback: %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("failed readback output was not removed: %v", statErr)
	}
}

func legacyEEPROMBackupManifest(
	t *testing.T,
	format string,
	mutate func([]byte),
) (string, string) {
	t.Helper()
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	valueBytes := eepromLegacyV1ValueBytes
	flags := byte(0x83) // Silent, retired/reserved bit, and legacy 100 ms break.
	if format == EEPROMMigrationLegacyV2 {
		valueBytes = eepromLegacyV2ValueBytes
		flags = 0x01
	}
	values := data[EEPROMSettingsAddress : EEPROMSettingsAddress+valueBytes]
	values[0] = flags
	values[1] = 1
	values[2] = 180
	values[3] = 7
	values[4] = 5
	values[5] = 128
	values[6] = 2 // Retired automatic PWM test mode; never reinterpret as restore flags.
	binary.LittleEndian.PutUint16(values[7:9], 500)
	copy(values[9:17], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	values[17] = 14
	values[18] = 0
	if format == EEPROMMigrationLegacyV2 {
		binary.LittleEndian.PutUint16(values[19:21], (1<<14)|(1<<3))
		copy(values[21:29], []byte{0xE3, 0x10, 0x42, 0x65, 0x87, 0xA9, 0xCB, 0xFD})
	}
	data[EEPROMSettingsAddress+valueBytes] = avrCRC8(values)
	for address := EEPROMSettingsAddress + valueBytes + 1; address < EEPROMSettingsAddress+EEPROMSettingsRecordBytes; address++ {
		data[address] = byte(address ^ 0xA5)
	}
	data[500] = 0xA5
	if mutate != nil {
		mutate(data)
	}
	image := &IntelHexImage{data: make(map[uint32]byte, len(data))}
	for address, value := range data {
		image.data[uint32(address)] = value
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	runner := newFakeAVRRunner(t)
	runner.eepromHEX = content
	directory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(t.TempDir()), io.Discard, runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "manifest.json")
	validated, err := ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, validated.Files["eeprom"].SHA256
}

func containsMigrationDecision(decisions []string, fragment string) bool {
	for _, decision := range decisions {
		if strings.Contains(decision, fragment) {
			return true
		}
	}
	return false
}
