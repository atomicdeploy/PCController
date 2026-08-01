package discovery

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestSSDPAdvertisementContainsOnlyNonSecretServiceMetadata(t *testing.T) {
	packet := ssdpNotify("Lab\r\nInjected: no", 8787, "ssdp:alive")
	for _, expected := range []string{
		"NOTIFY * HTTP/1.1", "NT: " + SSDPType,
		"NTS: ssdp:alive", "X-PCController-Name: Lab  Injected: no",
	} {
		if !strings.Contains(packet, expected) {
			t.Fatalf("SSDP packet missing %q:\n%s", expected, packet)
		}
	}
	for _, forbidden := range []string{"auth_token", "access_token", "Authorization:"} {
		if strings.Contains(packet, forbidden) {
			t.Fatalf("SSDP packet exposed %q", forbidden)
		}
	}
}

func TestParseSSDPResponseRequiresControllerServiceType(t *testing.T) {
	data := strings.Join([]string{
		"HTTP/1.1 200 OK",
		"ST: " + SSDPType,
		"LOCATION: http://192.0.2.15:8787/healthz",
		"USN: uuid:test::" + SSDPType,
		"X-PCController-Name: Workshop",
		"", "",
	}, "\r\n")
	instance, ok := parseSSDPResponse(
		[]byte(data),
		&net.UDPAddr{IP: net.ParseIP("192.0.2.15"), Port: 1900},
	)
	if !ok || instance.Protocol != "ssdp" || instance.Name != "Workshop" ||
		instance.Host != "192.0.2.15" || instance.Port != 8787 {
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
