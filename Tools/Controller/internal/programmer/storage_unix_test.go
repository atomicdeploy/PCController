//go:build !windows

package programmer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostDataPathsResolveExistingUnixSymlinkPrefix(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Fatal(err)
	}

	paths, err := HostDataPathsFor(filepath.Join(alias, "host-data"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalRealRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalRealRoot, "host-data")
	if paths.DataDir != want {
		t.Fatalf("data dir=%q want=%q", paths.DataDir, want)
	}
	if err := EnsureHostDataPaths(paths); err != nil {
		t.Fatalf("ensure canonical host data paths: %v", err)
	}
}
