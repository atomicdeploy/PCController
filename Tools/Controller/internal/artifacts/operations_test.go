package artifacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationJournalMarksUnsafeReplayInterrupted(t *testing.T) {
	store := newTestStore(t)
	status := UpdateStatus{
		ID: "op-interrupted", Kind: "firmware", State: "programming",
		StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		IdempotencyKey: "deploy-7", ProgrammingMethod: ProgrammingMethodUrclock,
	}
	directory := filepath.Join(store.Root(), "operations")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(directory, status.ID+".json"), operationJournal{
		Schema: operationJournalSchema, Status: status, Scope: "firmware", Fingerprint: "request-hash",
	}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	recovered, err := service.Status(status.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "failed" || recovered.ErrorCode != "host_restarted" {
		t.Fatalf("recovered=%#v", recovered)
	}
	content, err := os.ReadFile(filepath.Join(directory, status.ID+".json"))
	if err != nil || len(content) == 0 {
		t.Fatalf("journal was not retained: %v", err)
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	for _, invalid := range []string{"contains space", "slash/key", string(make([]byte, 129))} {
		if _, err := normalizeIdempotencyKey(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
