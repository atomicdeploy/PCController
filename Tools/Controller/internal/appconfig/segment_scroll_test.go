package appconfig

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDefaultSegmentScrollIsValidAndDoorFocused(t *testing.T) {
	value := DefaultSegmentScroll()
	if err := validateSegmentScroll(value); err != nil {
		t.Fatal(err)
	}
	if !value.Enabled || len(value.Pages) != 1 || value.Pages[0] != "door" ||
		value.SpeedMS != 220 || value.GapCells != 0 || value.Repeat != "interval" ||
		value.IntervalSeconds != 30 {
		t.Fatalf("defaults=%+v", value)
	}
}

func TestSegmentScrollRejectsDuplicatePagesAndOversizedTextWithGap(t *testing.T) {
	duplicate := DefaultSegmentScroll()
	duplicate.Pages = []string{"door", "DOOR"}
	if err := validateSegmentScroll(duplicate); err == nil {
		t.Fatal("duplicate page was accepted")
	}
	overflow := DefaultSegmentScroll()
	overflow.DoorOpenText = "1234567890123456789012345678901234567890"
	overflow.GapCells = 1
	if err := validateSegmentScroll(overflow); err == nil {
		t.Fatal("text plus gap beyond native payload was accepted")
	}
}

func TestSegmentScrollJSONCanonicalizesPersistedPageReferences(t *testing.T) {
	value := DefaultSegmentScroll()
	if err := json.Unmarshal([]byte(`{"pages":[" Door ","STATUS"]}`), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Pages) != 2 || value.Pages[0] != "door" || value.Pages[1] != "status" {
		t.Fatalf("canonical pages=%q", value.Pages)
	}
	if value.SpeedMS != 220 || value.DoorOpenText != "door is open" {
		t.Fatalf("partial JSON discarded defaults: %+v", value)
	}
	if err := validateSegmentScroll(value); err != nil {
		t.Fatal(err)
	}

	value.Pages = []string{" Door ", "STATUS"}
	persisted, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(persisted, []byte(`"pages":["door","status"]`)) {
		t.Fatalf("persisted JSON was not canonical: %s", persisted)
	}
}

func TestSegmentScrollJSONCanonicalizationStillRejectsDuplicates(t *testing.T) {
	value := DefaultSegmentScroll()
	if err := json.Unmarshal([]byte(`{"pages":[" Door ","door"]}`), &value); err != nil {
		t.Fatal(err)
	}
	if err := validateSegmentScroll(value); err == nil {
		t.Fatal("canonical duplicate page was accepted")
	}
}
