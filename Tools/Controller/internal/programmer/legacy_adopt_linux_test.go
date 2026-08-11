//go:build linux

package programmer

import (
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/ownedstorage"
)

func TestAdoptKnownLegacyHostDataPathsAcceptsOnlyExpectedLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pccontroller")
	paths, err := HostDataPathsFor(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{
		filepath.Join(root, "tools", "toolchain", "arduino-cli"),
		filepath.Join(root, "tools", "toolchain", "data"),
		filepath.Join(root, "tools", "toolchain", "downloads"),
		filepath.Join(root, "tools", "toolchain", "user"),
		filepath.Join(root, "virtual-board"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.ToolchainDir, "firmware-cli.yaml"), []byte("directories: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	eeprom := filepath.Join(root, "virtual-board", "eeprom.bin")
	if err := os.WriteFile(eeprom, make([]byte, legacyEEPROMBytes), 0o664); err != nil {
		t.Fatal(err)
	}
	if err := AdoptKnownLegacyHostDataPaths(paths); err != nil {
		t.Fatal(err)
	}
	if err := ownedstorage.Verify(root); err != nil {
		t.Fatalf("legacy layout did not receive a valid ownership marker: %v", err)
	}
	info, err := os.Stat(eeprom)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("EEPROM permissions were not normalized: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(paths.StateDir); err != nil {
		t.Fatalf("normal host data directories were not created: %v", err)
	}
}

func TestAdoptKnownLegacyHostDataPathsRejectsUnknownTopLevel(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pccontroller")
	paths, err := HostDataPathsFor(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "unexpected"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AdoptKnownLegacyHostDataPaths(paths); err == nil {
		t.Fatal("unknown legacy top-level entry was adopted")
	}
	if _, err := os.Stat(filepath.Join(root, ownedstorage.MarkerName)); !os.IsNotExist(err) {
		t.Fatalf("invalid legacy layout published a marker: %v", err)
	}
}
