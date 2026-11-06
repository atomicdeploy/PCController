package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfUpdateCleanupRemovesOnlyExactTerminalScratchAndRetainsJournal(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "controller.exe")
	digest := strings.Repeat("a", 64)
	stem := filepath.Join(root, ".controller.exe.update-"+digest[:12])
	journal := selfUpdateJournal{State: "prepared", CurrentPath: current, ReplacementSHA256: digest,
		HelperPath: stem + platformHelperExtension(), StagedPath: stem + ".new", BackupPath: stem + ".old",
		HealthPath: stem + ".healthy", JournalPath: stem + ".journal.json"}
	for _, path := range []string{current, journal.HelperPath, journal.StagedPath, journal.BackupPath, journal.HealthPath} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeUpdateJSON(journal.JournalPath, journal)
	if cleanupTerminalSelfUpdate(journal.JournalPath, current) == nil {
		t.Fatal("uncommitted scratch removed")
	}
	if _, err := os.Stat(journal.HelperPath); err != nil {
		t.Fatal(err)
	}
	journal.State = "committed"
	ownedHelper := journal.HelperPath
	journal.HelperPath = filepath.Join(root, "user-file.exe")
	writeUpdateJSON(journal.JournalPath, journal)
	if cleanupTerminalSelfUpdate(journal.JournalPath, current) == nil {
		t.Fatal("forged cleanup path accepted")
	}
	journal.HelperPath = ownedHelper
	writeUpdateJSON(journal.JournalPath, journal)
	if err := cleanupTerminalSelfUpdate(journal.JournalPath, current); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{journal.HelperPath, journal.StagedPath, journal.BackupPath, journal.HealthPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("scratch retained: %s %v", path, err)
		}
	}
	for _, path := range []string{current, journal.JournalPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}
