package appconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Pixel indices are zero based and ordered. The same pixel may intentionally
// belong to several segments; enabled layers are composited in array order.
type StripSegment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Pixels []int  `json:"pixels"`
}
type StripLayer struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	SegmentID   string     `json:"segment_id"`
	Enabled     bool       `json:"enabled"`
	Primitive   string     `json:"primitive"`
	Primary     StripColor `json:"primary"`
	Secondary   StripColor `json:"secondary"`
	PeriodMS    int        `json:"period_ms"`
	PhaseMS     int        `json:"phase_ms"`
	DutyPercent int        `json:"duty_percent"`
	Easing      string     `json:"easing"`
	Reverse     bool       `json:"reverse"`
}

// The layered-program contract has no implicit enabled/disabled or color
// defaults. Reject incomplete imported JSON instead of silently changing it.
func (layer *StripLayer) UnmarshalJSON(data []byte) error {
	type wireLayer StripLayer
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"id", "name", "segment_id", "enabled", "primitive", "primary", "secondary", "period_ms", "phase_ms", "duty_percent", "easing", "reverse"} {
		value, present := fields[key]
		if !present || string(value) == "null" {
			return fmt.Errorf("strip layer requires %s", key)
		}
	}
	for _, key := range []string{"primary", "secondary"} {
		var color map[string]json.RawMessage
		if err := json.Unmarshal(fields[key], &color); err != nil {
			return err
		}
		for _, channel := range []string{"red", "green", "blue"} {
			if value, ok := color[channel]; !ok || string(value) == "null" {
				return fmt.Errorf("strip layer %s requires %s", key, channel)
			}
		}
	}
	var value wireLayer
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*layer = StripLayer(value)
	return nil
}

func ValidateStripLayers(p StripProgram, count int) error {
	if count < 1 || count > 100 {
		return fmt.Errorf("pixel count must be 1..100")
	}
	if p.PixelsPerMeter < 1 || p.PixelsPerMeter > 1000 {
		return fmt.Errorf("pixels_per_meter must be 1..1000")
	}
	if len(p.Segments) < 1 || len(p.Segments) > 64 || len(p.Layers) < 1 || len(p.Layers) > 64 {
		return fmt.Errorf("use 1..64 segments and layers")
	}
	nameOK := func(s string) bool {
		return strings.TrimSpace(s) != "" && len([]rune(s)) <= 64 && strings.IndexFunc(s, unicode.IsControl) < 0
	}
	segments := map[string]bool{}
	for _, s := range p.Segments {
		if !profileToken(s.ID) || len(s.ID) > 64 || s.ID == "" || segments[s.ID] || !nameOK(s.Name) {
			return fmt.Errorf("segments require unique stable IDs and names")
		}
		segments[s.ID] = true
		if len(s.Pixels) < 1 || len(s.Pixels) > count {
			return fmt.Errorf("segment %s must select pixels", s.ID)
		}
		seen := map[int]bool{}
		for _, pixel := range s.Pixels {
			if pixel < 0 || pixel >= count || seen[pixel] {
				return fmt.Errorf("segment %s has a duplicate or out-of-range pixel", s.ID)
			}
			seen[pixel] = true
		}
	}
	layers := map[string]bool{}
	for _, l := range p.Layers {
		if l.ID == "" || len(l.ID) > 64 || !profileToken(l.ID) || layers[l.ID] || !nameOK(l.Name) || !segments[l.SegmentID] {
			return fmt.Errorf("layer requires a unique ID, name and existing segment")
		}
		layers[l.ID] = true
		if l.PeriodMS < 50 || l.PeriodMS > 3_600_000 || l.PhaseMS < 0 || l.PhaseMS >= l.PeriodMS || l.DutyPercent < 0 || l.DutyPercent > 100 {
			return fmt.Errorf("invalid layer cycle, phase or duty")
		}
		switch l.Primitive {
		case "solid", "gradient", "ramp", "blink", "breathe":
		default:
			return fmt.Errorf("unsupported layer primitive %q", l.Primitive)
		}
		switch l.Easing {
		case "linear", "ease-in", "ease-out", "ease-in-out":
		default:
			return fmt.Errorf("unsupported layer easing %q", l.Easing)
		}
	}
	return nil
}
