package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

const (
	macroPanelCommandTimeout = 750 * time.Millisecond
	macroPanelActiveHold     = 1500 * time.Millisecond
	macroPanelTerminalHold   = 2500 * time.Millisecond
)

type macroPanelPresentation struct {
	segments string
	line1    string
	line2    string
	hold     time.Duration
}

// queueMacroPresentation is latest-only: a burst of sub-second macro steps
// cannot create a backlog of decorative serial traffic or delay queue refills.
// The MCU event timestamps remain authoritative; presentation is best-effort.
func (runner *MacroRunner) queueMacroPresentation(state MacroState) {
	if runner == nil || runner.runtime == nil {
		return
	}
	runner.presentOnce.Do(func() {
		runner.present = make(chan MacroState, 1)
		go runner.runMacroPresentation()
	})
	for {
		select {
		case runner.present <- state:
			return
		default:
			select {
			case <-runner.present:
			default:
			}
		}
	}
}

func (runner *MacroRunner) runMacroPresentation() {
	for state := range runner.present {
		panel := formatMacroPanel(state, time.Now())
		if panel.segments == "" || !runner.runtime.Snapshot().Connected {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), macroPanelCommandTimeout)
		durationMS := uint16(panel.hold / time.Millisecond)
		payload, err := native.DisplayTextPayload(native.DisplaySegments, durationMS, panel.segments)
		if err == nil {
			err = runner.runtime.Command(ctx, native.OpDisplayText, payload)
		}
		cancel()
		if err != nil {
			runner.runtime.PublishHostEvent("macro.display", "macro progress display unavailable: "+err.Error())
			continue
		}
		// LCDPresenter mirrors through firmware and the optional HOST-owned
		// physical LCD in one serialized path. It is intentionally invoked by
		// this latest-only worker rather than the macro timing/event loop.
		runner.runtime.LCDPresenter().ShowPriority(
			"macro", panel.line1, panel.line2, panel.hold,
		)
	}
}

func formatMacroPanel(state MacroState, now time.Time) macroPanelPresentation {
	hold := macroPanelActiveHold
	segments := macroStepSegments(state.Step, state.StepCount)
	line1 := fitMacroASCII(fmt.Sprintf("#%d %s", state.ID, state.Name), 16)
	elapsed := time.Duration(0)
	if !state.StartedAt.IsZero() {
		end := now
		if !state.Running && !state.FinishedAt.IsZero() {
			end = state.FinishedAt
		}
		if end.After(state.StartedAt) {
			elapsed = end.Sub(state.StartedAt)
		}
	}
	duration := time.Duration(state.DurationUS) * time.Microsecond
	if duration > 0 && elapsed > duration {
		elapsed = duration
	}
	line2 := fitMacroASCII(fmt.Sprintf(
		"%d/%d %s/%s", state.Step, state.StepCount,
		compactMacroDuration(elapsed), compactMacroDuration(duration),
	), 16)
	if !state.Running {
		hold = macroPanelTerminalHold
		switch strings.ToLower(strings.TrimSpace(state.Lifecycle)) {
		case "completed":
			segments = "done"
			line2 = fitMacroASCII("Complete "+line2, 16)
		case "cancelled":
			segments = "StoP"
			line2 = fitMacroASCII("Cancelled "+line2, 16)
		case "failed":
			segments = "Err"
			line2 = fitMacroASCII("Failed "+line2, 16)
		default:
			segments = "run"
		}
	}
	return macroPanelPresentation{segments: segments, line1: line1, line2: line2, hold: hold}
}

func macroStepSegments(step, total int) string {
	if step <= 0 {
		return "run"
	}
	if step < 10 && total < 10 {
		return fmt.Sprintf("%d-%d", step, total)
	}
	if step < 100 && total < 100 {
		return fmt.Sprintf("%02d%02d", step, total)
	}
	percent := 100
	if total > 0 && step < total {
		percent = step * 100 / total
	}
	return fmt.Sprintf("P%02d", percent)
}

func compactMacroDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	if value < 10*time.Second {
		return strconv.FormatFloat(value.Seconds(), 'f', 1, 64)
	}
	seconds := int(value.Round(time.Second) / time.Second)
	if seconds < 60 {
		return strconv.Itoa(seconds)
	}
	minutes := seconds / 60
	seconds %= 60
	if minutes < 100 {
		return fmt.Sprintf("%d:%02d", minutes, seconds)
	}
	return ">99m"
}

func fitMacroASCII(value string, width int) string {
	var result strings.Builder
	for _, character := range value {
		if result.Len() >= width {
			break
		}
		if character < 0x20 || character > 0x7E {
			character = '?'
		}
		result.WriteRune(character)
	}
	return result.String()
}
