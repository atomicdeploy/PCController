package deviceconfig

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/diagnostics"
)

func TestProjectWhitelistsTypedFieldsAndCannotSerializeRawCredentials(t *testing.T) {
	const (
		stationSecret = "station-password-must-not-escape"
		accessSecret  = "access-password-must-not-escape"
	)
	connected := true
	dhcp := true
	uptime := int64(1234)
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	source := diagnostics.Snapshot{
		DeviceURL: "http://pccontroller-desktop.local",
		Device: diagnostics.DeviceInfo{
			DeviceName:         "PCController",
			Hostname:           "pccontroller-desktop",
			Firmware:           "1.2.3",
			FirmwareHash:       "abc123",
			Build:              "Jul 24 2026",
			ProtocolRevision:   "7",
			UptimeSeconds:      &uptime,
			WiFiConnected:      &connected,
			SSID:               "Office",
			WiFiIP:             "192.168.1.44",
			StationDHCP:        &dhcp,
			StationNetmask:     "255.255.255.0",
			StationGateway:     "192.168.1.1",
			StationDNS:         "192.168.1.1",
			StationStatus:      "Connected",
			AccessPointSSID:    "PCControllerDesktop-AP",
			AccessPointIP:      "192.168.4.1",
			AccessPointNetmask: "255.255.255.0",
			AccessPointStatus:  "Enabled",
			Country:            "IR",
			Description: diagnostics.DeviceDescription{
				ModelName:    "TestController",
				Manufacturer: "AtomicDeploy",
			},
		},
		Endpoints: map[string]diagnostics.EndpointResult{
			diagnostics.EndpointConfigST: {
				Document: diagnostics.Document{
					Endpoint: diagnostics.EndpointConfigST,
					Raw:      "pass: " + stationSecret,
					SafeRaw:  "pass: [redacted]",
				},
			},
			diagnostics.EndpointConfigAP: {
				Document: diagnostics.Document{
					Endpoint: diagnostics.EndpointConfigAP,
					Raw:      "pass: " + accessSecret,
					SafeRaw:  "pass: [redacted]",
				},
			},
		},
		UpdatedAt: now,
	}
	activity := now
	got := Project(source, deviceterminal.Snapshot{
		State:        deviceterminal.StateConnected,
		ReadOnly:     false,
		LastActivity: &activity,
	}, true)

	if got.Hostname != "pccontroller-desktop" ||
		got.Station.SSID != "Office" ||
		got.AccessPoint.SSID != "PCControllerDesktop-AP" ||
		got.Firmware.UptimeSeconds == nil ||
		*got.Firmware.UptimeSeconds != uptime {
		t.Fatalf("projection = %+v", got)
	}
	if !got.Capabilities.HTTPRead ||
		!got.Capabilities.TerminalReachable ||
		got.Capabilities.TerminalReadOnly ||
		got.Capabilities.WriteAvailable {
		t.Fatalf("capabilities = %+v", got.Capabilities)
	}

	serialized, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{
		strings.ToLower(stationSecret),
		strings.ToLower(accessSecret),
		`"password"`,
		`"pass"`,
		`"raw"`,
		`"endpoints"`,
		`[redacted]`,
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("serialized device configuration contains %q: %s", forbidden, serialized)
		}
	}
}

func TestProjectDoesNotAdvertiseWritesBeforeTerminalCapabilityIsKnown(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		terminal  deviceterminal.Snapshot
		reachable bool
		readOnly  bool
		writable  bool
	}{
		{
			name:     "disconnected",
			terminal: deviceterminal.Snapshot{State: deviceterminal.StateDisconnected},
		},
		{
			name:      "connected before banner",
			terminal:  deviceterminal.Snapshot{State: deviceterminal.StateConnected},
			reachable: true,
		},
		{
			name: "audited read-only firmware",
			terminal: deviceterminal.Snapshot{
				State:        deviceterminal.StateConnected,
				ReadOnly:     true,
				LastActivity: &now,
			},
			reachable: true,
			readOnly:  true,
		},
		{
			name: "activity without a structured capability handshake",
			terminal: deviceterminal.Snapshot{
				State:        deviceterminal.StateConnected,
				LastActivity: &now,
			},
			reachable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Project(diagnostics.NewSnapshot("http://device.local"), test.terminal, false)
			if got.Capabilities.TerminalReachable != test.reachable ||
				got.Capabilities.TerminalReadOnly != test.readOnly ||
				got.Capabilities.WriteAvailable != test.writable {
				t.Fatalf("capabilities = %+v", got.Capabilities)
			}
		})
	}
}
