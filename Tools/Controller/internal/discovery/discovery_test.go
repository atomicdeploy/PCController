package discovery

import (
	"context"
	"encoding/binary"
	"encoding/xml"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSSDPAdvertisementContainsOnlyNonSecretServiceMetadata(t *testing.T) {
	packet := ssdpNotifyWithText(
		"Lab\r\nInjected: no", 8787, "ssdp:alive",
		[]string{"web=/", "board.connected=true", "auth_token=forbidden"},
	)
	for _, expected := range []string{
		"NOTIFY * HTTP/1.1", "NT: " + SSDPType,
		"NTS: ssdp:alive", "X-PCController-Name: Lab  Injected: no",
		"X-PCController-Meta: web=/",
		"X-PCController-Meta: board.connected=true",
	} {
		if !strings.Contains(packet, expected) {
			t.Fatalf("SSDP packet missing %q:\n%s", expected, packet)
		}
	}
	for _, forbidden := range []string{"auth_token", "forbidden", "access_token", "Authorization:"} {
		if strings.Contains(packet, forbidden) {
			t.Fatalf("SSDP packet exposed %q", forbidden)
		}
	}
}

func TestDiscoveryOptionsNormalizeDNSAndUPnPAliases(t *testing.T) {
	options := (Options{DNSSD: true, UPnP: true}).normalized()
	if !options.MDNS || !options.SSDP || options.BroadcastPort != BroadcastPort {
		t.Fatalf("normalized options=%#v", options)
	}
}

func TestBroadcastAndWSDiscoveryPacketsAreSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	advertiser, err := AdvertiseWithOptions(ctx, "Workshop", 8787, Options{}, []string{"instance.id=0123456789abcdef0123456789abcdef", "board.name=Alpha", "auth_token=hidden"})
	if err != nil {
		t.Fatal(err)
	}
	defer advertiser.Close()
	packet := broadcastPacketFor(advertiser, "announce")
	if packet.Magic != broadcastMagic || packet.Port != 8787 || packet.Name != "Workshop" || len(packet.TXT) != 2 {
		t.Fatalf("broadcast packet=%#v", packet)
	}
	probe, ok := parseWSProbe([]byte(wsProbeMessage()))
	if !ok {
		t.Fatal("generated WS-Discovery probe is not standards-valid")
	}
	response := wsProbeResponse(advertiser, &net.UDPAddr{IP: net.ParseIP("192.0.2.10")}, probe.Header.MessageID)
	for _, expected := range []string{wsProbeMatchesAction, "<a:RelatesTo>" + probe.Header.MessageID + "</a:RelatesTo>", "<a:EndpointReference>", "<d:AppSequence", "/upnp/device.xml", "uuid:0123456789abcdef0123456789abcdef"} {
		if !strings.Contains(response, expected) {
			t.Fatalf("WS-Discovery response missing %q:\n%s", expected, response)
		}
	}
	if !strings.Contains(wsProbeMessage(), WSDiscoveryType) {
		t.Fatal("WS-Discovery probe/response missing service metadata")
	}
	var match wsMatchEnvelope
	if err := xml.Unmarshal([]byte(response), &match); err != nil || match.Header.Action != wsProbeMatchesAction ||
		match.Body.Matches.Match.Endpoint.Address != "uuid:0123456789abcdef0123456789abcdef" || match.Body.Matches.Match.XAddrs == "" {
		t.Fatalf("generated WS-Discovery match is not consumable: match=%#v err=%v", match, err)
	}
}

func TestWSDiscoveryRejectsLooseOrUnrelatedProbePayloads(t *testing.T) {
	valid := wsProbeMessage()
	for _, payload := range []string{
		`<Probe/>`,
		strings.Replace(valid, wsProbeAction, wsProbeMatchesAction, 1),
		strings.Replace(valid, `<a:To s:mustUnderstand="1">`+wsDiscoveryTo+`</a:To>`, "", 1),
		strings.Replace(valid, WSDiscoveryType, "other:UnrelatedDevice", 1),
		strings.Replace(valid, wsDiscoveryNamespace, "urn:wrong-discovery", 1),
	} {
		if _, ok := parseWSProbe([]byte(payload)); ok {
			t.Fatalf("accepted invalid WS-Discovery probe: %s", payload)
		}
	}
}

func TestNetBIOSNodeStatusWireFormat(t *testing.T) {
	query := netbiosNodeStatusQuery()
	if len(query) != 50 || binary.BigEndian.Uint16(query[46:48]) != 0x0021 || binary.BigEndian.Uint16(query[48:50]) != 1 {
		t.Fatalf("invalid NBNS query: % X", query)
	}
	if query[13] != 'C' || query[14] != 'K' || strings.Trim(string(query[15:45]), "A") != "" {
		t.Fatalf("NBNS wildcard name is not RFC 1002 encoded: % X", query[12:46])
	}
	if !isNetBIOSNodeStatusQuery(query) {
		t.Fatal("generated NBNS query is not recognized")
	}
	response := netbiosNodeStatusResponse(query, "PCController")
	if len(response) != 127 || binary.BigEndian.Uint16(response[60:62]) != 65 {
		t.Fatalf("invalid NBNS node-status response size: len=%d packet=% X", len(response), response)
	}
	if got := parseNetBIOSNodeStatus(response); got != "PCCONTROLLER" {
		t.Fatalf("node status name=%q packet=% X", got, response)
	}
	if parseNetBIOSNodeStatus(response[:len(response)-1]) != "" {
		t.Fatal("accepted truncated NBNS response")
	}
}

func TestSSDPIdentityMatchesUPnPDescriptionContract(t *testing.T) {
	const instanceID = "0123456789abcdef0123456789abcdef"
	packet := ssdpNotifyWithText("Workshop", 8787, "ssdp:alive", []string{"instance.id=" + instanceID})
	for _, expected := range []string{
		"NT: urn:pccontroller-org:service:Controller:1",
		"USN: uuid:" + instanceID + "::" + SSDPType,
	} {
		if !strings.Contains(packet, expected) {
			t.Fatalf("SSDP packet missing canonical identity %q:\n%s", expected, packet)
		}
	}
}

func TestAdvertiserReportsSoleResponderStartupFailure(t *testing.T) {
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Skipf("UDP unavailable: %v", err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port
	advertiser, err := AdvertiseWithOptions(context.Background(), "blocked", 8787, Options{Broadcast: true, BroadcastPort: port}, nil)
	if err == nil || advertiser != nil {
		if advertiser != nil {
			advertiser.Close()
		}
		t.Fatalf("sole blocked responder advertiser=%#v err=%v", advertiser, err)
	}
}

func TestBroadcastAdvertiserAndScannerDiscoverEachOther(t *testing.T) {
	if runtime.GOOS == "windows" && os.Getenv("PCCONTROLLER_TEST_LAN") != "1" {
		t.Skip("Windows LAN acceptance uses the stable packaged controller.exe; set PCCONTROLLER_TEST_LAN=1 to opt in")
	}
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Skipf("UDP unavailable: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	advertiser, err := AdvertiseWithOptions(ctx, "broadcast-test", 8787, Options{Broadcast: true, BroadcastPort: port}, []string{"board.name=Test"})
	if err != nil {
		t.Skipf("broadcast advertiser unavailable: %v", err)
	}
	defer advertiser.Close()
	instances, err := DiscoverWithOptions(ctx, Options{Broadcast: true, BroadcastPort: port})
	if err != nil {
		t.Skipf("broadcast scan unavailable: %v", err)
	}
	for _, instance := range instances {
		if instance.Protocol == "broadcast" && instance.Name == "broadcast-test" && instance.Port == 8787 {
			return
		}
	}
	t.Skipf("broadcast is filtered by the local network: %#v", instances)
}

func TestParseSSDPResponseRequiresControllerServiceType(t *testing.T) {
	data := strings.Join([]string{
		"HTTP/1.1 200 OK",
		"ST: " + SSDPType,
		"LOCATION: http://192.0.2.15:8787/upnp/device.xml",
		"USN: uuid:test::" + SSDPType,
		"X-PCController-Name: Workshop",
		"X-PCController-Meta: web=/",
		"X-PCController-Meta: board.connected=true",
		"", "",
	}, "\r\n")
	instance, ok := parseSSDPResponse(
		[]byte(data),
		&net.UDPAddr{IP: net.ParseIP("192.0.2.15"), Port: 1900},
	)
	if !ok || instance.Protocol != "ssdp" || instance.Name != "Workshop" ||
		instance.Host != "192.0.2.15" || instance.Port != 8787 ||
		len(instance.TXT) != 2 || instance.TXT[0] != "web=/" ||
		instance.TXT[1] != "board.connected=true" {
		t.Fatalf("parsed SSDP instance=%#v ok=%v", instance, ok)
	}
	wrong := strings.Replace(data, SSDPType, "urn:example:other:1", 1)
	if _, ok := parseSSDPResponse([]byte(wrong), nil); ok {
		t.Fatal("accepted unrelated SSDP service type")
	}
}

func TestParseSSDPResponseUsesPacketSourceNotAdvertisedRedirect(t *testing.T) {
	data := strings.Join([]string{
		"HTTP/1.1 200 OK",
		"ST: " + SSDPType,
		"LOCATION: http://127.0.0.1:9999/upnp/device.xml",
		"X-PCController-Public: http://127.0.0.1:9999/upnp/public.json",
		"", "",
	}, "\r\n")
	instance, ok := parseSSDPResponse([]byte(data), &net.UDPAddr{IP: net.ParseIP("192.0.2.15"), Port: 1900})
	if !ok || instance.Host != "192.0.2.15" || instance.Port != 9999 {
		t.Fatalf("parsed SSDP instance=%#v ok=%v", instance, ok)
	}
	if trustedDiscoveryURL(instance.Location, instance) || trustedDiscoveryURL(instance.PublicURL, instance) {
		t.Fatal("SSDP response was allowed to redirect enrichment away from its packet source")
	}
}

func TestDisabledAdvertiserHasDeterministicLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	advertiser, err := Advertise(ctx, "Test", 8787, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	advertiser.Close()
}

func TestMetadataRefreshSignalsEveryAnnouncementTransport(t *testing.T) {
	advertiser := &Advertiser{
		ssdp: true, broadcast: true,
		ssdpRefresh: make(chan struct{}, 1), broadcastRefresh: make(chan struct{}, 1),
	}
	advertiser.UpdateText([]string{"board.connected=true"})
	if len(advertiser.ssdpRefresh) != 1 || len(advertiser.broadcastRefresh) != 1 {
		t.Fatalf("refresh signals ssdp=%d broadcast=%d", len(advertiser.ssdpRefresh), len(advertiser.broadcastRefresh))
	}
}
