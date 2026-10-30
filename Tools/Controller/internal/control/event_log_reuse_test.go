package control

import "testing"

func TestEventLogsRetainOrderWithoutReplacingBoundedStorage(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	runtime.eventLog = make([]Event, 512, 513)
	runtime.activityLog = make([]Event, 512, 513)
	runtime.rfActivityLog = make([]Event, 20, 21)
	for i := range runtime.eventLog {
		runtime.eventLog[i].ID = uint64(i + 1)
		runtime.activityLog[i].ID = uint64(i + 1)
	}
	for i := range runtime.rfActivityLog {
		runtime.rfActivityLog[i].ID = uint64(i + 1)
	}
	eventBase, activityBase, rfBase := &runtime.eventLog[0], &runtime.activityLog[0], &runtime.rfActivityLog[0]
	runtime.nextEventID = 512
	event := runtime.publishEvent(Event{Kind: "rf.fixture", Stream: EventStreamActivity})
	if &runtime.eventLog[0] != eventBase || &runtime.activityLog[0] != activityBase || &runtime.rfActivityLog[0] != rfBase {
		t.Fatal("bounded event publication replaced backing storage")
	}
	for _, entries := range [][]Event{runtime.eventLog, runtime.activityLog, runtime.rfActivityLog} {
		if entries[0].ID != 2 || entries[len(entries)-1].ID != event.ID {
			t.Fatal("trim changed event order")
		}
		if entries[:cap(entries)][len(entries)].ID != 0 {
			t.Fatal("trim retained discarded tail references")
		}
	}
}
