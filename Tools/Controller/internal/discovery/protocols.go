package discovery

// This file contains the wire-level adapters that complement DNS-SD and
// SSDP. Discovery is intentionally metadata-only: finding a host never grants
// access, and all API/WebSocket operations still require the configured token.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const broadcastMagic = "PCCONTROLLER-DISCOVERY/1"

const (
	soap12Namespace       = "http://www.w3.org/2003/05/soap-envelope"
	wsAddressingNamespace = "http://www.w3.org/2005/08/addressing"
	wsDiscoveryNamespace  = "http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"
	wsDiscoveryTo         = "urn:docs-oasis-open-org:ws-dd:ns:discovery:2009:01"
	wsProbeAction         = wsDiscoveryNamespace + "/Probe"
	wsProbeMatchesAction  = wsDiscoveryNamespace + "/ProbeMatches"
	wsAnonymousRole       = wsAddressingNamespace + "/anonymous"
	wsControllerNamespace = "urn:pccontroller-org:ws-discovery:1"
)

var wsMessageSequence atomic.Uint64

type broadcastPacket struct {
	Magic     string   `json:"magic"`
	Action    string   `json:"action"`
	Name      string   `json:"name"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Location  string   `json:"location,omitempty"`
	PublicURL string   `json:"public_url,omitempty"`
	USN       string   `json:"usn,omitempty"`
	TXT       []string `json:"txt,omitempty"`
}

func broadcastPacketFor(advertiser *Advertiser, action string) broadcastPacket {
	host, _ := osHostName()
	return broadcastPacket{Magic: broadcastMagic, Action: action, Name: advertiser.name,
		Host: host, Port: advertiser.port, Location: "http://" + net.JoinHostPort(host, strconv.Itoa(advertiser.port)) + "/upnp/device.xml",
		PublicURL: "http://" + net.JoinHostPort(host, strconv.Itoa(advertiser.port)) + PublicInfoPath,
		USN:       ssdpUSNWithText(advertiser.name, advertiser.port, advertiser.text()), TXT: advertiser.text()}
}

func osHostName() (string, error) {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "127.0.0.1", err
	}
	return name, nil
}

func runBroadcastAdvertiser(ctx context.Context, advertiser *Advertiser) {
	connection := advertiser.broadcastConn
	if connection == nil {
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
		case <-advertiser.broadcastRefresh:
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
			Addresses: []string{host}, Location: packet.Location, PublicURL: packet.PublicURL, USN: packet.USN, TXT: normalizeTXT(packet.TXT), SeenAt: time.Now()})
	}
}

type wsProbeEnvelope struct {
	XMLName xml.Name      `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  wsProbeHeader `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
	Body    wsProbeBody   `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}
type wsProbeHeader struct {
	Action    string `xml:"http://www.w3.org/2005/08/addressing Action"`
	MessageID string `xml:"http://www.w3.org/2005/08/addressing MessageID"`
	To        string `xml:"http://www.w3.org/2005/08/addressing To"`
}
type wsProbeBody struct {
	Probe wsProbe `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Probe"`
}
type wsProbe struct {
	XMLName xml.Name `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Probe"`
	Types   string   `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Types"`
}
type wsMatchEnvelope struct {
	XMLName xml.Name      `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Header  wsProbeHeader `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
	Body    wsMatchBody   `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}
type wsMatchBody struct {
	Matches wsMatches `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 ProbeMatches"`
}
type wsMatches struct {
	Match wsMatch `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 ProbeMatch"`
}
type wsMatch struct {
	Endpoint wsEndpointReference `xml:"http://www.w3.org/2005/08/addressing EndpointReference"`
	Types    string              `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 Types"`
	XAddrs   string              `xml:"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01 XAddrs"`
}
type wsEndpointReference struct {
	Address string `xml:"http://www.w3.org/2005/08/addressing Address"`
}

func wsProbeMessage() string {
	messageID := newWSMessageID("probe")
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="` + soap12Namespace + `" xmlns:a="` + wsAddressingNamespace + `" xmlns:d="` + wsDiscoveryNamespace + `" xmlns:pc="` + wsControllerNamespace + `"><s:Header><a:Action s:mustUnderstand="1">` + wsProbeAction + `</a:Action><a:MessageID>` + messageID + `</a:MessageID><a:To s:mustUnderstand="1">` + wsDiscoveryTo + `</a:To></s:Header><s:Body><d:Probe><d:Types>` + WSDiscoveryType + `</d:Types></d:Probe></s:Body></s:Envelope>`
}

func wsProbeResponse(advertiser *Advertiser, target *net.UDPAddr, relatesTo ...string) string {
	host := localAddressFor(target)
	base := "http://" + net.JoinHostPort(host, strconv.Itoa(advertiser.port))
	relation := ""
	if len(relatesTo) != 0 {
		relation = strings.TrimSpace(relatesTo[0])
	}
	udn := ssdpUSNWithText(advertiser.name, advertiser.port, advertiser.text())
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="` + soap12Namespace + `" xmlns:a="` + wsAddressingNamespace + `" xmlns:d="` + wsDiscoveryNamespace + `" xmlns:pc="` + wsControllerNamespace + `"><s:Header><a:Action s:mustUnderstand="1">` + wsProbeMatchesAction + `</a:Action><a:MessageID>` + newWSMessageID(udn) + `</a:MessageID><a:RelatesTo>` + escapeXMLText(relation) + `</a:RelatesTo><a:To s:mustUnderstand="1">` + wsAnonymousRole + `</a:To><d:AppSequence InstanceId="` + strconv.FormatUint(wsInstanceID(udn), 10) + `" MessageNumber="` + strconv.FormatUint(wsMessageSequence.Add(1), 10) + `"/></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>` + escapeXMLText(udn) + `</a:Address></a:EndpointReference><d:Types>` + WSDiscoveryType + `</d:Types><d:XAddrs>` + escapeXMLText(base+`/upnp/device.xml `+base+PublicInfoPath) + `</d:XAddrs><d:MetadataVersion>1</d:MetadataVersion></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`
}

func parseWSProbe(data []byte) (wsProbeEnvelope, bool) {
	var envelope wsProbeEnvelope
	if xml.Unmarshal(data, &envelope) != nil || envelope.XMLName.Space != soap12Namespace ||
		envelope.Header.Action != wsProbeAction || strings.TrimSpace(envelope.Header.MessageID) == "" ||
		envelope.Header.To != wsDiscoveryTo ||
		envelope.Body.Probe.XMLName.Space != wsDiscoveryNamespace {
		return wsProbeEnvelope{}, false
	}
	types := strings.Fields(envelope.Body.Probe.Types)
	if len(types) != 0 {
		matched := false
		for _, value := range types {
			if value == WSDiscoveryType || strings.HasSuffix(value, ":PCControllerBridge") {
				matched = true
				break
			}
		}
		if !matched {
			return wsProbeEnvelope{}, false
		}
	}
	return envelope, true
}

func newWSMessageID(seed string) string {
	sequence := wsMessageSequence.Add(1)
	digest := sha256.Sum256([]byte(seed + ":" + strconv.FormatInt(time.Now().UnixNano(), 10) + ":" + strconv.FormatUint(sequence, 10)))
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

func wsInstanceID(value string) uint64 {
	digest := sha256.Sum256([]byte(value))
	return binary.BigEndian.Uint64(digest[:8])
}

func escapeXMLText(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func runWSDiscoveryAdvertiser(ctx context.Context, advertiser *Advertiser) {
	connection := advertiser.wsListener
	if connection == nil {
		return
	}
	defer connection.Close()
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 16*1024)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr == nil {
			probe, ok := parseWSProbe(buffer[:count])
			if ok {
				_, _ = connection.WriteToUDP([]byte(wsProbeResponse(advertiser, source, probe.Header.MessageID)), source)
			}
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
		if xml.Unmarshal(buffer[:count], &envelope) != nil || envelope.XMLName.Space != soap12Namespace ||
			envelope.Header.Action != wsProbeMatchesAction || envelope.Body.Matches.Match.XAddrs == "" {
			continue
		}
		locations := strings.Fields(envelope.Body.Matches.Match.XAddrs)
		location := locations[0]
		publicURL := ""
		for _, candidate := range locations {
			if strings.HasSuffix(candidate, PublicInfoPath) {
				publicURL = candidate
			}
		}
		parsed, parseErr := neturlParse(location)
		if parseErr != nil {
			continue
		}
		host := parsed.host
		addresses := []string{}
		if source != nil {
			host = source.IP.String()
			addresses = append(addresses, host)
		}
		address := envelope.Body.Matches.Match.Endpoint.Address
		add(Instance{Protocol: "ws-discovery", Name: address, Host: host, Port: parsed.port, Addresses: addresses, Location: location, PublicURL: publicURL, USN: address, SeenAt: time.Now()})
	}
}

func runNetBIOSAdvertiser(ctx context.Context, advertiser *Advertiser) {
	// NBNS is normally owned by the operating system. If it is unavailable we
	// leave the OS service untouched; discovery still works through DNS-SD,
	// SSDP/UPnP, WS-Discovery, and the authenticated-safe UDP broadcast.
	connection := advertiser.netbiosConn
	if connection == nil {
		return
	}
	defer connection.Close()
	go func() { <-ctx.Done(); _ = connection.Close() }()
	buffer := make([]byte, 2048)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		count, source, readErr := connection.ReadFromUDP(buffer)
		if readErr == nil && isNetBIOSNodeStatusQuery(buffer[:count]) {
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
	if !isNetBIOSNodeStatusQuery(query) {
		return nil
	}
	const rdataLength = 65 // name count + one 18-byte name + RFC 1002's 46-byte statistics block
	response := make([]byte, len(query)+12+rdataLength)
	copy(response, query)
	binary.BigEndian.PutUint16(response[2:4], 0x8400)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], 1)
	binary.BigEndian.PutUint16(response[8:10], 0)
	binary.BigEndian.PutUint16(response[10:12], 0)
	index := len(query)
	response[index], response[index+1] = 0xC0, 0x0C
	binary.BigEndian.PutUint16(response[index+2:index+4], 0x0021)
	binary.BigEndian.PutUint16(response[index+4:index+6], 1)
	binary.BigEndian.PutUint32(response[index+6:index+10], 0)
	binary.BigEndian.PutUint16(response[index+10:index+12], rdataLength)
	response[index+12] = 1
	copy(response[index+13:index+28], []byte(name))
	response[index+28] = 0 // workstation service suffix
	binary.BigEndian.PutUint16(response[index+29:index+31], 0x0400)
	// The final 46 node-statistics bytes are zero-filled when unavailable.
	return response
}

func isNetBIOSNodeStatusQuery(packet []byte) bool {
	if len(packet) < 12 || binary.BigEndian.Uint16(packet[2:4])&0x8000 != 0 || binary.BigEndian.Uint16(packet[4:6]) == 0 {
		return false
	}
	index, ok := skipDNSName(packet, 12)
	return ok && index+4 <= len(packet) && binary.BigEndian.Uint16(packet[index:index+2]) == 0x0021 && binary.BigEndian.Uint16(packet[index+2:index+4]) == 1
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
			add(Instance{Protocol: "netbios", Name: name, Host: source.IP.String(), Port: 8787, Addresses: []string{source.IP.String()}, SeenAt: time.Now()})
		}
	}
}

func netbiosNodeStatusQuery() []byte {
	packet := make([]byte, 50)
	binary.BigEndian.PutUint16(packet[0:2], uint16(time.Now().UnixNano()))
	binary.BigEndian.PutUint16(packet[4:6], 1)
	packet[12] = 0x20
	var wildcard [16]byte
	wildcard[0] = '*'
	for index, value := range wildcard {
		packet[13+index*2] = 'A' + (value >> 4)
		packet[14+index*2] = 'A' + (value & 0xf)
	}
	packet[45] = 0
	binary.BigEndian.PutUint16(packet[46:48], 0x0021)
	binary.BigEndian.PutUint16(packet[48:50], 1)
	return packet
}

func parseNetBIOSNodeStatus(packet []byte) string {
	if len(packet) < 12 || binary.BigEndian.Uint16(packet[2:4])&0x8000 == 0 {
		return ""
	}
	index := 12
	questions := int(binary.BigEndian.Uint16(packet[4:6]))
	for range questions {
		var ok bool
		index, ok = skipDNSName(packet, index)
		if !ok || index+4 > len(packet) {
			return ""
		}
		index += 4
	}
	answers := int(binary.BigEndian.Uint16(packet[6:8]))
	for range answers {
		var ok bool
		index, ok = skipDNSName(packet, index)
		if !ok || index+10 > len(packet) {
			return ""
		}
		typeCode := binary.BigEndian.Uint16(packet[index : index+2])
		rdataLength := int(binary.BigEndian.Uint16(packet[index+8 : index+10]))
		index += 10
		if index+rdataLength > len(packet) {
			return ""
		}
		if typeCode == 0x0021 && rdataLength >= 19 {
			count := int(packet[index])
			if count > 0 && 1+count*18 <= rdataLength {
				return strings.TrimRight(strings.TrimSpace(string(packet[index+1:index+16])), "\x00 ")
			}
		}
		index += rdataLength
	}
	return ""
}

func skipDNSName(packet []byte, index int) (int, bool) {
	for {
		if index >= len(packet) {
			return 0, false
		}
		length := int(packet[index])
		if length&0xC0 == 0xC0 {
			if index+2 > len(packet) {
				return 0, false
			}
			return index + 2, true
		}
		index++
		if length == 0 {
			return index, true
		}
		if length > 63 || index+length > len(packet) {
			return 0, false
		}
		index += length
	}
}

func probeController(ctx context.Context, host string, port int) bool {
	requestContext, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+"/healthz", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Transport: publicHTTPTransport}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}
