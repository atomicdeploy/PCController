//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/ownedstorage"
)

func TestOpenWindowsServiceLogEstablishesOwnershipFirst(t *testing.T) {
	root := filepath.Join(t.TempDir(), "service-data")
	logFile, err := openWindowsServiceLog(root)
	if err != nil {
		t.Fatalf("open service log: %v", err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("close service log: %v", err)
	}
	if err := ownedstorage.Verify(root); err != nil {
		t.Fatalf("service data ownership was not established: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "service.log")); err != nil {
		t.Fatalf("service log was not created: %v", err)
	}
}

func TestOpenWindowsServiceLogRefusesForeignNonEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "foreign.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatalf("write foreign fixture: %v", err)
	}
	if _, err := openWindowsServiceLog(root); err == nil {
		t.Fatal("unmarked non-empty root was adopted")
	}
	if _, err := os.Stat(filepath.Join(root, "service.log")); !os.IsNotExist(err) {
		t.Fatalf("service log changed rejected root: %v", err)
	}
}
