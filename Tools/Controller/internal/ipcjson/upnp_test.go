package ipcjson

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func TestSOAPActionCatalogIncludesBoardOperationsEventsAndOpcodes(t *testing.T) {
	for _, action := range []string{"GetStatus", "GetBoardIdentity", "GetProtocolInfo", "GetCommandCatalog", "GetEventInfo", "GetOpcodeInfo", "GetPublicInfo"} {
		body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:` + action + ` xmlns:u="` + upnpServiceType + `"/></s:Body></s:Envelope>`
		if got := soapActionFromBody([]byte(body)); got != action {
			t.Fatalf("soap action %q resolved as %q", action, got)
		}
		if got, ok := resolveSOAPAction(upnpServiceType+"#"+action, []byte(body)); !ok || got != action {
			t.Fatalf("SOAP header/body action %q resolved as %q ok=%t", action, got, ok)
		}
	}
	if got := soapActionFromBody([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetStatus xmlns:u="urn:attacker"/></s:Body></s:Envelope>`)); got != "" {
		t.Fatalf("wrong-namespace SOAP action accepted as %q", got)
	}
	if _, ok := resolveSOAPAction(upnpServiceType+"#GetStatus", []byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetPublicInfo xmlns:u="`+upnpServiceType+`"/></s:Body></s:Envelope>`)); ok {
		t.Fatal("mismatched SOAP header and body action was accepted")
	}
	if strings.Contains(soapActionFromBody([]byte("<u:Unknown/>")), "Unknown") {
		t.Fatal("unknown SOAP action was accepted")
	}
}

func TestPublicTelemetryOmitsUnadvertisedKeysButPreservesValidZero(t *testing.T) {
	encoded, err := json.Marshal(discovery.PublicTelemetry{
		Available: true, INA219Present: true, INA219Available: true,
		BluetoothAudioState: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	value := string(encoded)
	if !strings.Contains(value, `"supply_mv":0`) || !strings.Contains(value, `"ina219_available":true`) {
		t.Fatalf("advertised valid zero disappeared: %s", value)
	}
	for _, absent := range []string{"bluetooth_audio_state", "door_open", "pwm_available", "temperature_led_centi_c"} {
		if strings.Contains(value, absent) {
			t.Fatalf("unadvertised key %q escaped: %s", absent, value)
		}
	}
}

func TestSOAPStatusOmitsCapabilitySpecificValuesUntilAdvertised(t *testing.T) {
	unknown := soapStatusBody("host", controllerapi.Snapshot{
		Connected: true, HaveStatus: true,
		Status: native.Status{SupplyMV: 12000, DoorOpen: true},
	})
	if strings.Contains(unknown, "SupplyMV") || strings.Contains(unknown, "DoorOpen") {
		t.Fatalf("unadvertised SOAP status leaked capability values: %s", unknown)
	}
	advertised := soapStatusBody("host", controllerapi.Snapshot{
		Connected: true, HaveStatus: true,
		Hello:  native.Hello{Capabilities: native.CapabilityINA219 | native.CapabilityRelayMotion},
		Status: native.Status{INA219Available: true, SupplyMV: 0, DoorOpen: false},
	})
	if !strings.Contains(advertised, "<SupplyMV>0</SupplyMV>") || !strings.Contains(advertised, "<DoorOpen>false</DoorOpen>") {
		t.Fatalf("advertised SOAP valid zero/false disappeared: %s", advertised)
	}
}

func TestUPnPServiceDescriptionDeclaresActionArgumentsAndStateTable(t *testing.T) {
	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal([]byte(upnpServiceDescription), &document); err != nil {
		t.Fatalf("invalid SCPD XML: %v", err)
	}
	for _, expected := range []string{
		"<serviceStateTable>", "<name>GetStatus</name>", "<name>SupplyMV</name>",
		"<name>GetEventInfo</name>", "<name>SocketIOPath</name>", "<relatedStateVariable>",
	} {
		if !strings.Contains(upnpServiceDescription, expected) {
			t.Fatalf("SCPD missing %q", expected)
		}
	}
}

func TestInvalidSOAPActionIsRejectedWithoutPublishingEvent(t *testing.T) {
	client := controllerapi.AttachSharedRuntime(control.New(control.Options{}), shell.New(8))
	defer client.Shutdown()
	service := &Service{Client: client}
	mux := http.NewServeMux()
	registerUPnPHTTP(mux, service)
	before := client.LatestEventID()
	request := httptest.NewRequest(http.MethodPost, "http://controller.test/upnp/control", strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetStatus xmlns:u="urn:attacker"/></s:Body></s:Envelope>`))
	request.Header.Set("SOAPAction", `"urn:attacker#GetStatus"`)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || client.LatestEventID() != before {
		t.Fatalf("invalid SOAP status=%d events=%d->%d body=%s", response.Code, before, client.LatestEventID(), response.Body.String())
	}
}

func TestSOAPEventInfoUsesConfiguredTransportPaths(t *testing.T) {
	config := appconfig.Defaults()
	config.IPC.WebSocketPath = "/control"
	config.IPC.SocketIOPath = "/engine.io/"
	client := controllerapi.AttachSharedRuntime(control.New(control.Options{}), shell.New(8))
	service := &Service{Client: client, HostConfig: func() appconfig.Config { return config }}
	mux := http.NewServeMux()
	registerUPnPHTTP(mux, service)
	request := httptest.NewRequest(http.MethodPost, "http://controller.test/upnp/control", strings.NewReader(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetEventInfo xmlns:u="`+upnpServiceType+`"/></s:Body></s:Envelope>`))
	request.Header.Set("SOAPAction", upnpServiceType+"#GetEventInfo")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request.WithContext(context.Background()))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<WebSocketPath>/control</WebSocketPath>") ||
		!strings.Contains(response.Body.String(), "<SocketIOPath>/engine.io/</SocketIOPath>") {
		t.Fatalf("configured GetEventInfo response=%d %s", response.Code, response.Body.String())
	}
}
