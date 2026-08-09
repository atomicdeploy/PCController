//go:build !windows

package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAbsoluteCanonicalizesExistingSymlinkPrefix(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveAbsolute(filepath.Join(alias, "missing", "data"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realRoot, "missing", "data")
	if resolved != want {
		t.Fatalf("resolved=%q want=%q", resolved, want)
	}
	if err := ValidateComponents(filepath.Join(alias, "missing", "data"), true); err == nil {
		t.Fatal("user-owned symlink prefix was accepted")
	}
	if err := ValidateComponents(resolved, true); err != nil {
		t.Fatalf("canonical path should not traverse the alias: %v", err)
	}
}
