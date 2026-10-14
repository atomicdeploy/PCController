package control

import (
	"testing"
	"time"
)

func TestTimelineSmallPageAvoidsCloningUnrequestedHistory(t *testing.T) {
	runtime := &Runtime{timelineLimit: 10000}
	for i := 0; i < 10000; i++ {
		runtime.timeline = append(runtime.timeline, TimelineEntry{ID: uint64(i + 1), Time: time.Unix(int64(i), 0), Metadata: map[string]string{"example": "value"}})
	}
	allocations := testing.AllocsPerRun(10, func() {
		page := runtime.Timeline(time.Time{}, 1)
		if len(page) != 1 || page[0].ID != 10000 {
			t.Fatal("wrong last page")
		}
	})
	if allocations > 20 {
		t.Fatalf("small page cloned unrequested metadata: %.0f allocations", allocations)
	}
	page := runtime.Timeline(time.Unix(9997, 0), 2)
	if len(page) != 2 || page[0].ID != 9999 || page[1].ID != 10000 {
		t.Fatal("paging/filter order changed")
	}
	page[0].Metadata["example"] = "changed"
	if runtime.timeline[9998].Metadata["example"] != "value" {
		t.Fatal("page metadata aliases retained event")
	}
}
