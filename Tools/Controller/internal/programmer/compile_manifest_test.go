package programmer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileManifestAtomicallyReplacesStaleMetadataFromActualArtifacts(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, ".build", "firmware")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	identity := CompileIdentity{
		SourceHash: 0x1234ABCD, SourceSHA256: "abcdef", SourceFiles: 3,
		Features:        []FirmwareFeature{FirmwareFeatureEEPROMBootOpcodes},
		PackedTimestamp: 0x35019D5D, SourceRoot: root, OutputDir: output,
	}
	application := manifestApplicationFixture(t, identity)
	artifacts := map[string]string{
		"PCController.ino.eep":                 ":00000001FF\n",
		"PCController.ino.hex":                 application,
		"PCController.ino.with_bootloader.hex": ":020000000102FB\n:017F8000AA56\n:00000001FF\n",
		defaultEEPROMCompileArtifact:           ":01000000BB44\n:00000001FF\n",
	}
	for name, content := range artifacts {
		if err := os.WriteFile(filepath.Join(output, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(output, firmwareManifestName)
	if err := os.WriteFile(manifestPath, []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stackBudget := compileManifestStackBudget{
		Analyzer: "test", SRAMCapacityBytes: 2048, StaticSRAMBytes: 1500,
		SerialPathBytes: 100, RFInterruptAllowanceBytes: 40,
		EstimatedPeakSRAMBytes: 1640, EstimatedFreeSRAMBytes: 408,
		MinimumFreeSRAMBytes: 96,
	}
	written, err := writeCompileManifest(Options{FQBN: DefaultFQBN()}, identity, stackBudget)
	if err != nil {
		t.Fatal(err)
	}
	if written != manifestPath {
		t.Fatalf("manifest path=%s want=%s", written, manifestPath)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest compileManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != firmwareManifestFormat ||
		manifest.Source.BuildHash != "1234ABCD" ||
		len(manifest.Source.CompileFeatures) != 1 || manifest.Source.CompileFeatures[0] != string(FirmwareFeatureEEPROMBootOpcodes) ||
		manifest.Source.PackedTimestamp != "35019D5D" ||
		manifest.Source.BuildTimestamp != "260801194258" ||
		manifest.StackBudget.EstimatedFreeSRAMBytes != 408 ||
		len(manifest.Artifacts) != 4 || len(manifest.PatchRegions) != 1 {
		t.Fatalf("manifest=%#v", manifest)
	}
	defaultEEPROMPath := filepath.Join(output, defaultEEPROMCompileArtifact)
	defaultEEPROM, err := os.ReadFile(defaultEEPROMPath)
	if err != nil {
		t.Fatal(err)
	}
	wantDefaultEEPROM, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(defaultEEPROM, wantDefaultEEPROM) {
		t.Fatal("compiled default EEPROM artifact does not match the canonical generator")
	}
	foundDefaultEEPROM := false
	for _, artifact := range manifest.Artifacts {
		if artifact.SHA256 == "" || artifact.ContainerBytes == 0 {
			t.Fatalf("artifact lacks on-disk hash/size: %#v", artifact)
		}
		switch artifact.Role {
		case "application":
			if artifact.DataBytes != 14 || artifact.CapacityBytes != urbootApplicationCapacity {
				t.Fatalf("application artifact=%#v", artifact)
			}
		case "flash+bootloader":
			if artifact.DataBytes != 3 || artifact.CapacityBytes != ATmega328PFlashSize {
				t.Fatalf("merged artifact=%#v", artifact)
			}
		case "eeprom":
			if artifact.DataBytes != 0 || artifact.CapacityBytes != atmega328PEEPROMCapacity {
				t.Fatalf("EEPROM artifact=%#v", artifact)
			}
		case "default-eeprom":
			foundDefaultEEPROM = true
			if filepath.Base(artifact.Path) != defaultEEPROMCompileArtifact ||
				artifact.DataBytes != atmega328PEEPROMCapacity ||
				artifact.CapacityBytes != atmega328PEEPROMCapacity || artifact.FreeBytes != 0 {
				t.Fatalf("default EEPROM artifact=%#v", artifact)
			}
		default:
			t.Fatalf("unknown manifest role: %#v", artifact)
		}
	}
	if !foundDefaultEEPROM {
		t.Fatal("manifest does not contain the canonical default EEPROM artifact")
	}
	leftovers, err := filepath.Glob(filepath.Join(output, ".firmware-manifest-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("atomic manifest left temporary files: %v err=%v", leftovers, err)
	}
}

func manifestApplicationFixture(t *testing.T, identity CompileIdentity) string {
	t.Helper()
	image := &IntelHexImage{data: map[uint32]byte{0: 1, 1: 2}}
	value := make([]byte, FirmwareIdentityLength)
	binary.LittleEndian.PutUint32(value[0:4], FirmwareIdentityMagic)
	binary.LittleEndian.PutUint32(value[4:8], identity.SourceHash)
	binary.LittleEndian.PutUint32(value[8:12], identity.PackedTimestamp)
	for index, item := range value {
		image.data[FirmwareIdentityAddress+uint32(index)] = item
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestClearCompileManifestRemovesOnlyStaleManifest(t *testing.T) {
	output := t.TempDir()
	manifest := filepath.Join(output, firmwareManifestName)
	artifact := filepath.Join(output, "PCController.ino.hex")
	if err := os.WriteFile(manifest, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clearCompileManifest(output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatalf("stale manifest remains: %v", err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("artifact was unexpectedly removed: %v", err)
	}
}

func TestInspectManifestRegionsValidatesAllNamedMemoryDomains(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, ".build", "firmware")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	identity := CompileIdentity{
		SourceHash: 0x89ABCDEF, SourceSHA256: strings.Repeat("a", 64), SourceFiles: 4,
		PackedTimestamp: 0x35019D5D, SourceRoot: root, OutputDir: output,
	}
	fixtures := map[string]string{
		"PCController.ino.hex":                 manifestApplicationFixture(t, identity),
		"PCController.ino.with_bootloader.hex": ":020000000102FB\n:017F8000AA56\n:00000001FF\n",
		"PCController.ino.eep":                 ":00000001FF\n",
	}
	for name, content := range fixtures {
		if err := os.WriteFile(filepath.Join(output, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath, err := writeCompileManifest(
		Options{FQBN: DefaultFQBN()}, identity,
		compileManifestStackBudget{Analyzer: "fixture", SRAMCapacityBytes: 2048},
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := InspectManifestRegions(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ManifestSHA256) != 64 || len(report.Regions) != 4 ||
		report.Target.ApplicationLimitBytes != urbootApplicationCapacity {
		t.Fatalf("unexpected region report: %#v", report)
	}
	want := []struct {
		name, kind, role string
		start, length    uint32
		complete         bool
	}{
		{"application", "application", "application", 0, urbootApplicationCapacity, false},
		{"bootloader", "bootloader", "flash+bootloader", urbootApplicationCapacity, ATmega328PFlashSize - urbootApplicationCapacity, false},
		{"eeprom", "eeprom", "default-eeprom", 0, atmega328PEEPROMCapacity, true},
		{"firmware-identity", "metadata", "application", FirmwareIdentityAddress, FirmwareIdentityLength, true},
	}
	for index, expected := range want {
		region := report.Regions[index]
		if region.Name != expected.name || region.Kind != expected.kind ||
			region.ArtifactRole != expected.role || region.Start != expected.start ||
			region.Length != expected.length || region.Complete != expected.complete ||
			len(region.RegionSHA256) != 64 || !region.BoundsValidated ||
			!region.ChecksumValidated || !region.ManifestHashMatched {
			t.Fatalf("region %d = %#v", index, region)
		}
	}
	if !report.Regions[3].DeclaredMagicMatched {
		t.Fatalf("metadata magic was not validated: %#v", report.Regions[3])
	}

	applicationPath := filepath.Join(output, "PCController.ino.hex")
	file, err := os.OpenFile(applicationPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectManifestRegions(manifestPath); err == nil ||
		!strings.Contains(err.Error(), "size or SHA-256 differs") {
		t.Fatalf("tampered artifact was accepted: %v", err)
	}
}
