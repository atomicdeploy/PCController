package tui

import "testing"

func TestStripConfigurationPreservesHighFPS(t *testing.T) {
	for _, fps := range []int{60, 120} {
		model := Model{stripPixels: 3, stripFPS: fps}
		if _, got := model.stripConfiguration(); got != fps {
			t.Fatalf("FPS %d was reset to %d", fps, got)
		}
	}
}

func TestStripFPSAdjustmentCrossesThirtyAndWrapsAt120(t *testing.T) {
	for _, test := range []struct{ from, want int }{{30, 31}, {59, 60}, {119, 120}, {120, 1}} {
		model := readyModel(t, PageOutputs)
		model.previewMacroState.Running = false
		model.stripFPS = test.from
		found := false
		for index, row := range model.controlTableRows(model.snapshot(), 8) {
			if row.Adjust != "strip-fps" {
				continue
			}
			found = true
			model.cursor = index
			updated, _, _ := model.adjustSelection(1)
			if updated.stripFPS != test.want {
				t.Fatalf("adjust %d: got %d, want %d", test.from, updated.stripFPS, test.want)
			}
			break
		}
		if !found {
			t.Fatal("strip FPS control is missing")
		}
	}
}
