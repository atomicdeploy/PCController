package programmer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDefaultHostDataDirectoryUsesDurablePlatformLocations(t *testing.T) {
	environment := map[string]string{}
	lookup := func(name string) string { return environment[name] }
	base := t.TempDir()
	home := func() (string, error) { return filepath.Join(base, "users", "david"), nil }
	config := func() (string, error) { return filepath.Join(base, "config"), nil }

	environment[HostDataDirectoryEnvironment] = filepath.Join(base, "custom", "controller")
	value, err := defaultHostDataDirectory("windows", lookup, home, config)
	if err != nil || value != environment[HostDataDirectoryEnvironment] {
		t.Fatalf("override=%q err=%v", value, err)
	}
	delete(environment, HostDataDirectoryEnvironment)
	environment["LOCALAPPDATA"] = filepath.Join(base, "local")
	value, err = defaultHostDataDirectory("windows", lookup, home, config)
	if err != nil || value != filepath.Join(environment["LOCALAPPDATA"], "PCController") {
		t.Fatalf("Windows path=%q err=%v", value, err)
	}
	delete(environment, "LOCALAPPDATA")
	environment["XDG_DATA_HOME"] = filepath.Join(base, "xdg")
	value, err = defaultHostDataDirectory("linux", lookup, home, config)
	if err != nil || value != filepath.Join(environment["XDG_DATA_HOME"], "pccontroller") {
		t.Fatalf("Linux path=%q err=%v", value, err)
	}
}

func TestHostDataPathsAndDirectories(t *testing.T) {
	paths, err := HostDataPathsFor(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureHostDataPaths(paths); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		paths.BackupsDir, paths.BackupOperations, paths.FirmwareBlobsDir,
		paths.StateDir, paths.LogsDir,
	} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("durable directory missing %q: %v", path, err)
		}
	}
	if _, err := HostDataPathsFor("relative"); err == nil {
		t.Fatal("relative durable data path was accepted")
	}
}

func TestFirmwareBlobIsContentAddressedDeduplicatedAndRaceSafe(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "firmware.hex")
	image := &IntelHexImage{data: map[uint32]byte{0: 1, 1: 2, 2: 3}}
	content, _ := image.Canonical()
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	blobRoot := filepath.Join(directory, "blobs")
	const workers = 24
	results := make(chan FirmwareBlob, workers)
	errorsChannel := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			blob, err := StoreFirmwareBlob(blobRoot, source)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- blob
		}()
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent blob store: %v", err)
	}
	var expected FirmwareBlob
	created := 0
	count := 0
	for blob := range results {
		count++
		if expected.Path == "" {
			expected = blob
		}
		if blob.Path != expected.Path || blob.SHA256 != sha256Hex(content) ||
			blob.Reference != "firmware-sha256-"+sha256Hex(content)+".hex" {
			t.Errorf("inconsistent blob result: %#v", blob)
		}
		if !blob.Deduplicated {
			created++
		}
	}
	if count != workers || created != 1 {
		t.Fatalf("workers=%d created=%d, want %d and 1", count, created, workers)
	}
	files := 0
	if err := filepath.Walk(blobRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			files++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("content store has %d firmware files, want 1", files)
	}
	second, err := StoreFirmwareBlob(blobRoot, source)
	if err != nil || !second.Deduplicated {
		t.Fatalf("sequential dedup=%#v err=%v", second, err)
	}
}

func TestFirmwareTimestampSchema2ExactASAEncoding(t *testing.T) {
	date := uint32((2026-2000)<<9 | 8<<5 | 1)
	timeBits := uint32(19<<11 | 42<<5 | 58>>1)
	packed := date<<16 | timeBits
	decoded, err := DecodeFirmwareTimestampSchema2(packed)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Compact != "260801194258" || decoded.BuildTime != time.Date(2026, 8, 1, 19, 42, 58, 0, time.UTC) {
		t.Fatalf("packed=%08X decode=%#v", packed, decoded)
	}
	invalidDate := uint32((2026-2000)<<25 | 2<<21 | 31<<16)
	if _, err := DecodeFirmwareTimestampSchema2(invalidDate); err == nil {
		t.Fatal("invalid packed calendar date was accepted")
	}
}

func ExampleDecodeFirmwareTimestampSchema2() {
	date := uint32((2026-2000)<<9 | 8<<5 | 1)
	clock := uint32(19<<11 | 42<<5 | 58>>1)
	decoded, _ := DecodeFirmwareTimestampSchema2(date<<16 | clock)
	fmt.Println(decoded.Compact)
	// Output: 260801194258
}
