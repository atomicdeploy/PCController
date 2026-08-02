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
		lcdStatus = fmt.Sprintf("available · 0x%02X", status.LCDAddress)
	}
	if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityI2CTransfer != 0 && model.runtime != nil {
		lcd := model.runtime.LCDPresenter().State()
		lcdStatus = "not detected"
		if lcd.Physical {
			lcdStatus = fmt.Sprintf("available · 0x%02X", lcd.Address)
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
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.supply-voltage", "Supply Voltage"), formatVoltage(status.SupplyMV, model.prefs.VoltageDecimals)))
	}
	if model.prefs.Visible["bus"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.bus-voltage", "Bus Voltage"), formatVoltage(status.BusMV, model.prefs.VoltageDecimals)))
	}
	if model.prefs.Visible["current"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.current", "Load Current"), formatCurrent(status.CurrentMA, model.prefs.CurrentDecimals)))
	}
	if model.prefs.Visible["power"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.power", "Load Power"), formatPower(status.PowerMW, model.prefs.PowerDecimals)))
	}
	if model.prefs.Visible["temperature_led"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.temperature-led", "Temperature · Illumination LED"), formatTemperature(status.TLEDCenti, model.prefs.TemperatureDecimals)))
	}
	if model.prefs.Visible["temperature_bt"] {
		measurementLines = append(measurementLines, kvCard(sectionWidth, 33, model.peripheralName("sensor.temperature-audio", "Temperature · BT Audio"), formatTemperature(status.TBTCenti, model.prefs.TemperatureDecimals)))
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
		kvCard(sectionWidth, 22, "BT Audio", bluetoothAudioState(status.BluetoothState)),
		kvCard(sectionWidth, 22, "Active Keys", fmt.Sprintf("0x%02X", status.ActiveKeys)),
		kvCard(sectionWidth, 22, "Active Relays", relaySummary(status.ActiveRelays)),
		kvCard(sectionWidth, 22, model.peripheralName("display.segment", "Display Menu"), fmt.Sprintf("%d · %s", status.MenuPage, model.menuPageByID(status.MenuPage).Name)),
		kvCard(sectionWidth, 22, "Menu / Submode", fmt.Sprintf("%d · %s", status.ProgramMode, model.programModeName(status.ProgramMode))),
		kvCard(sectionWidth, 22, model.peripheralName(fmt.Sprintf("pwm.%d", status.PWMChannel), "PWM"), fmt.Sprintf("channel %d · %d%%", status.PWMChannel, int(status.PWMValue)*100/4095)),
		kvCard(sectionWidth, 22, model.peripheralName("display.lcd", "I2C LCD"), lcdStatus),
	}
	if model.prefs.Visible["diagnostics"] {
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
	tableWidth := model.presentationTableWidth(118)
	rows := make([][]string, 0, 27)
	for index := 0; index < 8; index++ {
		key := fmt.Sprintf("relay.%d", index+1)
		fallback, _ := appconfig.PeripheralDefaultName(key)
		label := fmt.Sprintf("R%d · %s", index+1, truncateText(model.peripheralName(key, fallback), 20))
		on := status.ActiveRelays&(1<<index) != 0
		state := "○ OFF · Enter turns ON"
		if on {
			state = "● ON · Enter turns OFF"
		}
		group := ""
		if index == 0 {
			group = "RELAYS"
		}
		rows = append(rows, []string{group, label, state})
	}
	rows = append(rows, []string{"ACTIONS", "All relays", "Turn OFF"})
	motionAFallback, _ := appconfig.PeripheralDefaultName("motion.a")
	motionBFallback, _ := appconfig.PeripheralDefaultName("motion.b")
	motionA := truncateText(model.peripheralName("motion.a", motionAFallback), 22)
	motionB := truncateText(model.peripheralName("motion.b", motionBFallback), 22)
	rows = append(rows,
		[]string{"MOTION", motionA + " · UP", "Run"},
		[]string{"", motionA + " · STOP", "Stop"},
		[]string{"", motionA + " · DOWN", "Run"},
		[]string{"", motionB + " · UP", "Run"},
		[]string{"", motionB + " · STOP", "Stop"},
		[]string{"", motionB + " · DOWN", "Run"},
	)
	columns := outputTableColumns(tableWidth)
	levelWidth := columns[2].Width - 7
	if levelWidth < 8 {
		levelWidth = 8
	}
	for channel := 0; channel <= 10; channel++ {
		value := uint16(0)
		if model.havePWMValues {
			value = model.pwmValues[channel]
		} else if byte(channel) == status.PWMChannel {
			value = status.PWMValue
		}
		key := fmt.Sprintf("pwm.%d", channel)
		fallback, _ := appconfig.PeripheralDefaultName(key)
		name := truncateText(model.peripheralName(key, fallback), 16)
		percent := int(value) * 100 / 4095
		group := ""
		if channel == 0 {
			group = "PWM"
		}
		rows = append(rows, []string{group, fmt.Sprintf("CH %02d · %s", channel, name), sliderPercent(percent, levelWidth) + fmt.Sprintf(" %3d%%", percent)})
	}
	rows = append(rows, []string{"ACTIONS", "All user PWM", "Set 0%"})
	return strings.Join([]string{
		sectionHeader(model.width, "CONTROL", "↑/↓ select · Enter activate · ←/→ PWM · Home/End min/max · F2 rename"),
		labelStyle.Render("R1 Direction A · R2 Output A · R3 Direction B · R4 Output B"),
		model.centeredDataTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, columns, rows),
	}, "\n")
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
		overlayState = "runtime directory/content enabled · volatile (HOST connection required)"
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
	}
	geometry := menuPageGeometry{frontPanelStart: lipgloss.Height(strings.Join(lines, "\n"))}
	lines = append(lines,
		renderFrontPanelButtons(),
		renderHostMenuDirectory(model.hostMenus, model.width),
		fmt.Sprintf("LCD prompt mirroring  %s  %s", valueStyle.Render(boolWord(model.lcdMirror, "ON", "OFF")), labelStyle.Render("M toggles · priority events temporarily override and restore")),
		labelStyle.Render(fmt.Sprintf("Catalog: %s · Layout: %s · Host overlay: %s · Search: %s · Sort: %s", model.menuCatalogSource, layoutState, overlayState, searchState, model.menuLayoutSort)),
	)
	geometry.frontPanelEnd = geometry.frontPanelStart + lipgloss.Height(renderFrontPanelButtons())
	geometry.entriesStart = lipgloss.Height(strings.Join(lines, "\n"))
	return lines, geometry
}

func (model Model) menusPage(snapshot control.Snapshot) string {
	active := snapshot.Status.MenuPage
	lines, _ := model.menuPagePrefix(snapshot)
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
	return strings.Join([]string{
		sectionHeader(model.width, "BOARD EEPROM SETTINGS", boolWord(snapshot.HaveSettings, "live + persisted on MCU", "not queried yet")),
		labelStyle.Render("↑/↓ select · Enter opens an isolated draft · ←/→ quick-adjusts · MCU and host settings remain separate"),
		model.centeredDataTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, settingsTableColumns(tableWidth), tableRows),
	}, "\n")
}

func (model Model) appSettingsPage() string {
	tableWidth := model.presentationTableWidth(112)
	rows := model.appSettingRows()
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{row.Group, row.Label, row.Value})
	}
	return strings.Join([]string{
		sectionHeader(model.width, "HOST SETTINGS", "saved in host JSON · never board EEPROM"),
		labelStyle.Render("↑/↓ select · Enter opens a modal editor · changes save and hot-apply only after confirmation"),
		model.centeredDataTable(tableWidth, tableBodyRows(model.contentHeight()), model.cursor, settingsTableColumns(tableWidth), tableRows),
	}, "\n")
}

func (model Model) rfPrimaryItems() []actionBarItem {
	items := []actionBarItem{
		{label: "L Learn", action: "rf-learn", style: buttonGoodStyle},
		{label: "Y Timed learn · 30s", action: "rf-timer", style: buttonStyle},
	}
	if model.preview == nil && model.runtime.RFLearnState().Active {
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
	learnState := "idle"
	if model.preview == nil {
		state := model.runtime.RFLearnState()
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
		lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("A Search action"), " ", buttonStyle.Render("N Rename"), " ", buttonStyle.Render("K Category"), " ", buttonStyle.Render("Z View in "+strings.ToUpper(radix)), " ", buttonStyle.Render("[ / ] Move ID"), " ", buttonStyle.Render("V Review"), " ", buttonBadStyle.Render("G Apply"), " ", buttonStyle.Render("X Rollback")),
		labelStyle.Render("Metadata (code, bits, protocol) follows each code when IDs are reordered."),
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

func (model Model) programmingPage(snapshot control.Snapshot) string {
	firstButtons := lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("P Urclock probe"), " ", buttonStyle.Render("M Metadata"), " ", buttonStyle.Render("B Backup"))
	secondButtons := lipgloss.JoinHorizontal(lipgloss.Top, buttonStyle.Render("R Reboot"), " ", buttonStyle.Render("D DTR/RTS reset"), " ", buttonGoodStyle.Render("U Flash"))
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

func outputTableColumns(width int) []dataColumn {
	available := max(42, width-4)
	group := min(11, max(8, available/8))
	name := min(34, max(20, available*36/100))
	value := max(14, available-group-name)
	return []dataColumn{
		{Title: "GROUP", Width: group, Align: lipgloss.Left},
		{Title: "OUTPUT", Width: name, Align: lipgloss.Left},
		{Title: "STATE / LEVEL", Width: value, Align: lipgloss.Left},
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
		{"Supply Voltage", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.SupplyMV) }), func(value float64) string { return formatVoltage(int32(value), model.prefs.VoltageDecimals) }},
		{"Bus Voltage", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.BusMV) }), func(value float64) string { return formatVoltage(int32(value), model.prefs.VoltageDecimals) }},
		{"Load Current", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.CurrentMA) }), func(value float64) string { return formatCurrent(int32(value), model.prefs.CurrentDecimals) }},
		{"Load Power", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.PowerMW) }), func(value float64) string { return formatPower(int32(value), model.prefs.PowerDecimals) }},
		{"Illumination Temperature", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.TLEDCenti) }), func(value float64) string { return formatTemperature(int16(value), model.prefs.TemperatureDecimals) }},
		{"BT Audio Temperature", sampleValues(model.samples, func(sample measurementSample) float64 { return float64(sample.TBTCenti) }), func(value float64) string { return formatTemperature(int16(value), model.prefs.TemperatureDecimals) }},
	}
	rows := make([][]string, 0, len(metrics))
	for _, item := range metrics {
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
	_ = capabilities
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
