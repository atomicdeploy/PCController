package control

import (
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestParseRFLearnOptionsDefaultsToIndefiniteMultiCode(t *testing.T) {
	options, err := parseRFLearnOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.Mode != RFLearnIndefinite || options.Timeout != 0 {
		t.Fatalf("unexpected default options: %#v", options)
	}
}

func TestParseRFLearnOptionsTimerAndDocumentedAliases(t *testing.T) {
	for _, mode := range []string{"timer", "single", "one-shot"} {
		options, err := parseRFLearnOptions([]string{mode, "45s"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if options.Mode != RFLearnTimer || options.Timeout != 45*time.Second {
			t.Fatalf("%s produced %#v", mode, options)
		}
	}
}

func TestParseRFLearnOptionsRejectsMixedOrLegacyGrammar(t *testing.T) {
	for _, args := range [][]string{
		{"indefinite", "30s"},
		{"30s"},
		{"timer", "30s", "multi"},
	} {
		if _, err := parseRFLearnOptions(args); err == nil {
			t.Fatalf("expected %q to fail", strings.Join(args, " "))
		}
	}
}

func TestRFLearnStateReportsLiveRemainingTime(t *testing.T) {
	runtime := New(Options{})
	runtime.rfLearnState = RFLearnState{
		Active: true, Mode: RFLearnTimer,
		ConfiguredMS: 30_000, RemainingMS: 30_000,
		EndsAt: time.Now().Add(2 * time.Second),
	}
	state := runtime.RFLearnState()
	if state.RemainingMS <= 0 || state.RemainingMS > 2_000 {
		t.Fatalf("unexpected live remaining time: %dms", state.RemainingMS)
	}
}

func TestRFLearnLifecycleEventsDriveSnapshotAndRequireExplicitMapping(t *testing.T) {
	runtime := New(Options{})
	mapping, _ := runtime.observeRFLearningEvent(native.DeviceEvent{
		Type: native.EventRFLearning, RFLearnState: native.RFLearningStarted,
		RFLearnMode: native.RFLearnModeTimer, RFLearnTotalSeconds: 30,
		RFLearnRemainingSeconds: 30, RFLearnCount: 4,
	})
	state := runtime.RFLearnState()
	if mapping || !state.Active || state.Mode != RFLearnTimer ||
		state.ConfiguredMS != 30_000 || state.RemainingMS <= 0 || state.Reason != "" {
		t.Fatalf("unexpected started state: %#v mapping=%t", state, mapping)
	}

	mapping, captured := runtime.observeRFLearningEvent(native.DeviceEvent{
		Type: native.EventRFLearned, RFID: 7,
	})
	if !mapping || captured != 1 || runtime.RFLearnState().Learned != 1 {
		t.Fatalf("capture did not require an explicit mapping: mapping=%t captured=%d state=%#v", mapping, captured, runtime.RFLearnState())
	}
	runtime.publishRFMappingRequired(native.DeviceEvent{Type: native.EventRFLearned, RFID: 7}, captured)
	select {
	case event := <-runtime.Events():
		if event.Kind != "rf.learn.mapping-required" || event.RFID != 7 ||
			event.Metadata["mapping"] != "unmapped" {
			t.Fatalf("unexpected mapping event: %#v", event)
		}
	default:
		t.Fatal("mapping-required event was not published")
	}

	runtime.observeRFLearningEvent(native.DeviceEvent{
		Type: native.EventRFLearning, RFLearnState: native.RFLearningProgress,
		RFLearnMode: native.RFLearnModeTimer, RFLearnTotalSeconds: 30,
		RFLearnRemainingSeconds: 12, RFLearnCount: 5,
	})
	if remaining := runtime.RFLearnState().RemainingMS; remaining <= 10_000 || remaining > 12_000 {
		t.Fatalf("unexpected progress remaining=%d", remaining)
	}

	runtime.observeRFLearningEvent(native.DeviceEvent{
		Type: native.EventRFLearning, RFLearnState: native.RFLearningFull,
		RFLearnMode: native.RFLearnModeTimer, RFLearnTotalSeconds: 30,
		RFLearnRemainingSeconds: 8, RFLearnCount: 20,
	})
	state = runtime.RFLearnState()
	if state.Active || state.RemainingMS != 0 || state.Reason != "storage full" {
		t.Fatalf("unexpected terminal state: %#v", state)
	}
}

func TestDescribeRFLearnLifecycleUsesSpecificEventKinds(t *testing.T) {
	for state, expected := range map[byte]string{
		native.RFLearningStarted:   "rf.learn.started",
		native.RFLearningProgress:  "rf.learn.progress",
		native.RFLearningEnded:     "rf.learn.ended",
		native.RFLearningCancelled: "rf.learn.cancelled",
		native.RFLearningFull:      "rf.learn.full",
	} {
		kind, text := describeDeviceEvent(native.DeviceEvent{
			Type: native.EventRFLearning, RFLearnState: state,
			RFLearnMode: native.RFLearnModeTimer, RFLearnTotalSeconds: 30,
			RFLearnRemainingSeconds: 12, RFLearnCount: 4,
		})
		if kind != expected || !strings.Contains(text, "mode=timer") ||
			!strings.Contains(text, "remaining=12s") {
			t.Fatalf("state %d description=%q %q", state, kind, text)
		}
	}
}
