// Package boardsensors exports only observed PCB telemetry, never PC host facts.
package boardsensors

import (
	"fmt"
	"io"
	"strconv"
	"time"

	controller "pccontroller.local/controller"
)

const MaxAge = 5 * time.Second

type Reading struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Unit  string   `json:"unit"`
	Value *float64 `json:"value"`
}

type Snapshot struct {
	Source               string    `json:"source"`
	HostInstanceID       string    `json:"host_instance_id"`
	ConnectionGeneration uint64    `json:"connection_generation"`
	Connected            bool      `json:"connected"`
	Fresh                bool      `json:"fresh"`
	ObservedAt           time.Time `json:"observed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	Readings             []Reading `json:"readings"`
}

func FromBoard(s controller.Snapshot, host string, now time.Time) Snapshot {
	fresh := s.Connected && s.HaveStatus && !s.Programming.Active && !s.StatusUpdated.IsZero() &&
		!s.StatusUpdated.After(now) && now.Sub(s.StatusUpdated) < MaxAge
	r := Snapshot{Source: "pccontroller-board-status", HostInstanceID: host, ConnectionGeneration: s.ConnectionGeneration,
		Connected: s.Connected, Fresh: fresh, ObservedAt: s.StatusUpdated, ExpiresAt: s.StatusUpdated.Add(MaxAge)}
	if s.StatusUpdated.IsZero() {
		r.ExpiresAt = time.Time{}
	}
	add := func(id, label, kind, unit string, value float64, valid bool) {
		v := Reading{ID: id, Label: label, Kind: kind, Unit: unit}
		if fresh && valid {
			v.Value = &value
		}
		r.Readings = append(r.Readings, v)
	}
	st := s.Status
	ina := st.INA219Available && st.INA219ValuesValid()
	add("supply", "PCB supply", "voltage", "V", float64(st.SupplyMV)/1000, ina)
	add("bus", "INA219 bus", "voltage", "V", float64(st.BusMV)/1000, ina)
	add("current", "PCB current", "current", "A", float64(st.CurrentMA)/1000, ina)
	add("power", "PCB power", "power", "W", float64(st.PowerMW)/1000, ina)
	add("temperature_led", "Lighting DS18B20", "temperature", "C", float64(st.TLEDCenti)/100, st.TLEDAvailable && st.TLEDValueValid())
	add("temperature_bt_audio", "Audio DS18B20", "temperature", "C", float64(st.TBTCenti)/100, st.TBTAvailable && st.TBTValueValid())
	return r
}

// Prometheus omits unavailable measurements, preserving real zero and negatives.
func (s Snapshot) Prometheus(w io.Writer) {
	boolean := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	fmt.Fprintln(w, "# HELP pccontroller_board_connected Controller transport connected.\n# TYPE pccontroller_board_connected gauge")
	fmt.Fprintf(w, "pccontroller_board_connected %d\n", boolean(s.Connected))
	fmt.Fprintln(w, "# HELP pccontroller_board_fresh Cached board status is less than five seconds old.\n# TYPE pccontroller_board_fresh gauge")
	fmt.Fprintf(w, "pccontroller_board_fresh %d\n", boolean(s.Fresh))
	if !s.ObservedAt.IsZero() {
		fmt.Fprintln(w, "# HELP pccontroller_board_observed_timestamp_seconds Host receipt time of board status, not scrape time.\n# TYPE pccontroller_board_observed_timestamp_seconds gauge")
		fmt.Fprintf(w, "pccontroller_board_observed_timestamp_seconds %.3f\n", float64(s.ObservedAt.UnixMilli())/1000)
	}
	for _, r := range s.Readings {
		fmt.Fprintf(w, "# HELP pccontroller_board_%s_available Valid fresh board measurement.\n# TYPE pccontroller_board_%s_available gauge\npccontroller_board_%s_available %d\n", r.ID, r.ID, r.ID, boolean(r.Value != nil))
		if r.Value == nil {
			continue
		}
		suffix := map[string]string{"V": "volts", "A": "amperes", "W": "watts", "C": "celsius"}[r.Unit]
		name := "pccontroller_board_" + r.ID + "_" + suffix
		fmt.Fprintf(w, "# HELP %s %s.\n# TYPE %s gauge\n%s %s\n", name, r.Label, name, name, strconv.FormatFloat(*r.Value, 'g', -1, 64))
	}
}
