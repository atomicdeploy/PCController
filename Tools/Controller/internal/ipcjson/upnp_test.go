package ipcjson

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
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
