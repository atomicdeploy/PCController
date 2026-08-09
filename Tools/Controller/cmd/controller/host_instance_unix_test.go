//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHostInstanceClaimRecoversStaleUnixLockFile(t *testing.T) {
	paths := testHostInstancePaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.LockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockPath, []byte("abandoned after power loss\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := claimHostInstance(paths, "web")
	if err != nil {
		t.Fatalf("stale lock file blocked recovery: %v", err)
	}
	second, err := claimHostInstance(paths, "tui")
	if second != nil {
		_ = second.Close()
		t.Fatal("concurrent host acquired an already-held advisory lock")
	}
	if !errors.Is(err, errHostInstanceOwned) {
		t.Fatalf("concurrent claim error=%v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := claimHostInstance(paths, "shell")
	if err != nil {
		t.Fatalf("released advisory lock was not reusable: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHostInstanceClaimRejectsSymlinkLock(t *testing.T) {
	paths := testHostInstancePaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.LockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("do not modify"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.LockPath); err != nil {
		t.Fatal(err)
	}
	claim, err := claimHostInstance(paths, "web")
	if claim != nil {
		_ = claim.Close()
		t.Fatal("symlink lock was accepted")
	}
	if err == nil {
		t.Fatal("symlink lock rejection had no error")
	}
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("symlink target permissions changed to %04o", info.Mode().Perm())
	}
}

func TestHostInstanceClaimSecuresOwnedStateDirectory(t *testing.T) {
	paths := testHostInstancePaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.LockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(paths.LockPath), 0o770); err != nil {
		t.Fatal(err)
	}
	claim, err := claimHostInstance(paths, "web")
	if err != nil {
		t.Fatalf("owned state directory could not be secured: %v", err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(paths.LockPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory permissions=%04o", info.Mode().Perm())
	}
}
