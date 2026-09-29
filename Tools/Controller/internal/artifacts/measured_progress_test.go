package artifacts

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestMeasuredStageProgressResetsForUnknownPhaseAndFailure(t *testing.T) {
	service, err := NewService(Options{Store: newTestStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	initial, _, err := service.reserveOperation("firmware", "", "queued", "", "", ProgrammingMethodUrclock)
	if err != nil {
		t.Fatal(err)
	}
	if initial.ProgressKnown {
		t.Fatal("queued duration is unknown")
	}
	service.updateStatus(initial.ID, "writing", 42, "writing flash", "", "")
	status, _ := service.Status(initial.ID)
	if !status.ProgressKnown || status.ProgressPercent != 42 || status.Stage != "writing" || status.StageStartedAt.IsZero() {
		t.Fatalf("measured stage lost: %+v", status)
	}
	service.updateStatus(initial.ID, "reconnecting", -1, "waiting for HELLO", "", "")
	status, _ = service.Status(initial.ID)
	if status.ProgressKnown || status.ProgressPercent != 0 || status.Stage != "reconnecting" {
		t.Fatalf("new stage retained old percentage: %+v", status)
	}
	service.failOperation(initial.ID, errors.New("HELLO timed out"))
	status, _ = service.Status(initial.ID)
	if status.State != "failed" || status.ProgressKnown || status.Stage != "reconnecting" {
		t.Fatalf("failure must retain failed stage, not claim progress: %+v", status)
	}
}

func TestDownloadProgressUsesOnlyKnownDenominator(t *testing.T) {
	for _, total := range []int64{-1, 10} {
		var seen []int
		reader := &downloadProgressReader{Reader: strings.NewReader("abcdefghij"), total: total, lastPercent: -2,
			progress: func(_ string, percent int, _ string) { seen = append(seen, percent) }}
		buffer := make([]byte, 2)
		for {
			_, err := reader.Read(buffer)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if total < 0 && (len(seen) != 1 || seen[0] != -1) {
			t.Fatalf("unknown size invented progress: %v", seen)
		}
		if total > 0 && (len(seen) != 5 || seen[0] != 20 || seen[4] != 100) {
			t.Fatalf("measured progress wrong: %v", seen)
		}
	}
}
