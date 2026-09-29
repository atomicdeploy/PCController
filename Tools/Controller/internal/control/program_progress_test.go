package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/programmer"
	"testing"
	"time"
)

func TestDirectProgrammingProgressUsesSharedEventsAndRealOutcome(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	progressContext, finish := directProgrammingProgress(ctx, runtime, "firmware")
	programmer.ReportProgress(progressContext, programmer.Progress{Stage: "flash write:writing", Percent: 42, Detail: "Writing"})
	finish(errors.New("verification failed"))
	first, err := runtime.WaitEvent(ctx, 0, "update.running")
	if err != nil || first.Metadata["progress_known"] != "false" {
		t.Fatalf("preflight=%#v %v", first, err)
	}
	second, err := runtime.WaitEvent(ctx, first.ID, "update.running")
	if err != nil || second.Metadata["progress_percent"] != "42" || second.Metadata["progress_known"] != "true" || second.Metadata["stage_started_at"] == "" {
		t.Fatalf("measured=%#v %v", second, err)
	}
	failed, err := runtime.WaitEvent(ctx, second.ID, "update.failed")
	if err != nil || failed.Metadata["detail"] != "verification failed" || failed.Metadata["error"] != "verification failed" || failed.Metadata["operation_id"] != first.Metadata["operation_id"] {
		t.Fatalf("failed=%#v %v", failed, err)
	}
}

func TestDirectProgrammingDoesNotDuplicateArtifactObserver(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	before := runtime.LatestEventID()
	calls := 0
	ctx := programmer.WithProgress(context.Background(), func(programmer.Progress) { calls++ })
	ctx, finish := directProgrammingProgress(ctx, runtime, "firmware")
	programmer.ReportProgress(ctx, programmer.Progress{Stage: "flash", Percent: -1})
	finish(nil)
	if calls != 1 || runtime.LatestEventID() != before {
		t.Fatal("duplicated artifact progress")
	}
}
