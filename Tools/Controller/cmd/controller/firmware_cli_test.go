package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/programmer"
)

func TestFirmwareIdentityCLIIsHardwareIndependent(t *testing.T) {
	input := filepath.Join(t.TempDir(), "firmware.hex")
	content := testFirmwareIdentityHex(t, 0x1234ABCD, 0x35019D5D)
	if err := os.WriteFile(input, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runFirmwareArtifact(
		[]string{"identity", "--input", input}, &stdout, &stderr,
	); err != nil {
		t.Fatalf("identity: %v stderr=%s", err, stderr.String())
	}
	for _, wanted := range []string{`"magic": "PCI1"`, `"source_hash_hex": "1234ABCD"`} {
		if !strings.Contains(stdout.String(), wanted) {
			t.Fatalf("identity output missing %s: %s", wanted, stdout.String())
		}
	}
}

func TestFirmwarePatchIdentityCLIRequiresExactSourceHash(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "firmware.hex")
	output := filepath.Join(root, "patched.hex")
	content := testFirmwareIdentityHex(t, 0x1234ABCD, 0x35019D5D)
	if err := os.WriteFile(input, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	var stdout, stderr bytes.Buffer
	err := runFirmwareArtifact([]string{
		"patch-identity", "--input", input, "--output", output,
		"--source-sha256", hex.EncodeToString(sum[:]),
		"--hash", "AABBCCDD", "--timestamp", "35023F05",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("patch: %v stderr=%s", err, stderr.String())
	}
	patched, err := programmer.InspectFirmwareIdentity(output)
	if err != nil {
		t.Fatal(err)
	}
	if patched.SourceHashHex != "AABBCCDD" || patched.TimestampHex != "35023F05" {
		t.Fatalf("patched=%#v", patched)
	}
	if _, err := os.Stat(input); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}

func testFirmwareIdentityHex(t *testing.T, sourceHash, timestamp uint32) []byte {
	t.Helper()
	// Reuse the public guarded patch surface by starting from a canonical
	// fixture encoded by the package's validated Intel HEX parser.
	data := make([]byte, programmer.FirmwareIdentityLength)
	data[0], data[1], data[2], data[3] = 'P', 'C', 'I', '1'
	for index, value := range []uint32{sourceHash, timestamp} {
		offset := 4 + index*4
		data[offset] = byte(value)
		data[offset+1] = byte(value >> 8)
		data[offset+2] = byte(value >> 16)
		data[offset+3] = byte(value >> 24)
	}
	return []byte(testIntelHexRecord(
		uint16(programmer.FirmwareIdentityAddress), 0, data,
	) + testIntelHexRecord(0, 1, nil))
}
