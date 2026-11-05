package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseASAInfoPlaintext(t *testing.T) {
	const payload = `hostname: pccontroller-desktop
wifi version: ESP8266/Arduino 3.1.2/WebServer:3.6.0
esp chip id: a1b2c3
flash chip id: deadbeef
firmware hash: 0123456789abcdef
protocol rev.: 7
st mac: AA:BB:CC:DD:EE:01
ap mac: AA:BB:CC:DD:EE:02
client ip: 192.168.1.20
flash size: 4.00 MB (real: 2.00 MB)
program usage: 37%
settings hash: cafe1234
heap memory: 49152
build: Feb 16 2026 16:47:39 by build@host
uptime: 2 days, 03:04:05
`
	document := Parse(EndpointInfo, []byte(payload), time.Unix(100, 0))
	if document.Format != FormatText {
		t.Fatalf("format = %q", document.Format)
	}
	if document.Raw != payload || document.SafeRaw != payload {
		t.Fatalf("raw response was not retained: raw=%q safe=%q", document.Raw, document.SafeRaw)
	}
	info := Project(map[string]Document{EndpointInfo: document})
	if info.Hostname != "pccontroller-desktop" ||
		info.WiFiVersion != "ESP8266/Arduino 3.1.2/WebServer:3.6.0" ||
		info.ESPChipID != "a1b2c3" ||
		info.ClientIP != "192.168.1.20" {
		t.Fatalf("identity projection = %+v", info)
	}
	if info.FlashSizeBytes == nil || *info.FlashSizeBytes != 4*1024*1024 {
		t.Fatalf("flash size = %v", info.FlashSizeBytes)
	}
	if info.FlashRealSizeBytes == nil || *info.FlashRealSizeBytes != 2*1024*1024 {
		t.Fatalf("real flash size = %v", info.FlashRealSizeBytes)
	}
	if info.ProgramUsagePercent == nil || *info.ProgramUsagePercent != 37 {
		t.Fatalf("program usage = %v", info.ProgramUsagePercent)
	}
	if info.HeapFreeBytes == nil || *info.HeapFreeBytes != 49152 {
		t.Fatalf("heap = %v", info.HeapFreeBytes)
	}
	const wantUptime = int64(2*86400 + 3*3600 + 4*60 + 5)
	if info.UptimeSeconds == nil || *info.UptimeSeconds != wantUptime {
		t.Fatalf("uptime = %v", info.UptimeSeconds)
	}
	if info.BuildTime == nil || info.BuildTime.Year() != 2026 ||
		info.BuildTime.Month() != time.February || info.BuildTime.Day() != 16 {
		t.Fatalf("build time = %v", info.BuildTime)
	}
}

func TestParseInfoJSONAndRedaction(t *testing.T) {
	payload := []byte(`{
	  "device": "PCController Mock",
	  "firmware": "mock-2",
	  "hostname": "mock-device",
	  "state": "ON",
	  "uptime_seconds": 42,
	  "nested": {"api_token": "do-not-serialize"}
	}`)
	document := Parse(EndpointInfo, payload, time.Time{})
	if document.Format != FormatJSON {
		t.Fatalf("format = %q warnings=%v", document.Format, document.Warnings)
	}
	if !strings.Contains(document.Raw, "do-not-serialize") {
		t.Fatal("lossless in-process raw response was altered")
	}
	if strings.Contains(document.SafeRaw, "do-not-serialize") ||
		!strings.Contains(document.SafeRaw, redactedValue) {
		t.Fatalf("safe raw was not redacted: %s", document.SafeRaw)
	}
	for _, field := range document.Fields {
		if field.Key == "nested_api_token" && (!field.Redacted || field.Display != redactedValue) {
			t.Fatalf("secret field = %+v", field)
		}
	}
	serialized, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "do-not-serialize") {
		t.Fatalf("serialized document leaked raw secret: %s", serialized)
	}
	info := Project(map[string]Document{EndpointInfo: document})
	if info.DeviceName != "PCController Mock" || info.Firmware != "mock-2" ||
		info.ReportedState != "on" || info.UptimeSeconds == nil ||
		*info.UptimeSeconds != 42 {
		t.Fatalf("JSON projection = %+v", info)
	}
}

func TestParseUpdateInfoJSONAndText(t *testing.T) {
	jsonDocument := Parse(
		EndpointUpdateInfo,
		[]byte(`{"hash":"abc123","size":1048576,"free":2097152,"date":"Feb 16 2026 16:47:39"}`),
		time.Time{},
	)
	jsonInfo := Project(map[string]Document{EndpointUpdateInfo: jsonDocument})
	if jsonInfo.Update.Hash != "abc123" ||
		jsonInfo.Update.SizeBytes == nil || *jsonInfo.Update.SizeBytes != 1048576 ||
		jsonInfo.Update.FreeBytes == nil || *jsonInfo.Update.FreeBytes != 2097152 ||
		jsonInfo.Update.Date == nil {
		t.Fatalf("JSON update projection = %+v", jsonInfo.Update)
	}

	textDocument := Parse(
		EndpointUpdateInfo,
		[]byte("hash = def456\nsize: 1 MB\nfree: 512 KB\ndate: 2026-02-16 16:47:39\n"),
		time.Time{},
	)
	textInfo := Project(map[string]Document{EndpointUpdateInfo: textDocument})
	if textInfo.Update.Hash != "def456" ||
		textInfo.Update.SizeBytes == nil || *textInfo.Update.SizeBytes != 1024*1024 ||
		textInfo.Update.FreeBytes == nil || *textInfo.Update.FreeBytes != 512*1024 ||
		textInfo.Update.Date == nil {
		t.Fatalf("text update projection = %+v", textInfo.Update)
	}
}

func TestParseWiFiFormats(t *testing.T) {
	signal := Parse(EndpointWiFiSignal, []byte("-55\nGood\n"), time.Time{})
	status := Parse(
		EndpointWiFiStatus,
		[]byte("ap status: 2\nst status: 1\n"),
		time.Time{},
	)
	country := Parse(EndpointWiFiCountry, []byte("IR\n"), time.Time{})
	info := Project(map[string]Document{
		EndpointWiFiSignal:  signal,
		EndpointWiFiStatus:  status,
		EndpointWiFiCountry: country,
	})
	if info.RSSIDBm == nil || *info.RSSIDBm != -55 ||
		info.SignalQuality != "Good" ||
		info.AccessPointStatus != "Disabled" ||
		info.StationStatus != "Connected" ||
		info.WiFiConnected == nil || !*info.WiFiConnected ||
		info.Country != "IR" {
		t.Fatalf("ASA Wi-Fi projection = %+v", info)
	}

	jsonStatus := Parse(
		EndpointWiFiStatus,
		[]byte(`{"connected":true,"ssid":"PCControllerDesktop-Mock","ip":"127.0.0.1","rssi":-42}`),
		time.Time{},
	)
	jsonInfo := Project(map[string]Document{EndpointWiFiStatus: jsonStatus})
	if jsonInfo.WiFiConnected == nil || !*jsonInfo.WiFiConnected ||
		jsonInfo.SSID != "PCControllerDesktop-Mock" || jsonInfo.WiFiIP != "127.0.0.1" ||
		jsonInfo.RSSIDBm == nil || *jsonInfo.RSSIDBm != -42 {
		t.Fatalf("JSON Wi-Fi projection = %+v", jsonInfo)
	}
}

func TestParseNetworkConfigsRedactsBeforeRetentionAndProjects(t *testing.T) {
	const secret = "must-never-be-retained"
	station := Parse(
		EndpointConfigST,
		[]byte(
			"ssid: Office Wi-Fi\n"+
				"pass: "+secret+"\n"+
				"dhcp: enabled\n"+
				"ip: 192.168.10.45\n"+
				"netmask: 255.255.255.0\n"+
				"gw: 192.168.10.1\n"+
				"dns: 192.168.10.1, 1.1.1.1\n",
		),
		time.Time{},
	)
	accessPoint := Parse(
		EndpointConfigAP,
		[]byte(
			"ssid: PCControllerDesktop-AP\n"+
				"pass: "+secret+"\n"+
				"ip: 192.168.4.1\n"+
				"netmask: 255.255.255.0\n",
		),
		time.Time{},
	)
	for _, document := range []Document{station, accessPoint} {
		serialized, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		for _, retained := range []string{
			document.Raw,
			document.SafeRaw,
			string(serialized),
		} {
			if strings.Contains(retained, secret) {
				t.Fatalf("%s retained a credential: %q", document.Endpoint, retained)
			}
		}
		for _, field := range document.Fields {
			if strings.Contains(field.Raw, secret) ||
				strings.Contains(field.Display, secret) {
				t.Fatalf("%s retained credential field %+v", document.Endpoint, field)
			}
		}
		if !strings.Contains(document.SafeRaw, redactedValue) {
			t.Fatalf("%s did not retain an explicit redaction marker: %q", document.Endpoint, document.SafeRaw)
		}
	}

	status := Parse(
		EndpointWiFiStatus,
		[]byte("ap status: 1\nst status: 1\n"),
		time.Time{},
	)
	info := Project(map[string]Document{
		EndpointWiFiStatus: status,
		EndpointConfigST:   station,
		EndpointConfigAP:   accessPoint,
	})
	if info.WiFiConnected == nil || !*info.WiFiConnected ||
		info.SSID != "Office Wi-Fi" ||
		info.WiFiIP != "192.168.10.45" ||
		info.StationDHCP == nil || !*info.StationDHCP ||
		info.StationNetmask != "255.255.255.0" ||
		info.StationGateway != "192.168.10.1" ||
		info.StationDNS != "192.168.10.1, 1.1.1.1" ||
		info.AccessPointStatus != "Enabled" ||
		info.AccessPointSSID != "PCControllerDesktop-AP" ||
		info.AccessPointIP != "192.168.4.1" {
		t.Fatalf("network configuration projection = %+v", info)
	}
}

func TestNetworkStatusCodesAndStationConfigPrecedence(t *testing.T) {
	tests := []struct {
		code      string
		label     string
		connected *bool
	}{
		{"0", "Unknown", nil},
		{"1", "Connected", boolPointer(true)},
		{"2", "Disconnected", boolPointer(false)},
		{"3", "Connecting", boolPointer(false)},
		{"4", "SSID not found", boolPointer(false)},
		{"5", "Wrong password", boolPointer(false)},
		{"6", "Connection failed", boolPointer(false)},
		{"7", "Idle", boolPointer(false)},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			status := Parse(
				EndpointWiFiStatus,
				[]byte("ap status: 2\nst status: "+test.code+"\n"),
				time.Time{},
			)
			info := Project(map[string]Document{EndpointWiFiStatus: status})
			if info.StationStatus != test.label {
				t.Fatalf("station status = %q, want %q", info.StationStatus, test.label)
			}
			switch {
			case test.connected == nil && info.WiFiConnected != nil:
				t.Fatalf("connected = %v, want unknown", *info.WiFiConnected)
			case test.connected != nil &&
				(info.WiFiConnected == nil ||
					*info.WiFiConnected != *test.connected):
				t.Fatalf("connected = %v, want %v", info.WiFiConnected, *test.connected)
			}
		})
	}

	legacyStatus := Parse(
		EndpointWiFiStatus,
		[]byte(`{"connected":true,"ssid":"Legacy SSID","ip":"127.0.0.1"}`),
		time.Time{},
	)
	station := Parse(
		EndpointConfigST,
		[]byte("ssid: Actual SSID\npass: protected\nip: 192.168.1.50\n"),
		time.Time{},
	)
	info := Project(map[string]Document{
		EndpointWiFiStatus: legacyStatus,
		EndpointConfigST:   station,
	})
	if info.SSID != "Actual SSID" || info.WiFiIP != "192.168.1.50" {
		t.Fatalf("station configuration did not win: %+v", info)
	}
}

func TestMalformedSensitiveJSONCannotRetainShortCredentialKey(t *testing.T) {
	const secret = "malformed-secret"
	document := Parse(
		EndpointConfigST,
		[]byte(`{"ssid":"Office","pass":"`+secret+`"`),
		time.Time{},
	)
	for _, value := range []string{document.Raw, document.SafeRaw} {
		if strings.Contains(value, secret) {
			t.Fatalf("malformed sensitive JSON leaked through fallback: %s", value)
		}
	}
}

func TestParseDescriptionXMLAndMalformedJSONFallback(t *testing.T) {
	description := Parse(
		EndpointDescription,
		[]byte(`<?xml version="1.0"?><root><device><friendlyName>PCController</friendlyName><modelName>ESP8266</modelName><manufacturer>Controller</manufacturer></device></root>`),
		time.Time{},
	)
	info := Project(map[string]Document{EndpointDescription: description})
	if description.Format != FormatXML ||
		info.Description.FriendlyName != "PCController" ||
		info.Description.ModelName != "ESP8266" ||
		info.DeviceName != "PCController" {
		t.Fatalf("description = %+v info=%+v", description, info)
	}

	malformed := Parse(EndpointInfo, []byte("{not-json\nhostname: fallback"), time.Time{})
	if malformed.Format != FormatText || len(malformed.Warnings) == 0 ||
		fieldText(malformed, "hostname") != "fallback" {
		t.Fatalf("malformed fallback = %+v", malformed)
	}

	malformedSecret := Parse(
		EndpointInfo,
		[]byte(`{"api_token":"must-not-escape"`),
		time.Time{},
	)
	if strings.Contains(malformedSecret.SafeRaw, "must-not-escape") {
		t.Fatalf("malformed JSON fallback leaked a secret: %s", malformedSecret.SafeRaw)
	}
}

func TestSnapshotCloneDoesNotAlias(t *testing.T) {
	integer := int64(7)
	original := Snapshot{
		Endpoints: map[string]EndpointResult{
			EndpointInfo: {
				Document: Document{
					Fields:   []Field{{Key: "value", Integer: &integer}},
					Warnings: []string{"one"},
				},
			},
		},
	}
	clone := original.Clone()
	result := clone.Endpoints[EndpointInfo]
	result.Document.Fields[0].Key = "changed"
	*result.Document.Fields[0].Integer = 99
	result.Document.Warnings[0] = "changed"
	clone.Endpoints[EndpointInfo] = result

	got := original.Endpoints[EndpointInfo]
	if got.Document.Fields[0].Key != "value" ||
		*got.Document.Fields[0].Integer != 7 ||
		got.Document.Warnings[0] != "one" {
		t.Fatalf("clone aliases original: %+v", got)
	}
}
