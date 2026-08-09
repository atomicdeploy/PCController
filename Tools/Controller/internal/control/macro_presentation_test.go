package control

import (
	"strings"
	"testing"
	"time"
)

func TestMacroPanelPresentationCarriesIdentityStepElapsedAndDuration(t *testing.T) {
	started := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	panel := formatMacroPanel(MacroState{
		Running: true, ID: 7, Name: "Door demonstration",
		Step: 2, StepCount: 5, DurationUS: 4_000_000, StartedAt: started,
		Lifecycle: "playing",
	}, started.Add(1300*time.Millisecond))
	if panel.segments != "2-5" || panel.line1 != "#7 Door demonstr" {
		t.Fatalf("macro panel identity=%#v", panel)
	}
	for _, wanted := range []string{"2/5", "1.3", "4.0"} {
		if !strings.Contains(panel.line2, wanted) {
			t.Fatalf("macro panel line %q omits %q", panel.line2, wanted)
		}
	}
	if len(panel.line1) > 16 || len(panel.line2) > 16 || panel.hold != macroPanelActiveHold {
		t.Fatalf("macro panel bounds=%#v", panel)
	}
}

func TestMacroPanelPresentationUsesBoundedLargeStepAndTerminalStates(t *testing.T) {
	started := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	running := formatMacroPanel(MacroState{
		Running: true, ID: 12, Name: "Service", Step: 425, StepCount: 500,
		DurationUS: 180_000_000, StartedAt: started, Lifecycle: "playing",
	}, started.Add(65*time.Second))
	if running.segments != "P85" || len(running.line2) > 16 {
		t.Fatalf("large macro panel=%#v", running)
	}
	completed := formatMacroPanel(MacroState{
		ID: 12, Name: "Service", Step: 500, StepCount: 500,
		DurationUS: 180_000_000, StartedAt: started, FinishedAt: started.Add(180 * time.Second),
		Lifecycle: "completed",
	}, started.Add(5*time.Minute))
	if completed.segments != "done" || completed.hold != macroPanelTerminalHold ||
		!strings.HasPrefix(completed.line2, "Complete") {
		t.Fatalf("terminal macro panel=%#v", completed)
	}
}

func TestMacroPresentationFormattingIsPrintableASCII(t *testing.T) {
	panel := formatMacroPanel(MacroState{Running: true, ID: 1, Name: "در", StepCount: 1}, time.Now())
	for _, value := range []string{panel.segments, panel.line1, panel.line2} {
		for _, character := range value {
			if character < 0x20 || character > 0x7E {
				t.Fatalf("unrenderable macro panel character %q in %q", character, value)
			}
		}
	}
}
