package hostbridge

import (
	"context"
	"path/filepath"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

// openHostBridgeTestStore keeps manager fixtures independent from production
// discovery and desktop defaults. Unit tests use only their explicit loopback
// peers; real LAN discovery belongs to the named packaged controller.
func openHostBridgeTestStore(t *testing.T, configure func(*appconfig.Config) error) *appconfig.Store {
	t.Helper()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.Discovery = appconfig.Discovery{}
		if configure != nil {
			return configure(config)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestHostBridgeTestStoreKeepsManagerLocal(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "defaults"
		var configure func(*appconfig.Config) error
		if configured {
			name = "configured"
			configure = func(config *appconfig.Config) error {
				config.Integrations.TextMappings = []appconfig.TextMapping{{
					Name: "test-mapping", Enabled: true, Source: "ipc", Target: "host",
					Type: "test", Contains: "test", Command: "mark",
				}}
				return nil
			}
		}
		t.Run(name, func(t *testing.T) {
			store := openHostBridgeTestStore(t, configure)
			config := store.Current()
			discovery := config.Integrations.Discovery
			// Fail before manager startup if normalization re-enables LAN sockets.
			if discovery.MDNSEnabled || discovery.DNSSDenabled || discovery.SSDPEnabled ||
				discovery.UPnPEnabled || discovery.WSDiscoveryEnabled || discovery.BroadcastEnabled ||
				discovery.NetBIOSEnabled || len(config.Integrations.Hotkeys) != 0 || config.Integrations.Notifications.Enabled {
				t.Fatal("test configuration retained production discovery or desktop side effects")
			}
			if configured && (len(config.Integrations.TextMappings) != 1 || config.Integrations.TextMappings[0].Name != "test-mapping") {
				t.Fatal("test-specific integration configuration was lost")
			}
			runtime := control.New(control.Options{})
			t.Cleanup(func() { _ = runtime.Close() })
			client := controller.AttachSharedRuntime(runtime, shell.New(8))
			manager, err := Start(context.Background(), client, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			status := manager.Status()
			if status.DiscoveryActive || len(status.DiscoveryProtocols) != 0 || status.HotkeysActive != 0 || status.Notifications {
				t.Fatalf("test manager activated discovery or desktop integrations: %#v", status)
			}
		})
	}
}
