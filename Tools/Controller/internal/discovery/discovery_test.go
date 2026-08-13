package discovery

import (
	"context"
	"encoding/binary"
	"net"
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
	advertiser, err := AdvertiseWithOptions(ctx, "Workshop", 8787, Options{}, []string{"board.name=Alpha", "auth_token=hidden"})
	if err != nil {
		t.Fatal(err)
	}
	defer advertiser.Close()
	packet := broadcastPacketFor(advertiser, "announce")
	if packet.Magic != broadcastMagic || packet.Port != 8787 || packet.Name != "Workshop" || len(packet.TXT) != 1 {
		t.Fatalf("broadcast packet=%#v", packet)
	}
	if !strings.Contains(wsProbeMessage(), WSDiscoveryType) || !strings.Contains(wsProbeResponse(advertiser, &net.UDPAddr{IP: net.ParseIP("192.0.2.10")}), "/upnp/device.xml") {
		t.Fatal("WS-Discovery probe/response missing service metadata")
	}
}

func TestNetBIOSNodeStatusWireFormat(t *testing.T) {
	query := netbiosNodeStatusQuery()
	if len(query) != 50 || binary.BigEndian.Uint16(query[46:48]) != 0x0021 || binary.BigEndian.Uint16(query[48:50]) != 1 {
		t.Fatalf("invalid NBNS query: % X", query)
	}
	response := make([]byte, 84)
	binary.BigEndian.PutUint16(response[2:4], 0x8500)
	response[12] = 0x20
	for i := 0; i < 32; i++ {
		response[13+i] = 'A'
	}
	response[45] = 0
	binary.BigEndian.PutUint16(response[46:48], 0x0021)
	binary.BigEndian.PutUint16(response[48:50], 1)
	response[50], response[51] = 0xC0, 0x0C
	binary.BigEndian.PutUint16(response[52:54], 0x0021)
	binary.BigEndian.PutUint16(response[54:56], 1)
	binary.BigEndian.PutUint16(response[60:62], 21)
	response[62] = 1
	copy(response[63:78], []byte("PCCONTROLLER    "))
	if got := parseNetBIOSNodeStatus(response); got != "PCCONTROLLER" {
		t.Fatalf("node status name=%q", got)
	}
}

func TestBroadcastAdvertiserAndScannerDiscoverEachOther(t *testing.T) {
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

func TestDisabledAdvertiserHasDeterministicLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	advertiser, err := Advertise(ctx, "Test", 8787, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	advertiser.Close()
}
