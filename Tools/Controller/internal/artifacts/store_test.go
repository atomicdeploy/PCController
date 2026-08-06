package artifacts

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
