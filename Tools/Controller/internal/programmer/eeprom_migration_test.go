package programmer

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyEEPROMSettingsEmitsOnlyCurrentSettingsRecord(t *testing.T) {
	legacyValues := []byte{
		0xA5, 0x02, 0xB4, 0x07, 0x05, 0x7B, 0x01, 0xFA, 0x00,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x0E, 0xD7,
	}
	input := writeLegacyMigrationFixture(t, legacyValues, true)
	output := filepath.Join(t.TempDir(), "settings-development-v2.hex")

	result, err := MigrateLegacyEEPROMSettings(input, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceFormat != "legacy/unversioned-19+crc8" ||
		result.OutputFormat != "development-v2/unversioned-29+crc8" ||
		result.PreservedValueBytes != EEPROMSettingsLegacyBytes ||
		result.OutputStart != 32 || result.OutputEndExclusive != 62 ||
		result.OutputBytes != 30 || len(result.SourceSHA256) != 64 ||
		len(result.OutputSHA256) != 64 {
		t.Fatalf("unexpected migration result: %#v", result)
	}

	document, err := LoadIntelHex(output)
	if err != nil {
		t.Fatal(err)
	}
	inspection := document.Inspection
	if inspection.DataBytes != 30 || inspection.MinimumAddress != 32 ||
		inspection.MaximumAddress != 61 || len(inspection.Segments) != 1 ||
		inspection.Segments[0].Start != 32 ||
		inspection.Segments[0].EndExclusive != 62 {
		t.Fatalf("output is not settings-only sparse HEX: %#v", inspection)
	}
	if _, present := document.Image.Byte(EEPROMRemoteHeaderAddress); present {
		t.Fatal("migration output unexpectedly includes RF EEPROM bytes")
	}
	if _, present := document.Image.Byte(EEPROMResetJournalAddress); present {
		t.Fatal("migration output unexpectedly includes reset-journal EEPROM bytes")
	}
	record, err := document.Image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := record[:EEPROMSettingsLegacyBytes]; !equalBytes(got, legacyValues) {
		t.Fatalf("legacy values changed: got %X want %X", got, legacyValues)
	}
	if mask := binary.LittleEndian.Uint16(record[19:21]); mask != 0x7FFF {
		t.Fatalf("visible menu mask=0x%04X, want 0x7FFF", mask)
	}
	wantOrder := []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC, 0xFE}
	if !equalBytes(record[21:29], wantOrder) {
		t.Fatalf("menu order=%X, want %X", record[21:29], wantOrder)
	}
	if record[29] != avrCRC8(record[:29]) || result.OutputChecksum != record[29] {
		t.Fatalf("current CRC mismatch: result=%02X record=%02X", result.OutputChecksum, record[29])
	}
	decoded := decodeOfflineSettings(document.Image)
	if !decoded.Valid || decoded.Legacy || decoded.Values.VisibleMenuMask != 0x7FFF ||
		decoded.Values.MenuOrder != eepromDevelopmentV2IdentityMenuOrder {
		t.Fatalf("migrated record did not decode as development-v2: %#v", decoded)
	}
}

func TestMigrateLegacyEEPROMSettingsRejectsDamagedOrCurrentInput(t *testing.T) {
	values := make([]byte, EEPROMSettingsLegacyBytes)
	values[1] = 1
	values[4] = 5
	binary.LittleEndian.PutUint16(values[7:9], 500)

	t.Run("bad legacy crc", func(t *testing.T) {
		input := writeLegacyMigrationFixture(t, values, false)
		output := filepath.Join(t.TempDir(), "must-not-exist.hex")
		_, err := MigrateLegacyEEPROMSettings(input, output)
		if err == nil || !strings.Contains(err.Error(), "invalid") ||
			!strings.Contains(err.Error(), "CRC-8") {
			t.Fatalf("expected legacy CRC rejection, got %v", err)
		}
		if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
			t.Fatalf("invalid migration created output: %v", statErr)
		}
	})

	t.Run("already current", func(t *testing.T) {
		input := writeEEPROMFixture(t, func(data []byte) {
			record := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
			copy(record[:EEPROMSettingsLegacyBytes], values)
			binary.LittleEndian.PutUint16(record[19:21], 0x7FFF)
			copy(record[21:29], eepromDevelopmentV2IdentityMenuOrder[:])
			record[29] = avrCRC8(record[:29])
		})
		_, err := MigrateLegacyEEPROMSettings(
			input,
			filepath.Join(t.TempDir(), "must-not-exist.hex"),
		)
		if err == nil || !strings.Contains(err.Error(), "require legacy") {
			t.Fatalf("expected current-layout rejection, got %v", err)
		}
	})
}

func TestMigrateLegacyEEPROMSettingsRejectsOutOfRangeInputAndExistingOutput(t *testing.T) {
	values := make([]byte, EEPROMSettingsLegacyBytes)
	values[1] = 1
	values[4] = 5
	binary.LittleEndian.PutUint16(values[7:9], 500)
	input := writeLegacyMigrationFixture(t, values, true)

	t.Run("existing output", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "existing.hex")
		if err := os.WriteFile(output, []byte("retain"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := MigrateLegacyEEPROMSettings(input, output)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected existing-output rejection, got %v", err)
		}
		content, _ := os.ReadFile(output)
		if string(content) != "retain" {
			t.Fatal("existing migration output was changed")
		}
	})

	t.Run("out of range input", func(t *testing.T) {
		document, err := LoadIntelHex(input)
		if err != nil {
			t.Fatal(err)
		}
		document.Image.data[PCControllerEEPROMBytes] = 0x55
		content, err := document.Image.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		badInput := filepath.Join(t.TempDir(), "out-of-range.hex")
		if err := os.WriteFile(badInput, content, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = MigrateLegacyEEPROMSettings(
			badInput,
			filepath.Join(t.TempDir(), "must-not-exist.hex"),
		)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected EEPROM range rejection, got %v", err)
		}
	})
}

func writeLegacyMigrationFixture(t *testing.T, values []byte, validCRC bool) string {
	t.Helper()
	if len(values) != int(EEPROMSettingsLegacyBytes) {
		t.Fatalf("legacy fixture requires %d values, got %d", EEPROMSettingsLegacyBytes, len(values))
	}
	return writeEEPROMFixture(t, func(data []byte) {
		record := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsLegacyRecord]
		copy(record[:EEPROMSettingsLegacyBytes], values)
		record[EEPROMSettingsLegacyBytes] = avrCRC8(values)
		if !validCRC {
			record[EEPROMSettingsLegacyBytes] ^= 0x5A
		}
		// Old firmware ignores bytes after its byte-19 CRC; a board can retain
		// arbitrary development-era tail bytes that are not part of the legacy
		// record and must not be mistaken for a valid current record.
		copy(data[EEPROMSettingsAddress+EEPROMSettingsLegacyRecord:EEPROMSettingsEndExclusive],
			[]byte{0x00, 0xF0, 0x34, 0xFF, 0x94, 0x03, 0x00, 0x00, 0xFF, 0xFF})
		// Sentinels prove that the source may contain unrelated RF/reset data while
		// the generated sparse migration artifact does not copy either region.
		data[EEPROMRemoteHeaderAddress] = 0x52
		data[EEPROMResetJournalAddress] = 0xA7
	})
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
