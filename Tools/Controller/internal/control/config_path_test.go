package control

import (
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestGenericHostConfigPathSupportsEveryPersistedField(t *testing.T) {
	config := appconfig.Defaults()
	updated, err := genericHostConfigSet(config, "integrations.discovery.instance_name", "edge-controller")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Integrations.Discovery.InstanceName != "edge-controller" {
		t.Fatalf("instance name=%q", updated.Integrations.Discovery.InstanceName)
	}
	output, err := genericHostConfigGet(updated, "integrations.discovery.instance_name")
	if err != nil {
		t.Fatal(err)
	}
	if output != `integrations.discovery.instance_name="edge-controller"` {
		t.Fatalf("output=%q", output)
	}
}

func TestGenericHostConfigPathSupportsArrayIndexes(t *testing.T) {
	config := appconfig.Defaults()
	config.Integrations.WebSocketClients = []appconfig.WebSocketClient{{
		Name: "edge", URL: "ws://127.0.0.1:8787/ipc", Enabled: true,
	}}
	updated, err := genericHostConfigSet(config, "integrations.websocket_clients[0].enabled", "false")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Integrations.WebSocketClients[0].Enabled {
		t.Fatal("array value was not updated")
	}
}

func TestGenericHostConfigGetRedactsInlineSecrets(t *testing.T) {
	config := appconfig.Defaults()
	config.IPC.AuthToken = strings.Repeat("x", 24)
	output, err := genericHostConfigGet(config, "ipc.auth_token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, config.IPC.AuthToken) || !strings.Contains(output, "<redacted>") {
		t.Fatalf("secret output=%q", output)
	}
}
