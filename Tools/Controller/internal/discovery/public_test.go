package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestMergeInstancesDeduplicatesTransportSourcesByPublicIdentity(t *testing.T) {
	info := &PublicInfo{Schema: PublicInfoSchema, Product: "PCController", InstanceID: "host-1", Hostname: "workshop", InstanceName: "Workshop"}
	values := []Instance{
		{Protocol: "mdns", Name: "Workshop", Host: "workshop.local", Port: 8787, Addresses: []string{"192.0.2.5"}, Public: info, SeenAt: time.Unix(1, 0)},
		{Protocol: "ssdp", Name: "Workshop", Host: "192.0.2.5", Port: 8787, USN: "uuid:one::service", Public: info, SeenAt: time.Unix(2, 0)},
		{Protocol: "broadcast", Name: "Workshop", Host: "192.0.2.5", Port: 8787, Public: info, SeenAt: time.Unix(3, 0)},
	}
	merged := mergeInstances(values)
	if len(merged) != 1 {
		t.Fatalf("merged instances=%#v", merged)
	}
	if got := merged[0].Protocols; len(got) != 3 || got[0] != "broadcast" || got[1] != "mdns" || got[2] != "ssdp" {
		t.Fatalf("protocols=%v", got)
	}
	if len(merged[0].Sources) != 3 || !merged[0].SeenAt.Equal(time.Unix(3, 0)) {
		t.Fatalf("sources/seen=%d/%s", len(merged[0].Sources), merged[0].SeenAt)
	}
}

func TestEnrichInstanceReadsBoundedPublicDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != PublicInfoPath {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(PublicInfo{Schema: PublicInfoSchema, Product: "PCController", InstanceID: "test-id", InstanceName: "Test host", Hostname: "test-host"})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	instance := enrichInstance(context.Background(), Instance{Protocol: "ssdp", Name: "raw", Host: parsed.Hostname(), Port: parsedPort(t, parsed), Location: server.URL + "/upnp/device.xml"})
	if instance.Public == nil || instance.Public.InstanceID != "test-id" || instance.Name != "Test host" || instance.PublicURL != server.URL+PublicInfoPath {
		t.Fatalf("enriched=%#v", instance)
	}
}

func TestPublicInfoFromTXTPreservesFallbackIdentityTelemetryAndEndpoints(t *testing.T) {
	info := publicInfoFromTXT([]string{
		"product=PCController", "protocol=pccontroller", "health=ok", "host.hostname=workshop",
		"instance.id=host-1", "instance.name=Workshop", "host.version=1.2.3", "remote.connectable=true",
		"board.connected=true", "board.connection_state=connected", "board.name=PCController",
		"board.build_hash=DEADBEEF", "board.capabilities=000000FF", "board.port=COM18",
		"board.status_at=2026-08-13T12:00:00Z", "board.supply_mv=12050", "board.current_ma=321",
		"board.temperature_led_centi_c=2812", "board.temperature_bt_audio_centi_c=2590", "board.door_open=true",
		"web=/", "operations=/api/rpc", "ws=/ipc", "public=/upnp/public.json",
	})
	absolutizePublicInfo(&info, Instance{Host: "192.0.2.5", Port: 8787})
	if !info.Valid() || !info.Health.Connectable || info.Host.Version != "1.2.3" || info.Board.Identity.Capabilities != 0xff ||
		!info.Board.Telemetry.Available || info.Board.Telemetry.SupplyMV != 12050 || info.Board.Telemetry.CurrentMA != 321 ||
		!info.Board.Telemetry.DoorOpen || info.Endpoints.Web != "http://192.0.2.5:8787/" || info.Endpoints.WebSocket != "ws://192.0.2.5:8787/ipc" {
		t.Fatalf("TXT public info=%#v", info)
	}
}

func TestNarrowUnsignedTXTValuesRejectOverflowInsteadOfWrapping(t *testing.T) {
	info := publicInfoFromTXT([]string{
		"product=PCController", "host.hostname=workshop",
		"board.bluetooth_audio_state=256", "board.active_relays=257", "board.active_keys=-1",
		"board.menu_page=999", "board.program_mode=invalid", "board.pwm_channel=256",
		"board.pwm_value=65536", "board.lcd_address=256",
	})
	telemetry := info.Board.Telemetry
	if telemetry.BluetoothAudioState != 0 || telemetry.ActiveRelays != 0 || telemetry.ActiveKeys != 0 ||
		telemetry.MenuPage != 0 || telemetry.ProgramMode != 0 || telemetry.PWMChannel != 0 ||
		telemetry.PWMValue != 0 || telemetry.LCDAddress != 0 {
		t.Fatalf("out-of-range TXT values wrapped: %#v", telemetry)
	}

	if got := parseUint8("255", 10); got != 255 {
		t.Fatalf("uint8 boundary=%d", got)
	}
	if got := parseUint16("65535", 10); got != 65535 {
		t.Fatalf("uint16 boundary=%d", got)
	}
}

func TestTrustedDiscoveryURLCannotRedirectEnrichmentAwayFromResponder(t *testing.T) {
	instance := Instance{Host: "192.0.2.5", Addresses: []string{"192.0.2.5"}, Port: 8787}
	for _, candidate := range []string{
		"http://192.0.2.5:8787/upnp/public.json",
		"http://192.0.2.5:8787/upnp/device.xml",
	} {
		if !trustedDiscoveryURL(candidate, instance) {
			t.Fatalf("rejected responder-owned URL %q", candidate)
		}
	}
	for _, candidate := range []string{
		"http://192.0.2.5/upnp/public.json",
		"http://127.0.0.1:8787/upnp/public.json",
		"http://192.0.2.5:9999/upnp/public.json",
		"http://user@192.0.2.5:8787/upnp/public.json",
		"https://192.0.2.5:8787/upnp/public.json",
		"http://198.51.100.7:8787/upnp/public.json",
	} {
		if trustedDiscoveryURL(candidate, instance) {
			t.Fatalf("trusted off-responder URL %q", candidate)
		}
	}
}

func TestPublicURLCandidatesPreferPacketSourceAddresses(t *testing.T) {
	instance := Instance{
		Host: "attacker.invalid", Addresses: []string{"192.0.2.5"}, Port: 8787,
		Location:  "http://attacker.invalid:8787/upnp/device.xml",
		PublicURL: "http://attacker.invalid:8787/upnp/public.json",
	}
	candidates := publicURLCandidates(instance)
	if len(candidates) != 1 || candidates[0] != "http://192.0.2.5:8787/upnp/public.json" {
		t.Fatalf("source-bound candidates=%#v", candidates)
	}
}

func TestEnrichInstanceRejectsCrossPortRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetCalled = true
		_ = json.NewEncoder(writer).Encode(PublicInfo{Schema: PublicInfoSchema, Product: "PCController", Hostname: "redirected"})
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+PublicInfoPath, http.StatusFound)
	}))
	defer redirect.Close()
	parsed, _ := url.Parse(redirect.URL)
	instance := enrichInstance(context.Background(), Instance{Host: parsed.Hostname(), Port: parsedPort(t, parsed), PublicURL: redirect.URL + PublicInfoPath})
	if targetCalled || instance.Public != nil {
		t.Fatalf("cross-port redirect was followed: target_called=%t instance=%#v", targetCalled, instance)
	}
}

func parsedPort(t *testing.T, value *url.URL) int {
	t.Helper()
	port := value.Port()
	if port == "" {
		t.Fatal("test server has no port")
	}
	result, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
