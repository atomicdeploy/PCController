//go:build !windows

package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateComponentsAcceptsPlatformTemporaryDirectory(t *testing.T) {
	// macOS commonly exposes t.TempDir through the root-owned /var ->
	// /private/var system alias. The alias is trusted by the Unix path guard,
	// and its followed target must still be classified as a directory.
	root := t.TempDir()
	if err := ValidateComponents(filepath.Join(root, "missing"), true); err != nil {
		t.Fatalf("temporary directory path was rejected: %v", err)
	}
}

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
	// A symlink created by UID 0 is intentionally classified as a trusted
	// system alias when its parent and target are also root-owned. Make the
	// fixture explicitly non-root-owned so this test exercises the intended
	// user-controlled-link case even when CI or an administrator runs as root.
	if os.Geteuid() == 0 {
		if err := os.Lchown(alias, 1, -1); err != nil {
			t.Fatalf("mark test symlink as user-owned: %v", err)
		}
	}

	resolved, err := ResolveAbsolute(filepath.Join(alias, "missing", "data"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRealRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalRealRoot, "missing", "data")
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
