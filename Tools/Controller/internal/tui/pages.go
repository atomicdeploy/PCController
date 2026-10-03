package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
)

func (model Model) pageView(snapshot control.Snapshot) string {
	if model.portPicker {
		return model.fitContent(model.portPickerPage(snapshot))
	}
	if model.settingEditor != nil {
		return model.fitContent(renderSettingEditor(model.settingEditor, model.width))
	}
	if model.displayEditor != nil {
		return model.fitContent(renderDisplayEditor(model.displayEditor, model.width))
	}
	var content string
	switch model.page {
	case PageDashboard:
		content = model.dashboardPage(snapshot)
	case PageOutputs:
		content = model.outputsPage(snapshot)
	case PageMenus:
		content = model.menusPage(snapshot)
	case PageBoardSettings:
		content = model.boardSettingsPage(snapshot)
	case PageAppSettings:
		content = model.appSettingsPage()
	case PageRF:
		content = model.rfPage()
	case PageProgramming:
		content = model.programmingPage(snapshot)
	case PageAutomations:
		content = model.automationsPage()
	case PageEvents:
		content = model.eventsPage()
	case PageConsole:
		content = model.consolePage()
	}
	return model.fitContent(content)
}

func (model Model) portPickerPage(snapshot control.Snapshot) string {
	status, detail, _ := tuiConnectionPresentation(snapshot, model.connectPending, time.Now())
	connection := status
	if detail != "" {
		connection += " · " + detail
	}
	lines := []string{
		sectionHeader(model.width, "SELECT SERIAL DEVICE", "↑/↓ select · Enter open · Esc cancel"),
		kvCard(model.width, 12, "Connection", connection),
	}
	if target := compactConnectionCandidate(snapshot); target != "" {
		lines = append(lines, kvCard(model.width, 12, "Target", target))
	}
	if reason := strings.TrimSpace(snapshot.ConnectionReason); reason != "" && !snapshot.Connected {
		lines = append(lines, kvCard(model.width, 12, "Last failure", reason))
	}
	if model.portLoading {
		lines = append(lines, warnStyle.Render(model.spinnerView()+" querying Windows serial devices…"))
	}
	if model.portError != "" {
		lines = append(lines, errorStyle.Render(model.portError))
	}
	if len(model.portCandidates) == 0 && !model.portLoading {
		lines = append(lines, warnStyle.Render("No serial devices found. Auto-reconnect remains armed unless explicitly closed."))
	}
	for index, candidate := range model.portCandidates {
		line := candidate.Label()
		if strings.TrimSpace(candidate.Name) != "" &&
			strings.EqualFold(strings.TrimSpace(candidate.Name), strings.TrimSpace(snapshot.Port.Name)) {
			line += "  · CURRENT"
		}
		if index == model.portCursor {
			line = selectedStyle.Copy().Width(model.width - 2).Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (model Model) dashboardPage(snapshot control.Snapshot) string {
	status := snapshot.Status
	haveStatus := snapshot.Connected && snapshot.HaveStatus
	capabilities := snapshot.Hello.Capabilities
	pageWidth := model.width
	if pageWidth <= 0 {
		pageWidth = 132
	}
	lcdAddress, lcdAvailable := model.lcdDisplayState(snapshot)
	lcdStatus := ""
	if lcdAvailable {
		lcdStatus = fmt.Sprintf("available · 0x%02X", lcdAddress)
	}
	sectionWidth := pageWidth
	if pageWidth >= 96 {
		outerCardWidth := (pageWidth - 1) / 2
		sectionWidth = outerCardWidth - cardStyle.GetHorizontalFrameSize()
	}
	if !snapshot.Connected {
		connectionStatus, _, phase := tuiConnectionPresentation(snapshot, model.connectPending, time.Now())
		connectionWidth := pageWidth - cardStyle.GetHorizontalFrameSize()
		if connectionWidth < 1 {
			connectionWidth = 1
		}
		connectionLines := []string{
			sectionHeader(connectionWidth, "BOARD CONNECTION", connectionStatus),
		}
		if snapshot.EmergencyStop.Active {
			connectionLines = append(connectionLines,
				buttonBadStyle.Copy().Bold(true).Render("E · E-STOP LOCKED · release"),
				kvCard(connectionWidth, 14, "Interlock", emergencyStopSummary(snapshot.EmergencyStop)),
			)
		} else {
			connectionLines = append(connectionLines, buttonBadStyle.Render("E · Engage E-STOP"))
		}
		candidate := compactConnectionCandidate(snapshot)
		if candidate != "" {
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Device", candidate))
		} else {
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Device", connectionDeviceSummary(model)))
		}
		if snapshot.ConnectionAttempt != 0 {
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Attempt", fmt.Sprintf("%d", snapshot.ConnectionAttempt)))
		}
		if !snapshot.ConnectionAttemptStart.IsZero() && phase == "attempting" {
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Elapsed", formatConnectionDuration(time.Since(snapshot.ConnectionAttemptStart))))
		}
		if !snapshot.ConnectionNextRetry.IsZero() && phase == "waiting_retry" {
			remaining := time.Until(snapshot.ConnectionNextRetry)
			if remaining < 0 {
				remaining = 0
			}
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Retry", "in "+formatConnectionDuration(remaining)))
		}
		if reason := strings.TrimSpace(snapshot.ConnectionReason); reason != "" {
			label := "Reason"
			if phase == "waiting_retry" {
				label = "Last failure"
			}
			connectionLines = append(connectionLines, kvCard(connectionWidth, 14, label, reason))
		}
		if len(snapshot.HardwareProblems) != 0 {
			connectionLines = append(connectionLines, errorStyle.Render(kvCard(connectionWidth, 14, "Hardware", "⚠ "+hardwareProblemMessage(snapshot.HardwareProblems[0]))))
		}
		connectionLines = append(connectionLines, kvCard(connectionWidth, 14, "Next", connectionRecoveryAction(phase)))
		return cardStyle.Copy().Width(connectionWidth).Render(strings.Join(connectionLines, "\n"))
	}
	measurementLines := []string{
		sectionHeader(sectionWidth, "LIVE MEASUREMENTS", model.statusFreshnessLabel(snapshot, time.Now())),
	}
	if len(snapshot.HardwareProblems) != 0 {
		measurementLines = append(
			measurementLines,
			errorStyle.Copy().Bold(true).Render(
				truncateDisplayText("⚠ "+hardwareProblemMessage(snapshot.HardwareProblems[0]), sectionWidth),
			),
		)
	}
	if warning := model.remoteClockWarning(); warning != "" {
		measurementLines = append(measurementLines, warnStyle.Render(truncateDisplayText(warning, sectionWidth)))
	}
	measurementAdvertised := snapshot.Connected && capabilities&(native.CapabilityINA219|native.CapabilityTemperatures) != 0
	if measurementAdvertised && !snapshot.HaveStatus {
		measurementLines = append(measurementLines, warnStyle.Render("Waiting for the first STATUS frame…"))
	}
	if haveStatus && capabilities&native.CapabilityINA219 != 0 && status.INA219Available &&
		validVoltageReading(status.SupplyMV) && model.prefs.Visible["supply"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.supply-voltage", "Supply Voltage"), formatVoltage(status.SupplyMV, model.prefs.VoltageDecimals)))
	}
	if haveStatus && capabilities&native.CapabilityINA219 != 0 && status.INA219Available &&
		validVoltageReading(status.BusMV) && model.prefs.Visible["bus"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.bus-voltage", "Bus Voltage"), formatVoltage(status.BusMV, model.prefs.VoltageDecimals)))
	}
	if haveStatus && capabilities&native.CapabilityINA219 != 0 && status.INA219Available &&
		validCurrentReading(status.CurrentMA) && model.prefs.Visible["current"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.current", "Load Current"), formatCurrent(status.CurrentMA, model.prefs.CurrentDecimals)))
	}
	if haveStatus && capabilities&native.CapabilityINA219 != 0 && status.INA219Available &&
		validPowerReading(status.PowerMW) && model.prefs.Visible["power"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.power", "Load Power"), formatPower(status.PowerMW, model.prefs.PowerDecimals)))
	}
	if haveStatus && capabilities&native.CapabilityTemperatures != 0 && status.TLEDAvailable &&
		validTemperatureReading(status.TLEDCenti) && model.prefs.Visible["temperature_led"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.temperature-led", "Temperature · Illumination LED"), formatTemperature(status.TLEDCenti, model.prefs.TemperatureDecimals)))
	}
	if haveStatus && capabilities&native.CapabilityTemperatures != 0 &&
		capabilities&native.CapabilityBluetoothAudio != 0 && status.TBTAvailable &&
		validTemperatureReading(status.TBTCenti) && model.prefs.Visible["temperature_bt"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.temperature-audio", "BT Amplifier temperature"), formatTemperature(status.TBTCenti, model.prefs.TemperatureDecimals)))
	}

	stateTitle := ""
	if haveStatus && capabilities&native.CapabilityMenuRemote != 0 {
		stateTitle = model.menuPageByID(status.MenuPage).Name
	}
	stateLines := []string{sectionHeader(sectionWidth, "BOARD STATE", stateTitle)}
	if snapshot.EmergencyStop.Active {
		stateLines = append(stateLines,
			buttonBadStyle.Copy().Bold(true).Render("E · E-STOP LOCKED · release"),
			errorStyle.Render(kvCard(sectionWidth, 22, "Interlock", emergencyStopSummary(snapshot.EmergencyStop))),
		)
	} else {
		stateLines = append(stateLines, buttonBadStyle.Render("E · Engage E-STOP"))
	}
	if haveStatus && capabilities&native.CapabilityProgramState != 0 {
		stateLines = append(stateLines,
			lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("I · Idle"), " ", buttonGoodStyle.Render("R · Running")),
			kvCard(sectionWidth, 22, "PC Program State", programStateSummary(snapshot.ProgramState)),
		)
	}
	if haveStatus {
		stateLines = append(stateLines, kvCard(sectionWidth, 22, "Device Uptime", formatUptime(status.UptimeMS)))
		if capabilities&native.CapabilityRelayMotion != 0 {
			stateLines = append(stateLines,
				kvCard(sectionWidth, 22, "Enclosure Door", boolWord(status.DoorOpen, "OPEN", "CLOSED")),
				kvCard(sectionWidth, 22, "Active Relays", relaySummary(status.ActiveRelays)),
			)
		}
		if capabilities&native.CapabilityBluetoothAudio != 0 {
			stateLines = append(stateLines, kvCard(sectionWidth, 22, "Bluetooth audio", bluetoothAudioState(status.BluetoothState)))
		}
		if capabilities&native.CapabilityRemoteKeys != 0 {
			stateLines = append(stateLines, kvCard(sectionWidth, 22, "Active Keys", fmt.Sprintf("0x%02X", status.ActiveKeys)))
		}
		if capabilities&native.CapabilitySegments != 0 && capabilities&native.CapabilityMenuRemote != 0 {
			stateLines = append(stateLines,
				kvCard(sectionWidth, 22, model.peripheralName("display.segment", "Display Menu"), fmt.Sprintf("%d · %s", status.MenuPage, model.menuPageByID(status.MenuPage).Name)),
				kvCard(sectionWidth, 22, "Menu / Submode", fmt.Sprintf("%d · %s", status.ProgramMode, model.programModeName(status.ProgramMode))),
			)
		}
	}
	if haveStatus && capabilities&native.CapabilityPWM != 0 && status.PWMAvailable {
		stateLines = append(stateLines, kvCard(sectionWidth, 22, model.peripheralName(fmt.Sprintf("pwm.%d", status.PWMChannel), "PWM"), fmt.Sprintf("channel %d · %d%%", status.PWMChannel, int(status.PWMValue)*100/4095)))
	}
	if lcdAvailable {
		stateLines = append(stateLines, kvCard(sectionWidth, 22, model.peripheralName("display.lcd", "I2C LCD"), lcdStatus))
	}
	if haveStatus && model.prefs.Visible["diagnostics"] {
		stateLines = append(stateLines,
			kvCard(sectionWidth, 22, "Last Reset", fmt.Sprintf("cause 0x%02X", status.ResetCause)),
			kvCard(sectionWidth, 22, "Reset Count", fmt.Sprintf("%d", status.ResetCount)),
		)
		if status.FramingErrors != 0 || status.CRCErrors != 0 || status.PWMErrors != 0 {
			stateLines = append(stateLines, errorStyle.Render(kvCard(sectionWidth, 22, "Protocol Errors", fmt.Sprintf("frame %d · CRC %d · PWM %d", status.FramingErrors, status.CRCErrors, status.PWMErrors))))
		}
	}

	if pageWidth < 96 {
		return strings.Join(measurementLines, "\n") + "\n\n" + strings.Join(stateLines, "\n")
	}
	cardRenderWidth := sectionWidth + cardStyle.GetHorizontalPadding()
	left := cardStyle.Copy().Width(cardRenderWidth).Render(strings.Join(measurementLines, "\n"))
	right := cardStyle.Copy().Width(cardRenderWidth).Render(strings.Join(stateLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func emergencyStopSummary(state control.EmergencyStopState) string {
	parts := []string{"effects and motion blocked"}
	if source := strings.TrimSpace(state.Source); source != "" {
		parts = append(parts, "source "+source)
	}
	if reason := strings.TrimSpace(state.Reason); reason != "" {
		parts = append(parts, reason)
	}
	return strings.Join(parts, " · ")
}

func connectionDeviceSummary(model Model) string {
	if model.portLoading {
		return "Scanning serial devices…"
	}
	if count := len(model.portCandidates); count != 0 {
		return fmt.Sprintf("%d serial device%s available · P to choose", count, pluralSuffix(count))
	}
	return "No serial device selected"
}

func connectionRecoveryAction(phase string) string {
	switch phase {
	case "attempting":
		return "Waiting for board response"
	case "waiting_retry":
		return "Enter / click to retry now · P change device"
	case "queued":
		return "Connection worker queued · Enter to retry now"
	case "paused":
		return "Enter / click to connect · P choose device"
	case "blocked":
		return "Close the owning process or choose another device"
	default:
		return "Enter / click to connect · P choose device"
	}
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func portProcessSummary(process control.PortProcessSnapshot) string {
	if !process.Supported {
		return "unsupported"
	}
	if process.State == "owned" {
		return fmt.Sprintf("%s · PID %d", process.Name, process.PID)
	}
	if process.State == "unknown" && process.Error != "" {
		return "unknown: " + process.Error
	}
	if process.State == "free" && process.TakeoverReady {
		return "FREE · takeover armed"
	}
	return strings.ToUpper(process.State)
}

func programStateSummary(state control.ProgramStateSnapshot) string {
	mode := string(state.Mode)
	if mode == "" {
		mode = "Idle"
	}
	detail := strings.TrimSpace(state.Reason)
	if detail == "" {
		detail = "no active owner"
	}
	if len(state.Owners) != 0 {
		detail += fmt.Sprintf(" · %d owner(s)", len(state.Owners))
	}
	return mode + " · " + detail
}

func (model Model) outputsPage(snapshot control.Snapshot) string {
	tableWidth := model.presentationTableWidth(118)
	columns := outputTableColumns(tableWidth)
	rows := model.controlTableRows(snapshot, max(8, columns[1].Width-7))
	tableView := renderControlTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, columns, rows, model.uiValue.ControlValueColors)
	detail := "↑/↓ select · ←/→ adjust · Home/End limits · Enter activate · F2 rename"
	if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityAddressableLED != 0 {
		detail += " · WS2811 available below"
	}
	parts := []string{sectionHeader(model.width, "CONTROL", detail)}
	if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityRelayMotion != 0 && !snapshot.HaveStatus {
		parts = append(parts, warnStyle.Render(model.spinner.View()+" loading advertised relay and motion state…"))
	}
	if len(rows) != 0 {
		parts = append(parts, lipgloss.PlaceHorizontal(model.width, lipgloss.Center, tableView))
	}
	return strings.Join(parts, "\n")
}

func (model Model) controlTableRows(snapshot control.Snapshot, levelWidth int) []controlTableRow {
	status := snapshot.Status
	rows := make([]controlTableRow, 0, 27)
	if !snapshot.Connected {
		return rows
	}
	if snapshot.HaveStatus && snapshot.Hello.Capabilities&native.CapabilityRelayMotion != 0 {
		for index := 0; index < 8; index++ {
			key := fmt.Sprintf("relay.%d", index+1)
			fallback, _ := appconfig.PeripheralDefaultName(key)
			label := fmt.Sprintf("R%d · %s", index+1, model.peripheralName(key, fallback))
			on := status.ActiveRelays&(1<<index) != 0
			state := "○ OFF"
			tone := controlToneOff
			if on {
				state = "● ON"
				tone = controlToneOn
			}
			group := ""
			if index == 0 {
				group = "RELAYS"
			}
			rows = append(rows, controlTableRow{Group: group, Name: label, Value: state, Tone: tone, Action: fmt.Sprintf("relay %d toggle", index+1), Kind: "relay", Index: index, PeripheralKey: key})
		}
		rows = append(rows, controlTableRow{Name: "All relays", Value: "Turn OFF", Tone: controlToneAction, Action: "relay off", Kind: "relay-all"})
		motionAFallback, _ := appconfig.PeripheralDefaultName("motion.a")
		motionBFallback, _ := appconfig.PeripheralDefaultName("motion.b")
		motionA := model.peripheralName("motion.a", motionAFallback)
		motionB := model.peripheralName("motion.b", motionBFallback)
		rows = append(rows,
			controlTableRow{Group: "MOTION", Name: motionA + " · UP", Value: "Run", Tone: controlToneAction, Action: "relay side left up", Kind: "motion", PeripheralKey: "motion.a"},
			controlTableRow{Name: motionA + " · STOP", Value: "Stop", Tone: controlToneAction, Action: "relay side left stop", Kind: "motion", PeripheralKey: "motion.a"},
			controlTableRow{Name: motionA + " · DOWN", Value: "Run", Tone: controlToneAction, Action: "relay side left down", Kind: "motion", PeripheralKey: "motion.a"},
			controlTableRow{Name: motionB + " · UP", Value: "Run", Tone: controlToneAction, Action: "relay side right up", Kind: "motion", PeripheralKey: "motion.b"},
			controlTableRow{Name: motionB + " · STOP", Value: "Stop", Tone: controlToneAction, Action: "relay side right stop", Kind: "motion", PeripheralKey: "motion.b"},
			controlTableRow{Name: motionB + " · DOWN", Value: "Run", Tone: controlToneAction, Action: "relay side right down", Kind: "motion", PeripheralKey: "motion.b"},
		)
	}
	if snapshot.HaveStatus && snapshot.Hello.Capabilities&native.CapabilityPWM != 0 && status.PWMAvailable {
		for channel := 0; channel <= 10; channel++ {
			value := uint16(0)
			if model.havePWMValues {
				value = model.pwmValues[channel]
			} else if byte(channel) == status.PWMChannel {
				value = status.PWMValue
			}
			key := fmt.Sprintf("pwm.%d", channel)
			fallback, _ := appconfig.PeripheralDefaultName(key)
			name := model.peripheralName(key, fallback)
			percent := int(value) * 100 / 4095
			group := ""
			if channel == 0 {
				group = "PWM"
			}
			rows = append(rows, controlTableRow{
				Group: group, Name: fmt.Sprintf("CH %02d · %s", channel, name),
				Value: sliderPercentPlain(percent, levelWidth) + fmt.Sprintf(" %3d%%", percent), Tone: controlToneLevel,
				Kind: "pwm", Index: channel, PeripheralKey: key,
			})
		}
		value := uint16(0)
		if model.havePWMValues {
			value = model.pwmValues[11]
		} else if status.PWMChannel == 11 {
			value = status.PWMValue
		}
		percent := int(value) * 100 / 4095
		targetBrightness := snapshot.Settings.OffBrightness
		if snapshot.Settings.LightMode == 2 || (snapshot.Settings.LightMode == 1 && status.DoorOpen) {
			targetBrightness = snapshot.Settings.OnBrightness
		}
		target := uint16(targetBrightness)*16 + uint16(targetBrightness)/16
		rows = append(rows, controlTableRow{
			Group: "LIGHTING", Name: "CH 12 · Enclosure illumination · manual override",
			Value: sliderPercentPlain(percent, levelWidth) + fmt.Sprintf(" %3d%% · applied %d/4095 · policy %d/4095", percent, value, target), Tone: controlToneLevel,
			Kind: "pwm", Index: 11, PeripheralKey: "pwm.11",
		})
		rows = append(rows, controlTableRow{Name: "All user PWM", Value: "Set 0%", Tone: controlToneAction, Action: "pwm off", Kind: "pwm-all"})
	}
	if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityAddressableLED != 0 {
		pixels, fps := model.stripConfiguration()
		color := stripColorPresets[model.stripColorIndex()]
		busy := model.macroState().Running || model.macroRecordingState().Active
		busyValue := "Unavailable while macro recording/playback owns MCU workspace"
		mutation := func(row controlTableRow) controlTableRow {
			if busy {
				row.Action = ""
				row.Adjust = ""
				row.Value = busyValue
				row.Tone = controlToneNeutral
			}
			return row
		}
		rows = append(rows,
			mutation(controlTableRow{Group: "WS2811 STRIP", Name: "Pixel count", Value: fmt.Sprintf("%d · ←/→ adjust · Enter configure", pixels), Tone: controlToneLevel, Action: fmt.Sprintf("strip config %d", pixels), Adjust: "strip-count", Kind: "strip"}),
			mutation(controlTableRow{Name: "Rainbow", Value: fmt.Sprintf("%d px @ %d FPS · Enter start", pixels, fps), Tone: controlToneAction, Action: fmt.Sprintf("strip rainbow %d %d", pixels, fps), Adjust: "strip-fps", Kind: "strip"}),
			mutation(controlTableRow{Name: "Fill color", Value: fmt.Sprintf("#%02X%02X%02X · ←/→ color · Enter apply", color[0], color[1], color[2]), Tone: controlToneLevel, Action: fmt.Sprintf("strip fill %d %d %d", color[0], color[1], color[2]), Adjust: "strip-color", Kind: "strip"}),
		)
		for _, effect := range model.stripEffectCatalog() {
			rows = append(rows, mutation(controlTableRow{
				Name: effect.Name, Value: fmt.Sprintf("%s · %d px @ %d FPS · Enter start", effect.Reference, pixels, fps),
				Tone: controlToneAction, Action: fmt.Sprintf("effect play %s %d %d", effect.Reference, pixels, fps), Kind: "strip",
			}))
		}
		rows = append(rows,
			mutation(controlTableRow{Name: "Clear strip", Value: "Enter clear", Tone: controlToneAction, Action: "strip clear", Kind: "strip"}),
			controlTableRow{Name: "Stop stream", Value: "Enter stop", Tone: controlToneAction, Action: "strip stop", Kind: "strip"},
			controlTableRow{Name: "Stream status", Value: "Enter inspect", Tone: controlToneAction, Action: "strip status", Kind: "strip"},
		)
	}
	return rows
}

var stripColorPresets = [][3]byte{{255, 255, 255}, {255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 128, 0}, {128, 0, 255}, {0, 255, 255}}

func (model Model) stripConfiguration() (int, int) {
	pixels, fps := model.stripPixels, model.stripFPS
	if pixels < 1 || pixels > native.StripMaximumPixels {
		pixels = native.StripMaximumPixels
	}
	if fps < 1 || fps > 30 {
		fps = 30
	}
	return pixels, fps
}

func (model Model) stripColorIndex() int {
	if model.stripColor < 0 || model.stripColor >= len(stripColorPresets) {
		return 0
	}
	return model.stripColor
}

type menuPageGeometry struct {
	frontPanelStart int
	frontPanelEnd   int
	entriesStart    int
}

var frontPanelButtonLabels = []string{
	"K1 · previous", "K2 · next", "K3 · decrease", "K4 · increase",
}

func renderFrontPanelButtons() string {
	buttons := make([]string, 0, len(frontPanelButtonLabels))
	for _, label := range frontPanelButtonLabels {
		buttons = append(buttons, buttonStyle.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, intersperseStrings(buttons, " ")...)
}

// menuPagePrefix owns both rendering and hit-test geometry so styling or
// device-detail changes cannot silently shift mouse clicks onto another menu.
func (model Model) menuPagePrefix(snapshot control.Snapshot) ([]string, menuPageGeometry) {
	layoutState := "read-only · firmware capability 23 unavailable"
	if model.menuLayoutStaged.Supported && model.menuLayoutStaged.Persistent {
		layoutState = "MCU EEPROM · GET/SET + readback"
	}
	if model.menuLayoutDirty {
		layoutState += " · STAGED"
	}
	overlayState := "unavailable · capability 24 absent · host-only pages show no false live state"
	if native.SupportsHostMenuOverlay(snapshot.Hello) {
		overlayState = "runtime directory/content enabled · volatile (HOST connection required)"
	}
	searchState := model.menuLayoutSearch
	if searchState == "" {
		searchState = "all"
	}
	if model.menuLayoutSearchEditing {
		searchState = "✎ " + searchState
	}
	headerDetail := ""
	if active, ok := activeMenuPage(snapshot); ok {
		headerDetail = fmt.Sprintf("active %d · %s", active, model.menuPageByID(active).Name)
	}
	lines := []string{sectionHeader(model.width, "DISPLAY MENU MIRROR", headerDetail)}
	if frontPanelSnapshotAvailable(snapshot) {
		lines = append(lines, renderFrontPanel(model.currentFrontPanel(snapshot)))
	} else if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityFrontPanelSnapshot != 0 {
		lines = append(lines, warnStyle.Render(model.spinnerView()+" loading advertised front-panel state…"))
	}
	geometry := menuPageGeometry{}
	if model.frontPanelControlsAvailable(snapshot) {
		buttons := renderFrontPanelButtons()
		geometry.frontPanelStart = lipgloss.Height(strings.Join(lines, "\n"))
		lines = append(lines, buttons)
		geometry.frontPanelEnd = geometry.frontPanelStart + lipgloss.Height(buttons)
	}
	if len(model.displayTargetsFor(snapshot)) != 0 {
		lines = append(lines, buttonGoodStyle.Render("D · Send arbitrary message"))
	}
	lines = append(lines, renderHostMenuDirectory(model.hostMenus, model.width, model.frontPanelControlsAvailable(snapshot)))
	if model.lcdPromptMirrorAvailable(snapshot) {
		lines = append(lines, fmt.Sprintf("LCD prompt mirroring  %s  %s", valueStyle.Render(boolWord(model.lcdMirror, "ON", "OFF")), labelStyle.Render("M toggles · priority events temporarily override and restore")))
	}
	lines = append(lines, labelStyle.Render(fmt.Sprintf("Catalog: %s · Layout: %s · Host overlay: %s · Search: %s · Sort: %s", model.menuCatalogSource, layoutState, overlayState, searchState, model.menuLayoutSort)))
	geometry.entriesStart = lipgloss.Height(strings.Join(lines, "\n"))
	return lines, geometry
}

func activeMenuPage(snapshot control.Snapshot) (byte, bool) {
	if frontPanelSnapshotAvailable(snapshot) {
		return snapshot.FrontPanel.MenuPage, true
	}
	if snapshot.Connected && snapshot.HaveStatus &&
		snapshot.Hello.Capabilities&native.CapabilityMenuRemote != 0 {
		return snapshot.Status.MenuPage, true
	}
	return 0, false
}

func (model Model) menusPage(snapshot control.Snapshot) string {
	active, haveActive := activeMenuPage(snapshot)
	lines, _ := model.menuPagePrefix(snapshot)
	entries := model.menuConfigurationEntries()
	for index, entry := range entries {
		page := entry.Page
		marker := "  "
		if haveActive && page.ID == active {
			marker = "● "
		}
		visibility := "○ hidden"
		if entry.Visible {
			visibility = "✓ shown "
		}
		line := fmt.Sprintf(
			"%srank %02d  %s  %2d %-4s  %s › %-20s %s",
			marker, entry.Rank, visibility, page.ID, page.Short,
			page.Category, page.Name, labelStyle.Render(page.Description),
		)
		lines = append(lines, model.selectionLine(index, line))
	}
	if len(entries) == 0 {
		lines = append(lines, errorStyle.Render("No board menu matches the current search. Press / and Ctrl+U to clear it."))
	} else if selected, ok := model.selectedMenuConfiguration(); ok {
		lines = append(lines,
			sectionHeader(model.width, "NESTED SEVEN-SEGMENT PREVIEW", fmt.Sprintf("%s › %s · immutable stable/wire ID %d · persistent Order ID/rank %d", selected.Page.Category, selected.Page.Name, selected.Page.ID, selected.Rank)),
			renderSevenSegments(selected.Page.Short, [4]byte{}, false, 0, false),
			labelStyle.Render("/ search · S sort · V/Space show-hide · ←/→ or [/] rank · Home/End · E edit host label/content · A apply · X discard · R refresh · Enter jump"),
		)
	}
	if model.menuLayoutError != "" {
		lines = append(lines, errorStyle.Render("Menu layout: "+model.menuLayoutError))
	}
	return strings.Join(lines, "\n")
}

func (model Model) boardSettingsPage(snapshot control.Snapshot) string {
	tableWidth := model.presentationTableWidth(112)
	rows := model.boardSettingRows()
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		value := row.Value
		if !row.Editable {
			value += " · read-only"
		}
		tableRows = append(tableRows, []string{row.Group, row.Label, value})
	}
	lines := []string{sectionHeader(model.width, "BOARD EEPROM SETTINGS", "")}
	advertised := snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityPersistentSettings != 0
	if advertised && !snapshot.HaveSettings {
		lines = append(lines, warnStyle.Render(model.spinner.View()+" loading advertised EEPROM settings…"))
	}
	if len(rows) != 0 {
		lines = append(lines,
			labelStyle.Render("↑/↓ select · Enter opens an isolated draft · ←/→ quick-adjusts · MCU and host settings remain separate"),
			model.centeredDataTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, settingsTableColumns(tableWidth), tableRows),
		)
	}
	return strings.Join(lines, "\n")
}

func (model Model) appSettingsPage() string {
	tableWidth := model.presentationTableWidth(112)
	rows := model.appSettingRows()
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{row.Group, row.Label, row.Value})
	}
	status := "D discover · select a DISCOVERED row + Enter/C to connect"
	if model.networkDiscoveryPending {
		status = model.spinner.View() + " discovering over all enabled network transports…"
	} else if model.networkDiscoveryError != "" {
		status = "discovery error: " + model.networkDiscoveryError
	}
	return strings.Join([]string{
		sectionHeader(model.width, "HOST SETTINGS", "host JSON · "+status),
		labelStyle.Render("↑/↓ select · Enter edits/connects · D discover · F2 quick-renames · Ctrl+U restores default"),
		model.centeredDataTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, settingsTableColumns(tableWidth), tableRows),
	}, "\n")
}

func (model Model) rfPrimaryItems() []actionBarItem {
	items := []actionBarItem{
		{label: "L Learn", action: "rf-learn", style: buttonGoodStyle},
		{label: "Y Timed learn · 30s", action: "rf-timer", style: buttonStyle},
	}
	if model.preview == nil && model.rfLearnState().Active {
		items = []actionBarItem{{label: "C Cancel learning", action: "rf-cancel", style: buttonBadStyle}}
	}
	return append(items,
		actionBarItem{label: "R Refresh list", action: "rf-refresh", style: buttonStyle},
		actionBarItem{label: "T Transmit", action: "rf-transmit", style: buttonStyle},
	)
}

func (model Model) rfPage() string {
	if model.rfActionPicker {
		return model.rfActionPickerPage()
	}
	if model.rfCategoryPicker {
		return model.rfCategoryPickerPage()
	}
	if model.rfEditMode == "category-color" {
		return model.rfCategoryColorPage()
	}
	if model.rfGuideActive {
		return model.rfGuidedPage()
	}
	learnState := "idle"
	if model.preview == nil {
		state := model.rfLearnState()
		if state.Active {
			if state.Mode == control.RFLearnTimer {
				configured := time.Duration(state.ConfiguredMS) * time.Millisecond
				remaining := (time.Duration(state.RemainingMS) * time.Millisecond).Round(time.Second)
				learnState = fmt.Sprintf("ACTIVE · TIMER · configured=%s · remaining=%s · captured=%d", configured, remaining, state.Learned)
			} else {
				learnState = fmt.Sprintf("ACTIVE · LEARN · captured=%d", state.Learned)
			}
		} else if state.Reason != "" {
			modeName := "learn"
			configured := "continuous"
			if state.Mode == control.RFLearnTimer {
				modeName = "timer"
				configured = (time.Duration(state.ConfiguredMS) * time.Millisecond).String()
			}
			learnState = fmt.Sprintf("ended · %s · configured=%s · %s · captured=%d", modeName, configured, state.Reason, state.Learned)
		}
	}
	radix := strings.ToLower(strings.TrimSpace(model.rfValue.DisplayRadix))
	if radix != "decimal" {
		radix = "hex"
	}
	stageState := "IDs match device readback"
	if model.rfStageDirty {
		stageState = "STAGED · review required"
		if model.rfReview {
			stageState = "REVIEWED · apply is capability-gated"
		}
	}
	support := model.currentRFReplaceSupport()
	applyState := "unavailable: " + support.Reason
	if support.Supported && model.rfApplyOrder != nil && model.rfFetch != nil {
		applyState = "available (" + support.Reason + ") · full snapshot + readback + automatic rollback"
	}
	primaryItems := model.rfPrimaryItems()
	primaryButtons := make([]string, 0, len(primaryItems))
	for _, item := range primaryItems {
		primaryButtons = append(primaryButtons, item.render())
	}
	lines := []string{
		sectionHeader(model.width, "433 MHz RF", "receive INT0 · transmit INT1 · learning "+learnState),
		lipgloss.JoinHorizontal(lipgloss.Top, intersperseStrings(primaryButtons, " ")...),
		lipgloss.JoinHorizontal(lipgloss.Top, buttonGoodStyle.Render("W Guided A/B/C/D"), " ", buttonStyle.Render("A Search action"), " ", buttonStyle.Render("N Rename"), " ", buttonStyle.Render("K Category"), " ", buttonStyle.Render("Z View in "+strings.ToUpper(radix)), " ", buttonStyle.Render("[ / ] Move ID"), " ", buttonStyle.Render("V Review"), " ", buttonBadStyle.Render("G Apply"), " ", buttonStyle.Render("X Rollback")),
		labelStyle.Render("Metadata (code, bits, protocol) follows each code when IDs are reordered."),
		kv("Staged order", stageState),
		kv("Apply transaction", applyState),
		"",
		titleStyle.Render("ID  CODE        BITS  PROTO  NAME / CATEGORY                 BOARD MAPPING"),
	}
	if model.rfPending {
		lines = append(lines, warnStyle.Render(model.spinnerView()+" loading live RF list…"))
	}
	if model.rfError != "" {
		lines = append(lines, errorStyle.Render("RF list: "+model.rfError))
	}
	if len(model.rfStaged) == 0 && !model.rfPending {
		lines = append(lines, labelStyle.Render("No learned RF codes. Press L to learn continuously or Y for a 30-second timer."))
	}
	for index, entry := range model.rfStaged {
		metadata, _ := model.rfValue.MetadataFor(appconfig.RFCodeKey{
			Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol,
		})
		name := strings.TrimSpace(metadata.Name)
		if name == "" {
			name = "unnamed"
		}
		metadataLabel := name
		if metadata.Category != "" {
			metadataLabel = truncateText(name+" · "+metadata.Category, 28) + " " + categorySwatch(model.rfCategoryColor(metadata.Category))
		} else {
			metadataLabel = truncateText(metadataLabel, 31)
		}
		line := fmt.Sprintf(
			"%-3d %-11s %-5d %-6d %-31s %s",
			entry.ID,
			appconfig.FormatRFCode(entry.Code, model.rfValue.DisplayRadix),
			entry.Bits,
			entry.Protocol,
			metadataLabel,
			formatRFMappingUI(entry),
		)
		lines = append(lines, model.selectionLine(index, line))
	}
	lines = append(lines, "", titleStyle.Render("RECENT RF EVENTS"))
	count := 0
	for index := len(model.timeline) - 1; index >= 0 && count < 3; index-- {
		entry := model.timeline[index]
		if !strings.HasPrefix(entry.Kind, "rf") {
			continue
		}
		text := normalizeRFCodeTokens(entry.Text, model.rfValue.DisplayRadix)
		lines = append(lines, fmt.Sprintf("%s  %-12s %s", labelStyle.Render(entry.At.Format("15:04:05.000")), entry.Kind, text))
		count++
	}
	if count == 0 {
		lines = append(lines, labelStyle.Render("No RF frames in this session yet."))
	}
	lines = append(lines, labelStyle.Render("Category colors: red · blue · violet/purple · green · white  |  Enter opens action search"))
	return model.scrollSelection(lines, 9)
}

func (model Model) programmingContent(snapshot control.Snapshot) []string {
	lines := []string{
		sectionHeader(model.width, "FIRMWARE", boolWord(snapshot.Connected, "Board connected", "Board disconnected")),
	}
	lines = append(lines, model.updateProgressLines()...)
	if model.update.State != "" {
		lines = append(lines, "")
	}
	if snapshot.Hello.Name != "" {
		lines = append(lines, kv("Board", snapshot.Hello.Name))
	}
	if snapshot.Port.Name != "" {
		lines = append(lines, kv("Port", snapshot.Port.Name))
	}
	if snapshot.Hello.BuildHash != 0 {
		lines = append(lines, kv("Firmware build", fmt.Sprintf("%08X", snapshot.Hello.BuildHash)))
	}
	if snapshot.Hello.BuildStamp != "" {
		lines = append(lines, kv("Built", snapshot.Hello.BuildStamp))
	}
	lines = append(lines, "")
	buttons := []string{buttonGoodStyle.Render("U Flash"), buttonStyle.Render("B Backup"), buttonStyle.Render("R Reboot"), buttonStyle.Render("D Reset"), buttonStyle.Render("M Identity"), buttonStyle.Render("P Probe bootloader"), buttonStyle.Render("I Initialize"), buttonStyle.Render("Z USBasp driver"), buttonBadStyle.Render("X Blank…")}
	row := ""
	for _, button := range buttons {
		if row != "" && lipgloss.Width(row)+1+lipgloss.Width(button) > max(24, model.width-4) {
			lines = append(lines, row)
			row = ""
		}
		if row == "" {
			row = button
		} else {
			row = lipgloss.JoinHorizontal(lipgloss.Top, row, " ", button)
		}
	}
	if row != "" {
		lines = append(lines, row)
	}
	return strings.Split(strings.Join(lines, "\n"), "\n")
}

func (model Model) programmingPage(snapshot control.Snapshot) string {
	lines := model.programmingContent(snapshot)
	height := max(3, model.contentHeight())
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := max(0, min(model.update.Scroll, len(lines)-height+1))
	visible := append([]string(nil), lines[start:start+height-1]...)
	visible = append(visible, labelStyle.Render(fmt.Sprintf("↑ ↓  Scroll · %d–%d of %d", start+1, start+height-1, len(lines))))
	return strings.Join(visible, "\n")
}

func (model Model) integrationStatusLines() []string {
	if model.integrations == nil {
		return nil
	}
	status := model.integrations()
	lines := make([]string, 0, 7)
	if status.Hotkeys.Supported {
		hotkeyState := "stopped"
		if status.Hotkeys.Running {
			hotkeyState = fmt.Sprintf("active · %d bindings", len(status.Hotkeys.Bindings))
		}
		hotkeyDetail := status.Hotkeys.LastError
		if hotkeyDetail == "" && len(status.Hotkeys.Bindings) != 0 {
			hotkeyDetail = status.Hotkeys.Bindings[0].Accelerator + " → " + status.Hotkeys.Bindings[0].Command
		}
		lines = append(lines, serviceLine("Global hotkeys", hotkeyState, hotkeyDetail))
	}
	if status.Keyboard.Supported {
		keyboardState := "stopped"
		if status.Keyboard.Running {
			keyboardState = fmt.Sprintf("active · %d bindings", len(status.Keyboard.Bindings))
		}
		keyboardDetail := status.Keyboard.LastError
		if keyboardDetail == "" && len(status.Keyboard.Bindings) != 0 {
			keyboardDetail = status.Keyboard.Bindings[0].Key + " → " + status.Keyboard.Bindings[0].Name
		}
		lines = append(lines, serviceLine("Keyboard control", keyboardState, keyboardDetail))
	}
	if status.Notifications.Available {
		lines = append(lines, serviceLine("Desktop toasts", fmt.Sprintf("ready · %d accepted", status.Notifications.Accepted), status.Notifications.LastError))
	}
	for _, item := range []struct {
		label  string
		status hostui.ServiceStatus
	}{
		{"Text messaging", status.Messaging},
		{"Device discovery", status.Discovery},
		{"Webhooks", status.Webhooks},
		{"Socket.IO", status.SocketIO},
	} {
		if strings.TrimSpace(item.status.Name) != "" {
			lines = append(lines, serviceFromStatus(item.label, item.status))
		}
	}
	return lines
}

func serviceFromStatus(label string, status hostui.ServiceStatus) string {
	state := status.State
	if state == "" {
		state = boolWord(status.Enabled, "enabled", "disabled")
	}
	detail := status.Endpoint
	if status.Detail != "" {
		if detail != "" {
			detail += " · "
		}
		detail += status.Detail
	}
	if status.LastError != "" {
		if detail != "" {
			detail += " · "
		}
		detail += "error: " + status.LastError
	}
	return serviceLine(label, state, detail)
}

func serviceLine(label, state, detail string) string {
	line := labelStyle.Render(padRightVisible(label, 20)) + " " + valueStyle.Render(padRightVisible(state, 22))
	if detail != "" {
		line += labelStyle.Render(detail)
	}
	return line
}

func (model Model) eventsPage() string {
	graphState := "E expand"
	if model.eventsExpanded {
		graphState = "E compact"
	}
	lines := []string{sectionHeader(model.width, "24-HOUR HISTORY & EVENT TIMELINE", fmt.Sprintf("%d samples · %d events · %s", len(model.samples), len(model.timeline), graphState))}
	if model.prefs.Visible["graphs"] {
		graphWidth := min(model.width, 96)
		if model.eventsExpanded {
			graphWidth = model.width
		}
		lines = append(lines, lipgloss.PlaceHorizontal(model.width, lipgloss.Center, model.graphTable(graphWidth)))
	}
	timelineRows := make([][]string, 0, 10)
	for index := len(model.timeline) - 1; index >= 0 && len(timelineRows) < 10; index-- {
		entry := model.timeline[index]
		if !entry.Important {
			continue
		}
		timelineRows = append(timelineRows, []string{entry.At.Format("2006-01-02 15:04:05"), strings.ToUpper(entry.Kind), entry.Text})
	}
	lines = append(lines, "", titleStyle.Render("Important timeline"))
	if len(timelineRows) == 0 {
		lines = append(lines, labelStyle.Render("No important events recorded in this session."))
	} else {
		lines = append(lines, renderDataTable(model.width, 10, -1, timelineTableColumns(model.width), timelineRows))
	}
	return strings.Join(lines, "\n")
}

func (model Model) consolePage() string {
	quick := strings.Join([]string{
		labelStyle.Render("DEVICE") + " " + txStyle.Render("open close reconnect status menu settings"),
		labelStyle.Render("OUTPUT") + " " + txStyle.Render("relay pwm rgb strip melody display macro"),
		labelStyle.Render("RF & AUTOMATION") + " " + txStyle.Render("rf automation event"),
		labelStyle.Render("PROGRAMMING") + " " + txStyle.Render("boot program toolchain reset"),
		labelStyle.Render("CONSOLE") + " " + txStyle.Render("help clear quit exit"),
	}, "\n")
	return quick + "\n" + model.viewport.View()
}

func (model Model) welcomeView() string {
	frames := []string{"◇", "◈", "◆", "◈"}
	icon := frames[(model.welcomeFrame/2)%len(frames)]
	progress := 2
	phase := strings.ToLower(model.welcomePhase)
	switch {
	case strings.Contains(phase, "hello"):
		progress = 5
	case strings.Contains(phase, "ready/status"):
		progress = 8
	case strings.Contains(phase, "board welcome"), strings.Contains(phase, "buzzer"):
		progress = 12
	case strings.Contains(phase, "host welcome"), strings.Contains(phase, "scheduler"):
		progress = 16
	case strings.Contains(phase, "complete"), strings.Contains(phase, "ready"):
		progress = 20
	}
	bar := strings.Repeat("━", progress) + strings.Repeat("─", 20-progress)
	status := model.welcomePhase
	if status == "" {
		status = "Waiting for controller"
	}
	action := ""
	if model.welcomeCanContinue {
		action = buttonStyle.Render("Enter / click to continue with the warning")
	}
	errorLine := ""
	if model.welcomeError != "" {
		errorLine = errorStyle.Render(model.welcomeError)
	}
	body := lipgloss.JoinVertical(
		lipgloss.Center,
		titleStyle.Copy().Bold(true).Render(icon+"  "+model.prefs.AppTitle+"  "+icon),
		"",
		valueStyle.Render(model.prefs.Tagline),
		labelStyle.Render("Native opcodes · Urboot/Urclock · RF · motion · PWM · telemetry"),
		"",
		lipgloss.NewStyle().Foreground(colorAccent2).Render(bar),
		valueStyle.Render(status),
		errorLine,
		"",
		action,
	)
	return lipgloss.Place(model.width, model.height, lipgloss.Center, lipgloss.Center, cardStyle.Copy().Padding(2, 5).Render(body))
}

func (model Model) contentHeight() int {
	tabRows := strings.Count(model.tabBar(), "\n") + 1
	height := model.height - tabRows - 6
	if model.terminalIsVisible() {
		height -= 3
		if len(model.completion) > 0 {
			height--
		}
	}
	if height < 4 {
		height = 4
	}
	return height
}

func (model Model) fitContent(value string) string {
	height := model.contentHeight()
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (model Model) scrollSelection(lines []string, fixed int) string {
	height := model.contentHeight()
	if len(lines) <= height || fixed >= len(lines) {
		return strings.Join(lines, "\n")
	}
	available := height - fixed
	if available < 1 {
		available = 1
	}
	start := model.cursor - available/2
	if start < 0 {
		start = 0
	}
	if start+available > len(lines)-fixed {
		start = len(lines) - fixed - available
	}
	if start < 0 {
		start = 0
	}
	result := append([]string(nil), lines[:fixed]...)
	result = append(result, lines[fixed+start:fixed+start+available]...)
	return strings.Join(result, "\n")
}

func (model Model) selectionLine(index int, value string) string {
	prefix := "  "
	if index == model.cursor {
		return selectedStyle.Copy().Width(model.width - 2).Render("› " + value)
	}
	return prefix + value
}

func sectionHeader(width int, title, detail string) string {
	if width <= 0 {
		return titleStyle.Render(title) + "  " + labelStyle.Render(detail)
	}
	separator := "  "
	title = truncateDisplayText(title, width)
	remaining := width - lipgloss.Width(title)
	if detail != "" && remaining > lipgloss.Width(separator) {
		detail = truncateDisplayText(detail, remaining-lipgloss.Width(separator))
	} else {
		detail = ""
	}
	group := titleStyle.Render(title)
	if detail != "" {
		group += separator + labelStyle.Render(detail)
	}
	groupWidth := lipgloss.Width(group)
	left := (width - groupWidth) / 2
	right := width - groupWidth - left
	return strings.Repeat(" ", left) + group + strings.Repeat(" ", right)
}

func kv(key, value string) string {
	return labelStyle.Render(padRightVisible(key, 33)) + " " + valueStyle.Render(value)
}

func kvCard(width, labelWidth int, key, value string) string {
	if width <= 1 {
		return truncateDisplayText(key+" "+value, width)
	}
	if labelWidth > width-2 {
		labelWidth = width - 2
	}
	valueWidth := width - labelWidth - 1
	valueLines := wrapDisplayText(value, valueWidth)
	rows := make([]string, 0, len(valueLines))
	for index, line := range valueLines {
		label := strings.Repeat(" ", labelWidth)
		if index == 0 {
			label = labelStyle.Render(padRightVisible(key, labelWidth))
		}
		rows = append(rows, label+" "+valueStyle.Render(padRightVisible(line, valueWidth)))
	}
	return strings.Join(rows, "\n")
}

func wrapDisplayText(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
		for lipgloss.Width(word) > width {
			chunk, remainder := splitDisplayText(word, width)
			lines = append(lines, chunk)
			word = remainder
		}
		current = word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitDisplayText(value string, width int) (string, string) {
	used := 0
	byteIndex := 0
	for index, character := range value {
		characterWidth := lipgloss.Width(string(character))
		if used+characterWidth > width {
			if byteIndex == 0 {
				next := index + len(string(character))
				return value[:next], value[next:]
			}
			return value[:byteIndex], value[byteIndex:]
		}
		used += characterWidth
		byteIndex = index + len(string(character))
	}
	return value, ""
}

func padRightVisible(value string, width int) string {
	value = truncateDisplayText(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func truncateDisplayText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result strings.Builder
	used := 0
	for _, character := range value {
		characterWidth := lipgloss.Width(string(character))
		if used+characterWidth > width-1 {
			break
		}
		result.WriteRune(character)
		used += characterWidth
	}
	return result.String() + "…"
}

func relaySummary(bits byte) string {
	if bits == 0 {
		return "none"
	}
	var active []string
	for index := 0; index < 8; index++ {
		if bits&(1<<index) != 0 {
			active = append(active, fmt.Sprintf("R%d", index+1))
		}
	}
	return strings.Join(active, ", ")
}

func sliderPercent(percent, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := percent * width / 100
	if filled > width {
		filled = width
	}
	return "[" + lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("━", filled)) + labelStyle.Render(strings.Repeat("─", width-filled)) + "]"
}

func sliderPercentPlain(percent, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := min(width, percent*width/100)
	return "[" + strings.Repeat("━", filled) + strings.Repeat("─", width-filled) + "]"
}

func outputTableColumns(width int) []dataColumn {
	available := max(24, width-2)
	name := min(44, max(20, available*38/100))
	value := available - name
	return []dataColumn{
		{Title: "CONTROL", Width: name, Align: lipgloss.Left},
		{Title: "STATUS", Width: value, Align: lipgloss.Left},
	}
}

func settingsTableColumns(width int) []dataColumn {
	available := max(42, width-4)
	group := min(15, max(10, available/7))
	setting := min(38, max(22, available*36/100))
	value := max(10, available-group-setting)
	return []dataColumn{
		{Title: "GROUP", Width: group, Align: lipgloss.Left},
		{Title: "SETTING", Width: setting, Align: lipgloss.Left},
		{Title: "VALUE", Width: value, Align: lipgloss.Left},
	}
}

func timelineTableColumns(width int) []dataColumn {
	available := max(42, width-4)
	timestamp := min(19, max(12, available/5))
	kind := min(14, max(9, available/8))
	detail := max(16, available-timestamp-kind)
	return []dataColumn{
		{Title: "TIME", Width: timestamp, Align: lipgloss.Left},
		{Title: "EVENT", Width: kind, Align: lipgloss.Center},
		{Title: "DETAILS", Width: detail, Align: lipgloss.Left},
	}
}

func (model Model) graphTable(width int) string {
	available := max(50, width-4)
	labelWidth := min(25, max(18, available/5))
	rangeWidth := min(34, max(21, available/3))
	trendWidth := max(11, available-labelWidth-rangeWidth)
	type metric struct {
		label  string
		values []float64
		format func(float64) string
	}
	metrics := []metric{
		{"Supply Voltage", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HaveSupply }, func(sample measurementSample) float64 { return float64(sample.SupplyMV) }), func(value float64) string { return formatVoltage(int32(value), model.prefs.VoltageDecimals) }},
		{"Bus Voltage", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HaveBus }, func(sample measurementSample) float64 { return float64(sample.BusMV) }), func(value float64) string { return formatVoltage(int32(value), model.prefs.VoltageDecimals) }},
		{"Load Current", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HaveCurrent }, func(sample measurementSample) float64 { return float64(sample.CurrentMA) }), func(value float64) string { return formatCurrent(int32(value), model.prefs.CurrentDecimals) }},
		{"Load Power", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HavePower }, func(sample measurementSample) float64 { return float64(sample.PowerMW) }), func(value float64) string { return formatPower(int32(value), model.prefs.PowerDecimals) }},
		{"Illumination Temperature", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HaveTLED }, func(sample measurementSample) float64 { return float64(sample.TLEDCenti) }), func(value float64) string { return formatTemperature(int16(value), model.prefs.TemperatureDecimals) }},
		{"Bluetooth Audio Temperature", availableSampleValues(model.samples, func(sample measurementSample) bool { return sample.HaveTBT }, func(sample measurementSample) float64 { return float64(sample.TBTCenti) }), func(value float64) string { return formatTemperature(int16(value), model.prefs.TemperatureDecimals) }},
	}
	rows := make([][]string, 0, len(metrics))
	for _, item := range metrics {
		if len(item.values) == 0 {
			continue
		}
		rows = append(rows, []string{item.label, sparkline(item.values, trendWidth), graphRange(item.values, item.format)})
	}
	return renderDataTable(width, len(rows), -1, []dataColumn{
		{Title: "SIGNAL", Width: labelWidth, Align: lipgloss.Left},
		{Title: "TREND", Width: trendWidth, Align: lipgloss.Left},
		{Title: "MIN · MAX · LATEST", Width: rangeWidth, Align: lipgloss.Left},
	}, rows)
}

func graphRange(values []float64, formatter func(float64) string) string {
	if len(values) == 0 {
		return "waiting for samples"
	}
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	return formatter(minimum) + " · " + formatter(maximum) + " · " + formatter(values[len(values)-1])
}

func lightModeName(value byte) string {
	return map[byte]string{0: "off", 1: "on", 2: "auto"}[value]
}

func motionDoorPolicyName(value byte) string {
	return map[byte]string{
		native.MotionDoorAlways:     "always",
		native.MotionDoorClosedOnly: "door closed only",
		native.MotionDoorOpenOnly:   "door open only",
		native.MotionDoorNever:      "never · safety lockout",
	}[value]
}

func programModeNameForCapabilities(value byte, capabilities uint32) string {
	current := []string{
		"Boot", "Door", "Voltage", "Current", "Temperature LED", "Temperature BT Audio",
		"Illumination", "Sound", "PWM", "Relay", "Keys",
		"User PWM", "User Relays", "Motion", "RF",
		"Edit · illumination mode", "Edit · illumination on", "Edit · illumination off",
		"Edit · sound settings", "Edit · PWM channel", "Edit · PWM value",
		"Edit · relay channel", "Edit · relay value", "Edit · user PWM channel", "Edit · user PWM value",
		"Edit · user relay channel", "Edit · user relay behavior", "Control · user relays",
		"Control · motion", "Confirm · save or discard", "Flash message", "RF learning", "Fault",
	}
	if value == 5 && capabilities&native.CapabilityBluetoothAudio == 0 {
		return fmt.Sprintf("Unknown mode %d", value)
	}
	names := current
	if int(value) < len(names) {
		return names[value]
	}
	if value == 0xFF {
		return "Undefined"
	}
	return fmt.Sprintf("Unknown mode %d", value)
}

func (model Model) programModeName(value byte) string {
	return programModeNameForCapabilities(value, model.snapshot().Hello.Capabilities)
}

func visibilityValue(label string, visible bool) string {
	return settingsRow(label, boolWord(visible, "VISIBLE", "HIDDEN"))
}

func settingsRow(label, value string) string {
	return padRightVisible(label, 38) + " " + value
}

func firmwareIdentity(snapshot control.Snapshot) string {
	if snapshot.Hello.IdentitySchema == native.IdentitySchemaCompact {
		stamp := snapshot.Hello.BuildStamp
		if stamp == "" {
			stamp = "timestamp unavailable"
		}
		return fmt.Sprintf(
			"hash %08X · %s · packed %08X",
			snapshot.Hello.BuildHash,
			stamp,
			snapshot.Hello.BuildTimestamp,
		)
	}
	if snapshot.Hello.Name != "" {
		return snapshot.Hello.Name
	}
	return "not available"
}

func sampleValues(samples []measurementSample, value func(measurementSample) float64) []float64 {
	result := make([]float64, len(samples))
	for index, sample := range samples {
		result[index] = value(sample)
	}
	return result
}

func availableSampleValues(samples []measurementSample, available func(measurementSample) bool, value func(measurementSample) float64) []float64 {
	result := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if available(sample) {
			result = append(result, value(sample))
		}
	}
	return result
}
