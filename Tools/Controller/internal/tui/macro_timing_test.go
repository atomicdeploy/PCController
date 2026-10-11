package tui

import (
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

func TestMacroCatalogueDurationIncludesWholeEffectRepeatAndTail(t *testing.T) {
	count := uint16(3)
	macro := appconfig.Macro{RepeatCount: &count, RepeatIntervalMS: 200, Steps: []appconfig.MacroStep{{Kind: "beep", AtUS: 50_000, DurationMS: 100}}}
	if got := macroDefinitionDuration(macro); got != 550*time.Millisecond {
		t.Fatalf("whole span %v, want 550ms", got)
	}
	count = 0
	if got := formatMacroDuration(macroDefinitionDuration(macro)); got != "invalid" {
		t.Fatalf("invalid repeat invented duration %q", got)
	}
}

func TestMacroTimingSummaryKeepsStartupAndMaximumVisible(t *testing.T) {
	state := control.MacroState{Mode: "host", StartupDelayUS: 539722, MaximumTimingErrorUS: 539722, LastTimingDeltaUS: 17094, TimingToleranceUS: 100000, TimingViolations: 1}
	text := macroTimingSummary(state)
	for _, want := range []string{"startup " + formatMicros(539722), "max " + formatMicros(539722), "violations 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	state.Mode = "mcu"
	if text := macroTimingSummary(state); strings.Contains(text, "startup") {
		t.Fatalf("host startup delay leaked into MCU timing: %s", text)
	}
}
