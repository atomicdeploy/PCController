package hostfacts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLMSensorsNormalizesReadingsWithoutZeroingMissingData(t *testing.T) {
	rows, truncated, err := parseLMSensors([]byte(`{
	 "chip-isa-0290": {"Adapter":"ISA adapter",
	  "Core": {"temp1_input":42.25,"temp1_max":90,"temp1_alarm":1},
	  "+12V": {"in0_input":12.125}, "Motor": {"curr1_input":0},
	  "Power": {"power1_average":15.5}, "Fan": {"fan1_input":1450},
	  "Unavailable NaN": {"temp2_input":NaN,"temp2_max":100},
	  "Threshold only": {"temp3_crit":80,"temp3_input":20,"temp3_fault":1},
	  "Counter": {"energy1_input":12}, "Humidity": {"humidity1_input":55}
	 }} `))
	if err != nil || truncated || len(rows) != 9 {
		t.Fatalf("rows=%v truncated=%t err=%v", rows, truncated, err)
	}
	byID := make(map[string]map[string]any)
	for _, row := range rows {
		byID[row["ID"].(string)] = row
	}
	for _, want := range []struct {
		key, kind, unit string
		value           float64
	}{
		{"temp1", "temperature", "°C", 42.25}, {"in0", "voltage", "V", 12.125},
		{"curr1", "current", "A", 0}, {"power1", "power", "W", 15.5}, {"fan1", "fan", "rpm", 1450},
	} {
		row := byID["lm-sensors:chip-isa-0290/"+want.key]
		if row["Type"] != want.kind || row["Unit"] != want.unit || row["Value"] != want.value || row["Available"] != true {
			t.Fatalf("reading=%v want=%+v", row, want)
		}
	}
	for _, key := range []string{"temp2", "temp3"} {
		row := byID["lm-sensors:chip-isa-0290/"+key]
		if row["Value"] != nil || row["Available"] != false {
			t.Fatalf("missing value fabricated: %v", row)
		}
	}
	if byID["lm-sensors:chip-isa-0290/temp2"]["Name"] != "Unavailable NaN" {
		t.Fatal("rewrote label string")
	}
	if byID["lm-sensors:chip-isa-0290/temp1"]["Alarm"] != true {
		t.Fatal("alarm lost")
	}
	if byID["lm-sensors:chip-isa-0290/temp1"]["HighLimit"] != float64(90) ||
		byID["lm-sensors:chip-isa-0290/temp1"]["ObservedMax"] != nil ||
		byID["lm-sensors:chip-isa-0290/temp3"]["CriticalHigh"] != float64(80) ||
		byID["lm-sensors:chip-isa-0290/temp3"]["Fault"] != true ||
		byID["lm-sensors:chip-isa-0290/power1"]["Statistic"] != "average" {
		t.Fatal("threshold/fault/average semantics lost")
	}
}

func TestSensorParsersRejectMalformedAndBoundResponses(t *testing.T) {
	for _, input := range []string{"null", "[]", "{", `{"chip":{"Not a feature":2}}`} {
		if _, _, err := parseLMSensors([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	for _, input := range []string{"null", "{}", "[]", `{"Children":[{"SensorId":"/a","Type":"Surprise"}]}`} {
		if _, _, err := parseLibreSensors([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	for _, parser := range []func([]byte) ([]map[string]any, bool, error){parseLMSensors, parseLibreSensors} {
		if _, _, err := parser([]byte(strings.Repeat(" ", maxSensorInputBytes+1))); err == nil {
			t.Fatal("accepted oversized sensor response")
		}
	}
	var tree lhmNode
	for index := 0; index < 34; index++ {
		tree = lhmNode{Children: []lhmNode{tree}}
	}
	if _, _, err := libreSensorRows(tree); err == nil {
		t.Fatal("accepted excessively deep tree")
	}
}

func TestLibreSensorTreePreservesRawPrecisionAndNullableAvailability(t *testing.T) {
	rows, truncated, err := parseLibreSensors([]byte(`{"Children":[{"HardwareId":"/cpu/0","Children":[
	 {"SensorId":"/cpu/0/temperature/0","Text":"CPU Package","Type":"Temperature","RawValue":43.125,"Value":"43,1 °C","RawMin":32,"RawMax":70},
	 {"SensorId":"/cpu/0/current/0","Text":"Current","Type":"Current","RawValue":null},
	 {"SensorId":"/cpu/0/voltage/0","Type":"Voltage","RawValue":0},
	 {"SensorId":"/cpu/0/power/0","Type":"Power","RawValue":"NaN"}
	 ]}]} `))
	if err != nil || truncated || len(rows) != 4 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	for _, row := range rows {
		if row["Hardware"] != "/cpu/0" || row["Source"] != "librehardwaremonitor" {
			t.Fatalf("source lost: %v", row)
		}
		switch row["Type"] {
		case "temperature":
			if row["Value"] != 43.125 || row["Unit"] != "°C" || row["ObservedMin"] != float64(32) {
				t.Fatalf("raw numeric value lost: %v", row)
			}
		case "voltage":
			if row["Value"] != float64(0) || row["Available"] != true {
				t.Fatal("real zero was marked missing")
			}
		default:
			if row["Value"] != nil || row["Available"] != false {
				t.Fatal("unknown value became zero")
			}
		}
	}
	duplicate := lhmNode{Children: []lhmNode{{SensorID: "/x", Type: "Fan"}, {SensorID: "/x", Type: "Fan"}}}
	if _, _, err := libreSensorRows(duplicate); err == nil {
		t.Fatal("accepted duplicate sensor IDs")
	}
	for _, number := range []any{math.NaN(), math.Inf(1), math.Inf(-1), "12.4"} {
		if _, ok := sensorNumber(number); ok {
			t.Fatalf("accepted nonfinite/non-numeric %v", number)
		}
	}
}

func TestSensorRowsAreDeterministicallyTruncatedAndCatalogued(t *testing.T) {
	var root lhmNode
	for index := 0; index < maxGlobalRows+5; index++ {
		root.Children = append(root.Children, lhmNode{SensorID: fmt.Sprintf("/fan/%03d", index), Type: "Fan", Value: float64(index)})
	}
	rows, truncated, err := libreSensorRows(root)
	if err != nil || !truncated || len(rows) != maxGlobalRows || rows[0]["Value"] != float64(0) {
		t.Fatalf("rows=%d truncated=%t err=%v", len(rows), truncated, err)
	}
	if queryCatalog["sensors"].Class != "HostSensor" || queryCatalog["sensors"].MaxRows != 128 {
		t.Fatal("sensor profile not advertised")
	}
	provider := newCachedProvider(&fakeBackend{queryFunc: func(context.Context, querySpec) ([]map[string]any, bool, error) { return rows, truncated, nil }})
	result, err := provider.Query(context.Background(), "sensors")
	if err != nil || result.Source != sensorSource() || result.CollectedAt.IsZero() || !result.Truncated {
		t.Fatalf("result=%v err=%v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxResultBytes {
		t.Fatalf("result size=%d err=%v", len(encoded), err)
	}
}

func TestSensorEndpointRejectsRemoteProxyAndMutationTargets(t *testing.T) {
	for _, address := range []string{
		"http://example.com/data.json", "http://localhost/data.json", "http://192.168.1.2/data.json",
		"https://127.0.0.1/data.json", "http://user:pass@127.0.0.1/data.json", "http://127.0.0.1/Sensor?action=Set",
		"http://127.0.0.1/data.json?reset=1", "http://127.0.0.1/data.json#x", "http://127.0.0.1:65536/data.json",
	} {
		if _, err := validateSensorsURL(address); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
	for _, address := range []string{"", "http://127.0.0.1:8085/data.json", "http://[::1]:8085/data.json"} {
		if _, err := validateSensorsURL(address); err != nil {
			t.Fatalf("rejected %s: %v", address, err)
		}
	}
	transport := sensorHTTPClient.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("ambient proxy used for local sensors")
	}
}

func TestLibreSensorHTTPReadsOnlyLocalNumericDataAndRejectsRedirects(t *testing.T) {
	var calls, controls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" {
			controls.Add(1)
			t.Errorf("unexpected method %s", r.Method)
		}
		if r.URL.Path == "/Sensor" {
			if r.URL.Query().Get("action") != "Get" || r.URL.Query().Get("id") != "/cpu/temp/0" {
				controls.Add(1)
				t.Error("unexpected control query")
			}
			fmt.Fprint(w, `{"result":"ok","value":41.125,"min":30,"max":70}`)
			return
		}
		fmt.Fprint(w, `{"Children":[{"SensorId":"/cpu/temp/0","Type":"Temperature","RawValue":"41,1 °C"}]}`)
	}))
	defer server.Close()
	rows, _, err := readLibreSensors(context.Background(), sensorHTTPClient, server.URL+"/data.json")
	if err != nil || len(rows) != 1 || rows[0]["Value"] != 41.125 || calls.Load() != 2 || controls.Load() != 0 {
		t.Fatalf("rows=%v calls=%d err=%v", rows, calls.Load(), err)
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/data.json", http.StatusFound)
	}))
	defer redirect.Close()
	if _, _, err := readLibreSensors(context.Background(), sensorHTTPClient, redirect.URL+"/data.json"); err == nil || redirected.Load() != 0 {
		t.Fatal("sensor redirect followed")
	}
}

func TestLibreSensorHTTPTimeoutAndMalformedReadDoNotPublishPartialSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Sensor" {
			fmt.Fprint(w, `{"result":"ok","value":"42 °C"}`)
			return
		}
		fmt.Fprint(w, `{"Children":[{"SensorId":"/a","Type":"Temperature","RawValue":"42 °C"}]}`)
	}))
	defer server.Close()
	if rows, _, err := readLibreSensors(context.Background(), sensorHTTPClient, server.URL+"/data.json"); err == nil || rows != nil {
		t.Fatalf("partial success rows=%v err=%v", rows, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, _, err := readLibreSensors(ctx, sensorHTTPClient, server.URL+"/data.json"); err == nil {
		t.Fatal("ignored deadline")
	}
}
