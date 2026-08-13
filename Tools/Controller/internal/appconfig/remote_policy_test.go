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
	config.IPC.AllowedOrigins = nil
	if err := config.Validate(); err == nil {
		t.Fatal("expected missing remote origin allow-list to be rejected")
	}
	for _, invalid := range []string{"*", "*.example:*", "controller?.example:*", "https://controller.example", "controller.example"} {
		config.IPC.AllowedOrigins = []string{invalid}
		if err := config.Validate(); err == nil {
			t.Fatalf("expected wildcard or malformed remote origin %q to be rejected", invalid)
		}
	}
	config.IPC.AllowedOrigins = []string{"controller.example:*"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.IPC.AllowedOrigins = []string{"controller.example:8787", "[2001:db8::1]:8787"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePrincipalIsStableAndMachineSafe(t *testing.T) {
	config := Defaults()
	if config.IPC.RemotePrincipal != "remote-operator" {
		t.Fatalf("default remote principal=%q", config.IPC.RemotePrincipal)
	}
	for _, invalid := range []string{"", " leading", "two words", "name=value", "line\nbreak"} {
		config.IPC.RemotePrincipal = invalid
		if err := config.Validate(); err == nil {
			t.Fatalf("remote principal %q unexpectedly validated", invalid)
		}
	}
	config.IPC.RemotePrincipal = "maintenance-console"
	if err := config.Validate(); err != nil {
		t.Fatalf("named remote principal rejected: %v", err)
	}
}

func TestAlphaRemoteBridgeAcceptsCredentiallessStateSubscriptions(t *testing.T) {
	config := Defaults()
	config.Integrations.WebSocketClients = []WebSocketClient{{
		Name: "remote", Enabled: true, URL: "ws://192.0.2.10:8787/ipc",
		ForwardEvents: true, AllowCommands: true, Topics: []string{"events", "state", "status"},
	}}
	if err := config.Validate(); err != nil {
		t.Fatalf("credentialless alpha peer rejected: %v", err)
	}
	config.Integrations.WebSocketClients[0].AuthToken = "peer-secret"
	if err := config.Validate(); err != nil {
		t.Fatalf("optional compatibility credential rejected: %v", err)
	}
	config.Integrations.WebSocketClients[0].Topics = []string{"events", "state", "telemetry"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Integrations.WebSocketClients[0].Topics = []string{"status", "telemetry"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected duplicate normalized bridge topic to be rejected")
	}
}

func TestRemoteBridgeRejectsURLCredentialsAndAcceptsSecretReference(t *testing.T) {
	config := Defaults()
	config.Integrations.WebSocketClients = []WebSocketClient{{
		Name: "remote", Enabled: true, URL: "wss://example.test/ipc?access_token=secret",
		AuthTokenRef: "os:bridges/remote", ForwardEvents: true,
	}}
	if err := config.Validate(); err == nil {
		t.Fatal("credential-shaped query was accepted")
	}
	config.Integrations.WebSocketClients[0].URL = "wss://example.test/ipc"
	if err := config.Validate(); err != nil {
		t.Fatalf("secret-backed bridge rejected: %v", err)
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
