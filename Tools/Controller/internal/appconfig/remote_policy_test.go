package appconfig

import (
	"path/filepath"
	"testing"
)

func TestRemoteAccessPolicyDefaultsToObservationOnly(t *testing.T) {
	policy := Defaults().IPC.RemotePolicy
	if !policy.Read || !policy.Events {
		t.Fatalf("safe observation defaults=%#v", policy)
	}
	if policy.Messages || policy.BoardCommands || policy.HostConfiguration ||
		policy.ConnectionControl || policy.Reset || policy.Programming ||
		policy.Shutdown || policy.VirtualKeys || policy.PowerActions ||
		policy.HostAutomations || policy.BridgeCalls {
		t.Fatalf("mutating remote capability enabled by default: %#v", policy)
	}
}

func TestRemoteProgrammingRequiresConnectionControl(t *testing.T) {
	config := Defaults()
	config.IPC.RemotePolicy.Programming = true
	if err := config.Validate(); err == nil {
		t.Fatal("expected programming without connection control to be rejected")
	}
	config.IPC.RemotePolicy.ConnectionControl = true
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteAccessRejectsMissingOrWildcardBrowserOrigins(t *testing.T) {
	config := Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = "0123456789abcdefghijklmn"
	config.IPC.AllowedOrigins = nil
	if err := config.Validate(); err == nil {
		t.Fatal("expected missing remote origin allow-list to be rejected")
	}
	config.IPC.AllowedOrigins = []string{"*"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected wildcard remote origin to be rejected")
	}
	config.IPC.AllowedOrigins = []string{"controller.example:*"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteBridgeEventForwardingRequiresCredentials(t *testing.T) {
	config := Defaults()
	config.Integrations.WebSocketClients = []WebSocketClient{{
		Name: "remote", Enabled: true, URL: "ws://192.0.2.10:8787/ipc",
		ForwardEvents: true, Topics: []string{"events"},
	}}
	if err := config.Validate(); err == nil {
		t.Fatal("expected unauthenticated remote event forwarding to be rejected")
	}
	config.Integrations.WebSocketClients[0].AuthToken = "peer-secret"
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Integrations.WebSocketClients[0].Topics = []string{"events", "telemetry"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Integrations.WebSocketClients[0].Topics = []string{"status", "telemetry"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected duplicate normalized bridge topic to be rejected")
	}
}

func TestPublishedExampleIncludesValidRemotePolicy(t *testing.T) {
	config, _, err := Load(filepath.Join("..", "..", "examples", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.IPC.RemotePolicy.Read || !config.IPC.RemotePolicy.Events ||
		config.IPC.RemotePolicy.Programming {
		t.Fatalf("example remote policy=%#v", config.IPC.RemotePolicy)
	}
}
