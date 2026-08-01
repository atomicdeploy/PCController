package programmer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompileManifestAtomicallyReplacesStaleMetadataFromActualArtifacts(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, ".build", "firmware")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	artifacts := map[string]string{
		"PCController.ino.eep":                 ":00000001FF\n",
		"PCController.ino.hex":                 ":020000000102FB\n:00000001FF\n",
		"PCController.ino.with_bootloader.hex": ":020000000102FB\n:017F8000AA56\n:00000001FF\n",
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
	identity := CompileIdentity{
		SourceHash: 0x1234ABCD, SourceSHA256: "abcdef", SourceFiles: 3,
		PackedTimestamp: 0x35019D5D, SourceRoot: root, OutputDir: output,
	}
	stackBudget := compileManifestStackBudget{
		Analyzer: "test", SRAMCapacityBytes: 2048, StaticSRAMBytes: 1500,
		SerialPathBytes: 100, RFInterruptAllowanceBytes: 40,
		EstimatedPeakSRAMBytes: 1640, EstimatedFreeSRAMBytes: 408,
		MinimumFreeSRAMBytes: 96,
	}
	written, err := writeCompileManifest(Options{FQBN: DefaultFQBN}, identity, stackBudget)
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
		manifest.Source.PackedTimestamp != "35019D5D" ||
		manifest.Source.BuildTimestamp != "260801194258" ||
		manifest.StackBudget.EstimatedFreeSRAMBytes != 408 ||
		len(manifest.Artifacts) != 3 {
		t.Fatalf("manifest=%#v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.SHA256 == "" || artifact.ContainerBytes == 0 {
			t.Fatalf("artifact lacks on-disk hash/size: %#v", artifact)
		}
		switch artifact.Role {
		case "application":
			if artifact.DataBytes != 2 || artifact.CapacityBytes != urbootApplicationCapacity {
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
		default:
			t.Fatalf("unknown manifest role: %#v", artifact)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(output, ".firmware-manifest-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("atomic manifest left temporary files: %v err=%v", leftovers, err)
	}
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
