package discovery

// This file contains the wire-level adapters that complement DNS-SD and
// SSDP. Discovery is intentionally metadata-only: finding a host never grants
// access, and all API/WebSocket operations still require the configured token.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const broadcastMagic = "PCCONTROLLER-DISCOVERY/1"

type broadcastPacket struct {
	Magic    string   `json:"magic"`
	Action   string   `json:"action"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Location string   `json:"location,omitempty"`
	USN      string   `json:"usn,omitempty"`
	TXT      []string `json:"txt,omitempty"`
}

func broadcastPacketFor(advertiser *Advertiser, action string) broadcastPacket {
	host, _ := osHostName()
	return broadcastPacket{Magic: broadcastMagic, Action: action, Name: advertiser.name,
		Host: host, Port: advertiser.port, Location: "http://" + net.JoinHostPort(host, strconv.Itoa(advertiser.port)) + "/upnp/device.xml",
		USN: ssdpUSN(advertiser.name, advertiser.port), TXT: advertiser.text()}
}

func osHostName() (string, error) {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "127.0.0.1", err
	}
	return name, nil
}

func runBroadcastAdvertiser(ctx context.Context, advertiser *Advertiser) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: advertiser.broadcastPort})
	if err != nil {
		return
	}
	defer connection.Close()
	_ = connection.SetReadBuffer(64 * 1024)
	go func() { <-ctx.Done(); _ = connection.Close() }()
	beacon := func(target *net.UDPAddr) {
		payload, _ := json.Marshal(broadcastPacketFor(advertiser, "announce"))
		_, _ = connection.WriteToUDP(payload, target)
	}
	beacon(&net.UDPAddr{IP: net.IPv4bcast, Port: advertiser.broadcastPort})
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buffer := make([]byte, 8192)
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr == nil {
			var packet broadcastPacket
			if json.Unmarshal(buffer[:count], &packet) == nil && packet.Magic == broadcastMagic && packet.Action == "discover" {
				beacon(source)
			}
		} else if netErr, ok := readErr.(net.Error); !ok || !netErr.Timeout() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-advertiser.refresh:
			beacon(&net.UDPAddr{IP: net.IPv4bcast, Port: advertiser.broadcastPort})
		case <-ticker.C:
			beacon(&net.UDPAddr{IP: net.IPv4bcast, Port: advertiser.broadcastPort})
		default:
		}
	}
}

func discoverBroadcast(ctx context.Context, port int, add func(Instance)) error {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("listen broadcast discovery: %w", err)
	}
	defer connection.Close()
	_ = connection.SetWriteBuffer(64 * 1024)
	payload, _ := json.Marshal(broadcastPacket{Magic: broadcastMagic, Action: "discover"})
	if _, err := connection.WriteToUDP(payload, &net.UDPAddr{IP: net.IPv4bcast, Port: port}); err != nil {
		return fmt.Errorf("broadcast discovery: %w", err)
	}
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 8192)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return readErr
		}
		var packet broadcastPacket
		if json.Unmarshal(buffer[:count], &packet) != nil || packet.Magic != broadcastMagic || packet.Action != "announce" {
			continue
		}
		host := packet.Host
		if source != nil {
			host = source.IP.String()
		}
		add(Instance{Protocol: "broadcast", Name: packet.Name, Host: host, Port: packet.Port,
			Location: packet.Location, USN: packet.USN, TXT: normalizeTXT(packet.TXT), SeenAt: time.Now()})
	}
}

type wsProbeEnvelope struct {
	Body wsProbeBody `xml:"Body"`
}
type wsProbeBody struct {
	Probe wsProbe `xml:"Probe"`
}
type wsProbe struct {
	Types string `xml:"Types"`
}
type wsMatchEnvelope struct {
	Body wsMatchBody `xml:"Body"`
}
type wsMatchBody struct {
	Matches wsMatches `xml:"ProbeMatches"`
}
type wsMatches struct {
	Match wsMatch `xml:"ProbeMatch"`
}
type wsMatch struct {
	Address string `xml:"Address"`
	Types   string `xml:"Types"`
	XAddrs  string `xml:"XAddrs"`
}

func wsProbeMessage() string {
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"><s:Header/><s:Body><d:Probe><d:Types>` + WSDiscoveryType + `</d:Types></d:Probe></s:Body></s:Envelope>`
}

func wsProbeResponse(advertiser *Advertiser, target *net.UDPAddr) string {
	host := localAddressFor(target)
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"><s:Header/><s:Body><d:ProbeMatches><d:ProbeMatch><d:Address>` + ssdpUSN(advertiser.name, advertiser.port) + `</d:Address><d:Types>` + WSDiscoveryType + `</d:Types><d:XAddrs>http://` + net.JoinHostPort(host, strconv.Itoa(advertiser.port)) + `/upnp/device.xml</d:XAddrs><d:MetadataVersion>1</d:MetadataVersion></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`
}

func runWSDiscoveryAdvertiser(ctx context.Context, advertiser *Advertiser) {
	group, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddress)
	if err != nil {
		return
	}
	connection, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return
	}
	defer connection.Close()
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 16*1024)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr == nil && strings.Contains(string(buffer[:count]), "Probe") {
			_, _ = connection.WriteToUDP([]byte(wsProbeResponse(advertiser, source)), source)
		} else if readErr != nil {
			if netErr, ok := readErr.(net.Error); !ok || !netErr.Timeout() {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func discoverWSDiscovery(ctx context.Context, add func(Instance)) error {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("listen WS-Discovery: %w", err)
	}
	defer connection.Close()
	group, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddress)
	if err != nil {
		return err
	}
	if _, err := connection.WriteToUDP([]byte(wsProbeMessage()), group); err != nil {
		return fmt.Errorf("send WS-Discovery probe: %w", err)
	}
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 16*1024)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return readErr
		}
		var envelope wsMatchEnvelope
		if xml.Unmarshal(buffer[:count], &envelope) != nil || envelope.Body.Matches.Match.XAddrs == "" {
			continue
		}
		location := strings.Fields(envelope.Body.Matches.Match.XAddrs)[0]
		parsed, parseErr := neturlParse(location)
		if parseErr != nil {
			continue
		}
		host := parsed.host
		if host == "" && source != nil {
			host = source.IP.String()
		}
		add(Instance{Protocol: "ws-discovery", Name: envelope.Body.Matches.Match.Address, Host: host, Port: parsed.port, Location: location, USN: envelope.Body.Matches.Match.Address, SeenAt: time.Now()})
	}
}

func runNetBIOSAdvertiser(ctx context.Context, advertiser *Advertiser) {
	// NBNS is normally owned by the operating system. If it is unavailable we
	// leave the OS service untouched; discovery still works through DNS-SD,
	// SSDP/UPnP, WS-Discovery, and the authenticated-safe UDP broadcast.
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: NetBIOSNameServicePort})
	if err != nil {
		<-ctx.Done()
		return
	}
	defer connection.Close()
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 2048)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr == nil && count >= 50 && binary.BigEndian.Uint16(buffer[46:48]) == 0x0021 {
			_, _ = connection.WriteToUDP(netbiosNodeStatusResponse(buffer[:count], advertiser.name), source)
		} else if readErr != nil {
			if netErr, ok := readErr.(net.Error); !ok || !netErr.Timeout() {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func netbiosNodeStatusResponse(query []byte, name string) []byte {
	name = strings.ToUpper(strings.TrimSpace(name))
	if len(name) > 15 {
		name = name[:15]
	}
	name = fmt.Sprintf("%-15s", name)
	response := make([]byte, 81)
	copy(response[0:2], query[0:2])
	binary.BigEndian.PutUint16(response[2:4], 0x8500)
	binary.BigEndian.PutUint16(response[6:8], 1)
	response[12], response[13] = 0xC0, 0x0C
	binary.BigEndian.PutUint16(response[14:16], 0x0021)
	binary.BigEndian.PutUint16(response[16:18], 1)
	binary.BigEndian.PutUint32(response[18:22], 0)
	binary.BigEndian.PutUint16(response[22:24], 19)
	response[24] = 1
	copy(response[25:40], []byte(name))
	response[40] = 0
	// The final six statistics bytes are reserved and zero-filled.
	return response
}

func discoverNetBIOS(ctx context.Context, add func(Instance)) error {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("listen NetBIOS discovery: %w", err)
	}
	defer connection.Close()
	query := netbiosNodeStatusQuery()
	if _, err := connection.WriteToUDP(query, &net.UDPAddr{IP: net.IPv4bcast, Port: NetBIOSNameServicePort}); err != nil {
		return fmt.Errorf("send NetBIOS node-status query: %w", err)
	}
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 4096)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return readErr
		}
		name := parseNetBIOSNodeStatus(buffer[:count])
		if name == "" || source == nil {
			continue
		}
		if probeController(ctx, source.IP.String(), 8787) {
			add(Instance{Protocol: "netbios", Name: name, Host: source.IP.String(), Port: 8787, SeenAt: time.Now()})
		}
	}
}

func netbiosNodeStatusQuery() []byte {
	packet := make([]byte, 50)
	binary.BigEndian.PutUint16(packet[0:2], uint16(time.Now().UnixNano()))
	binary.BigEndian.PutUint16(packet[4:6], 1)
	packet[12] = 0x20
	for index, value := range []byte("*                              ") {
		if index >= 16 {
			break
		}
		packet[13+index*2] = 'A' + (value >> 4)
		packet[14+index*2] = 'A' + (value & 0xf)
	}
	packet[45] = 0
	binary.BigEndian.PutUint16(packet[46:48], 0x0021)
	binary.BigEndian.PutUint16(packet[48:50], 1)
	return packet
}

func parseNetBIOSNodeStatus(packet []byte) string {
	if len(packet) < 57 || binary.BigEndian.Uint16(packet[2:4])&0x8000 == 0 {
		return ""
	}
	index := 12
	if index >= len(packet) || packet[index] == 0 {
		return ""
	}
	index += int(packet[index]) + 2 // length byte, encoded name, terminator
	if index+4 > len(packet) {
		return ""
	}
	index += 4 // QTYPE + QCLASS
	if index+12 > len(packet) {
		return ""
	}
	index += 12 // answer name/type/class/TTL/RDLENGTH
	count := int(packet[index])
	index++
	if count == 0 || index+18 > len(packet) {
		return ""
	}
	name := strings.TrimSpace(string(packet[index : index+15]))
	return strings.TrimRight(name, "\x00 ")
}

func probeController(ctx context.Context, host string, port int) bool {
	requestContext, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+"/healthz", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}
