package artifacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The helper cannot delete its own mapped PE on Windows. Its healthy
// replacement cleans completed scratch files after the coordinator exits;
// small transaction journals remain available for durable operation truth.
func scheduleSelfUpdateCleanup(journalPath string) {
	go func() {
		current, err := os.Executable()
		if err != nil {
			return
		}
		for attempt := 0; attempt < 10; attempt++ {
			time.Sleep(2 * time.Second)
			if cleanupTerminalSelfUpdate(journalPath, current) == nil {
				return
			}
		}
	}()
}

func cleanupTerminalSelfUpdate(journalPath, current string) error {
	journal, err := loadSelfUpdateJournal(journalPath)
	if err != nil {
		return err
	}
	if journal.CurrentPath != current || !selfUpdateTerminal(journal.State) {
		return errors.New("self-update transaction is not terminal for this executable")
	}
	digest, err := normalizeSHA256(journal.ReplacementSHA256)
	if err != nil {
		return err
	}
	stem := filepath.Join(filepath.Dir(current), "."+filepath.Base(current)+".update-"+digest[:12])
	// Derive exact owned names; never delete arbitrary paths read from JSON.
	paths := []struct{ actual, expected string }{
		{journal.HelperPath, stem + platformHelperExtension()},
		{journal.StagedPath, stem + ".new"},
		{journal.BackupPath, stem + ".old"},
		{journal.HealthPath, stem + ".healthy"},
		{journal.JournalPath, stem + ".journal.json"},
	}
	for _, path := range paths {
		if path.actual != path.expected {
			return fmt.Errorf("self-update cleanup path identity mismatch")
		}
	}
	var failures []error
	for _, path := range paths[:4] {
		if err := os.Remove(path.expected); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
