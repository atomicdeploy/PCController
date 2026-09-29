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
	// WriteFile applies the process umask to newly-created files. Canonical host
	// tests run under UMask=0077, so normalize the sentinel before exercising
	// the lock code and assert the precondition explicitly.
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0o640 {
		t.Fatalf("symlink target initial permissions=%04o", before.Mode().Perm())
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

func TestHostInstanceOpenAtSurvivesStateDirectoryPathSwap(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state")
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := openHostInstanceLockDirectory(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()

	relocatedPath := filepath.Join(root, "validated-state")
	if err := os.Rename(statePath, relocatedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "replacement-target")
	if err := os.WriteFile(target, []byte("do not modify"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	const lockName = "host-instance.lock"
	replacementLock := filepath.Join(statePath, lockName)
	if err := os.Symlink(target, replacementLock); err != nil {
		t.Fatal(err)
	}

	lock, err := openHostInstanceLockAt(directory, lockName, replacementLock)
	if err != nil {
		t.Fatalf("descriptor-relative lock open followed replacement path: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(relocatedPath, lockName))
	if err != nil {
		t.Fatalf("lock was not created in validated directory: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("validated lock mode=%v", info.Mode())
	}
	info, err = os.Lstat(replacementLock)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("replacement-path symlink was modified")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if string(content) != "do not modify" || info.Mode().Perm() != 0o640 {
		t.Fatalf("replacement target changed: content=%q mode=%04o", content, info.Mode().Perm())
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
