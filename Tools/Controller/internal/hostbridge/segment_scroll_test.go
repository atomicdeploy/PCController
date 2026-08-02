package hostbridge

import (
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func authenticatedDoorSnapshot(open bool) controller.Snapshot {
	return controller.Snapshot{
		Connected:  true,
		HaveStatus: true,
		Hello:      controller.Hello{BoardKind: 1, Name: "PCController"},
		Status:     controller.Status{MenuPage: 0, DoorOpen: open},
	}
}

func TestSegmentScrollTargetTracksDoorStateAndConfiguredGap(t *testing.T) {
	config := appconfig.DefaultSegmentScroll()
	open := segmentScrollTargetFor(config, authenticatedDoorSnapshot(true))
	if !open.active || open.page != 0 || open.text != "door is open   " ||
		open.dwell != 220*time.Millisecond {
		t.Fatalf("open target=%+v", open)
	}
	closed := segmentScrollTargetFor(config, authenticatedDoorSnapshot(false))
	if !closed.active || closed.text != "door is closed   " {
		t.Fatalf("closed target=%+v", closed)
	}
}

func TestSegmentScrollStopsOnPageChangeDisconnectOrIdentityFailure(t *testing.T) {
	config := appconfig.DefaultSegmentScroll()
	for name, mutate := range map[string]func(*controller.Snapshot){
		"page":       func(value *controller.Snapshot) { value.Status.MenuPage = 1 },
		"disconnect": func(value *controller.Snapshot) { value.Connected = false },
		"status":     func(value *controller.Snapshot) { value.HaveStatus = false },
		"identity":   func(value *controller.Snapshot) { value.Hello.Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := authenticatedDoorSnapshot(true)
			mutate(&snapshot)
			if target := segmentScrollTargetFor(config, snapshot); target.active {
				t.Fatalf("target remained active: %+v", target)
			}
		})
	}
}

func TestSegmentScrollPageReferencesAcceptCanonicalNameLabelAndID(t *testing.T) {
	for _, reference := range []string{"door", "Door", "0", "0x0"} {
		if !segmentScrollPageEnabled([]string{reference}, 0) {
			t.Fatalf("reference %q did not select Door page", reference)
		}
	}
	if segmentScrollPageEnabled([]string{"voltage", "1"}, 0) {
		t.Fatal("non-Door references unexpectedly selected Door")
	}
}
