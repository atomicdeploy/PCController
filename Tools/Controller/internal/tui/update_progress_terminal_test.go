package tui

import (
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
		if view := strings.Join(model.updateProgressLines(), "\n"); !strings.Contains(view, "%") {
			t.Fatalf("active update missing progress: %s", view)
		}
	}
}
