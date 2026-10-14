package control

import (
	"testing"
	"time"
)

func TestRFStopAfterCaptureCannotCancelLaterOrContinuousSession(t *testing.T) {
	runtime := New(Options{})
	started := time.Now()
	runtime.rfLearnState = RFLearnState{Active: true, Continuous: true, StartedAt: started}
	runtime.stopAfterRFLearnCapture(0, 1, started)
	if !runtime.RFLearnState().Active {
		t.Fatal("continuous learning stopped")
	}
	runtime.rfLearnState.Continuous = false
	runtime.stopAfterRFLearnCapture(0, 1, started.Add(-time.Second))
	if !runtime.RFLearnState().Active {
		t.Fatal("stale capture cancelled later session")
	}
}
