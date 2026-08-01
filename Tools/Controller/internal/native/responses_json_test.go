package native

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusJSONUsesStableProtocolNamesAndRetainsRawUptime(t *testing.T) {
	encoded, err := json.Marshal(Status{
		UptimeMS:  4_392_210,
		SupplyMV:  12_224,
		TLEDCenti: 2_650,
		TBTCenti:  2_410,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"uptime_ms":4392210`,
		`"supply_mv":12224`,
		`"temperature_led_centi_c":2650`,
		`"temperature_bt_audio_centi_c":2410`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status JSON %s does not contain %s", text, want)
		}
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
			FirmwareMajor: 1, BuildHash: 0x12345678,
			BuildTimestamp: 0x35019D5D, BuildStamp: "260801194258",
		},
		Settings: Settings{
			Flags: SettingsMotionBreak100MS, StreamPeriodMS: 250, DefaultPage: 1,
		},
		PWM: PWMValues{SelectedChannel: 3},
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{
		`"firmware_major":1`, `"build_hash":305419896`,
		`"build_timestamp_packed":889298269`,
		`"build_timestamp":"260801194258"`,
		`"stream_period_ms":250`, `"default_page":1`,
		`"motion_break_ms":100`,
		`"selected_channel":3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("public JSON %s does not contain %s", text, want)
		}
	}
}
