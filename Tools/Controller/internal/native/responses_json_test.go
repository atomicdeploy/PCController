package native

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusJSONUsesStableProtocolNamesAndRetainsRawUptime(t *testing.T) {
	original := Status{
		UptimeMS:        4_392_210,
		SupplyMV:        12_224,
		TLEDCenti:       2_650,
		TBTCenti:        2_410,
		Flags:           StatusINA219Available | StatusTLEDAvailable | StatusTBTAvailable,
		INA219Available: true, TLEDAvailable: true, TBTAvailable: true,
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"uptime_ms":4392210`,
		`"uptime":"1h13m12.21s"`,
		`"supply_mv":12224`,
		`"temperature_led_centi_c":2650`,
		`"temperature_bt_audio_centi_c":2410`,
		`"ina219_available":true`,
		`"temperature_led_available":true`,
		`"temperature_bt_audio_available":true`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status JSON %s does not contain %s", text, want)
		}
	}
	if status := (Status{UptimeMS: 250}).ReadableUptime(); status != "250ms" {
		t.Fatalf("readable uptime=%q", status)
	}
	var decoded Status
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != original {
		t.Fatalf("status JSON round trip decoded=%#v error=%v", decoded, err)
	}
	if strings.Contains(text, "UptimeMS") || strings.Contains(text, "TLEDCenti") {
		t.Fatalf("Go field names leaked into public JSON: %s", text)
	}
}

func TestHelloSettingsAndPWMJSONUseStableProtocolNames(t *testing.T) {
	value := struct {
		Hello    Hello     `json:"hello"`
		Settings Settings  `json:"settings"`
		PWM      PWMValues `json:"pwm"`
	}{
		Hello: Hello{
			BuildHash:      0x12345678,
			BuildTimestamp: 0x35019D5D, BuildStamp: "260801194258",
		},
		Settings: Settings{
			StreamPeriodMS: 250, DefaultPage: 1, MotionBreakMSValue: 100,
			DisplayClosedBrightness: 2, MotionExitHoldSeconds: 9,
			OutputPersistence: OutputPersistUserPWM, RelayRestoreMask: 0xF0,
		},
		PWM: PWMValues{SelectedChannel: 3},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"build_hash":305419896`,
		`"build_timestamp_packed":889298269`,
		`"build_timestamp":"260801194258"`,
		`"stream_period_ms":250`, `"default_page":1`,
		`"display_closed_brightness":2`,
		`"motion_exit_hold_seconds":9`,
		`"output_persistence":4`,
		`"relay_restore_mask":240`,
		`"motion_break_ms":100`,
		`"selected_channel":3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("public JSON %s does not contain %s", text, want)
		}
	}
}

func TestSettingsJSONRoundTripPreservesMotionBreakAndRejectsMissingValue(t *testing.T) {
	want := Settings{
		LightMode: 2, DisplayBrightness: 6, StreamPeriodMS: 250,
		MotionBreakMSValue: 37,
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Settings
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("settings JSON round trip got %#v, want %#v", got, want)
	}
	for _, invalid := range []string{
		`{"light_mode":2}`,
		`{"motion_break_ms":0}`,
		`{"motion_break_ms":256}`,
	} {
		if err := json.Unmarshal([]byte(invalid), &got); err == nil {
			t.Fatalf("invalid settings JSON accepted: %s", invalid)
		}
	}
}
