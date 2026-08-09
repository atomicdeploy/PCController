//go:build linux

package aptmirror

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationLockUsesKernelLifetimeAndDoesNotConflictAcrossPaths(t *testing.T) {
	root := t.TempDir()
	adoptionPath := filepath.Join(root, "locks", "adoption.lock")
	refreshPath := filepath.Join(root, "locks", "refresh.lock")

	releaseAdoption, err := acquireOperationLock(adoptionPath, "APT mirror adoption")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAdoption()

	if _, err := acquireOperationLock(adoptionPath, "APT mirror adoption"); err == nil ||
		!strings.Contains(err.Error(), "adoption is already running") {
		t.Fatalf("second adoption lock err=%v", err)
	}

	// Install holds the adoption lock while its nested Refresh takes the
	// separate refresh lock. Prove those descriptors do not self-deadlock.
	releaseRefresh, err := acquireOperationLock(refreshPath, "APT mirror refresh")
	if err != nil {
		t.Fatalf("nested refresh lock conflicted with adoption lock: %v", err)
	}
	releaseRefresh()

	// flock is descriptor/process scoped: closing the owner releases it without
	// deleting a marker, which is the same stale-lock behavior after a crash.
	releaseAdoption()
	replacement, err := acquireOperationLock(adoptionPath, "APT mirror adoption")
	if err != nil {
		t.Fatalf("closed adoption descriptor left a stale lock: %v", err)
	}
	replacement()
}
