package diagnostics

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var realFlashSizePattern = regexp.MustCompile(`(?i)\(\s*real\s*:\s*([^)]+)\)`)

// Project merges the latest successful endpoint documents into one typed
// device view. Projection is deterministic and may be called after each
// endpoint update; a removed field therefore cannot linger indefinitely.
func Project(documents map[string]Document) DeviceInfo {
	var info DeviceInfo
	if document, ok := documents[EndpointInfo]; ok {
		projectInfo(&info, document)
	}
	if document, ok := documents[EndpointWiFiSignal]; ok {
		projectSignal(&info, document)
	}
	if document, ok := documents[EndpointWiFiStatus]; ok {
		projectWiFiStatus(&info, document)
	}
	if document, ok := documents[EndpointConfigST]; ok {
		projectStationConfig(&info, document)
	}
	if document, ok := documents[EndpointConfigAP]; ok {
		projectAccessPointConfig(&info, document)
	}
	if document, ok := documents[EndpointWiFiCountry]; ok {
		if field, found := findField(document, "country", "country_code", "code", "value", "line_1"); found {
			info.Country = strings.ToUpper(strings.TrimSpace(field.Display))
		}
	}
	if document, ok := documents[EndpointUpdateInfo]; ok {
		projectUpdate(&info, document)
	}
	if document, ok := documents[EndpointDescription]; ok {
		projectDescription(&info, document)
	}
	if info.DeviceName == "" {
		info.DeviceName = info.Description.FriendlyName
	}
	return info
}

func projectInfo(info *DeviceInfo, document Document) {
	info.DeviceName = fieldText(document, "device", "device_name", "name")
	info.Hostname = fieldText(document, "hostname", "host_name")
	info.Firmware = fieldText(document, "firmware", "firmware_version", "version")
	info.WiFiVersion = fieldText(document, "wifi_version")
	info.ESPChipID = fieldText(document, "esp_chip_id", "chip_id")
	info.FlashChipID = fieldText(document, "flash_chip_id")

	if value := fieldText(document, "firmware_hash", "hash"); value != "" {
		corrupted := strings.Contains(strings.ToUpper(value), "CORRUPTED")
		info.FirmwareCorrupted = boolPointer(corrupted)
		info.FirmwareHash = strings.TrimSpace(strings.SplitN(value, "(", 2)[0])
	}
	if value := fieldText(document, "protocol_rev", "protocol_revision"); value != "" {
		match := !strings.Contains(strings.ToLower(value), "no match")
		info.ProtocolMatch = boolPointer(match)
		info.ProtocolRevision = strings.TrimSpace(strings.SplitN(value, "(", 2)[0])
	}

	info.StationMAC = fieldText(document, "st_mac", "station_mac")
	info.AccessPointMAC = fieldText(document, "ap_mac", "access_point_mac")
	info.ClientIP = fieldText(document, "client_ip")

	if field, ok := findField(document, "flash_size"); ok {
		if field.Integer != nil {
			info.FlashSizeBytes = int64Pointer(*field.Integer)
		} else if value, parsed := parseByteCount(field.Raw); parsed {
			info.FlashSizeBytes = int64Pointer(value)
		}
		if match := realFlashSizePattern.FindStringSubmatch(field.Raw); len(match) > 1 {
			if value, parsed := parseByteCount(match[1]); parsed {
				info.FlashRealSizeBytes = int64Pointer(value)
			}
		}
	}
	if field, ok := findField(document, "program_usage", "program_usage_percent"); ok {
		switch {
		case field.Number != nil:
			info.ProgramUsagePercent = float64Pointer(*field.Number)
		case field.Integer != nil:
			info.ProgramUsagePercent = float64Pointer(float64(*field.Integer))
		default:
			if value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(field.Raw), "%"), 64); err == nil {
				info.ProgramUsagePercent = float64Pointer(value)
			}
		}
	}
	if value := fieldText(document, "settings_hash"); value != "" {
		parts := strings.SplitN(value, ",", 2)
		info.SettingsHash = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			status := strings.TrimSpace(parts[1])
			status = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(status), "status:"))
			info.SettingsStatus = status
		}
	}
	if field, ok := findField(document, "heap_memory", "heap_free", "free_heap"); ok {
		if field.Integer != nil {
			info.HeapFreeBytes = int64Pointer(*field.Integer)
		} else if value, parsed := parseByteCount(field.Raw); parsed {
			info.HeapFreeBytes = int64Pointer(value)
		}
	}
	info.Build = fieldText(document, "build")
	if parsed, ok := findEmbeddedBuildTime(info.Build); ok {
		info.BuildTime = timePointer(parsed)
	}
	if field, ok := findField(document, "uptime", "uptime_seconds"); ok {
		switch {
		case field.DurationSeconds != nil:
			info.UptimeSeconds = int64Pointer(*field.DurationSeconds)
		case field.Integer != nil:
			info.UptimeSeconds = int64Pointer(*field.Integer)
		default:
			if seconds, parsed := parseUptime(field.Raw); parsed {
				info.UptimeSeconds = int64Pointer(seconds)
			}
		}
	}
	info.ReportedState = strings.ToLower(fieldText(document, "state"))
}

func projectSignal(info *DeviceInfo, document Document) {
	if field, ok := findField(document, "rssi", "signal", "signal_strength", "line_1"); ok {
		switch {
		case field.Integer != nil:
			info.RSSIDBm = int64Pointer(*field.Integer)
		default:
			raw := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(field.Raw), "dbm"))
			if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
				info.RSSIDBm = int64Pointer(value)
			}
		}
	}
	info.SignalQuality = fieldText(document, "quality", "signal_quality", "line_2")
}

func projectWiFiStatus(info *DeviceInfo, document Document) {
	if field, ok := findField(document, "connected", "wifi_connected"); ok {
		if field.Boolean != nil {
			info.WiFiConnected = boolPointer(*field.Boolean)
		} else if value, parsed := parseBoolean(field.Raw); parsed {
			info.WiFiConnected = boolPointer(value)
		}
	}
	// JSON development doubles historically placed configuration in
	// /wifi/status. Preserve that compatibility, while the real firmware's
	// station values come from the projection-only /config/st source.
	if value := fieldText(document, "ssid"); value != "" {
		info.SSID = value
	}
	if value := fieldText(document, "ip", "wifi_ip"); value != "" {
		info.WiFiIP = value
	}
	if value := fieldText(document, "ap_status", "access_point_status"); value != "" {
		info.AccessPointStatus = accessPointStatus(value)
	}
	if value := fieldText(document, "st_status", "station_status"); value != "" {
		info.StationStatus, info.WiFiConnected = stationStatus(value)
	} else if info.WiFiConnected != nil && info.StationStatus == "" {
		if *info.WiFiConnected {
			info.StationStatus = "Connected"
		} else {
			info.StationStatus = "Disconnected"
		}
	}
	if info.RSSIDBm == nil {
		if field, ok := findField(document, "rssi", "signal"); ok {
			if field.Integer != nil {
				info.RSSIDBm = int64Pointer(*field.Integer)
			}
		}
	}
}

func projectStationConfig(info *DeviceInfo, document Document) {
	if value := fieldText(document, "ssid"); value != "" {
		info.SSID = value
	}
	if value := fieldText(document, "ip", "wifi_ip"); value != "" {
		info.WiFiIP = value
	}
	info.StationNetmask = fieldText(document, "netmask", "subnet_mask")
	info.StationGateway = fieldText(document, "gw", "gateway", "gateway_ip")
	info.StationDNS = fieldText(document, "dns", "dns_servers")
	if field, ok := findField(document, "dhcp", "dhcp_enabled"); ok {
		if field.Boolean != nil {
			info.StationDHCP = boolPointer(*field.Boolean)
		} else if value, parsed := parseBoolean(field.Raw); parsed {
			info.StationDHCP = boolPointer(value)
		}
	}
}

func projectAccessPointConfig(info *DeviceInfo, document Document) {
	info.AccessPointSSID = fieldText(document, "ssid")
	info.AccessPointIP = fieldText(document, "ip", "access_point_ip")
	info.AccessPointNetmask = fieldText(document, "netmask", "subnet_mask")
}

func stationStatus(value string) (string, *bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "1", "connected", "ok", "got ip", "got_ip", "online":
		return "Connected", boolPointer(true)
	case "2", "disconnected", "offline":
		return "Disconnected", boolPointer(false)
	case "3", "connecting":
		return "Connecting", boolPointer(false)
	case "4", "not found", "ssid not found", "no ssid", "no ap found":
		return "SSID not found", boolPointer(false)
	case "5", "wrong password":
		return "Wrong password", boolPointer(false)
	case "6", "connect fail", "connection failed":
		return "Connection failed", boolPointer(false)
	case "7", "idle":
		return "Idle", boolPointer(false)
	case "0", "unknown", "undefined":
		return "Unknown", nil
	default:
		return strings.TrimSpace(value), nil
	}
}

func accessPointStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "ok", "enabled", "on":
		return "Enabled"
	case "2", "disconnected", "disabled", "off":
		return "Disabled"
	case "0", "unknown", "undefined":
		return "Unknown"
	default:
		return strings.TrimSpace(value)
	}
}

func projectUpdate(info *DeviceInfo, document Document) {
	info.Update.Hash = fieldText(document, "hash", "firmware_hash")
	if field, ok := findField(document, "size", "firmware_size", "size_bytes"); ok {
		if field.Integer != nil {
			info.Update.SizeBytes = int64Pointer(*field.Integer)
		} else if value, parsed := parseByteCount(field.Raw); parsed {
			info.Update.SizeBytes = int64Pointer(value)
		}
	}
	if field, ok := findField(document, "free", "free_space", "free_bytes"); ok {
		if field.Integer != nil {
			info.Update.FreeBytes = int64Pointer(*field.Integer)
		} else if value, parsed := parseByteCount(field.Raw); parsed {
			info.Update.FreeBytes = int64Pointer(value)
		}
	}
	info.Update.Build = fieldText(document, "date", "build", "build_date")
	if field, ok := findField(document, "date", "build_date"); ok {
		if field.Time != nil {
			info.Update.Date = timePointer(*field.Time)
		} else if parsed, valid := parseWireTime(field.Raw); valid {
			info.Update.Date = timePointer(parsed)
		}
	}
}

func projectDescription(info *DeviceInfo, document Document) {
	info.Description.FriendlyName = fieldText(document, "friendly_name")
	info.Description.ModelName = fieldText(document, "model_name")
	info.Description.ModelNumber = fieldText(document, "model_number")
	info.Description.Manufacturer = fieldText(document, "manufacturer")
	info.Description.ManufacturerURL = fieldText(document, "manufacturer_url")
	info.Description.SerialNumber = fieldText(document, "serial_number")
	info.Description.UDN = fieldText(document, "udn")
}

func findField(document Document, keys ...string) (Field, bool) {
	for _, wanted := range keys {
		wanted = canonicalKey(wanted)
		for _, field := range document.Fields {
			if field.Key == wanted {
				return field, true
			}
		}
	}
	return Field{}, false
}

func fieldText(document Document, keys ...string) string {
	field, ok := findField(document, keys...)
	if !ok || field.Redacted {
		return ""
	}
	return strings.TrimSpace(field.Display)
}

func findEmbeddedBuildTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	// ASA appends "by user@host"; parse only the firmware build prefix.
	if before, _, found := strings.Cut(value, " by "); found {
		value = before
	}
	return parseWireTime(value)
}
