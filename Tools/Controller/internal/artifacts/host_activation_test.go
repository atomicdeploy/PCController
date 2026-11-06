package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostActivationRequiresCommittedTransactionNotStagedOrCandidateHealthy(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "controller.exe")
	if err := os.WriteFile(current, []byte("candidate executable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, _ := fileDigest(current)
	status := UpdateStatus{ID: "op-activation", Kind: "host", State: "awaiting-restart", Stage: "awaiting-restart", ArtifactSHA256: digest, StartedAt: time.Now().Add(-time.Minute)}
	journal := selfUpdateJournal{CurrentPath: current, ReplacementSHA256: digest, OperationID: status.ID, UpdatedAt: time.Now(),
		JournalPath: filepath.Join(root, ".controller.exe.update-"+digest[:12]+".journal.json")}
	for _, state := range []string{"prepared", "replacing", "awaiting-health", "candidate-healthy", "committed", "rolled-back", "candidate-start-failed"} {
		journal.State = state
		if err := writeUpdateJSON(journal.JournalPath, journal); err != nil {
			t.Fatal(err)
		}
		got := hostActivationStatus(status, current)
		if state == "committed" {
			if got.State != "completed" || got.Stage != "completed" {
				t.Fatalf("commit=%+v", got)
			}
		} else if state == "rolled-back" || strings.HasSuffix(state, "-failed") {
			if got.State != "failed" {
				t.Fatalf("failure=%+v", got)
			}
		} else if got.State == "completed" {
			t.Fatalf("premature success at %s", state)
		}
	}
	journal.State, journal.OperationID = "committed", "op-different"
	writeUpdateJSON(journal.JournalPath, journal)
	if hostActivationStatus(status, current).State == "completed" {
		t.Fatal("another operation committed this update")
	}
	journal.OperationID, journal.UpdatedAt = status.ID, status.StartedAt.Add(-time.Second)
	writeUpdateJSON(journal.JournalPath, journal)
	if hostActivationStatus(status, current).State == "completed" {
		t.Fatal("stale transaction committed a later update")
	}
	journal.UpdatedAt = time.Now()
	writeUpdateJSON(journal.JournalPath, journal)
	os.WriteFile(current, []byte("different image"), 0600)
	if hostActivationStatus(status, current).State != "completed" {
		t.Fatal("a later image replaced the durable result of an earlier committed update")
	}
}

func TestHostStagedCompletionIsDowngradedAndPendingOperationSurvivesRestart(t *testing.T) {
	store := newTestStore(t)
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := service.reserveOperation("host", strings.Repeat("a", 64), "fixture", "pending-fixture", "fingerprint", ProgrammingMethodNone)
	if err != nil {
		t.Fatal(err)
	}
	service.run(status.ID, func(context.Context, ProgressFunc) (string, error) {
		return status.ArtifactSHA256, ErrHostActivationPending
	})
	got, _ := service.Status(status.ID)
	if got.State != "awaiting-restart" || got.ProgressKnown {
		t.Fatalf("staging was treated as terminal: %+v", got)
	}
	for _, kind := range []string{"host", "firmware"} {
		if _, _, err := service.reserveOperation(kind, status.ArtifactSHA256, "overlap", "different-key", "other", ProgrammingMethodNone); err == nil {
			t.Fatal("pending host activation admitted overlapping ", kind)
		}
	}
	if observed, reused, err := service.reserveOperation("host", status.ArtifactSHA256, "repeat", "pending-fixture", "fingerprint", ProgrammingMethodNone); err != nil || !reused || observed.ID != status.ID {
		t.Fatal("idempotent observation was blocked or replayed")
	}
	service.Close()
	restarted, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, _ = restarted.Status(status.ID)
	if got.State != "awaiting-restart" {
		t.Fatalf("pending activation lost at restart: %+v", got)
	}
	staged := status
	staged.State, staged.Stage = "completed", "staged"
	if hostActivationStatus(staged, filepath.Join(t.TempDir(), "controller.exe")).State == "completed" {
		t.Fatal("staged legacy completion was trusted without activation")
	}
}
