package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/programmer"
)

func TestEEPROMInspectCLIIsConfigAndDeviceIndependent(t *testing.T) {
	input := filepath.Join(t.TempDir(), "current settings.hex")
	values := currentSettingsFixture()
	record := append(append([]byte(nil), values...), testAVRCRC8(values))
	fixture := testIntelHexRecord(0x0020, 0, record) + testIntelHexRecord(0, 1, nil)
	if err := os.WriteFile(input, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	missingConfig := filepath.Join(t.TempDir(), "does-not-exist", "config.json")
	err := run([]string{
		"eeprom", "inspect", "--input", input, "--config", missingConfig,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("offline inspection failed: %v\nstderr=%s", err, stderr.String())
	}
	for _, want := range []string{
		`"supported": true`, `"valid": true`,
		`"format": "current/unversioned-31+crc8"`, `"visible_menu_mask": 16383`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("inspection output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestEEPROMInspectRejectsUnpublishedShortLayout(t *testing.T) {
	input := filepath.Join(t.TempDir(), "unsupported.hex")
	values := make([]byte, 19)
	values[1], values[4], values[5], values[6] = 1, 5, 128, 2
	binary.LittleEndian.PutUint16(values[7:9], 500)
	record := append(append([]byte(nil), values...), testAVRCRC8(values))
	fixture := testIntelHexRecord(0x0020, 0, record) + testIntelHexRecord(0, 1, nil)
	if err := os.WriteFile(input, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{"eeprom", "inspect", "--input", input}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported current EEPROM settings layout") ||
		!strings.Contains(stdout.String(), `"supported": false`) {
		t.Fatalf("unsupported layout err=%v output=%s", err, stdout.String())
	}
}

func TestEEPROMCLIExposesCurrentAndExplicitMigrationOperations(t *testing.T) {
	for _, args := range [][]string{
		{"eeprom"},
		{"eeprom", "migrate"},
		{"eeprom", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "eeprom inspect") ||
			!strings.Contains(err.Error(), "migrate --backup-manifest") {
			t.Fatalf("args=%v expected current/migration usage, got %v", args, err)
		}
	}
}

func TestEEPROMMigrateCLIIsBackupBoundAndDeviceIndependent(t *testing.T) {
	manifest, sourceHash := eepromMigrationCLIBackup(t)
	output := filepath.Join(t.TempDir(), "migrated current EEPROM.hex")
	missingConfig := filepath.Join(t.TempDir(), "does-not-exist", "config.json")
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"eeprom", "migrate",
		"--backup-manifest", manifest,
		"--from", programmer.EEPROMMigrationLegacyV1,
		"--expect-eeprom-sha256", sourceHash,
		"--output", output,
		"--config", missingConfig,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("offline migration failed: %v\nstderr=%s", err, stderr.String())
	}
	for _, want := range []string{
		`"source_format": "` + programmer.EEPROMMigrationLegacyV1 + `"`,
		`"target_format": "` + programmer.EEPROMMigrationCurrent + `"`,
		`"readback_verified": true`,
		`"source_eeprom_sha256": "` + sourceHash + `"`,
		"Validated backup remained unchanged",
		"no serial port was opened",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("migration output missing %q:\n%s", want, stdout.String())
		}
	}
	decoded, err := programmer.DecodeOfflineEEPROMHex(output)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Settings.Valid || decoded.Settings.Format != programmer.EEPROMMigrationCurrent {
		t.Fatalf("CLI did not create current settings: %+v", decoded.Settings)
	}
}

func currentSettingsFixture() []byte {
	values := make([]byte, 31)
	values[1] = 1
	values[2] = 180
	values[4] = 5
	values[5] = 128
	values[6] = 0x06
	binary.LittleEndian.PutUint16(values[7:9], 500)
	binary.LittleEndian.PutUint16(values[19:21], 0x3FFF)
	copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
	values[28] = 2
	values[29] = 0
	values[30] = 1
	return values
}

func testIntelHexRecord(address uint16, recordType byte, data []byte) string {
	record := make([]byte, 0, len(data)+5)
	record = append(record, byte(len(data)), byte(address>>8), byte(address), recordType)
	record = append(record, data...)
	var sum byte
	for _, value := range record {
		sum += value
	}
	record = append(record, byte(-sum))
	return fmt.Sprintf(":%s\n", strings.ToUpper(hex.EncodeToString(record)))
}

func testAVRCRC8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for bit := 0; bit < 8; bit++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func eepromMigrationCLIBackup(t *testing.T) (string, string) {
	t.Helper()
	data := make([]byte, 1024)
	for index := range data {
		data[index] = 0xFF
	}
	values := data[32:51]
	values[1] = 1
	values[2] = 180
	values[4] = 5
	values[5] = 128
	values[6] = 0
	binary.LittleEndian.PutUint16(values[7:9], 500)
	values[17] = 0
	values[18] = 0
	data[51] = testAVRCRC8(values)
	data[500] = 0xA5
	eepromHEX := testFullIntelHex(data)
	flashHEX := testIntelHexRecord(0, 0, []byte{1, 2, 3, 4}) + testIntelHexRecord(0, 1, nil)
	runner := programmer.CommandRunnerFunc(func(
		_ context.Context,
		command programmer.Command,
		output io.Writer,
	) error {
		joined := strings.Join(command.Args, " ")
		if path := testCommandOutputPath(command, "-Uflash:r:"); path != "" {
			return os.WriteFile(path, []byte(flashHEX), 0o600)
		}
		if path := testCommandOutputPath(command, "-Ueeprom:r:"); path != "" {
			return os.WriteFile(path, []byte(eepromHEX), 0o600)
		}
		if strings.Contains(joined, "-xshowall") ||
			(strings.Contains(joined, "-c") && !strings.Contains(joined, "-U")) {
			_, err := io.WriteString(output, "test programmer metadata\n")
			return err
		}
		return fmt.Errorf("unexpected backup command: %s", joined)
	})
	root := t.TempDir()
	directory, err := programmer.BackupWithRunner(
		context.Background(),
		programmer.Options{
			Method: programmer.MethodUrclock, Operation: programmer.OperationBackup,
			Port: "COM18", OutputPath: root, Avrdude: "test-avrdude",
			AvrdudeConf: "test-avrdude.conf", MCU: "atmega328p",
		},
		io.Discard,
		runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "manifest.json")
	validated, err := programmer.ValidateBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, validated.Files["eeprom"].SHA256
}

func testCommandOutputPath(command programmer.Command, prefix string) string {
	for _, argument := range command.Args {
		if strings.HasPrefix(argument, prefix) && strings.HasSuffix(argument, ":i") {
			return strings.TrimSuffix(strings.TrimPrefix(argument, prefix), ":i")
		}
	}
	return ""
}

func testFullIntelHex(data []byte) string {
	var output strings.Builder
	for address := 0; address < len(data); address += 16 {
		end := address + 16
		if end > len(data) {
			end = len(data)
		}
		output.WriteString(testIntelHexRecord(uint16(address), 0, data[address:end]))
	}
	output.WriteString(testIntelHexRecord(0, 1, nil))
	return output.String()
}
