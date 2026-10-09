package hostui

import "testing"

func TestAudioCueVocabularyIsStableAndDefensive(t *testing.T) {
	want := []AudioCue{
		"focus", "select", "navigation", "success",
		"warning", "error", "connect", "disconnect",
	}
	got := AudioCueNames()
	if len(got) != len(want) {
		t.Fatalf("AudioCueNames() count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] || !got[index].Valid() {
			t.Fatalf("AudioCueNames()[%d] = %q, want valid %q", index, got[index], want[index])
		}
	}
	got[0] = "changed"
	if AudioCueNames()[0] != AudioCueFocus {
		t.Fatal("AudioCueNames returned mutable package storage")
	}
	if AudioCue("").Valid() || AudioCue("alarm").Valid() {
		t.Fatal("unknown cue unexpectedly validated")
	}
}

func TestPlayAudioCueRejectsUnknownIntent(t *testing.T) {
	if err := PlayAudioCue("alarm"); err == nil {
		t.Fatal("PlayAudioCue accepted an unknown intent")
	}
}
