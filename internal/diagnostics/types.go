package diagnostics

import (
	"maps"
	"time"
)

type ValueKind string

const (
	ValueString   ValueKind = "string"
	ValueInteger  ValueKind = "integer"
	ValueNumber   ValueKind = "number"
	ValueBoolean  ValueKind = "boolean"
	ValueBytes    ValueKind = "bytes"
	ValuePercent  ValueKind = "percent"
	ValueSignal   ValueKind = "signal"
	ValueDuration ValueKind = "duration"
	ValueTime     ValueKind = "time"
	ValueIP       ValueKind = "ip"
	ValueMAC      ValueKind = "mac"
	ValueHash     ValueKind = "hash"
	ValueState    ValueKind = "state"
	ValueRedacted ValueKind = "redacted"
)

// Field retains the exact in-process value while exposing only Display through
// JSON. Display is redacted when Key resembles a credential or session secret.
// Numeric pointers distinguish an observed zero from an unavailable value.
type Field struct {
	Key             string     `json:"key"`
	Label           string     `json:"label"`
	Raw             string     `json:"-"`
	Display         string     `json:"value"`
	Kind            ValueKind  `json:"kind"`
	Unit            string     `json:"unit,omitempty"`
	Integer         *int64     `json:"integer,omitempty"`
	Number          *float64   `json:"number,omitempty"`
	Boolean         *bool      `json:"boolean,omitempty"`
	Time            *time.Time `json:"time,omitempty"`
	DurationSeconds *int64     `json:"duration_seconds,omitempty"`
	Redacted        bool       `json:"redacted,omitempty"`
}

type Document struct {
	Endpoint  string    `json:"endpoint"`
	Format    Format    `json:"format"`
	FetchedAt time.Time `json:"fetched_at"`
	Bytes     int       `json:"bytes"`

	// Raw is intentionally excluded from JSON so an unexpected secret cannot
	// escape through IPC/local API serialization. It is exact for public
	// diagnostics. Credential-bearing projection sources are sanitized before
	// either Raw or any Field retains their response.
	Raw     string `json:"-"`
	SafeRaw string `json:"raw"`

	Fields   []Field  `json:"fields"`
	Warnings []string `json:"warnings,omitempty"`
}

type UpdateInfo struct {
	Hash      string     `json:"hash,omitempty"`
	SizeBytes *int64     `json:"size_bytes,omitempty"`
	FreeBytes *int64     `json:"free_bytes,omitempty"`
	Build     string     `json:"build,omitempty"`
	Date      *time.Time `json:"date,omitempty"`
}

type DeviceDescription struct {
	FriendlyName    string `json:"friendly_name,omitempty"`
	ModelName       string `json:"model_name,omitempty"`
	ModelNumber     string `json:"model_number,omitempty"`
	Manufacturer    string `json:"manufacturer,omitempty"`
	ManufacturerURL string `json:"manufacturer_url,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	UDN             string `json:"udn,omitempty"`
}

// DeviceInfo is a conservative projection of the latest successfully parsed
// endpoint documents. Strings preserve hashes and identifiers exactly; pointer
// numerics preserve unknown-vs-zero semantics.
type DeviceInfo struct {
	DeviceName          string            `json:"device_name,omitempty"`
	Hostname            string            `json:"hostname,omitempty"`
	Firmware            string            `json:"firmware,omitempty"`
	WiFiVersion         string            `json:"wifi_version,omitempty"`
	ESPChipID           string            `json:"esp_chip_id,omitempty"`
	FlashChipID         string            `json:"flash_chip_id,omitempty"`
	FirmwareHash        string            `json:"firmware_hash,omitempty"`
	FirmwareCorrupted   *bool             `json:"firmware_corrupted,omitempty"`
	ProtocolRevision    string            `json:"protocol_revision,omitempty"`
	ProtocolMatch       *bool             `json:"protocol_match,omitempty"`
	StationMAC          string            `json:"station_mac,omitempty"`
	AccessPointMAC      string            `json:"access_point_mac,omitempty"`
	ClientIP            string            `json:"client_ip,omitempty"`
	FlashSizeBytes      *int64            `json:"flash_size_bytes,omitempty"`
	FlashRealSizeBytes  *int64            `json:"flash_real_size_bytes,omitempty"`
	ProgramUsagePercent *float64          `json:"program_usage_percent,omitempty"`
	SettingsHash        string            `json:"settings_hash,omitempty"`
	SettingsStatus      string            `json:"settings_status,omitempty"`
	HeapFreeBytes       *int64            `json:"heap_free_bytes,omitempty"`
	Build               string            `json:"build,omitempty"`
	BuildTime           *time.Time        `json:"build_time,omitempty"`
	UptimeSeconds       *int64            `json:"uptime_seconds,omitempty"`
	ReportedState       string            `json:"reported_state,omitempty"`
	RSSIDBm             *int64            `json:"rssi_dbm,omitempty"`
	SignalQuality       string            `json:"signal_quality,omitempty"`
	WiFiConnected       *bool             `json:"wifi_connected,omitempty"`
	SSID                string            `json:"ssid,omitempty"`
	WiFiIP              string            `json:"wifi_ip,omitempty"`
	StationDHCP         *bool             `json:"station_dhcp,omitempty"`
	StationNetmask      string            `json:"station_netmask,omitempty"`
	StationGateway      string            `json:"station_gateway,omitempty"`
	StationDNS          string            `json:"station_dns,omitempty"`
	AccessPointSSID     string            `json:"access_point_ssid,omitempty"`
	AccessPointIP       string            `json:"access_point_ip,omitempty"`
	AccessPointNetmask  string            `json:"access_point_netmask,omitempty"`
	AccessPointStatus   string            `json:"access_point_status,omitempty"`
	StationStatus       string            `json:"station_status,omitempty"`
	Country             string            `json:"country,omitempty"`
	Update              UpdateInfo        `json:"update,omitempty"`
	Description         DeviceDescription `json:"description,omitempty"`
}

type EndpointResult struct {
	Document    Document  `json:"document"`
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	Error       string    `json:"error,omitempty"`
	Stale       bool      `json:"stale,omitempty"`
}

type Snapshot struct {
	DeviceURL   string                    `json:"device_url"`
	Device      DeviceInfo                `json:"device"`
	Endpoints   map[string]EndpointResult `json:"endpoints"`
	Refreshing  bool                      `json:"refreshing"`
	LastAttempt time.Time                 `json:"last_attempt,omitempty"`
	UpdatedAt   time.Time                 `json:"updated_at,omitempty"`
}

func NewSnapshot(deviceURL string) Snapshot {
	return Snapshot{
		DeviceURL: deviceURL,
		Endpoints: make(map[string]EndpointResult),
	}
}

func (snapshot Snapshot) Clone() Snapshot {
	result := snapshot
	result.Device = cloneDeviceInfo(snapshot.Device)
	result.Endpoints = make(map[string]EndpointResult, len(snapshot.Endpoints))
	for endpoint, endpointResult := range snapshot.Endpoints {
		result.Endpoints[endpoint] = cloneEndpointResult(endpointResult)
	}
	return result
}

func cloneEndpointResult(result EndpointResult) EndpointResult {
	result.Document.Fields = append([]Field(nil), result.Document.Fields...)
	for index := range result.Document.Fields {
		result.Document.Fields[index] = cloneField(result.Document.Fields[index])
	}
	result.Document.Warnings = append([]string(nil), result.Document.Warnings...)
	return result
}

func cloneField(field Field) Field {
	if field.Integer != nil {
		value := *field.Integer
		field.Integer = &value
	}
	if field.Number != nil {
		value := *field.Number
		field.Number = &value
	}
	if field.Boolean != nil {
		value := *field.Boolean
		field.Boolean = &value
	}
	if field.Time != nil {
		value := *field.Time
		field.Time = &value
	}
	if field.DurationSeconds != nil {
		value := *field.DurationSeconds
		field.DurationSeconds = &value
	}
	return field
}

func cloneDeviceInfo(info DeviceInfo) DeviceInfo {
	if info.FirmwareCorrupted != nil {
		value := *info.FirmwareCorrupted
		info.FirmwareCorrupted = &value
	}
	if info.ProtocolMatch != nil {
		value := *info.ProtocolMatch
		info.ProtocolMatch = &value
	}
	if info.FlashSizeBytes != nil {
		value := *info.FlashSizeBytes
		info.FlashSizeBytes = &value
	}
	if info.FlashRealSizeBytes != nil {
		value := *info.FlashRealSizeBytes
		info.FlashRealSizeBytes = &value
	}
	if info.ProgramUsagePercent != nil {
		value := *info.ProgramUsagePercent
		info.ProgramUsagePercent = &value
	}
	if info.HeapFreeBytes != nil {
		value := *info.HeapFreeBytes
		info.HeapFreeBytes = &value
	}
	if info.BuildTime != nil {
		value := *info.BuildTime
		info.BuildTime = &value
	}
	if info.UptimeSeconds != nil {
		value := *info.UptimeSeconds
		info.UptimeSeconds = &value
	}
	if info.RSSIDBm != nil {
		value := *info.RSSIDBm
		info.RSSIDBm = &value
	}
	if info.WiFiConnected != nil {
		value := *info.WiFiConnected
		info.WiFiConnected = &value
	}
	if info.StationDHCP != nil {
		value := *info.StationDHCP
		info.StationDHCP = &value
	}
	if info.Update.SizeBytes != nil {
		value := *info.Update.SizeBytes
		info.Update.SizeBytes = &value
	}
	if info.Update.FreeBytes != nil {
		value := *info.Update.FreeBytes
		info.Update.FreeBytes = &value
	}
	if info.Update.Date != nil {
		value := *info.Update.Date
		info.Update.Date = &value
	}
	return info
}

// CloneDocuments is useful to callers that want to re-project a snapshot
// without aliasing the store's endpoint map.
func CloneDocuments(input map[string]Document) map[string]Document {
	output := make(map[string]Document, len(input))
	for endpoint, document := range input {
		output[endpoint] = cloneEndpointResult(EndpointResult{Document: document}).Document
	}
	return output
}

// CopyEndpointMap is intentionally small and exists to keep callers from
// accidentally retaining the store's mutable map.
func CopyEndpointMap(input map[string]EndpointResult) map[string]EndpointResult {
	output := make(map[string]EndpointResult, len(input))
	maps.Copy(output, input)
	for endpoint, result := range output {
		output[endpoint] = cloneEndpointResult(result)
	}
	return output
}
