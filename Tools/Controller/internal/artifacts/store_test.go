package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validIntelHEX = ":0100000001FE\n:00000001FF\n"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreContentAddressesAndDeduplicatesFirmware(t *testing.T) {
	store := newTestStore(t)
	first, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "first.hex", Source: "test", Current: true,
		BuildHash: "F6D76FE4", PackedTimestamp: 0x35019D5D,
		Metadata: map[string]string{"provider": "github", "workflow_run_id": "12345"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "renamed.hex", Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 {
		t.Fatalf("same content produced %s and %s", first.SHA256, second.SHA256)
	}
	entries, err := os.ReadDir(filepath.Join(store.blobs, first.SHA256[:2]))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("content blobs=%d want=1", len(entries))
	}
	current, err := store.Current(KindFirmware)
	if err != nil || current == nil || current.SHA256 != first.SHA256 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	list, err := store.List(nil)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	if list[0].LocalPath != "" {
		t.Fatal("public list leaked a local path")
	}
	if list[0].Metadata["provider"] != "github" ||
		list[0].Metadata["workflow_run_id"] != "12345" {
		t.Fatalf("artifact provenance metadata was not retained: %#v", list[0].Metadata)
	}
	if list[0].Name != "first.hex" || list[0].Source != "test" || second.Name != "first.hex" {
		t.Fatalf("duplicate redefined canonical artifact identity: list=%#v second=%#v", list[0], second)
	}
	_, file, err := store.Open(KindFirmware, first.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(file)
	_ = file.Close()
	if !bytes.Equal(content, []byte(validIntelHEX)) {
		t.Fatalf("content=%q", content)
	}
}

func TestStoreEmbeddedDefaultIdentitySurvivesDuplicateUpload(t *testing.T) {
	store := newTestStore(t)
	embedded, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindEEPROM, Name: "default-eeprom.hex", Source: "embedded", Embedded: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploaded, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindEEPROM, Name: "temporary-test.hex", Source: "browser-upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(KindEEPROM, embedded.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Name != "default-eeprom.hex" || stored.Name != "default-eeprom.hex" ||
		stored.Source != "embedded" || !stored.Embedded {
		t.Fatalf("embedded default identity drifted: uploaded=%#v stored=%#v", uploaded, stored)
	}
}

func TestStoreRejectsInvalidAndMismatchedArtifacts(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Put(strings.NewReader("not hex"), PutOptions{
		Kind: KindFirmware, Name: "bad.hex",
	}); err == nil {
		t.Fatal("invalid firmware accepted")
	}
	if _, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindEEPROM, Name: "settings.eep", ExpectedSHA256: strings.Repeat("0", 64),
	}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("hash mismatch err=%v", err)
	}
	if _, err := store.Put(strings.NewReader("x"), PutOptions{
		Kind: KindHostExecutable, Name: "../escape.exe",
	}); err == nil {
		t.Fatal("unsafe name accepted")
	}
	if _, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "secret.hex",
		Metadata: map[string]string{"access_token": "must-not-persist"},
	}); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("credential-shaped metadata was accepted: %v", err)
	}
}

func TestStoreRejectsTransportPathsOutsideItsOpenedRoot(t *testing.T) {
	store := newTestStore(t)
	descriptor, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "safe.hex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(Kind("../firmware"), descriptor.SHA256); err == nil {
		t.Fatal("traversing artifact kind accepted")
	}
	if _, err := store.Get(KindFirmware, "../"+descriptor.SHA256); err == nil {
		t.Fatal("traversing artifact digest accepted")
	}
	if err := store.writeJSONAtomic("../escape.json", map[string]bool{"unsafe": true}); err == nil {
		t.Fatal("metadata write outside opened store root accepted")
	}
	if _, err := os.Stat(filepath.Join(store.root, "..", "escape.json")); !os.IsNotExist(err) {
		t.Fatalf("metadata escaped store root: %v", err)
	}
}

func TestStoreOpenKeepsTheVerifiedConfinedHandle(t *testing.T) {
	store := newTestStore(t)
	descriptor, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "verified.hex",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, file, err := store.Open(KindFirmware, descriptor.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	blob := filepath.Join(store.blobs, descriptor.SHA256[:2], descriptor.SHA256)
	originalName := blob + ".opened"
	if err := os.Rename(blob, originalName); err != nil {
		// Some filesystems prohibit renaming an open file. That behavior also
		// prevents the path-swap class this test exercises.
		t.Skipf("filesystem keeps open artifact names locked: %v", err)
	}
	if err := os.WriteFile(blob, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, []byte(validIntelHEX)) {
		t.Fatalf("open artifact followed a replaced path: %q", content)
	}
}

func TestStoreRequestsReadOnlyModeAndRevalidatesInPlaceChanges(t *testing.T) {
	store := newTestStore(t)
	descriptor, err := store.Put(strings.NewReader(validIntelHEX), PutOptions{
		Kind: KindFirmware, Name: "immutable.hex",
	})
	if err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(store.blobs, descriptor.SHA256[:2], descriptor.SHA256)
	info, err := os.Stat(blob)
	if err != nil {
		t.Fatal(err)
	}
	// Windows maps portable modes to its read-only attribute and may report
	// synthesized write bits. POSIX platforms expose the exact stored bits.
	if writable := info.Mode().Perm() & 0o222; runtime.GOOS != "windows" && writable != 0 {
		t.Fatalf("published blob mode=%#o retains write bits %#o", info.Mode().Perm(), writable)
	}
	// Model a privileged/out-of-boundary actor deliberately re-enabling write
	// access. The next open must still hash the same handle and reject it.
	if err := os.Chmod(blob, 0o600); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(validIntelHEX, "01FE", "02FD", 1)
	if len(mutated) != len(validIntelHEX) {
		t.Fatal("mutation fixture changed artifact length")
	}
	if err := os.WriteFile(blob, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	_, file, err := store.Open(KindFirmware, descriptor.SHA256)
	if file != nil {
		_ = file.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("in-place mutation error=%v", err)
	}
}

func TestVerifyRegularFileHandleHashesAndRewindsOneHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.hex")
	if err := os.WriteFile(path, []byte(validIntelHEX), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(validIntelHEX))
	if err := verifyRegularFileHandle(file, hex.EncodeToString(digest[:]), int64(len(validIntelHEX))); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, []byte(validIntelHEX)) {
		t.Fatalf("verified handle was not rewound: %q", content)
	}
}
