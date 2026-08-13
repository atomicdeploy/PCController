package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/discovery"
)

func TestNetworkPeerAddUsesSecretReferenceAndCanBeRemoved(t *testing.T) {
	t.Setenv("TEST_EDGE_TOKEN", "0123456789abcdefghijklmn")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runNetwork([]string{
		"peer-add", "--name", "edge", "--url", "ws://192.168.1.2:8787/ipc",
		"--secret-ref", "env:TEST_EDGE_TOKEN",
	}, &output, &output, store)
	if err != nil {
		t.Fatal(err)
	}
	peers := store.Current().Integrations.WebSocketClients
	if len(peers) != 1 || peers[0].AuthToken != "" || peers[0].AuthTokenRef != "env:TEST_EDGE_TOKEN" {
		t.Fatalf("unexpected persisted peer: %#v", peers)
	}
	output.Reset()
	if err := runNetwork([]string{"status"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("0123456789abcdefghijklmn")) {
		t.Fatal("network status leaked peer bearer token")
	}
	if err := runNetwork([]string{"peer-remove", "--name", "EDGE"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Current().Integrations.WebSocketClients); got != 0 {
		t.Fatalf("peer count after removal=%d want 0", got)
	}
}

func TestNetworkAdvertiseCanNarrowAndDisableDefaultProtocols(t *testing.T) {
	previous := currentPrimaryEndpoint()
	defer primaryEndpoint.Store(previous)
	isolated := previous
	isolated.Listen = "127.0.0.1:65529"
	isolated.AuthToken = ""
	primaryEndpoint.Store(isolated)

	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runNetwork([]string{"advertise", "--protocols", "dns-sd,broadcast", "--broadcast-port", "37901", "--instance", "Workshop"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	value := store.Current().Integrations.Discovery
	if !value.MDNSEnabled || !value.DNSSDenabled || !value.BroadcastEnabled || value.SSDPEnabled || value.WSDiscoveryEnabled || value.NetBIOSEnabled || value.BroadcastPort != 37901 || value.InstanceName != "Workshop" {
		t.Fatalf("narrow discovery=%#v", value)
	}
	if err := runNetwork([]string{"advertise", "--enabled=false"}, &output, &output, store); err != nil {
		t.Fatal(err)
	}
	value = store.Current().Integrations.Discovery
	if len(enabledProtocolNames(value)) != 0 {
		t.Fatalf("disabled discovery=%#v", value)
	}
}

func TestResolveDiscoveredInstanceUsesCanonicalPublicIdentity(t *testing.T) {
	instances := []discovery.Instance{{
		Name: "Workshop", Host: "192.0.2.4", Port: 8787,
		Public: &discovery.PublicInfo{InstanceID: "host-id", Hostname: "workshop-pc", InstanceName: "Workshop"},
	}}
	for _, target := range []string{"Workshop", "workshop-pc", "host-id", "192.0.2.4"} {
		resolved, err := resolveDiscoveredInstance(instances, target)
		if err != nil || resolved.Name != "Workshop" {
			t.Fatalf("resolve %q = %#v, %v", target, resolved, err)
		}
	}
}

func TestDiscoveredProbeEndpointsPreserveRoutesButPinResponder(t *testing.T) {
	instance := discovery.Instance{Public: &discovery.PublicInfo{Endpoints: discovery.PublicEndpoints{
		WebSocket: "ws://attacker.invalid/custom-ipc",
		SocketIO:  "ws://attacker.invalid/custom-socket/?tenant=workshop",
	}}}
	endpoints := discoveredProbeEndpoints(instance, "192.0.2.5:8787")
	if endpoints.WebSocketURL != "ws://192.0.2.5:8787/custom-ipc" ||
		endpoints.SocketIOURL != "ws://192.0.2.5:8787/custom-socket/?tenant=workshop" {
		t.Fatalf("responder-pinned endpoints=%#v", endpoints)
	}
}

func TestDiscoveredAddressNeverTrustsAdvertisedEndpointHost(t *testing.T) {
	instance := discovery.Instance{
		Host:      "responder.local.",
		Addresses: []string{"192.0.2.5"},
		Port:      8787,
		Public: &discovery.PublicInfo{Endpoints: discovery.PublicEndpoints{
			Operations: "http://attacker.invalid:9999/api/rpc",
		}},
	}
	address, err := discoveredAddress(instance)
	if err != nil {
		t.Fatalf("discoveredAddress: %v", err)
	}
	if address != "192.0.2.5:8787" {
		t.Fatalf("discoveredAddress=%q, want packet responder address", address)
	}
}

func TestLANProbeHTTPClientNeverUsesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client := lanProbeHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("LAN probe transport=%T", client.Transport)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://192.0.2.5:8787/healthz", nil)
	if redirect := client.CheckRedirect(request, nil); redirect == nil {
		t.Fatal("authenticated LAN probe accepted a redirect")
	}
}

func TestServerProofAudienceRejectsRelayEndpoint(t *testing.T) {
	ctx := context.Background()
	if err := verifyProofAudience(ctx, "192.0.2.5:8787", "192.0.2.5:8787"); err != nil {
		t.Fatalf("matching proof audience: %v", err)
	}
	for _, audience := range []string{"198.51.100.9:8787", "192.0.2.5:9999", "attacker.invalid:8787"} {
		if err := verifyProofAudience(ctx, "192.0.2.5:8787", audience); err == nil {
			t.Fatalf("accepted relayed proof audience %q", audience)
		}
	}
}
