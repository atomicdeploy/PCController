package boardsensors

import (
	"math"
	"strings"
	"testing"
	"time"

	controller "pccontroller.local/controller"
)

func boardFixture(now time.Time) controller.Snapshot {
	return controller.Snapshot{Connected: true, HaveStatus: true, StatusUpdated: now.Add(-time.Second), ConnectionGeneration: 7,
		Status: controller.Status{SupplyMV: 12000, BusMV: 11950, CurrentMA: -25, PowerMW: -300, TLEDCenti: -125, TBTCenti: 0, INA219Available: true, TLEDAvailable: true, TBTAvailable: true}}
}

func TestBoardSensorUnitsAndRealZero(t *testing.T) {
	now := time.Now()
	s := FromBoard(boardFixture(now), "host", now)
	want := []float64{12, 11.95, -.025, -.3, -1.25, 0}
	for i, r := range s.Readings {
		if r.Value == nil || *r.Value != want[i] {
			t.Fatalf("%s: %v", r.ID, r.Value)
		}
	}
	frame, err := s.HWMonFrame(now)
	if err != nil || frame != "1 4000 63 12000 11950 -25 -300000 -1250 0\n" {
		t.Fatal(frame, err)
	}
	var out strings.Builder
	s.Prometheus(&out)
	for _, text := range []string{"pccontroller_board_current_amperes -0.025", "pccontroller_board_temperature_bt_audio_celsius 0", "pccontroller_board_fresh 1"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal(out.String())
		}
	}
}

func TestBoardSensorMissingStaleAndInvalidNeverBecomeZero(t *testing.T) {
	now := time.Now()
	for _, condition := range []string{"disconnected", "no-status", "stale", "future", "programming", "invalid", "absent"} {
		t.Run(condition, func(t *testing.T) {
			b := boardFixture(now)
			switch condition {
			case "disconnected":
				b.Connected = false
			case "no-status":
				b.HaveStatus = false
			case "stale":
				b.StatusUpdated = now.Add(-MaxAge)
			case "future":
				b.StatusUpdated = now.Add(time.Second)
			case "programming":
				b.Programming.Active = true
			case "invalid":
				b.Status.SupplyMV = 100001
				b.Status.TLEDCenti = 12501
				b.Status.TBTCenti = -5501
			case "absent":
				b.Status.INA219Available = false
				b.Status.TLEDAvailable = false
				b.Status.TBTAvailable = false
			}
			s := FromBoard(b, "host", now)
			for _, r := range s.Readings {
				if r.Value != nil {
					t.Fatalf("unavailable %s became %v", r.ID, *r.Value)
				}
			}
			var out strings.Builder
			s.Prometheus(&out)
			if strings.Contains(out.String(), "pccontroller_board_supply_volts ") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestBoardSensorHWMonExpiresWithoutRenewingOldSamples(t *testing.T) {
	now := time.Now()
	s := FromBoard(boardFixture(now), "host", now)
	frame, err := s.HWMonFrame(now.Add(2 * time.Second))
	if err != nil || !strings.HasPrefix(frame, "1 2000 63 ") {
		t.Fatal(frame, err)
	}
	frame, err = s.HWMonFrame(now.Add(4 * time.Second))
	if err != nil || frame != "1 0 0 0 0 0 0 0 0\n" {
		t.Fatal(frame, err)
	}
	s.ExpiresAt = now.Add(MaxAge)
	frame, _ = s.HWMonFrame(now)
	if frame != "1 0 0 0 0 0 0 0 0\n" {
		t.Fatal("source age was renewed", frame)
	}
}

func TestBoardSensorHWMonRejectsMalformedReadings(t *testing.T) {
	now := time.Now()
	for _, condition := range []string{"duplicate", "units", "nan", "infinity", "range"} {
		t.Run(condition, func(t *testing.T) {
			s := FromBoard(boardFixture(now), "host", now)
			switch condition {
			case "duplicate":
				s.Readings = append(s.Readings, s.Readings[0])
			case "units":
				s.Readings[0].Unit = "mV"
			case "nan":
				*s.Readings[0].Value = math.NaN()
			case "infinity":
				*s.Readings[0].Value = math.Inf(1)
			case "range":
				*s.Readings[0].Value = -1
			}
			if _, err := s.HWMonFrame(now); err == nil {
				t.Fatal("accepted malformed sensor data")
			}
		})
	}
}
