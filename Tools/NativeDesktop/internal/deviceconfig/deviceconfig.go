// Package deviceconfig defines the public, read-only device-configuration
// projection shared by IPC and the loopback API.
//
// The projection is intentionally a whitelist of typed diagnostics fields.
// It never carries diagnostic documents, raw endpoint bodies, terminal
// transcripts, or Wi-Fi password fields.
package deviceconfig

import (
	"time"

	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/diagnostics"
)

// Capabilities describes what the currently running coordinator can safely do
// with the configured lamp. WriteAvailable stays false until a future
// credential-safe, prompt-aware capability handshake positively identifies an
// allow-listed writable protocol. A TCP connection or arbitrary banner is not
// sufficient evidence that mutations are supported.
type Capabilities struct {
	HTTPRead          bool `json:"http_read"`
	TerminalReachable bool `json:"terminal_reachable"`
	TerminalReadOnly  bool `json:"terminal_read_only"`
	WriteAvailable    bool `json:"write_available"`
}

// Station is the sanitized station-mode network projection. Passwords are
// deliberately absent rather than represented by a redaction placeholder.
type Station struct {
	SSID      string `json:"ssid,omitempty"`
	Status    string `json:"status,omitempty"`
	DHCP      *bool  `json:"dhcp,omitempty"`
	IP        string `json:"ip,omitempty"`
	Netmask   string `json:"netmask,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	DNS       string `json:"dns,omitempty"`
	Connected *bool  `json:"connected,omitempty"`
}

// AccessPoint is the sanitized access-point network projection. Passwords are
// deliberately absent rather than represented by a redaction placeholder.
type AccessPoint struct {
	SSID    string `json:"ssid,omitempty"`
	Status  string `json:"status,omitempty"`
	IP      string `json:"ip,omitempty"`
	Netmask string `json:"netmask,omitempty"`
}

// Firmware contains useful read-only device metadata.
type Firmware struct {
	Version          string     `json:"version,omitempty"`
	Hash             string     `json:"hash,omitempty"`
	Build            string     `json:"build,omitempty"`
	BuildTime        *time.Time `json:"build_time,omitempty"`
	UptimeSeconds    *int64     `json:"uptime_seconds,omitempty"`
	ProtocolRevision string     `json:"protocol_revision,omitempty"`
}

// Snapshot is the public read-only device-configuration view. Its explicit
// field list is a privacy boundary: adding a new diagnostics field does not
// automatically expose it through IPC or HTTP.
type Snapshot struct {
	DeviceURL    string       `json:"device_url"`
	DeviceName   string       `json:"device_name,omitempty"`
	Hostname     string       `json:"hostname,omitempty"`
	ModelName    string       `json:"model_name,omitempty"`
	Manufacturer string       `json:"manufacturer,omitempty"`
	Country      string       `json:"country,omitempty"`
	Station      Station      `json:"station"`
	AccessPoint  AccessPoint  `json:"access_point"`
	Firmware     Firmware     `json:"firmware"`
	Capabilities Capabilities `json:"capabilities"`
	UpdatedAt    time.Time    `json:"updated_at,omitempty"`
}

// Project builds a caller-owned, serialization-safe device view from the
// already parsed diagnostics cache and terminal metadata.
func Project(
	source diagnostics.Snapshot,
	terminal deviceterminal.Snapshot,
	httpReachable bool,
) Snapshot {
	info := source.Device
	terminalReachable := terminal.State == deviceterminal.StateConnected
	return Snapshot{
		DeviceURL:    source.DeviceURL,
		DeviceName:   info.DeviceName,
		Hostname:     info.Hostname,
		ModelName:    info.Description.ModelName,
		Manufacturer: info.Description.Manufacturer,
		Country:      info.Country,
		Station: Station{
			SSID:      info.SSID,
			Status:    info.StationStatus,
			DHCP:      cloneBool(info.StationDHCP),
			IP:        info.WiFiIP,
			Netmask:   info.StationNetmask,
			Gateway:   info.StationGateway,
			DNS:       info.StationDNS,
			Connected: cloneBool(info.WiFiConnected),
		},
		AccessPoint: AccessPoint{
			SSID:    info.AccessPointSSID,
			Status:  info.AccessPointStatus,
			IP:      info.AccessPointIP,
			Netmask: info.AccessPointNetmask,
		},
		Firmware: Firmware{
			Version:          info.Firmware,
			Hash:             info.FirmwareHash,
			Build:            info.Build,
			BuildTime:        cloneTime(info.BuildTime),
			UptimeSeconds:    cloneInt64(info.UptimeSeconds),
			ProtocolRevision: info.ProtocolRevision,
		},
		Capabilities: Capabilities{
			HTTPRead:          httpReachable,
			TerminalReachable: terminalReachable,
			TerminalReadOnly:  terminalReachable && terminal.ReadOnly,
			WriteAvailable:    false,
		},
		UpdatedAt: source.UpdatedAt,
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
