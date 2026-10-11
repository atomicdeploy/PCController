package control

import (
	"pccontroller.local/controller/internal/appconfig"
	"reflect"
	"testing"
)

func TestStepRepeatsComposeWithWholeEffectAndPreserveStableReleases(t *testing.T) {
	count, whole := uint16(3), uint16(2)
	macro := appconfig.Macro{RepeatCount: &whole, Steps: []appconfig.MacroStep{
		{Kind: "motion", Value: 1, DurationMS: 200, RepeatCount: &count, RepeatIntervalMS: 200, ActionIDs: []string{"seat.a.up"}},
		{Kind: "motion", AtUS: 200000, Value: 2, DurationMS: 200, ActionIDs: []string{"seat.a.down"}},
	}}
	_, interval, total, err := macro.Repetition()
	if err != nil || interval != 600000 || total != 1200000 {
		t.Fatalf("plan %d/%d %v", interval, total, err)
	}
	points, err := expandEffectTimeline(macro)
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]uint32
	for _, p := range points {
		got = append(got, [2]uint32{p.AtUS, uint32(p.Value)})
		if p.RepeatCount != nil || p.RepeatIntervalMS != 0 || p.DurationMS != 0 {
			t.Fatal("runtime retained authoring metadata")
		}
	}
	want := [][2]uint32{{0, 1}, {200000, 0}, {200000, 1}, {200000, 2}, {400000, 0}, {400000, 1}, {400000, 0}, {600000, 0},
		{600000, 1}, {800000, 0}, {800000, 1}, {800000, 2}, {1000000, 0}, {1000000, 1}, {1000000, 0}, {1200000, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order\ngot %v\nwant %v", got, want)
	}
	if macro.Steps[0].RepeatCount == nil || *macro.Steps[0].RepeatCount != 3 {
		t.Fatal("source definition changed")
	}
	for _, mode := range []string{macroModeHost, macroModeMCU} {
		macro.Mode = mode
		compiled, compileErr := compileMacro(macro)
		if compileErr != nil || len(compiled.steps) != len(want) {
			t.Fatalf("%s: %v", mode, compileErr)
		}
		for i, point := range compiled.steps {
			if point.dueUS != want[i][0] {
				t.Fatalf("%s offset %d", mode, i)
			}
		}
	}
}

func TestStepRepeatAutoInstantOverlapAndBounds(t *testing.T) {
	count := uint16(3)
	step := appconfig.MacroStep{Kind: "relay-mask", AtUS: 500, RepeatCount: &count}
	_, interval, span, err := step.Repetition()
	if err != nil || interval != 1000 || span != 2000 {
		t.Fatalf("instant %d %d %v", interval, span, err)
	}
	step.DurationMS, step.RepeatIntervalMS = 200, 100
	if _, _, span, err = step.Repetition(); err != nil || span != 400000 {
		t.Fatalf("overlap %d %v", span, err)
	}
	for _, invalid := range []uint16{0, 1001} {
		step.RepeatCount = &invalid
		if _, _, _, err = step.Repetition(); err == nil {
			t.Fatal("invalid count accepted")
		}
	}
	step.RepeatCount, step.RepeatIntervalMS = &count, 3600000
	if _, _, _, err = step.Repetition(); err == nil {
		t.Fatal("overflow accepted")
	}
	count = 1000
	macro := appconfig.Macro{RepeatCount: &count, Steps: []appconfig.MacroStep{{Kind: "relay-mask", RepeatCount: &count}}}
	if _, err = expandEffectTimeline(macro); err == nil {
		t.Fatal("unbounded product accepted")
	}
}
