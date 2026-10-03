package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
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
	if initial.PeripheralNames["relay.5"] != "Workbench lamp" || len(initial.Peripherals) != 34 || len(initial.Controls) != 26 {
		t.Fatalf("initial peripheral contract names=%#v descriptors=%d controls=%d", initial.PeripheralNames, len(initial.Peripherals), len(initial.Controls))
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
	if len(updated.Peripherals) != 32 || len(updated.Controls) != 20 || updated.BoardProfile.Mode != appconfig.BoardModeUnconfigured || config.Connection.ResetOnReconnect {
		t.Fatalf("peripheral update descriptors=%d controls=%d reset-on-reconnect=%t", len(updated.Peripherals), len(updated.Controls), config.Connection.ResetOnReconnect)
	}
	if got := updated.Controls[0]; got.Key != "relay.5" || got.Kind != "relay" || got.Order != 5 || got.Name != "Bench lamp" {
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
	if response.StatusCode != http.StatusOK || result.Names["display.lcd"] != "Cabinet LCD" || len(result.Peripherals) != 32 || len(result.Controls) != 20 || result.BoardProfile.Configured {
		t.Fatalf("REST peripheral response status=%d result=%+v", response.StatusCode, result)
	}
}

func TestPeripheralAndPWMCapabilitiesSeparateReadConfigurationAndBoardWrites(t *testing.T) {
	checks := map[string]string{
		"controller.peripherals.get":                capabilityRead,
		"controller.peripherals.set":                capabilityHostConfig,
		"controller.board_profile.get":              capabilityRead,
		"controller.board_profile.update":           capabilityHostConfig,
		"controller.peripheral.presentation.update": capabilityHostConfig,
		"controller.action.invoke":                  capabilityBoard,
		"controller.pwm.values":                     capabilityRead,
		"controller.pwm.set":                        capabilityBoard,
		"controller.pwm.off":                        capabilityBoard,
		"controller.illumination.get":               capabilityRead,
		"controller.illumination.override":          capabilityBoard,
		"controller.illumination.set":               capabilityBoard,
	}
	for method, want := range checks {
		if got := requestCapability(method, nil); got != want {
			t.Fatalf("%s capability=%q, want %q", method, got, want)
		}
	}
}

func TestPeripheralCatalogUsesExplicitBoardProfileWithoutLegacyMotionAliases(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	config.Connection.LastDevice = &appconfig.DeviceIdentity{Port: "COM18", SerialNumber: "BOARD-42"}
	config.BoardProfiles = map[string]appconfig.BoardProfile{
		"serial:board-42": {
			Key: "cafe-cinema", Mode: appconfig.BoardModeCinemaSeatMotion,
			Presentation: map[string]appconfig.PeripheralPresentation{
				"seat.a": {Name: "Left bank", Icon: "seat", Color: "#38D27A", Group: "auditorium", Hidden: true, Locked: true},
			},
		},
	}
	response := service.Dispatch(context.Background(), Request{Method: "controller.peripherals.get"})
	settings, ok := response.Result.(peripheralSettings)
	if response.Error != nil || !ok {
		t.Fatalf("result=%#v error=%v", response.Result, response.Error)
	}
	if !settings.BoardProfile.Configured || settings.BoardProfile.Attached || settings.BoardProfile.Key != "cafe-cinema" ||
		settings.BoardProfile.BoardIdentity != "serial:board-42" || settings.BoardProfile.Mode != appconfig.BoardModeCinemaSeatMotion || len(settings.Controls) != 22 {
		t.Fatalf("profile=%+v controls=%d", settings.BoardProfile, len(settings.Controls))
	}
	seenSeat := false
	for _, control := range settings.Controls {
		if control.Key == "motion.a" || control.Key == "motion.b" || control.Key == "relay.1" {
			t.Fatalf("catalog advertised ambiguous legacy control %+v", control)
		}
		if control.Key == "seat.a" {
			seenSeat = control.Name == "Left bank" && control.Icon == "seat" && control.Color == "#38D27A" && control.Group == "auditorium" &&
				control.Hidden && control.Locked &&
				len(control.Actions) == 3 && control.Actions[2].ID == "seat.a.stop"
		}
	}
	if !seenSeat {
		t.Fatalf("seat.a missing from %+v", settings.Controls)
	}

	params, _ := json.Marshal(map[string]any{"key": "cafe-cinema", "mode": appconfig.BoardModeCinemaSeatMotion})
	rejected := service.Dispatch(context.Background(), Request{Method: "controller.board_profile.update", Params: params})
	if rejected.Error == nil || !strings.Contains(rejected.Error.Message, "attached") {
		t.Fatalf("unattached update=%+v", rejected)
	}
}

func TestPresentationUpdateResultCarriesTheAuthoritativeControl(t *testing.T) {
	settings := peripheralSettings{
		BoardProfile: boardProfileDescriptor{Key: "cafe-cinema", Revision: "next-revision"},
		Peripherals: []appconfig.PeripheralDescriptor{{
			Key: "relay.5", Kind: "relay", Index: 5, DefaultName: "User Relay 5",
			Name: "Aisle lamp", Icon: "lightbulb", Color: "#38D27A", Control: "relay",
		}},
		Controls: []appconfig.ControlDescriptor{{
			Key: "relay.5", Kind: "relay", Order: 5, Name: "Aisle lamp",
			Icon: "lightbulb", Color: "#38D27A", Control: "relay",
			Actions: []appconfig.ActionDescriptor{{ID: "relay.5.on", Verb: "on", Name: "On"}},
		}},
	}
	result, err := presentationResult(settings, "relay.5")
	if err != nil {
		t.Fatal(err)
	}
	if result.BoardProfile.Revision != "next-revision" || result.Peripheral.Name != "Aisle lamp" ||
		result.Control == nil || result.Control.Key != "relay.5" || result.Control.Icon != "lightbulb" ||
		len(result.Control.Actions) != 1 || result.Control.Actions[0].ID != "relay.5.on" || len(result.Controls) != 1 {
		t.Fatalf("presentation update result=%+v", result)
	}
	if _, err := presentationResult(settings, "relay.6"); err == nil {
		t.Fatal("missing updated peripheral was accepted")
	}
}

func TestPresentationReorderPersistsACompleteKindLocalPermutation(t *testing.T) {
	profile := appconfig.BoardProfile{Mode: appconfig.BoardModeOrdinaryRelays}
	changed, err := reorderPeripheralPresentation(&profile, nil, "relay.7", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 8 || changed[0] != "relay.7" {
		t.Fatalf("changed relay keys=%v", changed)
	}
	for rank, key := range changed {
		presentation, ok := profile.Presentation[key]
		if !ok || presentation.Order == nil || *presentation.Order != rank {
			t.Fatalf("%s presentation=%+v, want order %d", key, presentation, rank)
		}
	}
	if _, err := reorderPeripheralPresentation(&profile, nil, "relay.8", 8); err == nil {
		t.Fatal("out-of-range relay order was accepted")
	}
	if _, err := reorderPeripheralPresentation(&profile, nil, "display.segment", 0); err == nil {
		t.Fatal("read-only peripheral was accepted as an ordered control")
	}
}

func TestPeripheralLockTargetsStableRelayAndSeatKeys(t *testing.T) {
	service, config := browserUIConfigTestService(t)
	config.Connection.LastDevice = &appconfig.DeviceIdentity{Port: "COM18", SerialNumber: "BOARD-42"}
	config.BoardProfiles = map[string]appconfig.BoardProfile{
		"serial:board-42": {
			Key: "cafe-cinema", Mode: appconfig.BoardModeCinemaSeatMotion,
			Presentation: map[string]appconfig.PeripheralPresentation{
				"seat.a":  {Locked: true},
				"relay.5": {Locked: true},
				"pwm.0":   {Locked: true},
			},
		},
	}
	for command, want := range map[string]string{
		"relay 5 on":         "relay.5",
		"relay side left up": "seat.a",
		"relay side b stop":  "seat.b",
		"status":             "",
	} {
		if got := peripheralKeyForCommand(command); got != want {
			t.Fatalf("command %q key=%q, want %q", command, got, want)
		}
	}
	for _, key := range []string{"seat.a", "relay.5", "pwm.0"} {
		if err := service.rejectLockedPeripheral(key); err == nil {
			t.Fatalf("locked peripheral %q was accepted", key)
		}
	}
	if err := service.rejectLockedPeripheral("relay.6"); err != nil {
		t.Fatalf("unlocked peripheral rejected: %v", err)
	}
}

func TestSemanticActionDispatchUsesTheAdvertisedProfileCatalog(t *testing.T) {
	_, cinemaControls := appconfig.ProfileDescriptors(
		appconfig.BoardModeCinemaSeatMotion,
		false,
		nil,
		nil,
	)
	for _, actionID := range []string{"seat.a.up", "seat.b.stop", "relay.5.on", "relay.8.off"} {
		control, action, ok := advertisedSemanticAction(cinemaControls, actionID)
		if !ok || action.ID != actionID || control.Key == "" {
			t.Fatalf("advertised action %q was not executable: control=%+v action=%+v ok=%t", actionID, control, action, ok)
		}
	}
	if control, action, ok := advertisedSemanticAction(cinemaControls, "relay.1.on"); ok {
		t.Fatalf("seat-internal relay unexpectedly executable: control=%+v action=%+v", control, action)
	}
	_, rawCinemaControls := appconfig.ProfileDescriptors(
		appconfig.BoardModeCinemaSeatMotion,
		true,
		nil,
		nil,
	)
	for _, actionID := range []string{"seat.a.up", "seat.b.stop", "relay.1.on", "relay.4.off"} {
		if _, _, ok := advertisedSemanticAction(rawCinemaControls, actionID); !ok {
			t.Fatalf("raw cinema action %q was not executable", actionID)
		}
	}

	_, relayControls := appconfig.ProfileDescriptors(
		appconfig.BoardModeOrdinaryRelays,
		false,
		nil,
		nil,
	)
	for _, actionID := range []string{"relay.1.on", "relay.8.off"} {
		if _, _, ok := advertisedSemanticAction(relayControls, actionID); !ok {
			t.Fatalf("ordinary-relay action %q was not executable", actionID)
		}
	}
}

func TestPeripheralSettingsCarriesTypedStripEffectDescriptors(t *testing.T) {
	settings := peripheralSettings{StripEffects: control.SupportedStripEffectDescriptors(true, native.CapabilityAddressableLED)}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		StripEffects []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			DefaultFPS  int    `json:"default_fps"`
			MinPixels   int    `json:"min_pixels"`
			MaxPixels   int    `json:"max_pixels"`
			MinFPS      int    `json:"min_fps"`
			MaxFPS      int    `json:"max_fps"`
		} `json:"strip_effects"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.StripEffects) != 3 || decoded.StripEffects[0].ID != "police" ||
		decoded.StripEffects[1].ID != "white-thunder" || decoded.StripEffects[2].ID != "converging-red" ||
		decoded.StripEffects[0].Name == "" || decoded.StripEffects[0].Description == "" ||
		decoded.StripEffects[0].DefaultFPS != 20 || decoded.StripEffects[0].MinPixels != 1 ||
		decoded.StripEffects[0].MaxPixels != native.StripMaximumPixels || decoded.StripEffects[0].MinFPS != 1 || decoded.StripEffects[0].MaxFPS != 30 {
		t.Fatalf("strip effects=%+v json=%s", decoded.StripEffects, encoded)
	}
}

func TestAddressableStripControlContractIsCapabilityGated(t *testing.T) {
	if got := advertisedStripControl(false, native.CapabilityAddressableLED); got != nil {
		t.Fatalf("disconnected strip contract=%+v", got)
	}
	if got := advertisedStripControl(true, 0); got != nil {
		t.Fatalf("unsupported strip contract=%+v", got)
	}
	got := advertisedStripControl(true, native.CapabilityAddressableLED)
	if got == nil || got.MinimumPixels != 1 || got.MaximumPixels != native.StripMaximumPixels ||
		got.DefaultPixels != native.StripMaximumPixels || got.MinimumFPS != 1 || got.MaximumFPS != 30 ||
		got.DefaultFPS != 20 || strings.Join(got.Modes, ",") != "solid,pixel,frame,rainbow,effect" {
		t.Fatalf("advertised strip contract=%+v", got)
	}
}

func TestSemanticProfileRPCRejectsMalformedParamsAsInvalidParams(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	for _, request := range []Request{
		{Method: "controller.board_profile.update", Params: json.RawMessage(`{"key":"cafe","mode":"cinema-seat-motion","unknown":true}`)},
		{Method: "controller.peripheral.presentation.update", Params: json.RawMessage(`{"key":"seat.a","unknown":true}`)},
		{Method: "controller.action.invoke", Params: json.RawMessage(`{"action_id":""}`)},
	} {
		response := service.Dispatch(context.Background(), request)
		if response.Error == nil || response.Error.Code != -32602 {
			t.Fatalf("%s response=%+v", request.Method, response)
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
