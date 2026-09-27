package hostbridge

import (
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

// openHostBridgeTestStore creates a test-safe bridge configuration. Production
// defaults deliberately enable LAN discovery, but unit tests must never bind
// multicast/broadcast sockets: Windows associates a firewall permission with
// each ephemeral go test executable path.
func openHostBridgeTestStore(
	t *testing.T,
	configure func(*appconfig.Config) error,
) *appconfig.Store {
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
