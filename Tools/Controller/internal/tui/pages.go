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
	lines := []string{
		sectionHeader(model.width, "SELECT SERIAL DEVICE", "↑/↓ select · Enter open · Esc cancel"),
		labelStyle.Render("Friendly name, COM ID, VID/PID and serial identity are shown; authentication still verifies HELLO before use."),
	}
	if model.portLoading {
		lines = append(lines, warnStyle.Render(model.spinner.View()+" querying Windows serial devices…"))
	}
	if model.portError != "" {
		lines = append(lines, errorStyle.Render(model.portError))
	}
	if len(model.portCandidates) == 0 && !model.portLoading {
		lines = append(lines, warnStyle.Render("No serial devices found. Auto-reconnect remains armed unless explicitly closed."))
	}
	for index, candidate := range model.portCandidates {
		line := candidate.Label()
		if candidate.Name == snapshot.Port.Name {
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
	pageWidth := model.width
	if pageWidth <= 0 {
		pageWidth = 132
	}
	lcdStatus := "offline · physical contents unverified"
	if snapshot.Connected {
		lcdStatus = fmt.Sprintf("firmware-owned · 0x%02X", status.LCDAddress)
	}
	if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityI2CTransfer != 0 && model.runtime != nil {
		lcd := model.runtime.LCDPresenter().State()
		lcdStatus = "PC-owned · not detected"
		if lcd.Physical {
			lcdStatus = fmt.Sprintf("PC-owned · 0x%02X", lcd.Address)
		} else if lcd.LastError != "" {
			lcdStatus += " · " + lcd.LastError
		}
	}
	sectionWidth := pageWidth
	if pageWidth >= 96 {
		outerCardWidth := (pageWidth - 1) / 2
		sectionWidth = outerCardWidth - cardStyle.GetHorizontalFrameSize()
	}
	measurementLines := []string{
		sectionHeader(sectionWidth, "LIVE MEASUREMENTS", freshnessLabel(snapshot.StatusUpdated, time.Now())),
	}
	if !snapshot.HaveStatus {
		measurementLines = append(measurementLines, warnStyle.Render("Waiting for the first STATUS frame…"))
	}
	if model.prefs.Visible["supply"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "Supply Voltage", formatVoltage(status.SupplyMV, model.prefs.VoltageDecimals)))
	}
	if model.prefs.Visible["bus"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "INA219 Bus Voltage", formatVoltage(status.BusMV, model.prefs.VoltageDecimals)))
	}
	if model.prefs.Visible["current"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "Load Current", formatCurrent(status.CurrentMA, model.prefs.CurrentDecimals)))
	}
	if model.prefs.Visible["power"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "Load Power", formatPower(status.PowerMW, model.prefs.PowerDecimals)))
	}
	if model.prefs.Visible["temperature_led"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "Temperature · Illumination LED", formatTemperature(status.TLEDCenti, model.prefs.TemperatureDecimals)))
	}
	if model.prefs.Visible["temperature_bt"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, "Temperature · BT Audio", formatTemperature(status.TBTCenti, model.prefs.TemperatureDecimals)))
	}

	stateLines := []string{
		sectionHeader(sectionWidth, "BOARD STATE", model.menuPageByID(status.MenuPage).Name),
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			buttonStyle.Render("I · Idle"), " ",
			buttonGoodStyle.Render("R · Running"),
		),
		kvCard(sectionWidth, 22, "PC Program State", programStateSummary(snapshot.ProgramState)),
		kvCard(sectionWidth, 22, "Device Uptime", formatUptime(status.UptimeMS)),
		kvCard(sectionWidth, 22, "Enclosure Door", boolWord(status.DoorOpen, "OPEN", "CLOSED")),
		kvCard(sectionWidth, 22, "Bluetooth", bluetoothAudioState(status.BluetoothState)),
		kvCard(sectionWidth, 22, "Active Keys", fmt.Sprintf("0x%02X", status.ActiveKeys)),
		kvCard(sectionWidth, 22, "Active Relays", relaySummary(status.ActiveRelays)),
		kvCard(sectionWidth, 22, "Display Menu", fmt.Sprintf("%d · %s", status.MenuPage, model.menuPageByID(status.MenuPage).Name)),
		kvCard(sectionWidth, 22, "Menu / Submode", fmt.Sprintf("%d · %s", status.ProgramMode, model.programModeName(status.ProgramMode))),
		kvCard(sectionWidth, 22, "PWM", fmt.Sprintf("mode %d · channel %d · %d/4095", status.PWMMode, status.PWMChannel, status.PWMValue)),
		kvCard(sectionWidth, 22, "I2C LCD", lcdStatus),
	}
	if model.prefs.Visible["diagnostics"] {
		stateLines = append(stateLines,
			kvCard(sectionWidth, 22, "Reset Telemetry", fmt.Sprintf("#%d · cause 0x%02X", status.ResetCount, status.ResetCause)),
			kvCard(sectionWidth, 22, "Protocol Errors", fmt.Sprintf("frame %d · CRC %d · PWM %d", status.FramingErrors, status.CRCErrors, status.PWMErrors)),
		)
	}

	if pageWidth < 96 {
		return strings.Join(measurementLines, "\n") + "\n\n" + strings.Join(stateLines, "\n")
	}
	cardRenderWidth := sectionWidth + cardStyle.GetHorizontalPadding()
	left := cardStyle.Copy().Width(cardRenderWidth).Render(strings.Join(measurementLines, "\n"))
	right := cardStyle.Copy().Width(cardRenderWidth).Render(strings.Join(stateLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
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
	status := snapshot.Status
	lines := []string{
		sectionHeader(model.width, "OUTPUT CONTROL", "↑/↓ select · Enter activate · ←/→ PWM · Home/End min/max"),
		labelStyle.Render("Motion wiring: R1 Direction A · R2 Output A · R3 Direction B · R4 Output B"),
	}
	labels := []string{
		"R1 · Side A Direction", "R2 · Side A Output", "R3 · Side B Direction", "R4 · Side B Output",
		"R5 · User Relay", "R6 · User Relay", "R7 · User Relay", "R8 · User Relay",
	}
	for index, label := range labels {
		on := status.ActiveRelays&(1<<index) != 0
		state := errorStyle.Render("OFF")
		if on {
			state = lipgloss.NewStyle().Bold(true).Foreground(colorGood).Render("ON ")
		}
		lines = append(lines, model.selectionLine(index, fmt.Sprintf("%-27s %s   [ toggle ]", label, state)))
	}
	lines = append(lines, model.selectionLine(8, "ALL RELAYS OFF                         [ execute ]"))
	lines = append(lines,
		model.selectionLine(9, "Side A motion · UP                     [ run ]"),
		model.selectionLine(10, "Side A motion · STOP                   [ run ]"),
		model.selectionLine(11, "Side A motion · DOWN                   [ run ]"),
		model.selectionLine(12, "Side B motion · UP                     [ run ]"),
		model.selectionLine(13, "Side B motion · STOP                   [ run ]"),
		model.selectionLine(14, "Side B motion · DOWN                   [ run ]"),
	)
	for channel := 0; channel <= 10; channel++ {
		value := uint16(0)
		if model.havePWMValues {
			value = model.pwmValues[channel]
		} else if byte(channel) == status.PWMChannel {
			value = status.PWMValue
		}
		lines = append(lines, model.selectionLine(15+channel,
			fmt.Sprintf("PWM %02d · User MOSFET  %s %4d · %3d%%", channel, slider(value, 24), value, int(value)*100/4095)))
	}
	lines = append(lines,
		model.selectionLine(26, "ALL USER PWM OFF                       [ execute ]"),
		model.selectionLine(27, fmt.Sprintf("PWM mode · %s                         [ cycle ]", pwmModeName(status.PWMMode))),
	)
	return model.scrollSelection(lines, 2)
}

func (model Model) menusPage(snapshot control.Snapshot) string {
	active := snapshot.Status.MenuPage
	layoutState := "read-only · firmware capability 23 unavailable"
	if model.menuLayoutStaged.Supported && model.menuLayoutStaged.Persistent {
		layoutState = "MCU EEPROM · GET/SET + readback"
	}
	if model.menuLayoutDirty {
		layoutState += " · STAGED"
	}
	overlayState := "unavailable · capability 24 absent · host-only pages show no false live state"
	if native.SupportsHostMenuOverlay(snapshot.Hello) {
		overlayState = "runtime directory/content enabled · volatile (PC connection required)"
	}
	searchState := model.menuLayoutSearch
	if searchState == "" {
		searchState = "all"
	}
	if model.menuLayoutSearchEditing {
		searchState = "✎ " + searchState
	}
	lines := []string{
		sectionHeader(model.width, "DISPLAY MENU MIRROR", fmt.Sprintf("active %d · %s", active, model.menuPageByID(active).Name)),
		renderFrontPanel(model.currentFrontPanel(snapshot)),
		lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("K1 · previous"), " ", buttonStyle.Render("K2 · next"), " ", buttonStyle.Render("K3 · decrease"), " ", buttonStyle.Render("K4 · increase")),
		renderHostMenuDirectory(model.hostMenus, model.width),
		fmt.Sprintf("LCD prompt mirroring  %s  %s", valueStyle.Render(boolWord(model.lcdMirror, "ON", "OFF")), labelStyle.Render("M toggles · priority events temporarily override and restore")),
		labelStyle.Render(fmt.Sprintf("Catalog: %s · Layout: %s · Host overlay: %s · Search: %s · Sort: %s", model.menuCatalogSource, layoutState, overlayState, searchState, model.menuLayoutSort)),
	}
	entries := model.menuConfigurationEntries()
	for index, entry := range entries {
		page := entry.Page
		marker := "  "
		if page.ID == active {
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
	settings := snapshot.Settings
	lines := []string{
		sectionHeader(model.width, "BOARD EEPROM SETTINGS", boolWord(snapshot.HaveSettings, "live + persisted on MCU", "not queried yet")),
		labelStyle.Render("Select a row and use ←/→ or Enter. Every change is written through SET_SETTINGS; PC config remains separate."),
	}
	values := []string{
		settingsRow("Silent mode", boolWord(settings.Flags&native.SettingsSilent != 0, "ON", "OFF")),
		settingsRow("I²C LCD", boolWord(settings.Flags&native.SettingsLCDEnabled != 0, "ENABLED", "DISABLED")),
		settingsRow("Swap temperature roles", boolWord(settings.Flags&native.SettingsSwapTemperatureRoles != 0, "YES", "NO")),
		settingsRow("Enclosure illumination mode", lightModeName(settings.LightMode)),
		settingsRow("Illumination brightness · on", fmt.Sprintf("%3d", settings.OnBrightness)),
		settingsRow("Illumination brightness · off", fmt.Sprintf("%3d", settings.OffBrightness)),
		settingsRow("Seven-segment brightness", fmt.Sprintf("%d / 7", settings.DisplayBrightness)),
		settingsRow("Status LED brightness", fmt.Sprintf("%3d", settings.StatusBrightness)),
		settingsRow("PWM boot mode", pwmModeName(settings.PWMBootMode)),
		settingsRow("Board stream period", fmt.Sprintf("%d ms", settings.StreamPeriodMS)),
		settingsRow("Default display page", fmt.Sprintf("%d · %s", settings.DefaultPage, model.menuPageByID(settings.DefaultPage).Name)),
		settingsRow("Save last page as default", boolWord(settings.SaveLastPage(), "YES", "NO")),
		settingsRow("Status palette color", fmt.Sprintf("%d / 7", settings.StatusColor())),
		settingsRow("Board voltage decimals", fmt.Sprintf("%d", settings.VoltageDecimals())),
		settingsRow("Board current decimals", fmt.Sprintf("%d", settings.CurrentDecimals())),
		settingsRow("Motion allowed by door state", motionDoorPolicyName(settings.MotionDoorPolicy())),
		settingsRow("Door open/close audio cues", boolWord(settings.DoorAudioEnabled(), "ENABLED", "DISABLED")),
		settingsRow("Relay on/off audio cues", boolWord(settings.RelayAudioEnabled(), "ENABLED", "DISABLED")),
	}
	for index, value := range values {
		lines = append(lines, model.selectionLine(index, value))
	}
	return model.scrollSelection(lines, 2)
}

func (model Model) appSettingsPage() string {
	visible := model.prefs.Visible
	statusLED := model.hostIntegrationValue.StatusLED
	lines := []string{
		sectionHeader(model.width, "PC HOST SETTINGS", "saved in host JSON · never board EEPROM"),
		labelStyle.Render("Select with ↑/↓; ←/→ adjusts; Enter toggles visibility. Changes hot-apply through the config store."),
	}
	values := []string{
		settingsRow("Application title", model.prefs.AppTitle),
		settingsRow("Active polling interval", model.prefs.PollInterval.String()),
		settingsRow("History retention", model.prefs.HistoryWindow.String()),
		settingsRow("Voltage display decimals", fmt.Sprintf("%d", model.prefs.VoltageDecimals)),
		settingsRow("Current display decimals", fmt.Sprintf("%d", model.prefs.CurrentDecimals)),
		settingsRow("Power display decimals", fmt.Sprintf("%d", model.prefs.PowerDecimals)),
		settingsRow("Temperature display decimals", fmt.Sprintf("%d", model.prefs.TemperatureDecimals)),
		visibilityValue("Supply voltage", visible["supply"]),
		visibilityValue("INA219 bus voltage", visible["bus"]),
		visibilityValue("Load current", visible["current"]),
		visibilityValue("Load power", visible["power"]),
		visibilityValue("Temperature · Illumination LED", visible["temperature_led"]),
		visibilityValue("Temperature · BT Audio", visible["temperature_bt"]),
		visibilityValue("I/O state", visible["io"]),
		visibilityValue("Diagnostics", visible["diagnostics"]),
		visibilityValue("Graphs", visible["graphs"]),
		settingsRow("Event transcript limit", fmt.Sprintf("%d", model.prefs.EventLogLimit)),
		visibilityValue("PC-owned LCD service", model.uiValue.LCDServiceEnabled),
		visibilityValue("Mirror prompt/completion to LCD", model.lcdMirror),
		visibilityValue("Host status-light policy", statusLED.Enabled),
		settingsRow("Status-light eased transition", fmt.Sprintf("%d ms", statusLED.TransitionMS)),
		settingsRow("RF violet activity hold", fmt.Sprintf("%d ms", statusLED.RFHoldMS)),
		settingsRow("Host HOT threshold", fmt.Sprintf("%.2f °C", float64(statusLED.HotThresholdCentiC)/100)),
		statusLEDColorValue("Idle", statusLED.Idle),
		statusLEDColorValue("Running", statusLED.Running),
		statusLEDColorValue("BT Audio connected", statusLED.BluetoothAudioConnected),
		statusLEDColorValue("BT Audio searching", statusLED.BluetoothAudioSearching),
		statusLEDColorValue("BT Audio powered off", statusLED.BluetoothAudioOff),
		statusLEDColorValue("RF activity", statusLED.RFActivity),
		statusLEDColorValue("HOT", statusLED.Hot),
		statusLEDColorValue("Running + door open", statusLED.RunningDoorOpen),
		statusLEDColorValue("PC offline", statusLED.PCOffline),
	}
	for index, value := range values {
		lines = append(lines, model.selectionLine(index, value))
	}
	return model.scrollSelection(lines, 2)
}

func statusLEDColorValue(label string, visual appconfig.StatusLEDVisual) string {
	return settingsRow(
		"Status · "+label,
		fmt.Sprintf("#%02X%02X%02X · %s", visual.Color.Red, visual.Color.Green, visual.Color.Blue, visual.Effect),
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
	learnState := "idle"
	if model.preview == nil {
		state := model.runtime.RFLearnState()
		if state.Active {
			remaining := "indefinite"
			if !state.EndsAt.IsZero() {
				remaining = time.Until(state.EndsAt).Round(time.Second).String()
			}
			learnState = fmt.Sprintf("ACTIVE · multi=%t · %s · captured=%d", state.Multiple, remaining, state.Learned)
		} else if state.Reason != "" {
			learnState = fmt.Sprintf("ended · %s · captured=%d", state.Reason, state.Learned)
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
	lines := []string{
		sectionHeader(model.width, "433 MHz RF", "receive INT0 · transmit INT1 · learning "+learnState),
		lipgloss.JoinHorizontal(lipgloss.Top, buttonGoodStyle.Render("L Learn indefinite + multi"), " ", buttonStyle.Render("C Cancel"), " ", buttonStyle.Render("R Refresh list"), " ", buttonStyle.Render("T Transmit")),
		lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("A Search action"), " ", buttonStyle.Render("N Rename"), " ", buttonStyle.Render("K Category"), " ", buttonStyle.Render("Z Radix: "+radix), " ", buttonStyle.Render("[ / ] Move ID"), " ", buttonStyle.Render("V Review"), " ", buttonBadStyle.Render("G Apply"), " ", buttonStyle.Render("X Rollback")),
		labelStyle.Render("Learned codes start UNMAPPED. Metadata follows the stable (code, bits, protocol) tuple even when IDs move."),
		kv("Staged order", stageState),
		kv("Apply transaction", applyState),
		"",
		titleStyle.Render("ID  CODE        BITS  PROTO  NAME / CATEGORY                 BOARD MAPPING"),
	}
	if model.rfPending {
		lines = append(lines, warnStyle.Render(model.spinner.View()+" loading live RF list…"))
	}
	if model.rfError != "" {
		lines = append(lines, errorStyle.Render("RF list: "+model.rfError))
	}
	if len(model.rfStaged) == 0 && !model.rfPending {
		lines = append(lines, labelStyle.Render("No learned RF codes. Press L to begin indefinite multi-learn."))
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
	lines = append(lines, labelStyle.Render("Palette: red · blue · violet/purple · green · white  |  Enter opens action search"))
	return model.scrollSelection(lines, 9)
}

func (model Model) programmingPage(snapshot control.Snapshot) string {
	firstButtons := lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("P Urclock probe"), " ", buttonStyle.Render("M Metadata"), " ", buttonStyle.Render("B Backup"))
	secondButtons := lipgloss.JoinHorizontal(lipgloss.Top, buttonBadStyle.Render("R Safe app reset"), " ", buttonBadStyle.Render("D DTR/RTS reset"), " ", buttonGoodStyle.Render("U Safe flash"), " ", buttonBadStyle.Render("A Advanced USBasp"))
	return strings.Join([]string{
		sectionHeader(model.width, "PROGRAMMING", "application opcodes and bootloader operations are mutually exclusive"),
		firstButtons,
		secondButtons,
		"",
		kv("Application protocol", boolWord(snapshot.Connected, "authenticated and available", "not connected")),
		kv("Boot protocol", "Urboot/Urclock via the installed MiniCore AVRDUDE backend"),
		kv("Current firmware", firmwareIdentity(snapshot)),
		kv("Normal flash gate", "inspect HEX → backup flash + EEPROM + metadata → verify manifest → flash → HELLO"),
		kv("Backup storage", "content-addressed SHA-256 blobs; identical firmware is never duplicated"),
		"",
		warnStyle.Render("Normal writes use `program flash HEX [PORT]` and refuse to proceed without a complete verified backup. Progress and hashes appear in Console."),
		errorStyle.Render("USBasp is hidden troubleshooting only. It requires `--usbasp-troubleshooting`; incomplete-backup override is separately explicit."),
		labelStyle.Render("Commands: program flash · boot probe|metadata|backup|read|verify · toolchain bootstrap|sync|compile|core-info|install-bootloader"),
	}, "\n")
}

func (model Model) integrationStatusLines() []string {
	if model.integrations == nil {
		return []string{
			serviceLine("Global hotkeys", "not configured", "desktop registrar not wired"),
			serviceLine("Keyboard control", "not configured", "low-level hook not wired"),
			serviceLine("Desktop toasts", "not configured", "notifier not wired"),
			serviceLine("Text messaging", "not configured", "backend status unavailable"),
			serviceLine("Device discovery", "not configured", "backend status unavailable"),
			serviceLine("Webhooks", "not configured", "backend status unavailable"),
			serviceLine("Socket.IO", "not configured", "optional adapter unavailable"),
		}
	}
	status := model.integrations()
	hotkeyState := "stopped"
	if !status.Hotkeys.Supported {
		hotkeyState = "unsupported"
	} else if status.Hotkeys.Running {
		hotkeyState = fmt.Sprintf("active · %d bindings", len(status.Hotkeys.Bindings))
	}
	hotkeyDetail := status.Hotkeys.LastError
	if hotkeyDetail == "" && len(status.Hotkeys.Bindings) != 0 {
		hotkeyDetail = status.Hotkeys.Bindings[0].Accelerator + " → " + status.Hotkeys.Bindings[0].Command
	}
	keyboardState := "stopped"
	if !status.Keyboard.Supported {
		keyboardState = "unsupported"
	} else if status.Keyboard.Running {
		keyboardState = fmt.Sprintf("active · %d bindings", len(status.Keyboard.Bindings))
	}
	keyboardDetail := status.Keyboard.LastError
	if keyboardDetail == "" && len(status.Keyboard.Bindings) != 0 {
		keyboardDetail = status.Keyboard.Bindings[0].Key + " → " + status.Keyboard.Bindings[0].Name
	}
	if keyboardDetail == "" {
		keyboardDetail = "disabled by default; momentary actions release on focus loss"
	}
	toastState := "unavailable"
	if status.Notifications.Available {
		toastState = fmt.Sprintf("ready · %d accepted", status.Notifications.Accepted)
	} else if !status.Notifications.Supported {
		toastState = "unsupported"
	}
	toastDetail := status.Notifications.LastError
	if toastDetail == "" {
		toastDetail = "WinRT acceptance only; actions require registered pccontroller:// handler"
	}
	return []string{
		serviceLine("Global hotkeys", hotkeyState, hotkeyDetail),
		serviceLine("Keyboard control", keyboardState, keyboardDetail),
		serviceLine("Desktop toasts", toastState, toastDetail),
		serviceFromStatus("Text messaging", status.Messaging),
		serviceFromStatus("Device discovery", status.Discovery),
		serviceFromStatus("Webhooks", status.Webhooks),
		serviceFromStatus("Socket.IO", status.SocketIO),
	}
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
	lines := []string{sectionHeader(model.width, "24-HOUR HISTORY & EVENT TIMELINE", fmt.Sprintf("%d samples · %d events", len(model.samples), len(model.timeline)))}
	if model.prefs.Visible["graphs"] {
		width := model.width - 30
		if width < 16 {
			width = 16
		}
		if width > 72 {
			width = 72
		}
		lines = append(lines,
			graphLine("Supply Voltage", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.SupplyMV) }), width),
			graphLine("Bus Voltage", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.BusMV) }), width),
			graphLine("Current", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.CurrentMA) }), width),
			graphLine("Power", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.PowerMW) }), width),
			graphLine("Temperature LED", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.TLEDCenti) }), width),
			graphLine("Temperature BT Audio", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.TBTCenti) }), width),
		)
	}
	lines = append(lines, "", titleStyle.Render("Important timeline"))
	shown := 0
	for index := len(model.timeline) - 1; index >= 0 && shown < 10; index-- {
		entry := model.timeline[index]
		if !entry.Important {
			continue
		}
		kind := warnStyle.Render(entry.Kind)
		if entry.Kind == "error" {
			kind = errorStyle.Render(entry.Kind)
		}
		lines = append(lines, fmt.Sprintf("%s  %-14s %s", labelStyle.Render(entry.At.Format("2006-01-02 15:04:05")), kind, entry.Text))
		shown++
	}
	if shown == 0 {
		lines = append(lines, labelStyle.Render("No important events recorded in this session."))
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
	action := labelStyle.Render("Setup remains here until the board and audio path are ready")
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
		valueStyle.Render("One host. Every board surface."),
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
	height := model.height - tabRows - 9
	if len(model.completion) > 0 {
		height--
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

func slider(value uint16, width int) string {
	filled := int(value) * width / 4095
	if filled > width {
		filled = width
	}
	return "[" + lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("━", filled)) + labelStyle.Render(strings.Repeat("─", width-filled)) + "]"
}

func pwmModeName(value byte) string {
	return map[byte]string{0: "off", 1: "manual", 2: "auto"}[value]
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
		"Boot", "Status", "Voltage", "Current", "Temperature LED", "Temperature BT Audio",
		"Illumination", "BT Audio", "Sound", "PWM", "Relay", "Keys",
		"User PWM", "User Relays", "Motion", "RF",
		"Edit · illumination mode", "Edit · illumination on", "Edit · illumination off",
		"Edit · sound settings", "Edit · PWM mode", "Edit · PWM channel", "Edit · PWM value",
		"Edit · relay channel", "Edit · relay value", "Edit · user PWM channel", "Edit · user PWM value",
		"Edit · user relay channel", "Edit · user relay behavior", "Control · user relays",
		"Control · motion", "Confirm · save or discard", "Flash message", "RF learning", "Fault",
	}
	legacy := []string{
		"Boot", "Voltage", "Current", "Temperature LED", "Temperature BT Audio",
		"Illumination", "BT Audio", "Sound", "PWM", "Relay", "Keys",
		"User PWM", "User Relays", "Motion", "RF",
		"Edit · illumination mode", "Edit · illumination on", "Edit · illumination off",
		"Edit · sound settings", "Edit · PWM mode", "Edit · PWM channel", "Edit · PWM value",
		"Edit · relay channel", "Edit · relay value", "Edit · user PWM channel", "Edit · user PWM value",
		"Edit · user relay channel", "Edit · user relay behavior", "Control · user relays",
		"Control · motion", "Confirm · save or discard", "Flash message", "RF learning", "Fault",
		"Status & event overlay",
	}
	names := legacy
	if capabilities&native.CapabilityMenuDirectory != 0 {
		names = current
	}
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
	if snapshot.Hello.IdentitySchema == native.IdentitySchema {
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
	if snapshot.Hello.IdentitySchema == native.IdentitySchemaLegacy {
		return fmt.Sprintf("hash %08X · %s %s", snapshot.Hello.BuildHash, strings.TrimSpace(snapshot.Hello.BuildDate), strings.TrimSpace(snapshot.Hello.BuildTime))
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

func graphLine(label string, values []float64, width int) string {
	return labelStyle.Render(padRightVisible(label, 24)) + " " + lipgloss.NewStyle().Foreground(colorAccent).Render(sparkline(values, width))
}
