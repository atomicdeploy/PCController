package ipcjson

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"pccontroller.local/controller/internal/productidentity"
)

const upnpServiceType = "urn:pccontroller-org:service:Controller:1"

func registerUPnPHTTP(mux *http.ServeMux, service *Service) {
	if mux == nil || service == nil {
		return
	}
	mux.HandleFunc("/upnp/device.xml", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = io.WriteString(writer, upnpDeviceDescription(service, request))
	})
	mux.HandleFunc("/upnp/control", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(request.Body, 64*1024))
		action := strings.Trim(request.Header.Get("SOAPAction"), "\"")
		if action == "" {
			action = soapActionFromBody(body)
		}
		if !strings.Contains(action, "#") {
			action = upnpServiceType + "#" + action
		}
		service.Client.EmitHostActionEvent("upnp.soap.action", action, "upnp", "soap", map[string]string{"service": upnpServiceType, "action": action})
		snapshot := service.Client.Snapshot()
		writer.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch strings.ToLower(action[strings.LastIndex(action, "#")+1:]) {
		case "getstatus":
			writeSOAP(writer, "GetStatus", fmt.Sprintf("<Connected>%t</Connected><ConnectionState>%s</ConnectionState><Port>%s</Port>", snapshot.Connected, xmlEscape(snapshot.ConnectionState), xmlEscape(snapshot.Port.Name)))
		case "getboardidentity":
			writeSOAP(writer, "GetBoardIdentity", fmt.Sprintf("<BoardName>%s</BoardName><BuildHash>%08X</BuildHash><BuildStamp>%s</BuildStamp>", xmlEscape(snapshot.Hello.Name), snapshot.Hello.BuildHash, xmlEscape(snapshot.Hello.BuildStamp)))
		case "getprotocolinfo":
			writeSOAP(writer, "GetProtocolInfo", "<Protocol>PCController JSON-RPC 2.0 over HTTP/WebSocket/Socket.IO</Protocol><Authentication>Bearer token required for control</Authentication>")
		case "getcommandcatalog":
			writeSOAP(writer, "GetCommandCatalog", fmt.Sprintf("<CommandCatalogURL>/api/commands</CommandCatalogURL><CommandCount>%d</CommandCount><Authentication>Bearer token required for control</Authentication>", len(service.Client.CommandCatalog())))
		case "geteventinfo":
			writeSOAP(writer, "GetEventInfo", "<WebSocketPath>/ipc</WebSocketPath><SocketIOPath>/socket.io/</SocketIOPath><Topics>events,state,debug,status,opcodes</Topics><Authentication>Bearer token required for control</Authentication>")
		case "getopcodeinfo":
			writeSOAP(writer, "GetOpcodeInfo", "<OpcodeEndpoint>/api/opcode</OpcodeEndpoint><OpcodeRPC>controller.opcode.send,controller.opcode.exchange,controller.opcode.request</OpcodeRPC><OpcodeEvents>controller.opcode</OpcodeEvents><Authentication>Bearer token required for control</Authentication>")
		default:
			writer.WriteHeader(http.StatusInternalServerError)
			writeSOAPFault(writer, "Invalid Action")
		}
	})
	mux.HandleFunc("/upnp/scpd.xml", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = io.WriteString(writer, `<?xml version="1.0" encoding="utf-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetStatus</name></action><action><name>GetBoardIdentity</name></action><action><name>GetProtocolInfo</name></action><action><name>GetCommandCatalog</name></action><action><name>GetEventInfo</name></action><action><name>GetOpcodeInfo</name></action></actionList></scpd>`)
	})
	mux.HandleFunc("/upnp/events", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Allow", "SUBSCRIBE, UNSUBSCRIBE")
		writer.WriteHeader(http.StatusNotImplemented)
		writeSOAPFault(writer, "event subscriptions are delivered through authenticated WebSocket or Socket.IO")
	})
}

func upnpDeviceDescription(service *Service, request *http.Request) string {
	config := service.hostConfig()
	name := config.UI.AppTitle
	if strings.TrimSpace(name) == "" {
		name = productidentity.DefaultAppTitle()
	}
	base := "http://" + request.Host
	udn := strings.TrimSpace(service.HostInstanceID)
	if udn == "" {
		udn = "pccontroller-" + strings.NewReplacer(":", "-", ".", "-", "[", "", "]", "").Replace(request.Host)
	}
	return `<?xml version="1.0" encoding="utf-8"?>` +
		`<root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>1</minor></specVersion>` +
		`<device><deviceType>urn:schemas-upnp-org:device:Basic:1</deviceType><friendlyName>` + xmlEscape(name) + `</friendlyName>` +
		`<manufacturer>AtomicDeploy</manufacturer><modelName>PCController</modelName><UDN>uuid:` + xmlEscape(udn) + `</UDN>` +
		`<serviceList><service><serviceType>` + upnpServiceType + `</serviceType><serviceId>urn:upnp-org:serviceId:Controller</serviceId>` +
		`<controlURL>` + base + `/upnp/control</controlURL><eventSubURL>` + base + `/upnp/events</eventSubURL><SCPDURL>` + base + `/upnp/scpd.xml</SCPDURL></service></serviceList>` +
		`</device></root>`
}

func soapActionFromBody(body []byte) string {
	text := string(body)
	for _, action := range []string{"GetStatus", "GetBoardIdentity", "GetProtocolInfo", "GetCommandCatalog", "GetEventInfo", "GetOpcodeInfo"} {
		if strings.Contains(text, action) {
			return action
		}
	}
	return ""
}

func writeSOAP(writer http.ResponseWriter, action, payload string) {
	_, _ = fmt.Fprintf(writer, `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, action, upnpServiceType, payload, action)
}

func writeSOAPFault(writer http.ResponseWriter, message string) {
	_, _ = fmt.Fprintf(writer, `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>%s</faultstring></s:Fault></s:Body></s:Envelope>`, xmlEscape(message))
}

func xmlEscape(value string) string { return html.EscapeString(strings.TrimSpace(value)) }
