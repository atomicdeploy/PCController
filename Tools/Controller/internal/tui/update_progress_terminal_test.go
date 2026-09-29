package tui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestUpdateProgressBarOnlyWhileActive(t *testing.T) {
	for _, state := range []string{"idle", "downloaded", "staged", "completed", "failed"} {
		model := Model{}
		model.update.State = state
		model.update.Progress = 100
		view := strings.Join(model.updateProgressLines(), "\n")
		if strings.Contains(view, "%") || strings.Contains(view, "━") {
			t.Fatalf("terminal %s shows progress: %s", state, view)
		}
	}
	for _, value := range []int{-10, 0, 130} {
		model := Model{}
		model.update.State = "programming"
		model.update.Progress = value
		model.update.ProgressKnown = true
		if view := strings.Join(model.updateProgressLines(), "\n"); !strings.Contains(view, "%") {
			t.Fatalf("active update missing progress: %s", view)
		}
	}
}

func TestProgrammingViewWrapsAndScrollsFailureDetails(t *testing.T) {
	model := readyModel(t, PageProgramming)
	model.width, model.height = 54, 20
	model.update = updatePresentation{State: "failed", Stage: "flash write", ErrorCode: "write-failed", Detail: strings.Repeat("Check the board USB connection. ", 20) + "END OF ERROR"}
	content := model.programmingContent(model.snapshot())
	if !strings.Contains(strings.Join(content, "\n"), "END OF ERROR") {
		t.Fatal("full error detail was discarded")
	}
	for _, line := range model.updateProgressLines() {
		if ansi.StringWidth(line) > model.width {
			t.Fatalf("panel exceeded terminal width: %q", line)
		}
	}
	model.update.Scroll = len(content)
	if view := model.programmingPage(model.snapshot()); !strings.Contains(view, "Scroll") {
		t.Fatalf("long view has no scroll affordance: %s", view)
	}
}

func TestUnknownStageAndFailedResultNeverRetainMeasuredPercentage(t *testing.T) {
	model := Model{width: 64}
	model.update = updatePresentation{State: "programming", Stage: "preflight", Progress: 40}
	if view := strings.Join(model.updateProgressLines(), "\n"); strings.Contains(view, "%") {
		t.Fatalf("unknown stage has fabricated progress: %s", view)
	}
	model.update = updatePresentation{State: "failed", Stage: "reconnecting", Progress: 40, ProgressKnown: true, Detail: "The application handshake timed out. Check the USB connection and retry.", ErrorCode: "handshake-timeout"}
	view := strings.Join(model.updateProgressLines(), "\n")
	if strings.Contains(view, "%") || !strings.Contains(view, "FAILED AT RECONNECTING") || !strings.Contains(view, "handshake-timeout") {
		t.Fatalf("failed result lost stage or retained busy: %s", view)
	}
}
