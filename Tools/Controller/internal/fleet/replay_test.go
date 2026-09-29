package fleet

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReplayCacheReturnsInProgressAndTerminalRecords(t *testing.T) {
	cache, err := NewReplayCache(4, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	id := "00112233445566778899aabbccddeeff"
	first, fresh, err := cache.Begin(id, now)
	if err != nil || !fresh || first.State != ReplayInProgress {
		t.Fatalf("begin=%#v fresh=%t err=%v", first, fresh, err)
	}
	replay, fresh, err := cache.Begin(id, now.Add(time.Second))
	if err != nil || fresh || replay.State != ReplayInProgress {
		t.Fatalf("replay=%#v fresh=%t err=%v", replay, fresh, err)
	}
	result := json.RawMessage(`{"state":"applied"}`)
	if err := cache.Complete(id, result, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := cache.Complete(id, json.RawMessage(`{"state":"rejected"}`), now.Add(3*time.Second)); err == nil {
		t.Fatal("terminal replay result was overwritten")
	}
	result[2] = 'X'
	terminal, fresh, err := cache.Begin(id, now.Add(3*time.Second))
	if err != nil || fresh || terminal.State != ReplayComplete || string(terminal.Result) != `{"state":"applied"}` {
		t.Fatalf("terminal=%#v fresh=%t err=%v", terminal, fresh, err)
	}
	terminal.Result[2] = 'Y'
	again, ok := cache.Lookup(id, now.Add(4*time.Second))
	if !ok || string(again.Result) != `{"state":"applied"}` {
		t.Fatalf("cached result mutated: %#v", again)
	}
}

func TestReplayCacheExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	cache, err := NewReplayCache(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC)
	ids := []string{
		"00000000000000000000000000000001",
		"00000000000000000000000000000002",
		"00000000000000000000000000000003",
	}
	for _, id := range ids[:2] {
		if _, fresh, err := cache.Begin(id, now); err != nil || !fresh {
			t.Fatalf("begin %s fresh=%t err=%v", id, fresh, err)
		}
	}
	if _, ok := cache.Lookup(ids[0], now.Add(time.Second)); !ok {
		t.Fatal("first route missing before eviction")
	}
	if _, fresh, err := cache.Begin(ids[2], now.Add(2*time.Second)); err != nil || !fresh {
		t.Fatalf("third begin fresh=%t err=%v", fresh, err)
	}
	if _, ok := cache.Lookup(ids[1], now.Add(3*time.Second)); ok {
		t.Fatal("least recently used route was not evicted")
	}
	if _, fresh, err := cache.Begin(ids[0], now.Add(2*time.Minute)); err != nil || !fresh {
		t.Fatalf("expired route fresh=%t err=%v", fresh, err)
	}
}

func TestReplayCacheRejectsInvalidConfigurationAndResults(t *testing.T) {
	if _, err := NewReplayCache(0, time.Minute); err == nil {
		t.Fatal("zero capacity accepted")
	}
	if _, err := NewReplayCache(1, 0); err == nil {
		t.Fatal("zero ttl accepted")
	}
	cache, err := NewReplayCache(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	id := "00112233445566778899aabbccddeeff"
	now := time.Now().UTC()
	if _, _, err := cache.Begin(id, now); err != nil {
		t.Fatal(err)
	}
	if err := cache.Complete(id, json.RawMessage(`{`), now); err == nil {
		t.Fatal("invalid terminal JSON accepted")
	}
}
