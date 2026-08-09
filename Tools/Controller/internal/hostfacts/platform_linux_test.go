//go:build linux

package hostfacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxSystemAndStorageFactsUseNativeCatalog(t *testing.T) {
	provider := newCachedProvider(nativeBackend{})
	system, err := provider.Query(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if system.Source != "linux-native" || system.Class != "LinuxOperatingSystem" ||
		len(system.Rows) != 1 || system.Rows[0]["Caption"] == "" {
		t.Fatalf("system=%+v", system)
	}
	storage, err := provider.Query(context.Background(), "storage")
	if err != nil {
		t.Fatal(err)
	}
	if storage.Source != "linux-native" || storage.Class != "LinuxMount" || len(storage.Rows) == 0 {
		t.Fatalf("storage=%+v", storage)
	}
}

func TestReadLinuxOSReleaseUnquotesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(path, []byte("NAME=Example\nPRETTY_NAME=\"Example Linux\"\n# ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := readLinuxOSRelease(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["NAME"] != "Example" || values["PRETTY_NAME"] != "Example Linux" {
		t.Fatalf("values=%q", values)
	}
}

func TestLinuxSMBIOSVersionParsesBothEntryPointFormats(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "smbios")
	if err := os.WriteFile(path, []byte{'_', 'S', 'M', '3', '_', 0, 0, 3, 7}, 0o600); err != nil {
		t.Fatal(err)
	}
	if major, minor := linuxSMBIOSVersion(path); major != 3 || minor != 7 {
		t.Fatalf("SMBIOS3=%d.%d", major, minor)
	}
	if err := os.WriteFile(path, []byte{'_', 'S', 'M', '_', 0, 0, 2, 8}, 0o600); err != nil {
		t.Fatal(err)
	}
	if major, minor := linuxSMBIOSVersion(path); major != 2 || minor != 8 {
		t.Fatalf("SMBIOS2=%d.%d", major, minor)
	}
}
