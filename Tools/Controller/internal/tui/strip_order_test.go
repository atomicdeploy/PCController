package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestStripOrderControlUsesPersistedState(t *testing.T) {
	model := readyModel(t, PageOutputs)
	model.uiValue.StripColorOrder = "GRB"
	found := false
	for _, row := range model.controlTableRows(model.snapshot(), 20) {
		if row.Name == "Pixel color order" {
			found = true
			if !strings.HasPrefix(row.Value, "GRB") || row.Action != "strip order next" || row.Adjust != "strip-order" {
				t.Fatalf("incorrect order row: %#v", row)
			}
		}
	}
	if !found {
		t.Fatal("missing color-order control")
	}
	model.consumeStructuredResult(commandResultMsg{line: "strip order RGB", output: `{"color_order":"RGB"}`})
	if model.uiValue.StripColorOrder != "RGB" {
		t.Fatal("did not accept saved order")
	}
	model.consumeStructuredResult(commandResultMsg{line: "strip order BGR", output: `{"color_order":"BGR"}`, err: errors.New("save failed")})
	if model.uiValue.StripColorOrder != "RGB" {
		t.Fatal("failed save changed visible order")
	}
}
