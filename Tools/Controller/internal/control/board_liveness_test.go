package control

import (
	"errors"
	"testing"
)

func TestBoardLivenessRequiresConsecutiveFailuresAndSuccessResetsWindow(t *testing.T) {
	tracker := boardLivenessFailureTracker{}
	failure := errors.New("status timeout")
	for attempt := 1; attempt < boardLivenessFailureLimit; attempt++ {
		if tracker.observe(failure) {
			t.Fatalf("recovery triggered after only %d failures", attempt)
		}
	}
	if tracker.observe(nil) {
		t.Fatal("successful response triggered recovery")
	}
	if tracker.failures != 0 {
		t.Fatalf("successful response retained %d failures", tracker.failures)
	}
	for attempt := 1; attempt <= boardLivenessFailureLimit; attempt++ {
		triggered := tracker.observe(failure)
		if triggered != (attempt == boardLivenessFailureLimit) {
			t.Fatalf("attempt %d trigger=%t", attempt, triggered)
		}
	}
}
