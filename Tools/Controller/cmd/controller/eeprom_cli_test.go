package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		`"format": "current/unversioned-40+crc8"`, `"visible_menu_mask": 16383`,
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

func TestEEPROMCLIExposesOnlyCurrentSemanticOperations(t *testing.T) {
	for _, args := range [][]string{
		{"eeprom"},
		{"eeprom", "migrate"},
		{"eeprom", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "eeprom factory-defaults") ||
			strings.Contains(err.Error(), "migrate") {
			t.Fatalf("args=%v expected current semantic usage, got %v", args, err)
		}
	}
}

func TestEEPROMFactoryDefaultsCLIWritesCompleteImageWithoutBoard(t *testing.T) {
	output := filepath.Join(t.TempDir(), "factory.hex")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"eeprom", "factory-defaults", "--output", output}, &stdout, &stderr); err != nil {
		t.Fatalf("factory defaults failed: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "host-owned status LED profiles") {
		t.Fatalf("factory output missing ownership evidence: %s", stdout.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"eeprom", "factory-defaults", "--output", output}, &stdout, &stderr); err == nil {
		t.Fatal("factory defaults unexpectedly overwrote existing output")
	}
}

func currentSettingsFixture() []byte {
	values := make([]byte, 40)
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
	values[31] = 0
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
