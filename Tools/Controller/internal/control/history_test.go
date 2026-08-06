package control

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestHistorySamplesAtConfiguredRateAndPersistsImportantTimeline(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "timeline.jsonl")
	statusPath := filepath.Join(directory, "measurements.jsonl")
	runtime := New(Options{})
	if err := runtime.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: 100 * time.Millisecond,
		TimelineLimit: 100, TimelinePath: path, StatusPath: statusPath,
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
	if err := runtime.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: 100 * time.Millisecond,
		TimelineLimit: 100, TimelinePath: path, StatusPath: statusPath,
	}); err != nil {
		t.Fatal(err)
	}

	reloaded := New(Options{})
	if err := reloaded.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: time.Second,
		TimelineLimit: 100, TimelinePath: path, StatusPath: statusPath,
	}); err != nil {
		t.Fatal(err)
	}
	entries := reloaded.Timeline(time.Time{}, 100)
	if len(entries) != 1 || entries[0].Kind != "door" {
		t.Fatalf("reloaded timeline=%#v", entries)
	}
	if samples := reloaded.StatusHistory(time.Time{}); len(samples) != 2 {
		t.Fatalf("reloaded measurement samples=%d, want 2", len(samples))
	}
	if oversized := reloaded.Timeline(time.Time{}, math.MaxInt); len(oversized) > 100 {
		t.Fatalf("oversize timeline request returned %d entries, configured maximum is 100", len(oversized))
	}
}

func TestMeasurementHistorySurvivesRestartWithoutDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "measurements.jsonl")
	options := HistoryOptions{
		Retention: time.Hour, SampleInterval: 100 * time.Millisecond,
		TimelineLimit: 100, StatusPath: path,
	}
	first := New(Options{})
	if err := first.ConfigureHistory(options); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Millisecond)
	want := []StatusSample{
		{Time: base, Status: completeTestStatus(1)},
		{Time: base.Add(100 * time.Millisecond), Status: completeTestStatus(2)},
	}
	first.recordStatus(want[0].Time, want[0].Status)
	first.recordStatus(want[0].Time, want[0].Status)
	first.recordStatus(want[1].Time, want[1].Status)
	// Reconfiguration is also the synchronous persistence barrier used by the
	// watched-config path. It must not duplicate already retained samples.
	if err := first.ConfigureHistory(options); err != nil {
		t.Fatal(err)
	}
	if got := first.StatusHistory(time.Time{}); !equalStatusSamples(got, want) {
		t.Fatalf("live samples=%#v, want %#v", got, want)
	}

	second := New(Options{})
	if err := second.ConfigureHistory(options); err != nil {
		t.Fatal(err)
	}
	if got := second.StatusHistory(time.Time{}); !equalStatusSamples(got, want) {
		t.Fatalf("restarted samples=%#v, want %#v", got, want)
	}
	if err := second.ConfigureHistory(options); err != nil {
		t.Fatal(err)
	}
	if got := second.StatusHistory(time.Time{}); !equalStatusSamples(got, want) {
		t.Fatalf("reconfigured samples=%#v, want %#v", got, want)
	}
}

func TestMeasurementHistoryPrunesRetentionAndCorruptTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "measurements.jsonl")
	now := time.Now().UTC().Truncate(time.Millisecond)
	stale := StatusSample{Time: now.Add(-2 * time.Hour), Status: completeTestStatus(1)}
	fresh := StatusSample{Time: now.Add(-time.Minute), Status: completeTestStatus(2)}
	staleLine, err := marshalMeasurementSample(stale)
	if err != nil {
		t.Fatal(err)
	}
	freshLine, err := marshalMeasurementSample(fresh)
	if err != nil {
		t.Fatal(err)
	}
	content := append([]byte(nil), staleLine...)
	content = append(content, freshLine...)
	content = append(content, freshLine...)
	content = append(content, []byte(`{"t":`)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	runtime := New(Options{})
	if err := runtime.ConfigureHistory(HistoryOptions{
		Retention: time.Hour, SampleInterval: time.Second,
		TimelineLimit: 100, StatusPath: path,
	}); err != nil {
		t.Fatal(err)
	}
	got := runtime.StatusHistory(time.Time{})
	if len(got) != 1 || !equalStatusSamples(got, []StatusSample{fresh}) {
		t.Fatalf("pruned samples=%#v, want only fresh sample", got)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(rewritten, []byte{'\n'}) != 1 || rewritten[len(rewritten)-1] != '\n' {
		t.Fatalf("compacted measurement history still contains duplicates/corrupt tail: %q", rewritten)
	}
}

func TestMeasurementHistoryByteLimitKeepsNewestSamples(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Millisecond)
	samples := []StatusSample{
		{Time: base, Status: completeTestStatus(1)},
		{Time: base.Add(time.Second), Status: completeTestStatus(2)},
		{Time: base.Add(2 * time.Second), Status: completeTestStatus(3)},
	}
	second, _ := marshalMeasurementSample(samples[1])
	third, _ := marshalMeasurementSample(samples[2])
	limit := len(second) + len(third)
	content, err := encodeBoundedMeasurementHistory(samples, limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > limit {
		t.Fatalf("bounded content length=%d limit=%d", len(content), limit)
	}
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("bounded line count=%d, want 2", len(lines))
	}
	for index, line := range lines {
		got, err := unmarshalMeasurementSample(line)
		if err != nil {
			t.Fatal(err)
		}
		if !equalStatusSamples([]StatusSample{got}, []StatusSample{samples[index+1]}) {
			t.Fatalf("bounded sample[%d]=%#v, want %#v", index, got, samples[index+1])
		}
	}
}

func completeTestStatus(seed byte) native.Status {
	return native.Status{
		UptimeMS: uint32(seed) * 1000, SupplyMV: 12000 + int32(seed),
		BusMV: 11900 + int32(seed), CurrentMA: -200 + int32(seed),
		PowerMW: 2300 + int32(seed), TLEDCenti: 2800 + int16(seed),
		TBTCenti: 2500 + int16(seed), Flags: uint16(seed) | 0x300,
		ProgramRunning: seed&1 != 0, HostOffline: seed&2 != 0,
		Hot: seed&4 != 0, RawInputs: seed + 1, ActiveKeys: seed + 2,
		ActiveRelays: seed + 3, MenuPage: seed + 4, ProgramMode: seed + 5,
		DoorOpen: seed&1 == 0, BluetoothState: seed + 6,
		PWMAvailable: seed&1 != 0, PWMChannel: seed + 7,
		PWMValue: uint16(seed) * 100, LCDAddress: 0x27,
		PWMErrors: seed + 8, FramingErrors: uint16(seed) * 2,
		CRCErrors: uint16(seed) * 3, ResetCause: seed + 9,
		ResetCount: uint32(seed) * 4,
	}
}

func equalStatusSamples(left, right []StatusSample) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Time.Equal(right[index].Time) || left[index].Status != right[index].Status {
			return false
		}
	}
	return true
}
