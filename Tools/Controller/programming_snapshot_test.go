package controller

import (
	"encoding/json"
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestProgrammingSnapshotAndEventHaveTheSameTypedContract(t *testing.T) {
	state := control.ProgrammingSnapshot{Active: true, LatchKnown: true, ProgrammingLatch: true, IntentionalUARTRelease: true, RestorationPending: true, Revision: 4}
	event := publicEvent(control.Event{Kind: "programming.lifecycle", Programming: &state})
	for _, value := range []any{Snapshot{Programming: state}, event} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatal(err)
		}
		var decoded control.ProgrammingSnapshot
		if err := json.Unmarshal(parsed["programming"], &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != state {
			t.Fatal("programming contract was lost between runtime and public wire")
		}
	}
}
