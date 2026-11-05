package model

import (
	"encoding/json"
	"testing"
)

func TestOutputStateJSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, state := range []OutputState{StateUnknown, StateOff, StateOn} {
		state := state
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatalf("Marshal(%v): %v", state, err)
			}
			var decoded OutputState
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal(%s): %v", data, err)
			}
			if decoded != state {
				t.Fatalf("decoded state = %v, want %v", decoded, state)
			}
		})
	}
}

func TestOutputStateJSONRejectsInvalidValue(t *testing.T) {
	t.Parallel()
	var state OutputState
	if err := json.Unmarshal([]byte(`"maybe"`), &state); err == nil {
		t.Fatal("expected invalid state error")
	}
}

func TestOutputStateJSONNullMeansUnknown(t *testing.T) {
	t.Parallel()
	state := StateOn
	if err := json.Unmarshal([]byte("null"), &state); err != nil {
		t.Fatalf("Unmarshal(null): %v", err)
	}
	if state != StateUnknown {
		t.Fatalf("state = %v, want unknown", state)
	}
}
