package tui

import (
	"strings"
	"testing"

	"pccontroller.local/controller/internal/control"
)

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
