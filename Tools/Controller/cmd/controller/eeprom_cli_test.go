package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/programmer"
)

func TestEEPROMMigrateCLIIsConfigAndDeviceIndependent(t *testing.T) {
	input := filepath.Join(t.TempDir(), "legacy EEPROM.hex")
	output := filepath.Join(t.TempDir(), "migrated settings.hex")
	values := []byte{
		0x04, 0x01, 0x80, 0x00, 0x05, 0x80, 0x00, 0xF4, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	record := append(append([]byte(nil), values...), testAVRCRC8(values))
	fixture := testIntelHexRecord(0x0020, 0, record) + testIntelHexRecord(0, 1, nil)
	if err := os.WriteFile(input, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	missingConfig := filepath.Join(t.TempDir(), "does-not-exist", "config.json")
	err := run([]string{
		"eeprom", "migrate", "--input", input, "--output", output,
		"--config", missingConfig,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("offline migration failed: %v\nstderr=%s", err, stderr.String())
	}
	for _, want := range []string{
		"legacy/unversioned-19+crc8",
		"development-v2/unversioned-29+crc8",
		"0x0020..0x003D (30 bytes)",
		"No serial port was opened",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("migration output missing %q:\n%s", want, stdout.String())
		}
	}
	decoded, err := programmer.DecodeOfflineEEPROMHex(output)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Settings.Valid || decoded.Settings.Legacy ||
		decoded.Settings.Values.VisibleMenuMask != 0x7FFF ||
		decoded.Settings.Values.MenuOrder != [8]byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC, 0xFE} {
		t.Fatalf("CLI output is not a valid development-v2 settings image: %#v", decoded.Settings)
	}
}

func TestEEPROMMigrateCLIRequiresExplicitPaths(t *testing.T) {
	for _, args := range [][]string{
		{"eeprom"},
		{"eeprom", "migrate"},
		{"eeprom", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "controller eeprom migrate") {
			t.Fatalf("args=%v expected migration usage, got %v", args, err)
		}
	}
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
