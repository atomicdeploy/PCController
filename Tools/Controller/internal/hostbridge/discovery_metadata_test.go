package hostbridge

import (
	"slices"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func TestDiscoveryMetadataIncludesWebAppAndCurrentBoardValues(t *testing.T) {
	config := appconfig.Defaults()
	config.UI.AppTitle = "Lab Controller"
	config.UI.Appearance.Locale = "fa-ir"
	config.IPC.WebSocketPath = "/ipc"
	config.IPC.SocketIOPath = "/socket.io/"
	snapshot := controller.Snapshot{
		Connected: true, ConnectionState: "connected", HaveStatus: true, HaveSettings: true,
		StatusUpdated: time.Date(2026, 8, 3, 2, 3, 4, 0, time.UTC),
	}
	snapshot.Hello.Name = "PCController"
	snapshot.Hello.BuildHash = 0xADFAEDAB
	snapshot.Hello.BuildStamp = "260803042248"
	snapshot.Port.Name = "COM18"
	snapshot.Status.SupplyMV = 12280
	snapshot.Status.DoorOpen = true
	snapshot.Settings.Persisted = true
	values := discoveryMetadata(config, snapshot)
	for _, expected := range []string{
		"web=/", "webui=embedded", "api=/api", "snapshot=/api/snapshot",
		"app.title=Lab Controller", "app.locale=fa-ir", "board.connected=true",
		"board.build_hash=ADFAEDAB", "board.supply_mv=12280", "board.door_open=true",
		"board.settings_persisted=true",
	} {
		if !slices.Contains(values, expected) {
			t.Fatalf("metadata missing %q: %#v", expected, values)
		}
	}
}
