package programmer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneCompileCacheKeepsCurrentAndRecentEntries(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-time.Hour)
	keys := []string{"00000001", "00000002", "00000003", "00000004", "00000005"}
	for index, key := range keys {
		path := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(path, "work"), 0o755); err != nil {
			t.Fatal(err)
		}
		when := base.Add(time.Duration(index) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "pinned-evidence"), 0o755); err != nil {
		t.Fatal(err)
	}

	identity := CompileIdentity{SketchPath: filepath.Join(root, keys[0], "PCController")}
	removed, err := pruneCompileCache(identity, 3)
	if err != nil {
		t.Fatalf("prune compile cache: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed=%d want 2", removed)
	}
	for _, key := range []string{"00000001", "00000004", "00000005", "pinned-evidence"} {
		if _, err := os.Stat(filepath.Join(root, key)); err != nil {
			t.Fatalf("retained %s: %v", key, err)
		}
	}
	for _, key := range []string{"00000002", "00000003"} {
		if _, err := os.Stat(filepath.Join(root, key)); !os.IsNotExist(err) {
			t.Fatalf("stale %s still exists: %v", key, err)
		}
	}
}

func TestPruneCompileCacheRejectsUnsafeIdentity(t *testing.T) {
	root := t.TempDir()
	identity := CompileIdentity{SketchPath: filepath.Join(root, "not-a-key", "PCController")}
	if _, err := pruneCompileCache(identity, 3); err == nil {
		t.Fatal("unsafe compile cache identity was accepted")
	}
}
