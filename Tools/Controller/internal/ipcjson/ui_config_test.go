package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func browserUIConfigTestService(t *testing.T) (*Service, *appconfig.Config) {
	t.Helper()
	runtime := control.New(control.Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.UI.AppTitle = "Workshop Controller"
	config.UI.SetupComplete = false
	config.UI.PeripheralNames = map[string]string{"relay.5": "Workbench lamp"}
	config.Connection.ResetOnReconnect = false
	service := &Service{
		Client:     client,
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	}
	return service, &config
}

func TestBrowserUIConfigRPCPersistsOnlyRequestedHostFields(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	get := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.get"})
	initial, ok := get.Result.(browserUISettings)
	if get.Error != nil || !ok || initial.AppTitle != "Workshop Controller" || initial.SetupComplete {
		t.Fatalf("initial UI config=%#v error=%v", get.Result, get.Error)
	}
	if initial.PeripheralNames["relay.5"] != "Workbench lamp" || len(initial.Peripherals) != 34 {
		t.Fatalf("initial peripheral contract names=%#v descriptors=%d", initial.PeripheralNames, len(initial.Peripherals))
	}
	if !initial.SegmentScroll.Enabled || len(initial.SegmentScroll.Pages) != 1 || initial.SegmentScroll.Pages[0] != "door" {
		t.Fatalf("initial segment scroll config=%+v", initial.SegmentScroll)
	}

	params, _ := json.Marshal(map[string]any{
		"app_title":      "Control Room",
		"tagline":        "ONE HOST · EVERY BOARD",
		"setup_complete": true,
		"segment_scroll": map[string]any{
			"enabled":          true,
			"pages":            []string{"door", "9"},
			"door_open_text":   "enclosure open",
			"door_closed_text": "enclosure closed",
			"speed_ms":         180,
			"gap_cells":        4,
		},
	})
	set := service.Dispatch(context.Background(), Request{
		Method: "controller.ui.config.set", Params: params,
	})
	updated, ok := set.Result.(browserUISettings)
	if set.Error != nil || !ok || updated.AppTitle != "Control Room" || updated.Tagline != "ONE HOST · EVERY BOARD" || !updated.SetupComplete {
		t.Fatalf("updated UI config=%#v error=%v", set.Result, set.Error)
	}
	if config.UI.AppTitle != "Control Room" || config.UI.Tagline != "ONE HOST · EVERY BOARD" || !config.UI.SetupComplete {
		t.Fatalf("persistent UI config=%+v", config.UI)
	}
	if config.UI.SegmentScroll.SpeedMS != 180 || config.UI.SegmentScroll.DoorOpenText != "enclosure open" ||
		len(config.UI.SegmentScroll.Pages) != 2 {
		t.Fatalf("persistent segment scroll config=%+v", config.UI.SegmentScroll)
	}
	if config.Connection.ResetOnReconnect {
		t.Fatal("UI update changed reset_on_reconnect")
	}

	empty, _ := json.Marshal(map[string]string{"app_title": ""})
	rejected := service.Dispatch(context.Background(), Request{
		Method: "controller.ui.config.set", Params: empty,
	})
	if rejected.Error == nil || config.UI.AppTitle != "Control Room" {
		t.Fatalf("invalid title was not rejected atomically: response=%#v title=%q", rejected, config.UI.AppTitle)
	}
}

func TestBrowserUIConfigMeasurementTimingIsHostOwnedAndAtomicallyValidated(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	params, _ := json.Marshal(map[string]any{
		"status_interval_ms":       300,
		"measurement_freshness_ms": 1600,
	})
	response := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: params})
	updated, ok := response.Result.(browserUISettings)
	if response.Error != nil || !ok || updated.StatusIntervalMS != 300 || updated.MeasurementFreshnessMS != 1600 {
		t.Fatalf("measurement update=%#v error=%v", response.Result, response.Error)
	}
	if !reflect.DeepEqual(updated.ChangedFields, []string{"status_interval_ms", "measurement_freshness_ms"}) ||
		config.UI.StatusIntervalMS != 300 || config.UI.MeasurementFreshnessMS != 1600 {
		t.Fatalf("persisted timing=%+v fields=%#v", config.UI, updated.ChangedFields)
	}
	invalid, _ := json.Marshal(map[string]any{
		"status_interval_ms":       500,
		"measurement_freshness_ms": 550,
	})
	rejected := service.Dispatch(context.Background(), Request{Method: "controller.ui.config.set", Params: invalid})
	if rejected.Error == nil || config.UI.StatusIntervalMS != 300 || config.UI.MeasurementFreshnessMS != 1600 {
		t.Fatalf("invalid timing was not rejected atomically: %#v config=%+v", rejected, config.UI)
	}
}

func TestPeripheralNamesRPCNormalizesPersistsAndNeverTouchesBoardSettings(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	params, _ := json.Marshal(map[string]any{
		"peripheral_names": map[string]string{
			" relay.5 ":    "  Bench lamp  ",
			"sensor.power": "Power meter",
			"pwm.0":        "",
		},
	})
	set := service.Dispatch(context.Background(), Request{
		Method: "controller.peripherals.set", Params: params,
	})
	updated, ok := set.Result.(peripheralSettings)
	if set.Error != nil || !ok {
		t.Fatalf("peripheral set result=%#v error=%v", set.Result, set.Error)
	}
	if updated.Names["relay.5"] != "Bench lamp" || updated.Names["sensor.power"] != "Power meter" {
		t.Fatalf("normalized names=%#v", updated.Names)
	}
	if _, exists := updated.Names["pwm.0"]; exists {
		t.Fatalf("blank custom name was not restored to default: %#v", updated.Names)
	}
	if len(updated.Peripherals) != 34 || len(updated.Controls) != appconfig.MaxPresentedControls || config.Connection.ResetOnReconnect {
		t.Fatalf("peripheral update descriptors=%d controls=%d reset-on-reconnect=%t", len(updated.Peripherals), len(updated.Controls), config.Connection.ResetOnReconnect)
	}
	if got := updated.Controls[4]; got.Key != "relay.5" || got.Kind != "relay" || got.Order != 4 || got.Name != "Bench lamp" {
		t.Fatalf("relay control contract=%+v", got)
	}

	bad, _ := json.Marshal(map[string]any{
		"peripheral_names": map[string]string{"relay.6": strings.Repeat("x", 65)},
	})
	rejected := service.Dispatch(context.Background(), Request{
		Method: "controller.peripherals.set", Params: bad,
	})
	if rejected.Error == nil || config.UI.PeripheralNames["relay.5"] != "Bench lamp" {
		t.Fatalf("invalid peripheral update was not atomic: response=%#v config=%#v", rejected, config.UI.PeripheralNames)
	}
}

func TestPeripheralNamesRESTUsesTheSameTypedContract(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	body := strings.NewReader(`{"peripheral_names":{"display.lcd":"Cabinet LCD"}}`)
	request, err := http.NewRequest(http.MethodPut, server.URL+"/api/peripherals", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result peripheralSettings
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || result.Names["display.lcd"] != "Cabinet LCD" || len(result.Peripherals) != 34 || len(result.Controls) != appconfig.MaxPresentedControls {
		t.Fatalf("REST peripheral response status=%d result=%+v", response.StatusCode, result)
	}
}

func TestPeripheralPresentationBroadcastConvergesTwoWebSocketClients(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"
	dial := func(id string) *websocket.Conn {
		connection, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", id, err)
		}
		t.Cleanup(func() { connection.CloseNow() })
		request, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id, "method": "controller.subscribe",
			"params": map[string]any{"topics": []string{"events"}},
		})
		if err := connection.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatal(err)
		}
		for {
			_, data, err := connection.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var response Response
			if json.Unmarshal(data, &response) == nil && string(response.ID) == `"`+id+`"` {
				return connection
			}
		}
	}
	first, second := dial("first"), dial("second")
	mutation, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 17, "method": "controller.peripherals.set",
		"params": map[string]any{"peripheral_presentation": map[string]any{
			"motion.a": map[string]any{"name": "Left", "description": "Left lift", "order": 0},
			"motion.b": map[string]any{"name": "Right", "description": "Right lift", "order": 1},
		}},
	})
	if err := first.Write(ctx, websocket.MessageText, mutation); err != nil {
		t.Fatal(err)
	}
	readConfig := func(name string, connection *websocket.Conn) {
		for {
			_, data, err := connection.Read(ctx)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			var notification struct {
				Method string              `json:"method"`
				Params controllerapi.Event `json:"params"`
			}
			if json.Unmarshal(data, &notification) == nil && notification.Method == "controller.event" &&
				notification.Params.Kind == "config" && notification.Params.Action == "peripheral.presentation.set" {
				return
			}
		}
	}
	readConfig("first", first)
	readConfig("second", second)
	if config.UI.PeripheralPresentation["motion.a"].Name != "Left" ||
		config.UI.PeripheralNames["motion.b"] != "Right" {
		t.Fatalf("presentation did not persist with compatibility names: %+v / %+v", config.UI.PeripheralPresentation, config.UI.PeripheralNames)
	}
}

func TestPeripheralAndPWMCapabilitiesSeparateReadConfigurationAndBoardWrites(t *testing.T) {
	checks := map[string]string{
		"controller.peripherals.get": capabilityRead,
		"controller.peripherals.set": capabilityHostConfig,
		"controller.pwm.values":      capabilityRead,
		"controller.pwm.set":         capabilityBoard,
		"controller.pwm.off":         capabilityBoard,
	}
	for method, want := range checks {
		if got := requestCapability(method, nil); got != want {
			t.Fatalf("%s capability=%q, want %q", method, got, want)
		}
	}
}

func TestBootstrapUIConfigReportsPersistentFirstRunState(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/ui-config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Name             string `json:"name"`
		Tagline          string `json:"tagline"`
		SetupComplete    bool   `json:"setup_complete"`
		WelcomeMelody    string `json:"welcome_melody"`
		ResetOnReconnect *bool  `json:"reset_on_reconnect,omitempty"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || result.Name != "Workshop Controller" || result.Tagline == "" ||
		result.SetupComplete || result.WelcomeMelody == "" {
		t.Fatalf("bootstrap UI config status=%d result=%+v", response.StatusCode, result)
	}
	if result.ResetOnReconnect != nil {
		t.Fatal("bootstrap UI config leaked reset control state")
	}
}
