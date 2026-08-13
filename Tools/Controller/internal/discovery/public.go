package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// PublicInfoPath is the bounded, unauthenticated device-directory document.
	// It contains health and identity data only; control remains authenticated.
	PublicInfoPath   = "/upnp/public.json"
	PublicInfoSchema = "pccontroller.public.v1"
)

var publicHTTPTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Discovery is link-local/LAN traffic. Never send a public device document
	// or its destination through an Internet proxy inherited from the shell.
	transport.Proxy = nil
	return transport
}()

type PublicInfo struct {
	Schema       string          `json:"schema"`
	Product      string          `json:"product"`
	Protocol     string          `json:"protocol"`
	InstanceID   string          `json:"instance_id"`
	InstanceName string          `json:"instance_name"`
	Hostname     string          `json:"hostname"`
	Health       PublicHealth    `json:"health"`
	Host         PublicHost      `json:"host"`
	Board        PublicBoard     `json:"board"`
	Endpoints    PublicEndpoints `json:"endpoints"`
	Discovery    PublicDiscovery `json:"discovery"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type PublicHealth struct {
	OK          bool   `json:"ok"`
	Service     string `json:"service"`
	Connectable bool   `json:"connectable"`
	Auth        string `json:"auth"`
}

type PublicHost struct {
	Version    string `json:"version,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
	BuildTime  string `json:"build_time,omitempty"`
}

type PublicBoard struct {
	Connected        bool            `json:"connected"`
	ConnectionState  string          `json:"connection_state"`
	ConnectionReason string          `json:"connection_reason,omitempty"`
	Identity         PublicIdentity  `json:"identity"`
	Port             PublicPort      `json:"port"`
	Telemetry        PublicTelemetry `json:"telemetry"`
}

type PublicIdentity struct {
	Name           string `json:"name,omitempty"`
	Kind           byte   `json:"kind,omitempty"`
	Capabilities   uint32 `json:"capabilities,omitempty"`
	BuildHash      string `json:"build_hash,omitempty"`
	BuildTimestamp string `json:"build_timestamp,omitempty"`
}

type PublicPort struct {
	Name         string `json:"name,omitempty"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	Product      string `json:"product,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	FriendlyName string `json:"friendly_name,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
}

type PublicTelemetry struct {
	Available                bool      `json:"available"`
	INA219Available          bool      `json:"ina219_available"`
	TemperatureLEDAvailable  bool      `json:"temperature_led_available"`
	TemperatureBTAvailable   bool      `json:"temperature_bt_audio_available"`
	UpdatedAt                time.Time `json:"updated_at,omitempty"`
	UptimeMS                 uint32    `json:"uptime_ms,omitempty"`
	SupplyMV                 int32     `json:"supply_mv,omitempty"`
	BusMV                    int32     `json:"bus_mv,omitempty"`
	CurrentMA                int32     `json:"current_ma,omitempty"`
	PowerMW                  int32     `json:"power_mw,omitempty"`
	TemperatureLEDCentiC     int16     `json:"temperature_led_centi_c,omitempty"`
	TemperatureBTAudioCentiC int16     `json:"temperature_bt_audio_centi_c,omitempty"`
	DoorOpen                 bool      `json:"door_open"`
	BluetoothAudioState      byte      `json:"bluetooth_audio_state,omitempty"`
	ActiveRelays             byte      `json:"active_relays,omitempty"`
	ActiveKeys               byte      `json:"active_keys,omitempty"`
	MenuPage                 byte      `json:"menu_page,omitempty"`
	ProgramMode              byte      `json:"program_mode,omitempty"`
	ProgramRunning           bool      `json:"program_running"`
	HostOffline              bool      `json:"host_offline"`
	Hot                      bool      `json:"hot"`
	PWMAvailable             bool      `json:"pwm_available"`
	PWMChannel               byte      `json:"pwm_channel,omitempty"`
	PWMValue                 uint16    `json:"pwm_value,omitempty"`
	LCDAddress               byte      `json:"lcd_address,omitempty"`
	ResetCount               uint32    `json:"reset_count,omitempty"`
}

type PublicEndpoints struct {
	Web        string `json:"web"`
	API        string `json:"api"`
	Operations string `json:"operations"`
	Commands   string `json:"commands"`
	Events     string `json:"events"`
	Opcodes    string `json:"opcodes"`
	WebSocket  string `json:"websocket"`
	SocketIO   string `json:"socket_io"`
	PublicInfo string `json:"public_info"`
}

type PublicDiscovery struct {
	Enabled       bool     `json:"enabled"`
	Protocols     []string `json:"protocols"`
	BroadcastPort int      `json:"broadcast_port"`
}

type Source struct {
	Protocol string    `json:"protocol"`
	Host     string    `json:"host,omitempty"`
	Port     int       `json:"port,omitempty"`
	Location string    `json:"location,omitempty"`
	SeenAt   time.Time `json:"seen_at"`
}

func (info PublicInfo) Valid() bool {
	return info.Schema == PublicInfoSchema && strings.EqualFold(info.Product, "PCController") &&
		strings.TrimSpace(info.Hostname) != ""
}

func publicURLCandidates(instance Instance) []string {
	hosts := append([]string(nil), instance.Addresses...)
	if len(hosts) == 0 && strings.TrimSpace(instance.Host) != "" {
		hosts = append(hosts, instance.Host)
	}
	result := make([]string, 0, len(hosts)+1)
	if strings.TrimSpace(instance.PublicURL) != "" && trustedDiscoveryURL(instance.PublicURL, instance) {
		result = append(result, instance.PublicURL)
	}
	if strings.TrimSpace(instance.Location) != "" {
		if parsed, err := url.Parse(instance.Location); err == nil && parsed.Host != "" && trustedDiscoveryURL(instance.Location, instance) {
			parsed.Path, parsed.RawQuery, parsed.Fragment = PublicInfoPath, "", ""
			result = append(result, parsed.String())
		}
	}
	for _, host := range hosts {
		if instance.Port > 0 && strings.TrimSpace(host) != "" {
			result = append(result, "http://"+net.JoinHostPort(strings.TrimSuffix(host, "."), strconv.Itoa(instance.Port))+PublicInfoPath)
		}
	}
	return uniqueStrings(result)
}

func trustedDiscoveryURL(candidate string, instance Instance) bool {
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	port := 80
	if raw := parsed.Port(); raw != "" {
		port, err = strconv.Atoi(raw)
		if err != nil {
			return false
		}
	}
	if instance.Port > 0 && port != instance.Port {
		return false
	}
	allowed := append([]string(nil), instance.Addresses...)
	if len(allowed) == 0 {
		allowed = append(allowed, instance.Host)
	}
	candidateHost := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	for _, value := range allowed {
		if candidateHost == strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), ".")) {
			return true
		}
	}
	return false
}

func enrichInstance(ctx context.Context, instance Instance) Instance {
	client := &http.Client{
		Transport: publicHTTPTransport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !trustedDiscoveryURL(request.URL.String(), instance) {
				return errors.New("discovery public-info redirect left the responder endpoint")
			}
			return nil
		},
	}
	for _, candidate := range publicURLCandidates(instance) {
		requestContext, cancel := context.WithTimeout(ctx, 650*time.Millisecond)
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, candidate, nil)
		if err != nil {
			cancel()
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			cancel()
			continue
		}
		var info PublicInfo
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&info)
		_ = response.Body.Close()
		cancel()
		if response.StatusCode < 200 || response.StatusCode >= 300 || decodeErr != nil || !info.Valid() {
			continue
		}
		instance.Public = &info
		instance.PublicURL = candidate
		if info.InstanceName != "" {
			instance.Name = info.InstanceName
		}
		return instance
	}
	if info := publicInfoFromTXT(instance.TXT); info.Valid() {
		absolutizePublicInfo(&info, instance)
		instance.Public = &info
		instance.PublicURL = info.Endpoints.PublicInfo
	}
	return instance
}

func publicInfoFromTXT(values []string) PublicInfo {
	items := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, ok := strings.Cut(value, "=")
		if ok {
			items[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(raw)
		}
	}
	if !strings.EqualFold(items["product"], "PCController") || items["host.hostname"] == "" {
		return PublicInfo{}
	}
	connected, _ := strconv.ParseBool(items["board.connected"])
	connectable, _ := strconv.ParseBool(items["remote.connectable"])
	telemetryAvailable := items["board.status_at"] != "" || items["board.supply_mv"] != ""
	return PublicInfo{
		Schema: PublicInfoSchema, Product: "PCController", Protocol: items["protocol"],
		InstanceID: items["instance.id"], InstanceName: items["instance.name"], Hostname: items["host.hostname"],
		Health: PublicHealth{OK: items["health"] == "ok", Service: items["service"], Connectable: connectable, Auth: items["auth"]},
		Host:   PublicHost{Version: items["host.version"], SourceHash: items["host.source_hash"], BuildTime: items["host.build_time"]},
		Board: PublicBoard{Connected: connected, ConnectionState: items["board.connection_state"], Identity: PublicIdentity{
			Name: items["board.name"], BuildHash: items["board.build_hash"], BuildTimestamp: items["board.build_timestamp"], Capabilities: parseUint32(items["board.capabilities"], 16),
		}, Port: PublicPort{Name: items["board.port"], Product: items["board.port_product"]}, Telemetry: PublicTelemetry{
			Available: telemetryAvailable, INA219Available: items["board.supply_mv"] != "", TemperatureLEDAvailable: items["board.temperature_led_centi_c"] != "", TemperatureBTAvailable: items["board.temperature_bt_audio_centi_c"] != "",
			UpdatedAt: parseTime(items["board.status_at"]), UptimeMS: parseUint32(items["board.uptime_ms"], 10),
			SupplyMV: parseInt32(items["board.supply_mv"]), BusMV: parseInt32(items["board.bus_mv"]), CurrentMA: parseInt32(items["board.current_ma"]), PowerMW: parseInt32(items["board.power_mw"]),
			TemperatureLEDCentiC: parseInt16(items["board.temperature_led_centi_c"]), TemperatureBTAudioCentiC: parseInt16(items["board.temperature_bt_audio_centi_c"]),
			DoorOpen: parseBool(items["board.door_open"]), BluetoothAudioState: byte(parseUint32(items["board.bluetooth_audio_state"], 10)), ActiveRelays: byte(parseUint32(items["board.active_relays"], 10)), ActiveKeys: byte(parseUint32(items["board.active_keys"], 10)),
			MenuPage: byte(parseUint32(items["board.menu_page"], 10)), ProgramMode: byte(parseUint32(items["board.program_mode"], 10)), ProgramRunning: parseBool(items["board.program_running"]), HostOffline: parseBool(items["board.host_offline"]), Hot: parseBool(items["board.hot"]),
			PWMAvailable: parseBool(items["board.pwm_available"]), PWMChannel: byte(parseUint32(items["board.pwm_channel"], 10)), PWMValue: uint16(parseUint32(items["board.pwm_value"], 10)), LCDAddress: byte(parseUint32(items["board.lcd_address"], 10)), ResetCount: parseUint32(items["board.reset_count"], 10),
		}},
		Endpoints: PublicEndpoints{Web: items["web"], API: items["api"], Operations: items["operations"], Commands: items["commands"], Events: items["events"], Opcodes: items["opcodes"], WebSocket: items["ws"], SocketIO: items["socketio"], PublicInfo: items["public"]},
	}
}

func absolutizePublicInfo(info *PublicInfo, instance Instance) {
	if info == nil || instance.Port < 1 {
		return
	}
	host := strings.TrimSpace(instance.Host)
	if len(instance.Addresses) != 0 {
		host = instance.Addresses[0]
	}
	if host == "" {
		return
	}
	httpBase := "http://" + net.JoinHostPort(strings.TrimSuffix(host, "."), strconv.Itoa(instance.Port))
	wsBase := "ws://" + net.JoinHostPort(strings.TrimSuffix(host, "."), strconv.Itoa(instance.Port))
	absolute := func(value, base string) string {
		if strings.HasPrefix(value, "/") {
			return base + value
		}
		return value
	}
	info.Endpoints.Web = absolute(info.Endpoints.Web, httpBase)
	info.Endpoints.API = absolute(info.Endpoints.API, httpBase)
	info.Endpoints.Operations = absolute(info.Endpoints.Operations, httpBase)
	info.Endpoints.Commands = absolute(info.Endpoints.Commands, httpBase)
	info.Endpoints.Opcodes = absolute(info.Endpoints.Opcodes, httpBase)
	info.Endpoints.WebSocket = absolute(info.Endpoints.WebSocket, wsBase)
	info.Endpoints.SocketIO = absolute(info.Endpoints.SocketIO, wsBase)
	info.Endpoints.PublicInfo = absolute(info.Endpoints.PublicInfo, httpBase)
}

func parseUint32(value string, base int) uint32 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), base, 32)
	return uint32(parsed)
}

func parseInt32(value string) int32 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	return int32(parsed)
}

func parseInt16(value string) int16 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 16)
	return int16(parsed)
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return parsed
}

func mergeInstances(values []Instance) []Instance {
	result := make([]Instance, 0, len(values))
	for _, value := range values {
		match := -1
		for index := range result {
			if sameInstance(result[index], value) {
				match = index
				break
			}
		}
		if match < 0 {
			value.Protocols = uniqueStrings(append(value.Protocols, value.Protocol))
			value.Sources = []Source{{Protocol: value.Protocol, Host: value.Host, Port: value.Port, Location: value.Location, SeenAt: value.SeenAt}}
			result = append(result, value)
			continue
		}
		current := result[match]
		current.Protocols = uniqueStrings(append(current.Protocols, value.Protocol))
		current.Sources = append(current.Sources, Source{Protocol: value.Protocol, Host: value.Host, Port: value.Port, Location: value.Location, SeenAt: value.SeenAt})
		current.Addresses = uniqueStrings(append(current.Addresses, value.Addresses...))
		current.TXT = uniqueStrings(append(current.TXT, value.TXT...))
		if current.Public == nil && value.Public != nil {
			current.Public, current.PublicURL = value.Public, value.PublicURL
			current.Name = value.Name
		}
		if value.SeenAt.After(current.SeenAt) {
			current.SeenAt = value.SeenAt
		}
		result[match] = current
	}
	for index := range result {
		sort.Strings(result[index].Protocols)
		sort.Slice(result[index].Sources, func(i, j int) bool { return result[index].Sources[i].Protocol < result[index].Sources[j].Protocol })
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
		if left == right {
			return instanceKey(result[i]) < instanceKey(result[j])
		}
		return left < right
	})
	return result
}

func sameInstance(left, right Instance) bool {
	if left.Public != nil && right.Public != nil && left.Public.InstanceID != "" && right.Public.InstanceID != "" {
		return strings.EqualFold(left.Public.InstanceID, right.Public.InstanceID)
	}
	leftUSN := strings.TrimSpace(strings.Split(left.USN, "::")[0])
	rightUSN := strings.TrimSpace(strings.Split(right.USN, "::")[0])
	if leftUSN != "" && rightUSN != "" && strings.EqualFold(leftUSN, rightUSN) {
		return true
	}
	if left.Port != right.Port {
		return false
	}
	leftHosts := append([]string{left.Host}, left.Addresses...)
	rightHosts := append([]string{right.Host}, right.Addresses...)
	if left.Public != nil {
		leftHosts = append(leftHosts, left.Public.Hostname)
	}
	if right.Public != nil {
		rightHosts = append(rightHosts, right.Public.Hostname)
	}
	for _, leftHost := range leftHosts {
		leftHost = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(leftHost), "."))
		for _, rightHost := range rightHosts {
			rightHost = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rightHost), "."))
			if leftHost != "" && leftHost == rightHost {
				return true
			}
		}
	}
	return false
}

func instanceKey(value Instance) string {
	if value.Public != nil {
		if id := strings.ToLower(strings.TrimSpace(value.Public.InstanceID)); id != "" {
			return "id:" + id
		}
		if host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value.Public.Hostname), ".")); host != "" {
			return fmt.Sprintf("host:%s:%d", host, value.Port)
		}
	}
	if usn := strings.ToLower(strings.TrimSpace(strings.Split(value.USN, "::")[0])); usn != "" {
		return "usn:" + usn
	}
	return fmt.Sprintf("endpoint:%s:%d", strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value.Host), ".")), value.Port)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
