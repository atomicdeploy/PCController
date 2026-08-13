package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

// settingRow binds presentation to a stable semantic key. Rendering and input
// handling consume the same slice, preventing cursor/index drift.
type settingRow struct {
	Key      string
	Group    string
	Label    string
	Value    string
	Editable bool
}

type settingOption struct {
	Value int
	Label string
}

type settingEditorField struct {
	Key     string
	Label   string
	Value   int
	Min     int
	Max     int
	Step    int
	Unit    string
	Slider  bool
	Options []settingOption
}

// settingEditor is an isolated draft. No EEPROM or host configuration write
// occurs until the operator explicitly saves the dialog.
type settingEditor struct {
	Page          Page
	Key           string
	Title         string
	Fields        []settingEditorField
	Cursor        int
	Text          string
	IsText        bool
	NumberEditing bool
	NumberText    string
}

const peripheralNameSettingPrefix = "peripheral.name:"

func peripheralNameSettingKey(key string) string {
	return peripheralNameSettingPrefix + key
}

func peripheralDescriptorByKey(key string) (appconfig.PeripheralDescriptor, bool) {
	for _, descriptor := range appconfig.PeripheralDescriptors() {
		if descriptor.Key == key {
			return descriptor, true
		}
	}
	return appconfig.PeripheralDescriptor{}, false
}

func peripheralDescriptorForSettingKey(key string) (appconfig.PeripheralDescriptor, bool) {
	if !strings.HasPrefix(key, peripheralNameSettingPrefix) {
		return appconfig.PeripheralDescriptor{}, false
	}
	return peripheralDescriptorByKey(strings.TrimPrefix(key, peripheralNameSettingPrefix))
}

func (model Model) boardSettingRows() []settingRow {
	snapshot := model.snapshot()
	if !snapshot.Connected || !snapshot.HaveSettings || snapshot.Hello.Capabilities&native.CapabilityPersistentSettings == 0 {
		return nil
	}
	settings := snapshot.Settings
	return []settingRow{
		{Key: "sound.silent", Group: "BUZZER", Label: "Board silent mode", Value: boolWord(settings.Flags&native.SettingsSilent != 0, "ON", "OFF"), Editable: true},
		{Key: "programming.lock", Group: "", Label: "Programming lock", Value: boolWord(settings.Flags&native.SettingsProgrammingMode != 0, "ACTIVE", "CLEAR")},
		{Key: "illumination.mode", Group: "LIGHTING", Label: "Enclosure illumination", Value: lightModeName(settings.LightMode), Editable: true},
		{Key: "illumination.on", Group: "", Label: "Door-open brightness", Value: formatPercent(bytePercent(settings.OnBrightness)), Editable: true},
		{Key: "illumination.off", Group: "", Label: "Door-closed brightness", Value: formatPercent(bytePercent(settings.OffBrightness)), Editable: true},
		{Key: "display.open", Group: "DISPLAY", Label: "Door-open brightness", Value: formatPercent(levelPercent(settings.DisplayBrightness, 7)), Editable: true},
		{Key: "display.closed", Group: "", Label: "Door-closed brightness", Value: formatPercent(levelPercent(settings.DisplayClosedBrightness, 7)), Editable: true},
		{Key: "status.brightness", Group: "STATUS LED", Label: "Brightness", Value: formatPercent(bytePercent(settings.StatusBrightness)), Editable: true},
		{Key: "status.color", Group: "", Label: "Fallback color", Value: statusColorName(settings.StatusColor()), Editable: true},
		{Key: "output.persistence", Group: "OUTPUTS", Label: "Restore after reboot", Value: outputPersistenceSummary(settings.OutputPersistence), Editable: true},
		{Key: "relay.restore", Group: "", Label: "Relay restore selection", Value: relayMaskSummary(settings.RelayRestoreMask), Editable: true},
		{Key: "stream.period", Group: "MEASUREMENTS", Label: "Stream period", Value: formatStreamPeriod(settings.StreamPeriodMS), Editable: true},
		{Key: "measurement.decimals", Group: "", Label: "Decimal places", Value: fmt.Sprintf("Voltage %d  ·  Current %d", settings.VoltageDecimals(), settings.CurrentDecimals()), Editable: true},
		{Key: "menu.default", Group: "MENUS", Label: "Default page", Value: fmt.Sprintf("%d · %s", settings.DefaultPage, model.menuPageByID(settings.DefaultPage).Name), Editable: true},
		{Key: "menu.remember", Group: "", Label: "Remember last page", Value: boolWord(settings.SaveLastPage(), "YES", "NO"), Editable: true},
		{Key: "motion.door", Group: "MOTION", Label: "Motion allowed by door state", Value: motionDoorPolicyName(settings.MotionDoorPolicy()), Editable: true},
		{Key: "motion.exit", Group: "", Label: "Menu exit hold", Value: fmt.Sprintf("%d s", settings.MotionExitHoldSeconds), Editable: true},
		{Key: "motion.break", Group: "", Label: "Direction dead-time", Value: fmt.Sprintf("%d ms", settings.MotionBreakMS()), Editable: true},
		{Key: "audio.door", Group: "CUES", Label: "Door open/close buzzer cues", Value: boolWord(settings.DoorAudioEnabled(), "ENABLED", "DISABLED"), Editable: true},
		{Key: "audio.relay", Group: "", Label: "Relay on/off buzzer cues", Value: boolWord(settings.RelayAudioEnabled(), "ENABLED", "DISABLED"), Editable: true},
	}
}

func (model Model) appSettingRows() []settingRow {
	ui := model.uiValue
	appearance := appconfig.NormalizeAppearance(ui.Appearance)
	status := model.hostIntegrationValue.StatusLED
	buzzer := model.hostIntegrationValue.BuzzerMirror
	snapshot := model.snapshot()
	bluetoothAudio := snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityBluetoothAudio != 0
	buzzerPath := appconfig.BuzzerPathUnknown
	if snapshot.HaveSettings {
		buzzerPath = tuiBuzzerPath(snapshot.Settings.Flags&native.SettingsSilent != 0, !buzzer.Enabled)
	}
	requestedPath := buzzer.Path
	if requestedPath == "" && snapshot.HaveSettings {
		requestedPath = buzzerPath
	}
	if requestedPath == "" {
		requestedPath = appconfig.BuzzerPathUnknown
	}
	buzzerRuntime := appconfig.BuzzerRuntimeStatus{RequestedPath: requestedPath, EffectivePath: buzzerPath}
	if model.buzzerRuntime != nil {
		buzzerRuntime = model.buzzerRuntime()
	}
	pathSummary := strings.ToUpper(buzzerRuntime.EffectivePath)
	if buzzerRuntime.RequestedPath != "" && buzzerRuntime.RequestedPath != buzzerRuntime.EffectivePath {
		pathSummary = strings.ToUpper(buzzerRuntime.RequestedPath + " → " + buzzerRuntime.EffectivePath)
	}
	rows := []settingRow{
		{Key: "app.title", Group: "APPLICATION", Label: "Title", Value: model.prefs.AppTitle, Editable: true},
		{Key: "app.tagline", Group: "", Label: "First-run tagline", Value: model.prefs.Tagline, Editable: true},
		{Key: "network.advertisement", Group: "NETWORK", Label: "Discovery advertisement", Value: discoverySummary(model.hostIntegrationValue.Discovery), Editable: true},
		{Key: "network.instance", Group: "", Label: "Advertised instance name", Value: defaultText(model.hostIntegrationValue.Discovery.InstanceName, "system hostname / app title"), Editable: true},
		{Key: "buzzer.path", Group: "BUZZER", Label: "Playback path", Value: pathSummary, Editable: true},
		{Key: "buzzer.renderers", Group: "", Label: "Host renderers", Value: fmt.Sprintf("PC %s · WEB %s", onOff(buzzer.NativeEnabled), onOff(buzzer.WebAudioEnabled)), Editable: true},
		{Key: "buzzer.backend", Group: "", Label: "PC speaker backend", Value: strings.ToUpper(defaultText(buzzerRuntime.BackendRequested, "auto") + " → " + defaultText(buzzerRuntime.BackendEffective, "unavailable")), Editable: true},
		{Key: "buzzer.executable", Group: "", Label: "Beep executable", Value: defaultText(buzzer.Executable, "PATH lookup"), Editable: true},
		{Key: "appearance.identity", Group: "APPEARANCE", Label: "Theme · language · direction", Value: fmt.Sprintf("%s · %s · %s", appearanceThemeLabel(appearance.Theme), appearanceLocaleLabel(appearance.Locale), strings.ToUpper(appearance.Direction)), Editable: true},
		{Key: "appearance.accessibility", Group: "", Label: "Motion · number density", Value: fmt.Sprintf("%s · %s", boolWord(appearance.ReduceMotion, "REDUCED", "FULL"), boolWord(appearance.CompactNumbers, "COMPACT", "DETAILED")), Editable: true},
		{Key: "appearance.audio", Group: "", Label: "Interface audio", Value: fmt.Sprintf("%s · %.0f%%", boolWord(appearance.AudioMuted, "MUTED", "ON"), appearance.AudioVolume*100), Editable: true},
		{Key: "layout.tables", Group: "", Label: "Table layout", Value: strings.ToUpper(ui.TableLayout), Editable: true},
		{Key: "layout.control_colors", Group: "", Label: "Control state colors", Value: onOff(ui.ControlValueColors), Editable: true},
		{Key: "console.enabled", Group: "LOCAL CONSOLE", Label: "Manage local window", Value: onOff(ui.TUIConsole.Enabled), Editable: true},
		{Key: "console.window", Group: "", Label: "Window columns · rows", Value: fmt.Sprintf("%d × %d", ui.TUIConsole.Columns, ui.TUIConsole.Rows), Editable: true},
		{Key: "console.font", Group: "", Label: "Font face", Value: ui.TUIConsole.FontFace, Editable: true},
		{Key: "console.font_size", Group: "", Label: "Font height", Value: fmt.Sprintf("%d px", ui.TUIConsole.FontSize), Editable: true},
		{Key: "poll.active", Group: "MEASUREMENTS", Label: "Active polling", Value: model.prefs.PollInterval.String(), Editable: true},
		{Key: "history.retention", Group: "", Label: "History retention", Value: model.prefs.HistoryWindow.String(), Editable: true},
		{Key: "display.decimals", Group: "", Label: "Decimal places", Value: fmt.Sprintf("V %d  ·  A %d  ·  W %d  ·  °C %d", ui.VoltageDecimals, ui.CurrentDecimals, ui.PowerDecimals, ui.TemperatureDecimals), Editable: true},
		{Key: "diagnostic.visibility", Group: "", Label: "I/O · diagnostics · graphs", Value: fmt.Sprintf("%s · %s · %s", onOff(ui.ShowIO), onOff(ui.ShowDiagnostics), onOff(ui.ShowGraphs)), Editable: true},
		{Key: "events.limit", Group: "HISTORY", Label: "Event transcript", Value: fmt.Sprintf("%d entries", ui.EventLogLimit), Editable: true},
		{Key: "lcd.services", Group: "LCD", Label: "Service · prompt mirror", Value: fmt.Sprintf("%s · %s", onOff(ui.LCDServiceEnabled), onOff(ui.MirrorPromptToLCD)), Editable: true},
		{Key: "led.enabled", Group: "STATUS LED", Label: "Reactive status", Value: onOff(status.Enabled), Editable: true},
		{Key: "led.transition", Group: "", Label: "Eased transition", Value: fmt.Sprintf("%d ms", status.TransitionMS), Editable: true},
		{Key: "led.rf_hold", Group: "", Label: "RF activity hold", Value: fmt.Sprintf("%d ms", status.RFHoldMS), Editable: true},
		{Key: "led.door_hold", Group: "", Label: "Door cue hold", Value: fmt.Sprintf("%d ms", status.DoorCueHoldMS), Editable: true},
		{Key: "led.hot", Group: "", Label: "HOT threshold", Value: fmt.Sprintf("%.2f °C", float64(status.HotThresholdCentiC)/100), Editable: true},
	}
	if snapshot.Connected && snapshot.HaveSettings {
		rows = append(rows, settingRow{Key: "buzzer.path", Group: "BUZZER", Label: "Playback path", Value: strings.ToUpper(buzzerPath), Editable: true})
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.Key == "lcd.services" && snapshot.Hello.Capabilities&native.CapabilityLCD == 0 {
			continue
		}
		if strings.HasPrefix(row.Key, "led.") && snapshot.Hello.Capabilities&native.CapabilityStatusEffects == 0 {
			continue
		}
		filtered = append(filtered, row)
	}
	rows = filtered
	if count := model.availableMeasurementCount(); count != 0 {
		rows = append(rows, settingRow{Key: "measurement.visibility", Group: "VISIBILITY", Label: "Live measurements", Value: model.visibleMeasurementSummary(), Editable: true})
	}
	for index, device := range model.networkDevices {
		hostname, state := device.Host, "host discovered"
		detail := make([]string, 0, 9)
		if device.Public != nil {
			hostname = device.Public.Hostname
			state = boolWord(device.Public.Health.Connectable, "connectable", "advertisement only")
			if device.Public.Host.Version != "" {
				detail = append(detail, "host "+device.Public.Host.Version)
			}
			if device.Public.Board.Connected {
				board := defaultText(device.Public.Board.Identity.Name, "board")
				if device.Public.Board.Identity.BuildHash != "" {
					board += "@" + device.Public.Board.Identity.BuildHash
				}
				detail = append(detail, board)
				if device.Public.Board.Port.Name != "" {
					detail = append(detail, "port "+device.Public.Board.Port.Name)
				}
			}
			telemetry := device.Public.Board.Telemetry
			if telemetry.INA219Available && validVoltageReading(telemetry.SupplyMV) && validCurrentReading(telemetry.CurrentMA) {
				detail = append(detail, fmt.Sprintf("%.3f V · %.3f A", float64(telemetry.SupplyMV)/1000, float64(telemetry.CurrentMA)/1000))
			}
			if telemetry.TemperatureLEDAvailable && validTemperatureReading(telemetry.TemperatureLEDCentiC) {
				detail = append(detail, fmt.Sprintf("T1 %.2f °C", float64(telemetry.TemperatureLEDCentiC)/100))
			}
			if telemetry.TemperatureBTAvailable && validTemperatureReading(telemetry.TemperatureBTAudioCentiC) {
				detail = append(detail, fmt.Sprintf("T2 %.2f °C", float64(telemetry.TemperatureBTAudioCentiC)/100))
			}
		}
		prefix := fmt.Sprintf("%s · %s · %s", hostname, strings.Join(device.Protocols, "+"), state)
		if len(detail) != 0 {
			prefix += " · " + strings.Join(detail, " · ")
		}
		rows = append(rows, settingRow{Key: fmt.Sprintf("network.device.%d", index), Group: "DISCOVERED", Label: device.Name, Value: prefix, Editable: true})
	}
	for _, item := range []struct {
		key, label string
		visual     appconfig.StatusLEDVisual
	}{
		{"idle", "Idle", status.Idle},
		{"running", "Running", status.Running},
		{"bt-connected", "Bluetooth audio connected", status.BluetoothAudioConnected},
		{"bt-searching", "Bluetooth audio searching", status.BluetoothAudioSearching},
		{"bt-off", "Bluetooth audio powered off", status.BluetoothAudioOff},
		{"rf", "RF activity", status.RFActivity},
		{"door-opened", "Door opened", status.DoorOpened},
		{"door-closed", "Door closed", status.DoorClosed},
		{"hot", "HOT", status.Hot},
		{"running-door", "Running + door open", status.RunningDoorOpen},
		{"offline", "PC offline", status.PCOffline},
	} {
		if strings.HasPrefix(item.key, "bt-") && !bluetoothAudio {
			continue
		}
		rows = append(rows, settingRow{
			Key: "led.visual." + item.key, Group: "", Label: item.label,
			Value: visualSummary(item.visual), Editable: true,
		})
	}
	rows = append(rows, model.peripheralNameSettingRows()...)
	if model.remote != nil {
		for index := range rows {
			switch {
			case strings.HasPrefix(rows[index].Key, "led."):
				rows[index].Editable = false
				rows[index].Value = "unavailable from remote IPC"
			case rows[index].Key == "lcd.services":
				rows[index].Editable = false
				rows[index].Value = "local host service unavailable in remote mode"
			case rows[index].Key == "app.title", rows[index].Key == "app.tagline",
				strings.HasPrefix(rows[index].Key, peripheralNameSettingPrefix):
				rows[index].Editable = model.remote.SaveHostUI != nil
				if model.remote.SaveHostUI == nil {
					rows[index].Value = "remote host configuration unavailable"
				}
			}
		}
	}
	return rows
}

func (model Model) peripheralNameSettingRows() []settingRow {
	descriptors := appconfig.PeripheralDescriptors()
	rows := make([]settingRow, 0, len(descriptors))
	previousKind := ""
	for _, descriptor := range descriptors {
		if !model.peripheralAdvertised(descriptor) {
			continue
		}
		group := ""
		if descriptor.Kind != previousKind {
			group = map[string]string{
				"relay": "NAMES · RELAY", "motion": "MOTION", "pwm": "PWM",
				"display": "DISPLAYS", "sensor": "SENSORS",
			}[descriptor.Kind]
			previousKind = descriptor.Kind
		}
		name := model.peripheralName(descriptor.Key, descriptor.DefaultName)
		qualifier := "default"
		if custom := strings.TrimSpace(model.uiValue.PeripheralNames[descriptor.Key]); custom != "" && custom != descriptor.DefaultName {
			qualifier = "custom"
		}
		rows = append(rows, settingRow{
			Key: peripheralNameSettingKey(descriptor.Key), Group: group,
			Label: fmt.Sprintf("%s · %s", descriptor.Key, descriptor.DefaultName),
			Value: fmt.Sprintf("%s · %s", name, qualifier), Editable: true,
		})
	}
	return rows
}

func (model Model) peripheralAdvertised(descriptor appconfig.PeripheralDescriptor) bool {
	snapshot := model.snapshot()
	if !snapshot.Connected {
		return false
	}
	capabilities := snapshot.Hello.Capabilities
	switch descriptor.Kind {
	case "relay", "motion":
		return capabilities&native.CapabilityRelayMotion != 0
	case "pwm":
		return snapshot.HaveStatus && capabilities&native.CapabilityPWM != 0 && snapshot.Status.PWMAvailable
	case "display":
		if descriptor.Key == "display.segment" {
			return capabilities&native.CapabilitySegments != 0
		}
		return snapshot.HaveStatus && capabilities&native.CapabilityLCD != 0 && snapshot.Status.LCDAddress != 0
	case "sensor":
		switch descriptor.Role {
		case "supply-voltage":
			return snapshot.HaveStatus && capabilities&native.CapabilityINA219 != 0 && snapshot.Status.INA219Available && validVoltageReading(snapshot.Status.SupplyMV)
		case "bus-voltage":
			return snapshot.HaveStatus && capabilities&native.CapabilityINA219 != 0 && snapshot.Status.INA219Available && validVoltageReading(snapshot.Status.BusMV)
		case "current":
			return snapshot.HaveStatus && capabilities&native.CapabilityINA219 != 0 && snapshot.Status.INA219Available && validCurrentReading(snapshot.Status.CurrentMA)
		case "power":
			return snapshot.HaveStatus && capabilities&native.CapabilityINA219 != 0 && snapshot.Status.INA219Available && validPowerReading(snapshot.Status.PowerMW)
		case "temperature-led":
			return snapshot.HaveStatus && capabilities&native.CapabilityTemperatures != 0 && snapshot.Status.TLEDAvailable && validTemperatureReading(snapshot.Status.TLEDCenti)
		case "temperature-audio":
			return snapshot.HaveStatus && capabilities&native.CapabilityTemperatures != 0 && capabilities&native.CapabilityBluetoothAudio != 0 && snapshot.Status.TBTAvailable && validTemperatureReading(snapshot.Status.TBTCenti)
		}
	}
	return false
}

func (model Model) selectedSettingRow() (settingRow, bool) {
	var rows []settingRow
	switch model.page {
	case PageBoardSettings:
		rows = model.boardSettingRows()
	case PageAppSettings:
		rows = model.appSettingRows()
	default:
		return settingRow{}, false
	}
	if model.cursor < 0 || model.cursor >= len(rows) {
		return settingRow{}, false
	}
	return rows[model.cursor], true
}

func (model Model) beginSettingEditor() (Model, bool) {
	row, ok := model.selectedSettingRow()
	if !ok {
		return model, false
	}
	if !row.Editable {
		model.setNotice(row.Label + " is read-only")
		return model, true
	}
	editor := &settingEditor{Page: model.page, Key: row.Key, Title: row.Label}
	if model.page == PageBoardSettings {
		model.buildBoardSettingEditor(editor)
	} else {
		model.buildAppSettingEditor(editor)
	}
	if !editor.IsText && len(editor.Fields) == 0 {
		model.setNotice("No editor is available for " + row.Label)
		return model, true
	}
	model.settingEditor = editor
	if _, ok := peripheralDescriptorForSettingKey(editor.Key); ok {
		model.setNotice("Host-only name · Enter saves · Ctrl+U then Enter restores default · Esc discards")
	} else {
		model.setNotice("Edit draft · Enter saves · Esc discards")
	}
	return model, true
}

func (model Model) buildBoardSettingEditor(editor *settingEditor) {
	settings := model.snapshot().Settings
	boolean := func(key, label string, value bool) settingEditorField {
		return settingEditorField{Key: key, Label: label, Value: boolInt(value), Options: onOffOptions()}
	}
	switch editor.Key {
	case "sound.silent":
		editor.Fields = []settingEditorField{boolean("enabled", "Silent", settings.Flags&native.SettingsSilent != 0)}
	case "illumination.mode":
		editor.Fields = []settingEditorField{{Key: "mode", Label: "Mode", Value: int(settings.LightMode), Options: []settingOption{{0, "Off"}, {1, "On"}, {2, "Auto"}}}}
	case "illumination.on":
		editor.Fields = []settingEditorField{percentField("value", "Door open", bytePercent(settings.OnBrightness))}
	case "illumination.off":
		editor.Fields = []settingEditorField{percentField("value", "Door closed", bytePercent(settings.OffBrightness))}
	case "display.open":
		editor.Fields = []settingEditorField{levelField("value", "Door open", int(settings.DisplayBrightness), 7)}
	case "display.closed":
		editor.Fields = []settingEditorField{levelField("value", "Door closed", int(settings.DisplayClosedBrightness), 7)}
	case "status.brightness":
		editor.Fields = []settingEditorField{percentField("value", "Brightness", bytePercent(settings.StatusBrightness))}
	case "status.color":
		editor.Fields = []settingEditorField{{Key: "color", Label: "Fallback color", Value: int(settings.StatusColor()), Options: statusColorOptions()}}
	case "output.persistence":
		editor.Fields = []settingEditorField{
			boolean("motion", "Motion", settings.OutputPersistence&native.OutputPersistMotionDefault != 0),
			boolean("relays", "User relays", settings.OutputPersistence&native.OutputPersistUserRelays != 0),
			boolean("pwm", "User PWM", settings.OutputPersistence&native.OutputPersistUserPWM != 0),
			boolean("direction", "Keep direction on stop", settings.OutputPersistence&native.OutputPersistDirectionOnly != 0),
		}
	case "relay.restore":
		for relay := 1; relay <= 8; relay++ {
			editor.Fields = append(editor.Fields, boolean(fmt.Sprintf("r%d", relay), fmt.Sprintf("Relay %d", relay), settings.RelayRestoreMask&(1<<(relay-1)) != 0))
		}
	case "stream.period":
		editor.Fields = []settingEditorField{{Key: "period", Label: "Stream period", Value: int(settings.StreamPeriodMS), Unit: "ms", Options: intOptions([]int{0, 100, 125, 200, 250, 500, 1000, 2000, 5000}, "ms")}}
	case "measurement.decimals":
		editor.Fields = []settingEditorField{
			rangeField("voltage", "Voltage", int(settings.VoltageDecimals()), 0, 2, 1, "digits", false),
			rangeField("current", "Current", int(settings.CurrentDecimals()), 0, 2, 1, "digits", false),
		}
	case "menu.default":
		options := make([]settingOption, 0, len(model.visibleMenuPages()))
		for _, page := range model.visibleMenuPages() {
			options = append(options, settingOption{Value: int(page.ID), Label: fmt.Sprintf("%d · %s", page.ID, page.Name)})
		}
		editor.Fields = []settingEditorField{{Key: "page", Label: "Default page", Value: int(settings.DefaultPage), Options: options}}
	case "menu.remember":
		editor.Fields = []settingEditorField{boolean("enabled", "Remember last page", settings.SaveLastPage())}
	case "motion.door":
		editor.Fields = []settingEditorField{{Key: "policy", Label: "Door policy", Value: int(settings.MotionDoorPolicy()), Options: []settingOption{{0, "Always"}, {1, "Door closed"}, {2, "Door open"}, {3, "Never"}}}}
	case "motion.exit":
		editor.Fields = []settingEditorField{rangeField("seconds", "Hold to exit", int(settings.MotionExitHoldSeconds), 1, int(native.SettingsMaximumMotionExitHoldSeconds), 1, "s", true)}
	case "motion.break":
		editor.Fields = []settingEditorField{rangeField("milliseconds", "Direction dead-time", int(settings.MotionBreakMS()), 1, 255, 1, "ms", true)}
	case "audio.door":
		editor.Fields = []settingEditorField{boolean("enabled", "Door cues", settings.DoorAudioEnabled())}
	case "audio.relay":
		editor.Fields = []settingEditorField{boolean("enabled", "Relay cues", settings.RelayAudioEnabled())}
	}
}

func (model Model) buildAppSettingEditor(editor *settingEditor) {
	ui := model.uiValue
	status := model.hostIntegrationValue.StatusLED
	if descriptor, ok := peripheralDescriptorForSettingKey(editor.Key); ok {
		editor.IsText = true
		editor.Text = model.peripheralName(descriptor.Key, descriptor.DefaultName)
		return
	}
	boolean := func(key, label string, value bool) settingEditorField {
		return settingEditorField{Key: key, Label: label, Value: boolInt(value), Options: onOffOptions()}
	}
	switch editor.Key {
	case "app.title":
		editor.IsText = true
		editor.Text = ui.AppTitle
	case "app.tagline":
		editor.IsText = true
		editor.Text = ui.Tagline
	case "network.advertisement":
		discovery := model.hostIntegrationValue.Discovery
		editor.Fields = []settingEditorField{
			boolean("dns-sd", "mDNS / DNS-SD", discovery.MDNSEnabled || discovery.DNSSDenabled),
			boolean("ssdp", "SSDP / UPnP", discovery.SSDPEnabled || discovery.UPnPEnabled),
			boolean("ws-discovery", "WS-Discovery", discovery.WSDiscoveryEnabled),
			boolean("broadcast", "UDP broadcast", discovery.BroadcastEnabled),
			boolean("netbios", "NetBIOS", discovery.NetBIOSEnabled),
			rangeField("broadcast-port", "Broadcast port", discovery.BroadcastPort, 1024, 65535, 1, "", false),
		}
	case "network.instance":
		editor.IsText = true
		editor.Text = model.hostIntegrationValue.Discovery.InstanceName
	case "buzzer.path":
		path := model.hostIntegrationValue.BuzzerMirror.Path
		if path == "" && model.snapshot().HaveSettings {
			path = tuiBuzzerPath(model.snapshot().Settings.Flags&native.SettingsSilent != 0, !model.hostIntegrationValue.BuzzerMirror.Enabled)
		}
		if path == "" {
			path = appconfig.BuzzerPathNone
		}
		if model.buzzerRuntime != nil {
			runtime := model.buzzerRuntime()
			if runtime.EffectivePath != "" && runtime.EffectivePath != appconfig.BuzzerPathUnknown {
				path = runtime.EffectivePath
			} else if runtime.RequestedPath != "" && runtime.RequestedPath != appconfig.BuzzerPathUnknown {
				path = runtime.RequestedPath
			}
		}
		editor.Fields = []settingEditorField{{
			Key: "path", Label: "Buzzer path", Value: map[string]int{"board": 0, "host": 1, "both": 2, "none": 3}[path],
			Options: []settingOption{{0, "Board"}, {1, "PC host"}, {2, "Both"}, {3, "None"}},
		}}
	case "buzzer.renderers":
		buzzer := model.hostIntegrationValue.BuzzerMirror
		editor.Fields = []settingEditorField{
			boolean("native", "PC speaker renderer", buzzer.NativeEnabled),
			boolean("web", "Web browser renderer", buzzer.WebAudioEnabled),
		}
	case "buzzer.backend":
		backend := map[string]int{"auto": 0, "native": 1, "external": 2, "off": 3}[strings.ToLower(model.hostIntegrationValue.BuzzerMirror.Backend)]
		editor.Fields = []settingEditorField{{Key: "backend", Label: "PC speaker backend", Value: backend, Options: []settingOption{{0, "Automatic"}, {1, "Native"}, {2, "External command"}, {3, "Off"}}}}
	case "buzzer.executable":
		editor.IsText = true
		editor.Text = model.hostIntegrationValue.BuzzerMirror.Executable
	case "appearance.identity":
		appearance := appconfig.NormalizeAppearance(ui.Appearance)
		theme := map[string]int{"system": 0, "light": 1, "dark": 2}[appearance.Theme]
		locale := map[string]int{"en": 0, "fa": 1}[appearance.Locale]
		direction := map[string]int{"auto": 0, "ltr": 1, "rtl": 2}[appearance.Direction]
		editor.Fields = []settingEditorField{
			{Key: "theme", Label: "Theme", Value: theme, Options: []settingOption{{0, "Follow system"}, {1, "Light"}, {2, "Dark"}}},
			{Key: "locale", Label: "Language", Value: locale, Options: []settingOption{{0, "English"}, {1, "Persian"}}},
			{Key: "direction", Label: "Direction", Value: direction, Options: []settingOption{{0, "Automatic"}, {1, "Left to right"}, {2, "Right to left"}}},
		}
	case "appearance.accessibility":
		editor.Fields = []settingEditorField{
			boolean("reduced-motion", "Reduce motion", ui.Appearance.ReduceMotion),
			boolean("compact-numbers", "Compact numbers", ui.Appearance.CompactNumbers),
		}
	case "appearance.audio":
		editor.Fields = []settingEditorField{
			boolean("muted", "Mute interface audio", ui.Appearance.AudioMuted),
			rangeField("volume", "Interface volume", int(math.Round(ui.Appearance.AudioVolume*100)), 0, 100, 1, "%", true),
		}
	case "layout.tables":
		layout := 0
		if strings.EqualFold(ui.TableLayout, "expanded") {
			layout = 1
		}
		editor.Fields = []settingEditorField{{Key: "layout", Label: "Table layout", Value: layout, Options: []settingOption{{0, "Compact · centered"}, {1, "Expanded · full width"}}}}
	case "layout.control_colors":
		editor.Fields = []settingEditorField{boolean("enabled", "Color control states", ui.ControlValueColors)}
	case "console.enabled":
		editor.Fields = []settingEditorField{boolean("enabled", "Manage local window", ui.TUIConsole.Enabled)}
	case "console.window":
		editor.Fields = []settingEditorField{
			rangeField("columns", "Columns", ui.TUIConsole.Columns, 56, 300, 1, "columns", true),
			rangeField("rows", "Rows", ui.TUIConsole.Rows, 18, 120, 1, "rows", true),
		}
	case "console.font":
		editor.IsText = true
		editor.Text = ui.TUIConsole.FontFace
	case "console.font_size":
		editor.Fields = []settingEditorField{
			rangeField("pixels", "Font height", ui.TUIConsole.FontSize, 5, 72, 1, "px", true),
		}
	case "poll.active":
		editor.Fields = []settingEditorField{{Key: "interval", Label: "Polling interval", Value: ui.StatusIntervalMS, Options: intOptions([]int{100, 125, 200, 250, 500, 1000, 2000, 5000}, "ms")}}
	case "history.retention":
		editor.Fields = []settingEditorField{{Key: "hours", Label: "Retention", Value: ui.HistoryHours, Options: intOptions([]int{1, 6, 12, 24, 48, 72, 168}, "h")}}
	case "display.decimals":
		editor.Fields = []settingEditorField{
			rangeField("voltage", "Voltage", ui.VoltageDecimals, 0, 4, 1, "digits", false),
			rangeField("current", "Current", ui.CurrentDecimals, 0, 4, 1, "digits", false),
			rangeField("power", "Power", ui.PowerDecimals, 0, 4, 1, "digits", false),
			rangeField("temperature", "Temperature", ui.TemperatureDecimals, 0, 2, 1, "digits", false),
		}
	case "measurement.visibility":
		for _, item := range []struct {
			descriptor appconfig.PeripheralDescriptor
			key, label string
			value      bool
		}{
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "supply-voltage"}, "supply", "Supply voltage", ui.ShowSupplyVoltage},
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "bus-voltage"}, "bus", "Bus voltage", ui.ShowBusVoltage},
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "current"}, "current", "Load current", ui.ShowCurrent},
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "power"}, "power", "Load power", ui.ShowPower},
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "temperature-led"}, "temperature-led", "Illumination temperature", ui.ShowTemperatureLED},
			{appconfig.PeripheralDescriptor{Kind: "sensor", Role: "temperature-audio"}, "temperature-bt", "Bluetooth audio temperature", ui.ShowTemperatureBT},
		} {
			if model.peripheralAdvertised(item.descriptor) {
				editor.Fields = append(editor.Fields, boolean(item.key, item.label, item.value))
			}
		}
	case "diagnostic.visibility":
		editor.Fields = []settingEditorField{
			boolean("io", "I/O state", ui.ShowIO),
			boolean("diagnostics", "Diagnostics", ui.ShowDiagnostics),
			boolean("graphs", "Graphs", ui.ShowGraphs),
		}
	case "events.limit":
		editor.Fields = []settingEditorField{{Key: "limit", Label: "Transcript limit", Value: ui.EventLogLimit, Options: intOptions([]int{100, 250, 500, 1000, 2000, 5000, 10000, 50000}, "entries")}}
	case "lcd.services":
		editor.Fields = []settingEditorField{
			boolean("service", "LCD service", ui.LCDServiceEnabled),
			boolean("mirror", "Prompt mirroring", ui.MirrorPromptToLCD),
		}
	case "led.enabled":
		editor.Fields = []settingEditorField{boolean("enabled", "Reactive status", status.Enabled)}
	case "led.transition":
		editor.Fields = []settingEditorField{{Key: "milliseconds", Label: "Eased transition", Value: status.TransitionMS, Options: intOptions([]int{0, 100, 200, 300, 420, 600, 1000, 2000}, "ms")}}
	case "led.rf_hold":
		editor.Fields = []settingEditorField{{Key: "milliseconds", Label: "RF activity hold", Value: status.RFHoldMS, Options: intOptions([]int{250, 500, 900, 1400, 2000, 3000, 5000}, "ms")}}
	case "led.door_hold":
		editor.Fields = []settingEditorField{{Key: "milliseconds", Label: "Door cue hold", Value: status.DoorCueHoldMS, Options: intOptions([]int{250, 500, 900, 1400, 1600, 2000, 3000, 5000}, "ms")}}
	case "led.hot":
		editor.Fields = []settingEditorField{rangeField("centi", "HOT threshold", int(status.HotThresholdCentiC), 3000, 12500, 100, "°C", true)}
	default:
		if strings.HasPrefix(editor.Key, "led.visual.") {
			visual := statusVisualByKey(&status, strings.TrimPrefix(editor.Key, "led.visual."))
			if visual != nil {
				editor.Fields = visualEditorFields(*visual)
			}
		}
	}
}

func discoverySummary(value appconfig.Discovery) string {
	protocols := make([]string, 0, 5)
	if value.MDNSEnabled || value.DNSSDenabled {
		protocols = append(protocols, "DNS-SD")
	}
	if value.SSDPEnabled || value.UPnPEnabled {
		protocols = append(protocols, "UPnP")
	}
	if value.WSDiscoveryEnabled {
		protocols = append(protocols, "WSD")
	}
	if value.BroadcastEnabled {
		protocols = append(protocols, "broadcast")
	}
	if value.NetBIOSEnabled {
		protocols = append(protocols, "NetBIOS")
	}
	if len(protocols) == 0 {
		return "DISABLED"
	}
	return "ENABLED · " + strings.Join(protocols, "+")
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func tuiBuzzerPath(boardSilent, hostSilent bool) string {
	if !boardSilent && !hostSilent {
		return "both"
	}
	if !boardSilent {
		return "board"
	}
	if !hostSilent {
		return "host"
	}
	return "none"
}

func appearanceThemeLabel(value string) string {
	switch value {
	case "light":
		return "Light"
	case "dark":
		return "Dark"
	default:
		return "System"
	}
}

func appearanceLocaleLabel(value string) string {
	if value == "fa" {
		return "Persian"
	}
	return "English"
}

func renderSettingEditor(editor *settingEditor, width int) string {
	if editor == nil {
		return ""
	}
	dialogWidth := width - 12
	if dialogWidth > 76 {
		dialogWidth = 76
	}
	if dialogWidth < 44 {
		dialogWidth = 44
	}
	lines := []string{sectionHeader(dialogWidth, strings.ToUpper(editor.Title), "draft")}
	if editor.IsText {
		lines = append(lines,
			labelStyle.Render("Type the new value:"),
			lipgloss.NewStyle().Foreground(colorBright).Background(colorPanel).Padding(0, 1).Width(dialogWidth-4).Render(editor.Text+"▏"),
		)
	} else {
		rows := make([][]string, 0, len(editor.Fields))
		for index, field := range editor.Fields {
			value := formatEditorField(field)
			if editor.NumberEditing && index == editor.Cursor {
				value = editor.NumberText + numberInputUnit(field) + "▏"
			}
			rows = append(rows, []string{field.Label, value})
		}
		columns := []dataColumn{
			{Title: "SETTING", Width: max(18, (dialogWidth-6)*45/100), Align: lipgloss.Left},
			{Title: "VALUE", Width: max(18, (dialogWidth-6)-(dialogWidth-6)*45/100), Align: lipgloss.Left},
		}
		lines = append(lines, renderDataTable(dialogWidth-2, len(rows), editor.Cursor, columns, rows))
		if visual, ok := editorVisualPreview(editor); ok {
			hex := fmt.Sprintf("#%02X%02X%02X", visual.Red, visual.Green, visual.Blue)
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true).Render("● "+hex+" live color preview"))
		}
	}
	lines = append(lines, "", labelStyle.Render("↑/↓ field · ←/→ adjust · type a value · Enter apply/save · Esc discard"))
	body := strings.Join(lines, "\n")
	return lipgloss.Place(width, max(12, lipgloss.Height(body)+2), lipgloss.Center, lipgloss.Center,
		cardStyle.Copy().Padding(1, 2).Render(body))
}

func (model *Model) adjustSettingEditor(delta int) {
	if model.settingEditor == nil || model.settingEditor.IsText || len(model.settingEditor.Fields) == 0 {
		return
	}
	field := &model.settingEditor.Fields[model.settingEditor.Cursor]
	if len(field.Options) != 0 {
		index := 0
		for candidate, option := range field.Options {
			if option.Value == field.Value {
				index = candidate
				break
			}
		}
		field.Value = field.Options[wrapInt(index, delta, len(field.Options))].Value
		return
	}
	step := field.Step
	if step <= 0 {
		step = 1
	}
	candidate := field.Value + delta*step
	if candidate < field.Min {
		candidate = field.Max
	}
	if candidate > field.Max {
		candidate = field.Min
	}
	field.Value = candidate
}

func (model *Model) setSettingEditorBoundary(maximum bool) {
	if model.settingEditor == nil || model.settingEditor.IsText || len(model.settingEditor.Fields) == 0 {
		return
	}
	field := &model.settingEditor.Fields[model.settingEditor.Cursor]
	if len(field.Options) != 0 {
		index := 0
		if maximum {
			index = len(field.Options) - 1
		}
		field.Value = field.Options[index].Value
		return
	}
	field.Value = field.Min
	if maximum {
		field.Value = field.Max
	}
}

func formatEditorField(field settingEditorField) string {
	for _, option := range field.Options {
		if option.Value == field.Value {
			return option.Label
		}
	}
	value := fmt.Sprintf("%d", field.Value)
	if field.Unit == "°C" {
		value = fmt.Sprintf("%.2f °C", float64(field.Value)/100)
	} else if field.Unit != "" {
		value += " " + field.Unit
	}
	if field.Slider && field.Max > field.Min {
		percent := (field.Value - field.Min) * 100 / (field.Max - field.Min)
		value = sliderPercent(percent, 18) + "  " + value
	}
	return value
}

func editorField(editor *settingEditor, key string) int {
	for _, field := range editor.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return 0
}

func editorVisualPreview(editor *settingEditor) (appconfig.RGBColor, bool) {
	if editor == nil || !strings.HasPrefix(editor.Key, "led.visual.") {
		return appconfig.RGBColor{}, false
	}
	red, redErr := checkedUint8(editorField(editor, "red"))
	green, greenErr := checkedUint8(editorField(editor, "green"))
	blue, blueErr := checkedUint8(editorField(editor, "blue"))
	if redErr != nil || greenErr != nil || blueErr != nil {
		return appconfig.RGBColor{}, false
	}
	return appconfig.RGBColor{Red: red, Green: green, Blue: blue}, true
}

func visualEditorFields(visual appconfig.StatusLEDVisual) []settingEditorField {
	effects := []settingOption{{0, "Steady"}, {1, "Flash"}, {2, "Breathe"}, {3, "Crossfade"}}
	effect := map[string]int{"steady": 0, "flash": 1, "breathe": 2, "crossfade": 3}[strings.ToLower(visual.Effect)]
	return []settingEditorField{
		{Key: "effect", Label: "Effect", Value: effect, Options: effects},
		rangeField("red", "Primary red", int(visual.Color.Red), 0, 255, 5, "", true),
		rangeField("green", "Primary green", int(visual.Color.Green), 0, 255, 5, "", true),
		rangeField("blue", "Primary blue", int(visual.Color.Blue), 0, 255, 5, "", true),
		rangeField("alt-red", "Alternate red", int(visual.AlternateColor.Red), 0, 255, 5, "", true),
		rangeField("alt-green", "Alternate green", int(visual.AlternateColor.Green), 0, 255, 5, "", true),
		rangeField("alt-blue", "Alternate blue", int(visual.AlternateColor.Blue), 0, 255, 5, "", true),
		percentField("brightness", "Brightness", bytePercent(visual.Brightness)),
		percentField("minimum", "Minimum brightness", bytePercent(visual.MinimumBrightness)),
		rangeField("period", "Animation period", visual.PeriodMS, 0, 60_000, 100, "ms", true),
	}
}

func statusVisualByKey(policy *appconfig.StatusLEDPolicy, key string) *appconfig.StatusLEDVisual {
	switch key {
	case "idle":
		return &policy.Idle
	case "running":
		return &policy.Running
	case "bt-connected":
		return &policy.BluetoothAudioConnected
	case "bt-searching":
		return &policy.BluetoothAudioSearching
	case "bt-off":
		return &policy.BluetoothAudioOff
	case "rf":
		return &policy.RFActivity
	case "door-opened":
		return &policy.DoorOpened
	case "door-closed":
		return &policy.DoorClosed
	case "hot":
		return &policy.Hot
	case "running-door":
		return &policy.RunningDoorOpen
	case "offline":
		return &policy.PCOffline
	default:
		return nil
	}
}

func rangeField(key, label string, value, minimum, maximum, step int, unit string, slider bool) settingEditorField {
	return settingEditorField{Key: key, Label: label, Value: value, Min: minimum, Max: maximum, Step: step, Unit: unit, Slider: slider}
}

func percentField(key, label string, value int) settingEditorField {
	return rangeField(key, label, value, 0, 100, 1, "%", true)
}

func levelField(key, label string, value, maximum int) settingEditorField {
	return percentField(key, label, levelPercent(byte(value), maximum))
}

func onOffOptions() []settingOption {
	return []settingOption{{0, "Off"}, {1, "On"}}
}

func statusColorOptions() []settingOption {
	return []settingOption{{0, "Red"}, {1, "Blue"}, {2, "Violet"}, {3, "Green"}, {4, "White"}}
}

func intOptions(values []int, unit string) []settingOption {
	result := make([]settingOption, 0, len(values))
	for _, value := range values {
		label := fmt.Sprintf("%d", value)
		if value == 0 && unit == "ms" {
			label = "Off"
		} else if unit != "" {
			label += " " + unit
		}
		result = append(result, settingOption{Value: value, Label: label})
	}
	return result
}

func (model Model) availableMeasurementCount() int {
	count := 0
	for _, role := range []string{"supply-voltage", "bus-voltage", "current", "power", "temperature-led", "temperature-audio"} {
		if model.peripheralAdvertised(appconfig.PeripheralDescriptor{Kind: "sensor", Role: role}) {
			count++
		}
	}
	return count
}

func (model Model) visibleMeasurementSummary() string {
	visible, available := 0, 0
	for _, item := range []struct {
		role    string
		visible bool
	}{
		{"supply-voltage", model.uiValue.ShowSupplyVoltage},
		{"bus-voltage", model.uiValue.ShowBusVoltage},
		{"current", model.uiValue.ShowCurrent},
		{"power", model.uiValue.ShowPower},
		{"temperature-led", model.uiValue.ShowTemperatureLED},
		{"temperature-audio", model.uiValue.ShowTemperatureBT},
	} {
		if !model.peripheralAdvertised(appconfig.PeripheralDescriptor{Kind: "sensor", Role: item.role}) {
			continue
		}
		available++
		if item.visible {
			visible++
		}
	}
	return fmt.Sprintf("%d of %d visible", visible, available)
}

func outputPersistenceSummary(flags byte) string {
	var values []string
	for _, item := range []struct {
		mask byte
		name string
	}{{native.OutputPersistMotionDefault, "motion"}, {native.OutputPersistUserRelays, "relays"}, {native.OutputPersistUserPWM, "PWM"}, {native.OutputPersistDirectionOnly, "direction"}} {
		if flags&item.mask != 0 {
			values = append(values, item.name)
		}
	}
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, " · ")
}

func relayMaskSummary(mask byte) string {
	if mask == 0 {
		return "none"
	}
	return relaySummary(mask)
}

func visualSummary(visual appconfig.StatusLEDVisual) string {
	return fmt.Sprintf("#%02X%02X%02X · %s · %s", visual.Color.Red, visual.Color.Green, visual.Color.Blue, visual.Effect, formatPercent(bytePercent(visual.Brightness)))
}

func statusColorName(value byte) string {
	for _, option := range statusColorOptions() {
		if option.Value == int(value) {
			return option.Label
		}
	}
	return "Unknown"
}

func formatStreamPeriod(milliseconds uint16) string {
	if milliseconds == 0 {
		return "off"
	}
	return fmt.Sprintf("%d ms", milliseconds)
}

func onOff(value bool) string {
	return boolWord(value, "ON", "OFF")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bytePercent(value byte) int {
	return (int(value)*100 + 127) / 255
}

func percentByte(value int) byte {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return byte((value*255 + 50) / 100)
}

func levelPercent(value byte, maximum int) int {
	if maximum <= 0 {
		return 0
	}
	return (int(value)*100 + maximum/2) / maximum
}

func percentLevel(value, maximum int) byte {
	if maximum <= 0 {
		return 0
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return byte((value*maximum + 50) / 100)
}

func numberInputUnit(field settingEditorField) string {
	if field.Unit == "" {
		return " "
	}
	return " " + field.Unit + " "
}

func parseEditorNumber(field settingEditorField, text string) (int, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("enter a valid number")
	}
	if field.Unit == "°C" {
		value *= 100
	}
	converted := int(math.Round(value))
	if converted < field.Min || converted > field.Max {
		minimum, maximum := fmt.Sprintf("%d", field.Min), fmt.Sprintf("%d", field.Max)
		if field.Unit == "°C" {
			minimum = fmt.Sprintf("%.2f", float64(field.Min)/100)
			maximum = fmt.Sprintf("%.2f", float64(field.Max)/100)
		}
		return 0, fmt.Errorf("value must be %s..%s %s", minimum, maximum, field.Unit)
	}
	return converted, nil
}

func formatPercent(value int) string {
	return fmt.Sprintf("%d%%", value)
}
