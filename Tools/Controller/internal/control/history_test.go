package control

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestHistorySamplesAtConfiguredRateAndPersistsImportantTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	runtime := New(Options{})
	if err := runtime.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: 100 * time.Millisecond,
		TimelineLimit: 100, TimelinePath: path,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	runtime.recordStatus(now, native.Status{SupplyMV: 12000})
	runtime.recordStatus(now.Add(50*time.Millisecond), native.Status{SupplyMV: 12001})
	runtime.recordStatus(now.Add(100*time.Millisecond), native.Status{SupplyMV: 12002})
	if samples := runtime.StatusHistory(time.Time{}); len(samples) != 2 {
		t.Fatalf("status samples=%d, want 2", len(samples))
	}
	runtime.PublishHostEvent("telemetry", "not durable")
	runtime.PublishHostEvent("door", "door opened")
	deadline := time.Now().Add(time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil && len(content) != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeline was not written: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	reloaded := New(Options{})
	if err := reloaded.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: time.Second,
		TimelineLimit: 100, TimelinePath: path,
	}); err != nil {
		t.Fatal(err)
	}
	entries := reloaded.Timeline(time.Time{}, 100)
	if len(entries) != 1 || entries[0].Kind != "door" {
		t.Fatalf("reloaded timeline=%#v", entries)
	}
	if oversized := reloaded.Timeline(time.Time{}, math.MaxInt); len(oversized) > 100 {
		t.Fatalf("oversize timeline request returned %d entries, configured maximum is 100", len(oversized))
	}
}
