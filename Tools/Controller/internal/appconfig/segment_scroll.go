package appconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SegmentScroll configures host-supplied long text on selected built-in pages.
// The firmware owns timing-critical rendering and all higher-priority overlays.
type SegmentScroll struct {
	Enabled         bool     `json:"enabled"`
	Pages           []string `json:"pages"`
	DoorOpenText    string   `json:"door_open_text"`
	DoorClosedText  string   `json:"door_closed_text"`
	SpeedMS         int      `json:"speed_ms"`
	GapCells        int      `json:"gap_cells"`
	Repeat          string   `json:"repeat"`
	IntervalSeconds int      `json:"interval_seconds"`
}

type segmentScrollJSON SegmentScroll

func canonicalSegmentScroll(value SegmentScroll) SegmentScroll {
	if value.Pages == nil {
		return value
	}
	pages := make([]string, len(value.Pages))
	for index, page := range value.Pages {
		pages[index] = strings.ToLower(strings.TrimSpace(page))
	}
	value.Pages = pages
	value.Repeat = strings.ToLower(strings.TrimSpace(value.Repeat))
	return value
}

// UnmarshalJSON preserves defaults for omitted fields while canonicalizing page
// references at every external configuration boundary, including JSON-RPC.
func (value *SegmentScroll) UnmarshalJSON(data []byte) error {
	decoded := segmentScrollJSON(*value)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*value = canonicalSegmentScroll(SegmentScroll(decoded))
	return nil
}

// MarshalJSON guarantees persisted and returned settings use the same canonical
// page representation even when an in-process caller constructed the value.
func (value SegmentScroll) MarshalJSON() ([]byte, error) {
	canonical := segmentScrollJSON(canonicalSegmentScroll(value))
	return json.Marshal(canonical)
}

// DefaultSegmentScroll enables the Door-page presentation requested for the
// connected host while retaining the firmware's local OPEN/CLSD fallback.
func DefaultSegmentScroll() SegmentScroll {
	return SegmentScroll{
		Enabled: true, Pages: []string{"door"},
		DoorOpenText: "door is open", DoorClosedText: "door is closed",
		SpeedMS: 220, GapCells: 0, Repeat: "interval", IntervalSeconds: 30,
	}
}

func validateSegmentScroll(value SegmentScroll) error {
	value = canonicalSegmentScroll(value)
	if value.SpeedMS < 80 || value.SpeedMS > 5000 {
		return fmt.Errorf("ui.segment_scroll.speed_ms must be 80..5000")
	}
	if value.GapCells < 0 || value.GapCells > 12 {
		return fmt.Errorf("ui.segment_scroll.gap_cells must be 0..12")
	}
	if value.Repeat != "once" && value.Repeat != "loop" && value.Repeat != "interval" {
		return fmt.Errorf("ui.segment_scroll.repeat must be once, loop, or interval")
	}
	if value.Repeat == "interval" &&
		(value.IntervalSeconds < 1 || value.IntervalSeconds > 255) {
		return fmt.Errorf("ui.segment_scroll.interval_seconds must be 1..255")
	}
	if len(value.Pages) > 14 {
		return fmt.Errorf("ui.segment_scroll.pages must contain at most 14 entries")
	}
	seen := make(map[string]bool, len(value.Pages))
	for index, page := range value.Pages {
		if page == "" || seen[page] || !printableASCII(page) {
			return fmt.Errorf("ui.segment_scroll.pages[%d] is empty, duplicated, or invalid", index)
		}
		seen[page] = true
	}
	for name, text := range map[string]string{
		"door_open_text":   value.DoorOpenText,
		"door_closed_text": value.DoorClosedText,
	} {
		if len(text) < 5 || len(text)+value.GapCells > 40 || !printableASCII(text) {
			return fmt.Errorf("ui.segment_scroll.%s plus gap must be 5..40 printable ASCII bytes", name)
		}
	}
	return nil
}
