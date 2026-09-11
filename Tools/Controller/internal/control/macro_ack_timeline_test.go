package control

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
)

type macroTestClock struct{ current time.Time }

func (clock *macroTestClock) now() time.Time { return clock.current }
func (clock *macroTestClock) wait(ctx context.Context, due time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if due.After(clock.current) {
		clock.current = due
	}
	return nil
}

func TestHostMacroFirstAckTimelinePreservesRecordedOffsets(t *testing.T) {
	for _, test := range []struct {
		name                   string
		due, latency, dispatch []uint32
		deltas                 []int32
	}{
		{"observed startup stall", []uint32{0, 2265090, 3676547}, []uint32{539722, 4658, 17094}, []uint32{0, 2804812, 4216269}, []int32{539722, 4658, 17094}},
		{"leading wait", []uint32{200000, 500000}, []uint32{150000, 4000}, []uint32{200000, 650000}, []int32{150000, 4000}},
		{"later stall is not reanchored", []uint32{0, 200000, 300000}, []uint32{5000, 250000, 5000}, []uint32{0, 205000, 455000}, []int32{5000, 250000, 155000}},
		{"simultaneous commands stay ordered", []uint32{100000, 100000, 100000}, []uint32{5000, 6000, 7000}, []uint32{100000, 105000, 111000}, []int32{5000, 6000, 13000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			epoch := time.Unix(100, 0)
			clock := &macroTestClock{current: epoch}
			compiled := compiledMacro{}
			for index, due := range test.due {
				compiled.steps = append(compiled.steps, compiledMacroStep{dueUS: due, opcode: byte(index + 1)})
			}
			var dispatch []uint32
			var deltas []int32
			count, err := runHostMacroWithClock(context.Background(), compiled,
				func(_ context.Context, opcode byte, _ []byte) error {
					index := len(dispatch)
					if opcode != byte(index+1) {
						t.Fatalf("reordered opcode: %d", opcode)
					}
					dispatch = append(dispatch, uint32(clock.current.Sub(epoch)/time.Microsecond))
					clock.current = clock.current.Add(time.Duration(test.latency[index]) * time.Microsecond)
					return nil
				}, func(index int, delta int32, succeeded bool) {
					if index != len(deltas) || !succeeded {
						t.Fatalf("bad evidence: index=%d succeeded=%t", index, succeeded)
					}
					deltas = append(deltas, delta)
				}, clock.now, clock.wait)
			if err != nil || count != len(test.due) || !reflect.DeepEqual(dispatch, test.dispatch) || !reflect.DeepEqual(deltas, test.deltas) {
				t.Fatalf("count=%d dispatch=%v deltas=%v err=%v; want dispatch=%v deltas=%v", count, dispatch, deltas, err, test.dispatch, test.deltas)
			}
		})
	}
}

func TestHostMacroAckTimelineStopsOnFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"first-error", "cancel-after-first", "cancel-leading-wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clock := &macroTestClock{current: time.Unix(100, 0)}
			compiled := compiledMacro{steps: []compiledMacroStep{{dueUS: 200000}, {dueUS: 300000}}}
			calls, evidence := 0, 0
			wait := clock.wait
			if mode == "cancel-leading-wait" {
				wait = func(context.Context, time.Time) error { cancel(); return nil }
			}
			count, err := runHostMacroWithClock(ctx, compiled, func(context.Context, byte, []byte) error {
				calls++
				clock.current = clock.current.Add(500 * time.Millisecond)
				if mode == "first-error" {
					return errors.New("ACK failed")
				}
				cancel()
				return nil
			}, func(index int, delta int32, succeeded bool) {
				evidence++
				if index != 0 || delta != 500000 || succeeded == (mode == "first-error") {
					t.Fatalf("index=%d delta=%d success=%t", index, delta, succeeded)
				}
			}, clock.now, wait)
			wantCalls, wantCount := 1, 0
			if mode == "cancel-after-first" {
				wantCount = 1
			}
			if mode == "cancel-leading-wait" {
				wantCalls = 0
			}
			if err == nil || calls != wantCalls || evidence != wantCalls || count != wantCount {
				t.Fatalf("mode=%s calls=%d count=%d evidence=%d err=%v", mode, calls, count, evidence, err)
			}
		})
	}
}

func TestHostMacroStartupDelayRemainsVisibleAndUnfaithful(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	// Keep presentation in a latest-only buffer without starting a worker.
	runner.presentOnce.Do(func() { runner.present = make(chan MacroState, 1) })
	runner.state = MacroState{ID: 1, Name: "timing", Mode: "host", Running: true, StepCount: 3, TimingToleranceUS: defaultHostMacroToleranceUS}
	done := make(chan struct{})
	runner.done = done
	for index, delta := range []int32{539722, 4658, 17094} {
		runner.recordEvidence(index, delta, true)
	}
	runner.finishHostPlayback(done, appconfig.Macro{Steps: make([]appconfig.MacroStep, 3)}, 3, false, nil)
	state := runner.State()
	if state.StartupDelayUS != 539722 || state.MaximumTimingErrorUS != 539722 || state.TimingViolations != 1 || state.Faithful || state.LastTimingDeltaUS != 17094 {
		t.Fatalf("startup violation hidden: %+v", state)
	}
	text, err := macroCommand(context.Background(), runner, []string{"status"})
	if err != nil || !strings.Contains(text, "startup_delay_us=539722") {
		t.Fatalf("CLI startup evidence missing: %q %v", text, err)
	}
	runner.recordEvidence(1, 150000, true)
	if runner.State().TimingViolations != 2 {
		t.Fatal("later 100ms violation was weakened")
	}
}
