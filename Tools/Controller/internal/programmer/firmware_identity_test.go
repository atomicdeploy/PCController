package programmer

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareIdentityInspectAndGuardedPatch(t *testing.T) {
	image := &IntelHexImage{data: map[uint32]byte{0: 0xAA}}
	identityBytes := make([]byte, FirmwareIdentityLength)
	binary.LittleEndian.PutUint32(identityBytes[0:4], FirmwareIdentityMagic)
	binary.LittleEndian.PutUint32(identityBytes[4:8], 0x11223344)
	binary.LittleEndian.PutUint32(identityBytes[8:12], 0x35019D5D)
	for index, value := range identityBytes {
		image.data[FirmwareIdentityAddress+uint32(index)] = value
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.hex")
	output := filepath.Join(root, "patched.hex")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}

	inspected, err := InspectFirmwareIdentity(source)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Magic != "PCI1" || inspected.SourceHashHex != "11223344" ||
		inspected.TimestampHex != "35019D5D" || inspected.BuildTimestamp != "260801194258" {
		t.Fatalf("identity=%#v", inspected)
	}
	sum := sha256.Sum256(content)
	result, err := PatchFirmwareIdentity(
		source, output, hex.EncodeToString(sum[:]), 0xAABBCCDD, 0x35023F05,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Patches) != 1 || result.Patches[0].Address != FirmwareIdentityAddress {
		t.Fatalf("patch=%#v", result)
	}
	patched, err := InspectFirmwareIdentity(output)
	if err != nil {
		t.Fatal(err)
	}
	if patched.SourceHashHex != "AABBCCDD" || patched.TimestampHex != "35023F05" {
		t.Fatalf("patched identity=%#v", patched)
	}
}

func TestFirmwareIdentityRejectsMissingMagic(t *testing.T) {
	image := &IntelHexImage{data: make(map[uint32]byte)}
	for address := FirmwareIdentityAddress; address < FirmwareIdentityAddress+FirmwareIdentityLength; address++ {
		image.data[address] = 0
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "no-identity.hex")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFirmwareIdentity(path); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("missing magic error=%v", err)
	}
}
