package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasurementTimingDefaultsAndCurrentBounds(t *testing.T) {
	value := Defaults()
	if value.UI.StatusIntervalMS != 200 ||
		value.UI.MeasurementFreshnessMS != DefaultMeasurementFreshnessMS ||
		value.UI.IdleStatusIntervalMS != 0 {
		t.Fatalf("measurement timing defaults=%d/%d idle=%d", value.UI.StatusIntervalMS, value.UI.MeasurementFreshnessMS, value.UI.IdleStatusIntervalMS)
	}

	valid := value
	valid.UI.StatusIntervalMS = StatusIntervalMaxMS
	valid.UI.MeasurementFreshnessMS = StatusIntervalMaxMS + MeasurementFreshnessHeadroomMS
	valid.UI.IdleStatusIntervalMS = 60_000
	if err := valid.Validate(); err != nil {
		t.Fatalf("current maximum intervals rejected: %v", err)
	}

	tests := []struct {
		name      string
		refreshMS int
		freshMS   int
		want      string
	}{
		{name: "refresh below current minimum", refreshMS: StatusIntervalMinMS - 1, freshMS: DefaultMeasurementFreshnessMS, want: "status_interval_ms"},
		{name: "refresh above current maximum", refreshMS: StatusIntervalMaxMS + 1, freshMS: MeasurementFreshnessMaxMS, want: "status_interval_ms"},
		{name: "freshness crosses before next sample", refreshMS: 5000, freshMS: 5099, want: "measurement_freshness_ms"},
		{name: "freshness above maximum", refreshMS: 200, freshMS: MeasurementFreshnessMaxMS + 1, want: "measurement_freshness_ms"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := value
			candidate.UI.StatusIntervalMS = test.refreshMS
			candidate.UI.MeasurementFreshnessMS = test.freshMS
			if err := candidate.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error=%v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadMigratesFreshnessForLegacySlowCadenceWithoutChangingIdleInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "connection": {"baud_rate":115200,"startup_wait_ms":1200,"request_timeout_ms":1200,"hello_attempts":3},
  "ui": {"status_interval_ms":5000,"idle_status_interval_ms":7000,"event_log_limit":500}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	value, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if value.UI.MeasurementFreshnessMS != 5100 || value.UI.IdleStatusIntervalMS != 7000 {
		t.Fatalf("migrated timing=%d/%d idle=%d", value.UI.StatusIntervalMS, value.UI.MeasurementFreshnessMS, value.UI.IdleStatusIntervalMS)
	}
}

func TestLoadRejectsExplicitInvalidFreshness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "ui": {"status_interval_ms":5000,"measurement_freshness_ms":1499}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "measurement_freshness_ms") {
		t.Fatalf("Load() error=%v, want explicit freshness validation error", err)
	}
}

func TestDefaultHostMenuUsesCurrentMeasurementTimingBounds(t *testing.T) {
	var timing = map[string]HostMenuItem{}
	for _, menu := range Defaults().HostMenus.Menus {
		if menu.ID != "pc-settings" {
			continue
		}
		for _, item := range menu.Items {
			if item.ID == "poll" || item.ID == "fresh" {
				timing[item.ID] = item
			}
		}
	}
	if timing["poll"].Min != StatusIntervalMinMS || timing["poll"].Max != StatusIntervalMaxMS ||
		timing["fresh"].Max != MeasurementFreshnessMaxMS {
		t.Fatalf("host menu timing=%#v", timing)
	}
}
