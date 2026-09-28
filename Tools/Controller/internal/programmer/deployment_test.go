package programmer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentUploadSkipsRawCaptureAndRetainsExistingBackup(t *testing.T) {
	root := t.TempDir()
	firmware := filepath.Join(root, "candidate.hex")
	if err := os.WriteFile(firmware, []byte(":020000000102FB\n:00000001FF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(root, "previous.hex")
	if err := os.WriteFile(retained, []byte("known retained content"), 0o600); err != nil {
		t.Fatal(err)
	}
	reads, writes := 0, 0
	runner := CommandRunnerFunc(func(context.Context, Command, io.Writer) error {
		reads++
		return errors.New("raw read unavailable")
	})
	flash := func(context.Context, string, io.Writer) error { writes++; return nil }
	options := AutomaticPreflashOptions{FirmwarePath: firmware, Deployment: "development", Backup: fakeBackupOptions(root)}
	result, err := AutomaticBackupThenFlash(context.Background(), options, runner, flash, io.Discard)
	if err != nil || reads != 0 || writes != 1 || !result.BackupSkipped || result.BackupComplete || result.Deployment != "development" {
		t.Fatalf("result=%+v reads=%d writes=%d err=%v", result, reads, writes, err)
	}
	if content, err := os.ReadFile(retained); err != nil || string(content) != "known retained content" {
		t.Fatalf("retained backup changed: %q %v", content, err)
	}
	for _, classification := range []string{"", "production", "development"} {
		options.Deployment = classification
		options.ReinitializeEEPROM = classification == "development"
		reads, writes = 0, 0
		result, err = AutomaticBackupThenFlash(context.Background(), options, runner, flash, io.Discard)
		if err == nil || reads == 0 || writes != 0 || result.BackupSkipped {
			t.Fatalf("protected %q bypass: %+v reads=%d writes=%d err=%v", classification, result, reads, writes, err)
		}
	}
}

func TestDevelopmentUploadStillRejectsChangedTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.hex")
	if err := os.WriteFile(path, []byte(":020000000102FB\n:00000001FF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	_, err := AutomaticBackupThenFlash(context.Background(), AutomaticPreflashOptions{
		FirmwarePath: path, Deployment: "development", Backup: fakeBackupOptions(t.TempDir()),
		AfterBackup: func(context.Context, AutomaticPreflashResult, io.Writer) error {
			return os.WriteFile(path, []byte(":020000000304F7\n:00000001FF\n"), 0o600)
		},
	}, newFakeAVRRunner(t), func(context.Context, string, io.Writer) error { writes++; return nil }, io.Discard)
	if err == nil || writes != 0 {
		t.Fatalf("target replacement accepted: writes=%d err=%v", writes, err)
	}
}
