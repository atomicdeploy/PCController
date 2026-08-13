package ipcjson

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/discovery"
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
	mux.HandleFunc(discovery.PublicInfoPath, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writeHTTPJSON(writer, http.StatusOK, publicInfo(service, request))
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
			hostname, _ := os.Hostname()
			writeSOAP(writer, "GetStatus", fmt.Sprintf("<Healthy>true</Healthy><Hostname>%s</Hostname><Connected>%t</Connected><ConnectionState>%s</ConnectionState><Port>%s</Port><SupplyMV>%d</SupplyMV><BusMV>%d</BusMV><CurrentMA>%d</CurrentMA><PowerMW>%d</PowerMW><TemperatureLEDCentiC>%d</TemperatureLEDCentiC><TemperatureBTAudioCentiC>%d</TemperatureBTAudioCentiC><DoorOpen>%t</DoorOpen>", xmlEscape(hostname), snapshot.Connected, xmlEscape(snapshot.ConnectionState), xmlEscape(snapshot.Port.Name), snapshot.Status.SupplyMV, snapshot.Status.BusMV, snapshot.Status.CurrentMA, snapshot.Status.PowerMW, snapshot.Status.TLEDCenti, snapshot.Status.TBTCenti, snapshot.Status.DoorOpen))
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
		case "getpublicinfo":
			info := publicInfo(service, request)
			writeSOAP(writer, "GetPublicInfo", fmt.Sprintf("<PublicInfoURL>%s</PublicInfoURL><Hostname>%s</Hostname><InstanceID>%s</InstanceID><Connectable>%t</Connectable>", xmlEscape(info.Endpoints.PublicInfo), xmlEscape(info.Hostname), xmlEscape(info.InstanceID), info.Health.Connectable))
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
		_, _ = io.WriteString(writer, `<?xml version="1.0" encoding="utf-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><actionList><action><name>GetStatus</name></action><action><name>GetBoardIdentity</name></action><action><name>GetProtocolInfo</name></action><action><name>GetCommandCatalog</name></action><action><name>GetEventInfo</name></action><action><name>GetOpcodeInfo</name></action><action><name>GetPublicInfo</name></action></actionList></scpd>`)
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
		`<presentationURL>` + base + `/</presentationURL><modelURL>` + base + discovery.PublicInfoPath + `</modelURL>` +
		`</device></root>`
}

func publicInfo(service *Service, request *http.Request) discovery.PublicInfo {
	config := service.hostConfig()
	snapshot := service.Client.Snapshot()
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = request.Host
	}
	name := strings.TrimSpace(config.Integrations.Discovery.InstanceName)
	if name == "" {
		name = strings.TrimSpace(config.UI.AppTitle)
	}
	if name == "" {
		name = productidentity.DefaultAppTitle()
	}
	httpBase := "http://" + request.Host
	wsBase := "ws://" + request.Host
	protocols := enabledDiscoveryProtocols(config.Integrations.Discovery)
	broadcastPort := config.Integrations.Discovery.BroadcastPort
	if broadcastPort == 0 {
		broadcastPort = discovery.BroadcastPort
	}
	identity := discovery.PublicIdentity{}
	if snapshot.Connected {
		identity = discovery.PublicIdentity{Name: snapshot.Hello.Name, Kind: snapshot.Hello.BoardKind, Capabilities: snapshot.Hello.Capabilities, BuildHash: fmt.Sprintf("%08X", snapshot.Hello.BuildHash), BuildTimestamp: snapshot.Hello.BuildStamp}
	}
	info := discovery.PublicInfo{
		Schema: discovery.PublicInfoSchema, Product: "PCController", Protocol: "pccontroller",
		InstanceID: strings.TrimSpace(service.HostInstanceID), InstanceName: name, Hostname: hostname,
		Health: discovery.PublicHealth{OK: true, Service: productidentity.ServiceName(config.UI.AppTitle, "IPC"), Connectable: config.IPC.RemoteConnectable(), Auth: "bearer-session"},
		Host:   discovery.PublicHost{Version: strings.TrimSpace(service.HostVersion), SourceHash: strings.TrimSpace(service.HostSourceHash), BuildTime: strings.TrimSpace(service.HostBuildTime)},
		Board: discovery.PublicBoard{
			Connected: snapshot.Connected, ConnectionState: snapshot.ConnectionState, ConnectionReason: snapshot.ConnectionReason,
			Identity: identity,
			Port:     discovery.PublicPort{Name: snapshot.Port.Name, VID: snapshot.Port.VID, PID: snapshot.Port.PID, Product: snapshot.Port.Product, Manufacturer: snapshot.Port.Manufacturer, SerialNumber: snapshot.Port.SerialNumber, FriendlyName: snapshot.Port.FriendlyName, InstanceID: snapshot.Port.InstanceID},
		},
		Endpoints: discovery.PublicEndpoints{
			Web: httpBase + "/", API: httpBase + "/api/snapshot", Operations: httpBase + "/api/rpc", Commands: httpBase + "/api/commands", Events: wsBase + config.IPC.WebSocketPath, Opcodes: httpBase + "/api/opcode", WebSocket: wsBase + config.IPC.WebSocketPath, SocketIO: wsBase + config.IPC.SocketIOPath, PublicInfo: httpBase + discovery.PublicInfoPath,
		},
		Discovery: discovery.PublicDiscovery{Enabled: len(protocols) != 0, Protocols: protocols, BroadcastPort: broadcastPort},
		UpdatedAt: time.Now().UTC(),
	}
	if info.InstanceID == "" {
		info.InstanceID = strings.ToLower(strings.TrimSpace(hostname))
	}
	if snapshot.HaveStatus {
		status := snapshot.Status
		info.Board.Telemetry = discovery.PublicTelemetry{
			Available: true, INA219Available: status.INA219Available, TemperatureLEDAvailable: status.TLEDAvailable, TemperatureBTAvailable: status.TBTAvailable,
			UpdatedAt: snapshot.StatusUpdated.UTC(), UptimeMS: status.UptimeMS,
			SupplyMV: status.SupplyMV, BusMV: status.BusMV, CurrentMA: status.CurrentMA, PowerMW: status.PowerMW,
			TemperatureLEDCentiC: status.TLEDCenti, TemperatureBTAudioCentiC: status.TBTCenti,
			DoorOpen: status.DoorOpen, BluetoothAudioState: status.BluetoothState,
			ActiveRelays: status.ActiveRelays, ActiveKeys: status.ActiveKeys, MenuPage: status.MenuPage,
			ProgramMode: status.ProgramMode, ProgramRunning: status.ProgramRunning, HostOffline: status.HostOffline,
			Hot: status.Hot, PWMAvailable: status.PWMAvailable, PWMChannel: status.PWMChannel,
			PWMValue: status.PWMValue, LCDAddress: status.LCDAddress, ResetCount: status.ResetCount,
		}
	}
	return info
}

func enabledDiscoveryProtocols(value appconfig.Discovery) []string {
	result := make([]string, 0, 6)
	if value.MDNSEnabled || value.DNSSDenabled {
		result = append(result, "dns-sd")
	}
	if value.SSDPEnabled {
		result = append(result, "ssdp")
	}
	if value.UPnPEnabled {
		result = append(result, "upnp")
	}
	if value.WSDiscoveryEnabled {
		result = append(result, "ws-discovery")
	}
	if value.BroadcastEnabled {
		result = append(result, "broadcast")
	}
	if value.NetBIOSEnabled {
		result = append(result, "netbios")
	}
	return result
}

func soapActionFromBody(body []byte) string {
	text := string(body)
	for _, action := range []string{"GetStatus", "GetBoardIdentity", "GetProtocolInfo", "GetCommandCatalog", "GetEventInfo", "GetOpcodeInfo", "GetPublicInfo"} {
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
