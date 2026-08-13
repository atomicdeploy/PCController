package programmer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptToolchainReplacesIncompleteCoreAndCopiesLibraries(t *testing.T) {
	root := t.TempDir()
	sourceData := filepath.Join(root, "managed-data")
	sourceUser := filepath.Join(root, "managed-user")
	targetData := filepath.Join(root, "Arduino15")
	targetUser := filepath.Join(root, "Arduino")
	for _, vendor := range []string{"arduino", "builtin", "MiniCore"} {
		path := filepath.Join(sourceData, "packages", vendor, "payload.txt")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(vendor), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	core := filepath.Join(sourceData, "packages", "MiniCore", "hardware", "avr", "3.1.2", "cores", "MCUdude_corefiles", "Arduino.h")
	if err := os.MkdirAll(filepath.Dir(core), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("complete"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(targetData, "packages", "MiniCore", "hardware", "avr", "3.1.2", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(sourceUser, "libraries", "OneWire", "OneWire.h")
	if err := os.MkdirAll(filepath.Dir(library), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(library, []byte("wire"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := AdoptToolchain(sourceData, sourceUser, targetData, targetUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Packages) != 3 || len(report.Libraries) != 1 {
		t.Fatalf("unexpected adoption report: %+v", report)
	}
	if content, err := os.ReadFile(filepath.Join(targetData, "packages", "MiniCore", "hardware", "avr", "3.1.2", "cores", "MCUdude_corefiles", "Arduino.h")); err != nil || string(content) != "complete" {
		t.Fatalf("complete core not adopted: %q err=%v", content, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("partial target survived atomic replacement: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetUser, "libraries", "OneWire", "OneWire.h")); err != nil {
		t.Fatalf("verified library not adopted: %v", err)
	}
}
