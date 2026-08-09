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

func TestCurrentEEPROMExportImportAndRestoreRequireValidatedBackup(t *testing.T) {
	manifest := currentEEPROMBackupManifest(t)
	directory := t.TempDir()
	exported := filepath.Join(directory, "settings.hex")
	exportResult, err := ExportCurrentEEPROMSettings(manifest, exported)
	if err != nil {
		t.Fatal(err)
	}
	if exportResult.Action != "export" || exportResult.SettingsFormat != "schema1/core22+name9+crc8" {
		t.Fatalf("unexpected export result: %+v", exportResult)
	}
	exportDocument, exportDecoded, err := loadCurrentSettingsArtifact(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !exportDecoded.Settings.Valid || exportDocument.Inspection.DataBytes != EEPROMSettingsRecordBytes {
		t.Fatalf("invalid sparse settings export: %+v", exportDecoded.Settings)
	}

	record, err := exportDocument.Image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	record[0] |= 0x01 // Import a requested Silent setting.
	record[EEPROMSettingsValueBytes] = avrCRC8(record[:EEPROMSettingsValueBytes])
	settingsImage := &IntelHexImage{data: make(map[uint32]byte, len(record))}
	for offset, value := range record {
		settingsImage.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	settingsContent, _ := settingsImage.Canonical()
	modifiedSettings := filepath.Join(directory, "settings-silent.hex")
	if err := os.WriteFile(modifiedSettings, settingsContent, 0o600); err != nil {
		t.Fatal(err)
	}

	imported := filepath.Join(directory, "eeprom-import.hex")
	importResult, err := ImportCurrentEEPROMSettings(
		manifest, modifiedSettings, imported,
	)
	if err != nil {
		t.Fatal(err)
	}
	if importResult.Action != "import" || importResult.BackupManifestHash == "" ||
		importResult.SettingsSHA256 == "" {
		t.Fatalf("import lacks backup/settings evidence: %+v", importResult)
	}
	importedDecoded, err := DecodeOfflineEEPROMHex(imported)
	if err != nil {
		t.Fatal(err)
	}
	if !importedDecoded.Settings.Valid || !importedDecoded.Settings.Values.Silent {
		t.Fatalf("imported settings were not applied: %+v", importedDecoded.Settings)
	}
	importedDocument, _ := LoadIntelHex(imported)
	sentinel, err := importedDocument.Image.BytesAt(500, 1)
	if err != nil || sentinel[0] != 0xA5 {
		t.Fatalf("import did not preserve EEPROM outside settings: % X err=%v", sentinel, err)
	}

	restored := filepath.Join(directory, "eeprom-restore.hex")
	restoreResult, err := PrepareCurrentEEPROMRestore(manifest, restored)
	if err != nil {
		t.Fatal(err)
	}
	restoredDecoded, err := DecodeOfflineEEPROMHex(restored)
	if err != nil || !restoredDecoded.Settings.Valid || restoredDecoded.Settings.Values.Silent {
		t.Fatalf("restore artifact differs from validated backup: %+v err=%v", restoredDecoded.Settings, err)
	}
	if restoreResult.Action != "restore" || restoreResult.OutputSHA256 == "" {
		t.Fatalf("restore result lacks evidence: %+v", restoreResult)
	}
	if _, err := PrepareCurrentEEPROMRestore(manifest, restored); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("restore overwrote an existing artifact: %v", err)
	}
}

func TestLegacyBackupMigratesSemanticallyAndArmsProgBeforeFlash(t *testing.T) {
	manifest := currentEEPROMBackupManifest(t) // fixture is schema-2 alpha layout
	content, decoded, err := GenerateMigratedProgrammingEEPROMIntelHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != EEPROMSettingsRecordSchema || !decoded.Valid ||
		decoded.Values.Flags&(0x01|0x02) != 0x03 || decoded.Values.StreamPeriodMS != 500 {
		t.Fatalf("migration did not preserve semantics behind Silent/Prog: %#v", decoded)
	}
	document, err := ParseIntelHex(strings.NewReader(string(content)))
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := document.BytesAt(500, 1)
	if err != nil || sentinel[0] != 0xA5 {
		t.Fatalf("migration changed unrelated EEPROM: % X err=%v", sentinel, err)
	}
	tail, err := document.BytesAt(
		EEPROMSettingsAddress+EEPROMSettingsRecordBytes,
		EEPROMMenuLayoutRecordBytes-EEPROMSettingsRecordBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	for offset, value := range tail {
		if value != 0xFF {
			t.Fatalf("legacy raw tail byte %d was replayed: 0x%02X", offset, value)
		}
	}
}

func TestCurrentEEPROMTransferRejectsIncompleteBackupImage(t *testing.T) {
	root := t.TempDir()
	directory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, newFakeAVRRunner(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportCurrentEEPROMSettings(
		filepath.Join(directory, "manifest.json"), filepath.Join(root, "settings.hex"),
	)
	if err == nil || !strings.Contains(err.Error(), "complete restore base") {
		t.Fatalf("incomplete EEPROM backup was accepted: %v", err)
	}
}

func currentEEPROMBackupManifest(t *testing.T) string {
	t.Helper()
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	values := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMMenuLayoutValueBytes]
	values[0] = 0
	values[1] = 1
	values[2] = 180
	values[4] = 5
	values[5] = 128
	values[6] = 2
	binary.LittleEndian.PutUint16(values[7:9], 500)
	values[17] = 0
	values[18] = 0
	binary.LittleEndian.PutUint16(values[19:21], 0x3FFF)
	copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
	values[28] = 0
	values[30] = 1
	for index := 31; index < int(EEPROMMenuLayoutValueBytes); index++ {
		values[index] = 0
	}
	data[EEPROMSettingsAddress+EEPROMMenuLayoutValueBytes] = avrCRC8(values)
	data[500] = 0xA5
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
	return filepath.Join(directory, "manifest.json")
}
