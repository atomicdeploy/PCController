package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/shell"
)

func (model Model) handleKey(message tea.KeyMsg) (Model, tea.Cmd, bool) {
	key := message.String()
	inputEmpty := strings.TrimSpace(model.input.Value()) == ""
	if model.menuLayoutSearchEditing {
		switch key {
		case "ctrl+c":
			// Preserve the application's global clean-exit shortcut.
		case "esc", "enter":
			model.menuLayoutSearchEditing = false
			return model, nil, true
		case "ctrl+u":
			model.menuLayoutSearch = ""
			model.cursor = 0
			return model, nil, true
		case "backspace":
			runes := []rune(model.menuLayoutSearch)
			if len(runes) != 0 {
				model.menuLayoutSearch = string(runes[:len(runes)-1])
				model.cursor = 0
			}
			return model, nil, true
		default:
			if message.Type == tea.KeyRunes {
				model.menuLayoutSearch += string(message.Runes)
				model.cursor = 0
				return model, nil, true
			}
		}
	}
	if model.rfActionPicker {
		switch key {
		case "ctrl+c":
			// Preserve the application's global clean-exit shortcut.
		case "esc":
			model.cancelRFModal()
			return model, nil, true
		case "up":
			matches := model.filteredRFActions()
			if len(matches) != 0 {
				model.rfActionCursor = wrapInt(model.rfActionCursor, -1, len(matches))
			}
			return model, nil, true
		case "down":
			matches := model.filteredRFActions()
			if len(matches) != 0 {
				model.rfActionCursor = wrapInt(model.rfActionCursor, 1, len(matches))
			}
			return model, nil, true
		case "backspace":
			runes := []rune(model.rfActionQuery)
			if len(runes) != 0 {
				model.rfActionQuery = string(runes[:len(runes)-1])
				model.rfActionCursor = 0
			}
			return model, nil, true
		case "enter":
			matches := model.filteredRFActions()
			entry, ok := model.selectedRFEntry()
			if !ok || len(matches) == 0 {
				return model, nil, true
			}
			if model.rfActionCursor >= len(matches) {
				model.rfActionCursor = len(matches) - 1
			}
			selected := matches[model.rfActionCursor]
			model.cancelRFModal()
			return model.dispatchLine(fmt.Sprintf("rf map %d %s", entry.ID, selected.Args))
		default:
			if message.Type == tea.KeyRunes {
				model.rfActionQuery += string(message.Runes)
				model.rfActionCursor = 0
				return model, nil, true
			}
		}
	}
	if model.rfCategoryPicker {
		switch key {
		case "ctrl+c":
		case "esc":
			model.cancelRFModal()
			return model, nil, true
		case "up":
			model.rfCategoryCursor = wrapInt(model.rfCategoryCursor, -1, len(model.rfValue.Categories)+1)
			return model, nil, true
		case "down":
			model.rfCategoryCursor = wrapInt(model.rfCategoryCursor, 1, len(model.rfValue.Categories)+1)
			return model, nil, true
		case "c":
			model.beginRFCategoryCreate()
			return model, nil, true
		case "enter":
			model.assignRFCategory(model.rfCategoryCursor)
			return model, nil, true
		}
	}
	if model.rfEditMode == "category-color" {
		switch key {
		case "ctrl+c":
		case "esc":
			model.cancelRFModal()
			return model, nil, true
		case "up", "left":
			model.rfCategoryCursor = wrapInt(model.rfCategoryCursor, -1, len(appconfig.RFCategoryPalette))
			return model, nil, true
		case "down", "right":
			model.rfCategoryCursor = wrapInt(model.rfCategoryCursor, 1, len(appconfig.RFCategoryPalette))
			return model, nil, true
		case "enter":
			model.finishRFCategoryColor()
			return model, nil, true
		}
	}
	if model.rfEditMode == "name" || model.rfEditMode == "category-name" {
		switch key {
		case "esc":
			model.cancelRFModal()
			return model, nil, true
		case "enter":
			command, handled := model.finishRFEdit()
			return model, command, handled
		}
	}
	if model.portPicker {
		switch key {
		case "esc", "ctrl+p":
			model.portPicker = false
			return model, nil, true
		case "up":
			if len(model.portCandidates) != 0 {
				model.portCursor = wrapInt(model.portCursor, -1, len(model.portCandidates))
			}
			return model, nil, true
		case "down":
			if len(model.portCandidates) != 0 {
				model.portCursor = wrapInt(model.portCursor, 1, len(model.portCandidates))
			}
			return model, nil, true
		case "enter":
			if len(model.portCandidates) == 0 {
				return model, nil, true
			}
			selected := model.portCandidates[model.portCursor]
			model.portPicker = false
			return model.dispatchLine("open " + selected.Name)
		}
	}
	if inputEmpty && model.page == PageMenus {
		frontKeys := map[string]struct {
			key   int
			phase string
		}{
			"f1": {1, "press"}, "f2": {2, "press"}, "f3": {3, "press"}, "f4": {4, "press"},
			"shift+f1": {1, "hold"}, "shift+f2": {2, "hold"},
			"shift+f3": {3, "hold"}, "shift+f4": {4, "hold"},
		}
		if gesture, ok := frontKeys[key]; ok {
			return model.frontPanelGesture(gesture.key, gesture.phase)
		}
	}

	switch key {
	case "ctrl+c":
		if model.preview == nil {
			_ = model.runtime.Close()
		}
		return model, tea.Quit, true
	case "ctrl+o":
		return model.openPort()
	case "ctrl+p":
		return model.showPortPicker()
	case "ctrl+x":
		return model.closePort()
	case "ctrl+r":
		return model.dispatchLine("reset app")
	case "esc":
		model.input.SetValue("")
		model.completion = nil
		return model, nil, true
	case "enter":
		line := strings.TrimSpace(model.input.Value())
		if line != "" {
			return model.submitLine(line)
		}
		return model.activateSelection()
	case "tab":
		if inputEmpty {
			model.switchPage(model.page + 1)
			return model, nil, true
		}
		model.applyCompletion(false)
		return model, nil, true
	case "shift+tab":
		if inputEmpty {
			model.switchPage(model.page - 1)
			return model, nil, true
		}
		model.applyCompletion(true)
		return model, nil, true
	case "right":
		if !inputEmpty {
			model.acceptRecommendedCompletion()
			return model, nil, true
		}
		if model.input.Value() == "" && model.page == PageConsole {
			history := model.engine.History()
			if len(history) != 0 {
				model.input.SetValue(history[len(history)-1])
				model.input.CursorEnd()
				return model, nil, true
			}
		}
		if model.page == PageOutputs || model.page == PageMenus || model.page == PageBoardSettings || model.page == PageAppSettings || model.page == PageRF {
			return model.adjustSelection(1)
		}
		model.switchPage(model.page + 1)
		return model, nil, true
	case "left":
		if !inputEmpty {
			return model, nil, false
		}
		if model.page == PageOutputs || model.page == PageMenus || model.page == PageBoardSettings || model.page == PageAppSettings || model.page == PageRF {
			return model.adjustSelection(-1)
		}
		model.switchPage(model.page - 1)
		return model, nil, true
	case "up":
		if !inputEmpty || model.page == PageConsole {
			model.historyMove(-1)
			return model, nil, true
		}
		model.moveCursor(-1)
		return model, nil, true
	case "down":
		if !inputEmpty || model.page == PageConsole {
			model.historyMove(1)
			return model, nil, true
		}
		model.moveCursor(1)
		return model, nil, true
	case "home":
		if inputEmpty && model.page == PageOutputs && model.cursor >= 15 && model.cursor <= 25 {
			return model.setSelectedPWM(0)
		}
		if inputEmpty && model.page == PageMenus {
			return model.moveSelectedMenuToRank(0)
		}
	case "end":
		if inputEmpty && model.page == PageOutputs && model.cursor >= 15 && model.cursor <= 25 {
			return model.setSelectedPWM(4095)
		}
		if inputEmpty && model.page == PageMenus {
			return model.moveSelectedMenuToRank(len(model.activeMenuPages()) - 1)
		}
	case "pgup", "pgdown":
		if model.page == PageConsole {
			var command tea.Cmd
			model.viewport, command = model.viewport.Update(message)
			return model, command, true
		}
	}

	if inputEmpty && len(key) == 1 {
		if page, ok := pageForKey(key); ok {
			model.switchPage(page)
			return model, nil, true
		}
		if updated, command, handled := model.pageShortcut(strings.ToLower(key)); handled {
			return updated, command, true
		}
	}
	model.completion = nil
	return model, nil, false
}

func (model Model) showPortPicker() (Model, tea.Cmd, bool) {
	model.portPicker = true
	model.portCursor = 0
	model.portError = ""
	if model.preview != nil {
		model.portCandidates = []ports.Info{model.preview.Port}
		model.portLoading = false
		return model, nil, true
	}
	model.portLoading = true
	return model, listPorts(), true
}

func (model Model) submitLine(line string) (Model, tea.Cmd, bool) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "quit", "exit":
		model.appendLog("info", "Exiting PCController cleanly…")
		if model.preview == nil {
			_ = model.runtime.Close()
		}
		return model, tea.Quit, true
	case "clear", "cls":
		model.logs = nil
		model.updateViewport()
		model.input.SetValue("")
		model.completion = nil
		model.updateInputPlaceholder()
		return model, nil, true
	}
	updated, command, _ := model.dispatchLine(line)
	return updated, command, true
}

func (model Model) dispatchLine(line string) (Model, tea.Cmd, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return model, nil, true
	}
	model.appendLog("tx", "> "+line)
	model.input.SetValue("")
	model.completion = nil
	model.historyPos = -1
	model.historyBuf = ""
	model.updateInputPlaceholder()
	if model.preview != nil {
		return model.simulateCommand(line)
	}
	return model, execute(model.engine, line), true
}

func (model Model) simulateCommand(line string) (Model, tea.Cmd, bool) {
	words := strings.Fields(strings.ToLower(line))
	output := "preview accepted: " + line
	if len(words) >= 3 && words[0] == "relay" {
		if number, err := strconv.Atoi(words[1]); err == nil && number >= 1 && number <= 8 {
			mask := byte(1 << (number - 1))
			switch words[2] {
			case "toggle":
				model.preview.Status.ActiveRelays ^= mask
			case "on":
				model.preview.Status.ActiveRelays |= mask
			case "off":
				model.preview.Status.ActiveRelays &^= mask
			}
		}
	} else if line == "relay off" {
		model.preview.Status.ActiveRelays = 0
	} else if len(words) == 4 && words[0] == "pwm" && words[1] == "set" {
		channel, channelErr := strconv.Atoi(words[2])
		value, valueErr := strconv.Atoi(words[3])
		if channelErr == nil && valueErr == nil && channel >= 0 && channel < 16 && value >= 0 && value <= 4095 {
			model.pwmValues[channel] = uint16(value)
			model.havePWMValues = true
			model.preview.Status.PWMChannel = byte(channel)
			model.preview.Status.PWMValue = uint16(value)
		}
	} else if len(words) == 3 && words[0] == "menu" && words[1] == "page" {
		if resolved, err := control.ResolveMenuPageIn(model.menuPageInfoValues(), words[2]); err == nil {
			model.preview.Status.MenuPage = resolved.ID
			selected := model.menuPageByID(resolved.ID)
			model.previewPanel.MenuID = resolved.ID
			model.previewPanel.MenuName = selected.Name
			model.previewPanel.Segments = selected.Short
		}
	} else if len(words) == 2 && words[0] == "menu" && (words[1] == "prev" || words[1] == "next") {
		delta := 1
		if words[1] == "prev" {
			delta = -1
		}
		visible := model.visibleMenuPages()
		if len(visible) != 0 {
			index := 0
			for candidate, page := range visible {
				if page.ID == model.preview.Status.MenuPage {
					index = candidate
					break
				}
			}
			selected := visible[wrapInt(index, delta, len(visible))]
			model.preview.Status.MenuPage = selected.ID
			model.previewPanel.MenuID = selected.ID
			model.previewPanel.MenuName = selected.Name
			model.previewPanel.Segments = selected.Short
		}
	} else if line == "pwm get" {
		output = fmt.Sprintf("PWM mode=%d selected=%d values=%v", model.preview.Status.PWMMode, model.preview.Status.PWMChannel, model.pwmValues)
	}
	return model, func() tea.Msg { return commandResultMsg{line: line, output: output} }, true
}

func (model Model) openPort() (Model, tea.Cmd, bool) {
	if model.preview != nil {
		model.setNotice("Preview mode: serial open intentionally disabled")
		return model, nil, true
	}
	if model.connectPending {
		return model, nil, true
	}
	model.connectPending = true
	model.runtime.ResumeAuto()
	return model, connect(model.runtime), true
}

func (model Model) closePort() (Model, tea.Cmd, bool) {
	if model.preview != nil {
		model.setNotice("Preview mode: no serial port is owned")
		return model, nil, true
	}
	err := model.runtime.Close()
	model.appendResult("close", "serial port closed", err)
	return model, nil, true
}

func (model *Model) switchPage(page Page) {
	for page < 0 {
		page += pageCount
	}
	page %= pageCount
	model.page = page
	model.cursor = 0
	model.pageOffset = 0
	model.completion = nil
}

func pageForKey(value string) (Page, bool) {
	for index, definition := range pageDefinitions {
		if value == definition.Key {
			return Page(index), true
		}
	}
	return 0, false
}

func (model *Model) moveCursor(delta int) {
	count := model.selectionCount()
	if count == 0 {
		return
	}
	model.cursor = (model.cursor + delta + count) % count
}

func (model Model) selectionCount() int {
	switch model.page {
	case PageOutputs:
		return 28
	case PageMenus:
		return len(model.menuConfigurationEntries())
	case PageBoardSettings:
		return 18
	case PageAppSettings:
		return 32
	case PageRF:
		return len(model.rfStaged)
	default:
		return 0
	}
}

func (model Model) activateSelection() (Model, tea.Cmd, bool) {
	switch model.page {
	case PageOutputs:
		return model.activateOutput()
	case PageMenus:
		entry, ok := model.selectedMenuConfiguration()
		if !ok {
			return model, nil, true
		}
		return model.dispatchLine(fmt.Sprintf("menu page %d", entry.Page.ID))
	case PageBoardSettings:
		return model.adjustBoardSetting(0, true)
	case PageAppSettings:
		return model.adjustAppSetting(0, true)
	case PageRF:
		model.beginRFActionPicker()
		return model, nil, true
	}
	return model, nil, true
}

func (model Model) adjustSelection(delta int) (Model, tea.Cmd, bool) {
	switch model.page {
	case PageOutputs:
		if model.cursor >= 15 && model.cursor <= 25 {
			channel := model.cursor - 15
			value := int(model.pwmValues[channel]) + delta*64
			if value < 0 {
				value = 4095
			}
			if value > 4095 {
				value = 0
			}
			return model.setSelectedPWM(uint16(value))
		}
		if model.cursor == 27 {
			mode := (int(model.snapshot().Status.PWMMode) + delta + 3) % 3
			return model.dispatchLine(fmt.Sprintf("pwm mode %d", mode))
		}
	case PageMenus:
		entry, ok := model.selectedMenuConfiguration()
		if !ok {
			return model, nil, true
		}
		return model.moveSelectedMenuToRank(wrapInt(entry.Rank, delta, len(model.activeMenuPages())))
	case PageBoardSettings:
		return model.adjustBoardSetting(delta, false)
	case PageAppSettings:
		return model.adjustAppSetting(delta, false)
	case PageRF:
		model.moveRFStage(delta)
		return model, nil, true
	}
	return model, nil, true
}

func (model Model) activateOutput() (Model, tea.Cmd, bool) {
	switch {
	case model.cursor >= 0 && model.cursor <= 7:
		return model.dispatchLine(fmt.Sprintf("relay %d toggle", model.cursor+1))
	case model.cursor == 8:
		return model.dispatchLine("relay off")
	case model.cursor >= 9 && model.cursor <= 14:
		commands := []string{
			"relay side left up", "relay side left stop", "relay side left down",
			"relay side right up", "relay side right stop", "relay side right down",
		}
		return model.dispatchLine(commands[model.cursor-9])
	case model.cursor >= 15 && model.cursor <= 25:
		value := model.pwmValues[model.cursor-15]
		if value == 0 {
			value = 2048
		} else {
			value = 0
		}
		return model.setSelectedPWM(value)
	case model.cursor == 26:
		return model.dispatchLine("pwm off")
	case model.cursor == 27:
		mode := (model.snapshot().Status.PWMMode + 1) % 3
		return model.dispatchLine(fmt.Sprintf("pwm mode %d", mode))
	}
	return model, nil, true
}

func (model Model) setSelectedPWM(value uint16) (Model, tea.Cmd, bool) {
	channel := model.cursor - 15
	if channel < 0 || channel > 10 {
		return model, nil, true
	}
	// Preview mode simulates the board; live mode remains board-authoritative and
	// changes only after PWM_GET/STATUS readback confirms the command.
	if model.preview != nil {
		model.pwmValues[channel] = value
		model.havePWMValues = true
	}
	return model.dispatchLine(fmt.Sprintf("pwm set %d %d", channel, value))
}

func (model Model) adjustBoardSetting(delta int, activate bool) (Model, tea.Cmd, bool) {
	settings := model.snapshot().Settings
	toggle := activate || delta != 0
	switch model.cursor {
	case 0:
		if toggle {
			settings.Flags ^= native.SettingsSilent
		}
	case 1:
		if toggle {
			settings.Flags ^= native.SettingsLCDEnabled
		}
	case 2:
		if toggle {
			settings.Flags ^= native.SettingsSwapTemperatureRoles
		}
	case 3:
		settings.LightMode = wrapByte(settings.LightMode, deltaOrOne(delta), 3)
	case 4:
		settings.OnBrightness = wrapByte(settings.OnBrightness, deltaOrOne(delta)*16, 256)
	case 5:
		settings.OffBrightness = wrapByte(settings.OffBrightness, deltaOrOne(delta)*16, 256)
	case 6:
		settings.DisplayBrightness = wrapByte(settings.DisplayBrightness, deltaOrOne(delta), 8)
	case 7:
		settings.StatusBrightness = wrapByte(settings.StatusBrightness, deltaOrOne(delta)*16, 256)
	case 8:
		settings.PWMBootMode = wrapByte(settings.PWMBootMode, deltaOrOne(delta), 3)
	case 9:
		periods := []uint16{0, 100, 125, 200, 250, 500, 1000, 2000, 5000}
		settings.StreamPeriodMS = cycleUint16(periods, settings.StreamPeriodMS, deltaOrOne(delta))
	case 10:
		visible := model.visibleMenuPages()
		if len(visible) != 0 {
			index := 0
			for candidate, page := range visible {
				if page.ID == settings.DefaultPage {
					index = candidate
					break
				}
			}
			settings.DefaultPage = visible[wrapInt(index, deltaOrOne(delta), len(visible))].ID
		}
	case 11:
		if toggle {
			settings.SetSaveLastPage(!settings.SaveLastPage())
		}
	case 12:
		_ = settings.SetStatusColor(wrapByte(settings.StatusColor(), deltaOrOne(delta), 8))
	case 13:
		_ = settings.SetVoltageDecimals(wrapByte(settings.VoltageDecimals(), deltaOrOne(delta), 3))
	case 14:
		_ = settings.SetCurrentDecimals(wrapByte(settings.CurrentDecimals(), deltaOrOne(delta), 3))
	case 15:
		_ = settings.SetMotionDoorPolicy(wrapByte(settings.MotionDoorPolicy(), deltaOrOne(delta), 4))
	case 16:
		if toggle {
			settings.SetDoorAudioEnabled(!settings.DoorAudioEnabled())
		}
	case 17:
		if toggle {
			settings.SetRelayAudioEnabled(!settings.RelayAudioEnabled())
		}
	}
	if model.preview != nil {
		model.preview.Settings = settings
		model.preview.HaveSettings = true
	}
	line := fmt.Sprintf(
		"settings set %d %d %d %d %d %d %d %d %d %t %d %d %d",
		settings.Flags, settings.LightMode, settings.OnBrightness, settings.OffBrightness,
		settings.DisplayBrightness, settings.StatusBrightness, settings.PWMBootMode,
		settings.StreamPeriodMS, settings.DefaultPage, settings.SaveLastPage(),
		settings.StatusColor(), settings.VoltageDecimals(), settings.CurrentDecimals(),
	)
	return model.dispatchLine(line)
}

func (model Model) adjustAppSetting(delta int, activate bool) (Model, tea.Cmd, bool) {
	if model.cursor >= 19 {
		return model.adjustStatusLEDSetting(delta, activate)
	}
	ui := model.uiValue
	step := deltaOrOne(delta)
	switch model.cursor {
	case 0:
		// Title editing remains text-based so mouse/arrow controls cannot
		// accidentally erase it. The command bar gives the exact operation.
		model.input.SetValue("config set ui.app_title ")
		model.input.CursorEnd()
		return model, nil, true
	case 1:
		values := []int{100, 125, 200, 250, 500, 1000, 2000, 5000}
		ui.StatusIntervalMS = cycleInt(values, ui.StatusIntervalMS, step)
	case 2:
		values := []int{1, 6, 12, 24, 48, 72, 168}
		ui.HistoryHours = cycleInt(values, ui.HistoryHours, step)
	case 3:
		ui.VoltageDecimals = wrapInt(ui.VoltageDecimals, step, 5)
	case 4:
		ui.CurrentDecimals = wrapInt(ui.CurrentDecimals, step, 5)
	case 5:
		ui.PowerDecimals = wrapInt(ui.PowerDecimals, step, 5)
	case 6:
		ui.TemperatureDecimals = wrapInt(ui.TemperatureDecimals, step, 3)
	case 7:
		if activate || delta != 0 {
			ui.ShowSupplyVoltage = !ui.ShowSupplyVoltage
		}
	case 8:
		if activate || delta != 0 {
			ui.ShowBusVoltage = !ui.ShowBusVoltage
		}
	case 9:
		if activate || delta != 0 {
			ui.ShowCurrent = !ui.ShowCurrent
		}
	case 10:
		if activate || delta != 0 {
			ui.ShowPower = !ui.ShowPower
		}
	case 11:
		if activate || delta != 0 {
			ui.ShowTemperatureLED = !ui.ShowTemperatureLED
		}
	case 12:
		if activate || delta != 0 {
			ui.ShowTemperatureBT = !ui.ShowTemperatureBT
		}
	case 13:
		if activate || delta != 0 {
			ui.ShowIO = !ui.ShowIO
		}
	case 14:
		if activate || delta != 0 {
			ui.ShowDiagnostics = !ui.ShowDiagnostics
		}
	case 15:
		if activate || delta != 0 {
			ui.ShowGraphs = !ui.ShowGraphs
		}
	case 16:
		values := []int{100, 250, 500, 1000, 2000, 5000, 10000, 50000}
		ui.EventLogLimit = cycleInt(values, ui.EventLogLimit, step)
	case 17:
		if activate || delta != 0 {
			ui.LCDServiceEnabled = !ui.LCDServiceEnabled
		}
	case 18:
		if activate || delta != 0 {
			ui.MirrorPromptToLCD = !ui.MirrorPromptToLCD
		}
	}
	ui.SetupComplete = true
	model.uiValue = ui
	model.prefs = preferencesFromUI(ui)
	if model.saveUI != nil {
		if err := model.saveUI(ui); err != nil {
			model.appendLog("error", "save PC host settings: "+err.Error())
			return model, nil, true
		}
		model.setNotice("PC host setting saved and hot-applied")
	} else {
		model.setNotice("Setting applied for this session; persistent config hook unavailable")
	}
	return model, nil, true
}

func (model Model) adjustStatusLEDSetting(delta int, activate bool) (Model, tea.Cmd, bool) {
	value := model.hostIntegrationValue
	policy := value.StatusLED
	step := deltaOrOne(delta)
	switch model.cursor {
	case 19:
		if activate || delta != 0 {
			policy.Enabled = !policy.Enabled
		}
	case 20:
		policy.TransitionMS = cycleInt(
			[]int{0, 100, 200, 300, 420, 600, 1000, 2000},
			policy.TransitionMS,
			step,
		)
	case 21:
		policy.RFHoldMS = cycleInt(
			[]int{250, 500, 900, 1400, 2000, 3000, 5000},
			policy.RFHoldMS,
			step,
		)
	case 22:
		policy.HotThresholdCentiC += int16(step * 100)
		if policy.HotThresholdCentiC < 3000 {
			policy.HotThresholdCentiC = 12500
		}
		if policy.HotThresholdCentiC > 12500 {
			policy.HotThresholdCentiC = 3000
		}
	default:
		visual := statusLEDVisualByCursor(&policy, model.cursor)
		if visual != nil {
			visual.Color = cycleStatusLEDColor(visual.Color, step)
		}
	}
	value.StatusLED = policy
	model.hostIntegrationValue = value
	if model.saveHostIntegrations != nil {
		if err := model.saveHostIntegrations(value); err != nil {
			model.appendLog("error", "save status-light settings: "+err.Error())
			return model, nil, true
		}
		model.setNotice("Status-light policy saved and hot-applied")
	} else {
		model.setNotice("Status-light policy applied for this session; persistent hook unavailable")
	}
	return model, nil, true
}

func statusLEDVisualByCursor(
	policy *appconfig.StatusLEDPolicy,
	cursor int,
) *appconfig.StatusLEDVisual {
	switch cursor {
	case 23:
		return &policy.Idle
	case 24:
		return &policy.Running
	case 25:
		return &policy.BluetoothAudioConnected
	case 26:
		return &policy.BluetoothAudioSearching
	case 27:
		return &policy.BluetoothAudioOff
	case 28:
		return &policy.RFActivity
	case 29:
		return &policy.Hot
	case 30:
		return &policy.RunningDoorOpen
	case 31:
		return &policy.PCOffline
	default:
		return nil
	}
}

func cycleStatusLEDColor(current appconfig.RGBColor, delta int) appconfig.RGBColor {
	palette := []appconfig.RGBColor{
		{Red: 255},
		{Blue: 255},
		{Red: 190, Blue: 255},
		{Green: 255},
		{Red: 255, Green: 255, Blue: 255},
		{Red: 255, Green: 150},
		{Green: 80, Blue: 255},
		{Red: 255, Green: 72},
	}
	index := 0
	for candidate, color := range palette {
		if color == current {
			index = candidate
			break
		}
	}
	index = (index + delta + len(palette)) % len(palette)
	return palette[index]
}

func (model Model) pageShortcut(key string) (Model, tea.Cmd, bool) {
	switch model.page {
	case PageDashboard:
		switch key {
		case "i":
			return model.dispatchLine("program-state set tui idle")
		case "r":
			return model.dispatchLine("program-state set tui running TUI operator")
		}
	case PageRF:
		switch key {
		case "l":
			return model.dispatchLine("rf learn indefinite multi")
		case "c":
			return model.dispatchLine("rf cancel")
		case "r":
			if model.preview != nil {
				model.resetRFStage(model.rfEntries)
				model.setNotice("Preview RF list refreshed")
				return model, nil, true
			}
			if model.rfPending {
				return model, nil, true
			}
			model.rfPending = true
			return model, model.fetchRFEntriesCommand(), true
		case "t":
			model.input.SetValue("rf send ")
			model.input.CursorEnd()
			return model, nil, true
		case "a":
			model.beginRFActionPicker()
			return model, nil, true
		case "n":
			model.beginRFNameEdit()
			return model, nil, true
		case "k":
			model.beginRFCategoryPicker()
			return model, nil, true
		case "z":
			model.toggleRFRadix()
			return model, nil, true
		case "[":
			model.moveRFStage(-1)
			return model, nil, true
		case "]":
			model.moveRFStage(1)
			return model, nil, true
		case "v":
			if !model.rfStageDirty {
				model.setNotice("RF order already matches device readback")
			} else {
				model.rfReview = true
				model.setNotice("Staged RF ID order reviewed; apply remains capability-gated")
			}
			return model, nil, true
		case "g":
			if !model.rfStageDirty {
				model.setNotice("No staged RF ID changes to apply")
				return model, nil, true
			}
			if !model.rfReview {
				model.setNotice("Review the staged RF ID order before applying")
				return model, nil, true
			}
			support := model.currentRFReplaceSupport()
			if !support.Supported {
				if !support.Known && model.rfProbeReplace != nil {
					model.rfPending = true
					model.setNotice("Safely probing optional RF replacement opcode; EEPROM cannot be changed")
					return model, model.probeRFReplaceCommand(), true
				}
				model.setNotice("Apply disabled: " + support.Reason)
				return model, nil, true
			}
			if model.rfApplyOrder == nil || model.rfFetch == nil {
				model.setNotice("Apply disabled: transactional host integration is unavailable")
				return model, nil, true
			}
			model.rfPending = true
			return model, model.applyRFOrderCommand(), true
		case "x":
			model.resetRFStage(model.rfOriginal)
			model.setNotice("Staged RF ID changes rolled back locally")
			return model, nil, true
		}
	case PageProgramming:
		switch key {
		case "p":
			return model.dispatchLine("boot probe")
		case "m":
			return model.dispatchLine("boot metadata")
		case "b":
			return model.dispatchLine("boot backup backups")
		case "r":
			return model.dispatchLine("reset app")
		case "d":
			return model.dispatchLine("reset lines")
		case "u":
			model.input.SetValue("program flash ")
			model.input.CursorEnd()
			return model, nil, true
		case "a":
			model.input.SetValue("program flash  --usbasp-troubleshooting")
			model.input.SetCursor(len("program flash "))
			return model, nil, true
		}
	case PageAutomations:
		switch key {
		case "a":
			return model.dispatchLine("automation list")
		case "m":
			return model.dispatchLine("macro list")
		case "c":
			return model.dispatchLine("macro cancel")
		}
	case PageMenus:
		switch key {
		case "/":
			model.menuLayoutSearchEditing = true
			return model, nil, true
		case "s":
			sorts := []string{"rank", "category", "name", "visibility"}
			index := 0
			for candidate, value := range sorts {
				if value == model.menuLayoutSort {
					index = candidate
					break
				}
			}
			model.menuLayoutSort = sorts[(index+1)%len(sorts)]
			model.cursor = 0
			return model, nil, true
		case "v", " ":
			return model.toggleSelectedMenuVisibility()
		case "[":
			return model.adjustSelection(-1)
		case "]":
			return model.adjustSelection(1)
		case "a":
			return model.applyStagedMenuLayout()
		case "x":
			model.menuLayoutStaged = cloneMenuLayout(model.menuLayoutOriginal)
			model.menuLayoutDirty = false
			model.menuLayoutError = ""
			model.setNotice("Staged menu visibility and ranks discarded")
			return model, nil, true
		case "r":
			if model.preview != nil {
				model.menuLayoutStaged = cloneMenuLayout(model.menuLayout)
				model.menuLayoutOriginal = cloneMenuLayout(model.menuLayout)
				model.menuLayoutDirty = false
				model.setNotice("Preview menu layout refreshed")
				return model, nil, true
			}
			if model.menuCatalogPending {
				return model, nil, true
			}
			model.menuCatalogPending = true
			model.menuCatalogLastAttempt = time.Now()
			return model, refreshMenuCatalog(model.runtime), true
		case "e":
			return model.beginHostMenuDefinitionEdit()
		}
		if key == "h" && model.hostMenus != nil {
			if model.hostMenus.Snapshot().Active {
				return model, model.closeHostMenuCommand("Closed from TUI"), true
			}
			return model, model.openHostMenuCommand(""), true
		}
		if key == "m" {
			model.uiValue.MirrorPromptToLCD = !model.lcdMirror
			model.lcdMirror = model.uiValue.MirrorPromptToLCD
			if model.saveUI != nil {
				if err := model.saveUI(model.uiValue); err != nil {
					model.appendLog("error", "save LCD mirror setting: "+err.Error())
					return model, nil, true
				}
			}
			state := model.currentFrontPanel(model.snapshot())
			if model.mirrorLCD == nil {
				model.setNotice("LCD prompt mirror UI toggled; device mirror capability unavailable")
				return model, nil, true
			}
			return model, func() tea.Msg {
				err := model.mirrorLCD(state.LCDLine1, state.LCDLine2)
				return commandResultMsg{line: "LCD prompt mirror", err: err}
			}, true
		}
	}
	return model, nil, false
}

func (model Model) menuPageInfoValues() []control.MenuPageInfo {
	pages := model.activeMenuPages()
	values := make([]control.MenuPageInfo, 0, len(pages))
	for _, page := range pages {
		values = append(values, control.MenuPageInfo{
			ID: page.ID, Key: page.Key, Label: page.Short,
			Name: page.Name, Description: page.Description,
		})
	}
	return values
}

func (model Model) beginHostMenuDefinitionEdit() (Model, tea.Cmd, bool) {
	if model.hostMenus == nil {
		model.setNotice("Persistent host-menu configuration is unavailable")
		return model, nil, true
	}
	active := model.hostMenus.Snapshot()
	if active.Active {
		model.input.SetValue(shell.Join([]string{"host-menu", "set", active.MenuID}) + " ")
		model.input.CursorEnd()
		model.setNotice("Complete FIELD VALUE; fields include label, title, content, parent_id, node_id, read_only, brightness, edit_visual")
		return model, nil, true
	}
	entry, ok := model.selectedMenuConfiguration()
	if !ok {
		return model, nil, true
	}
	for _, override := range model.hostMenus.Config().BuiltinOverrides {
		if override.StableID == entry.Page.ID {
			model.input.SetValue(fmt.Sprintf("host-menu set builtin:%d ", entry.Page.ID))
			model.input.CursorEnd()
			model.setNotice("Editing online label override; stable/wire ID remains immutable")
			return model, nil, true
		}
	}
	parent := byte(0x70)
	switch entry.Page.Category {
	case "Environment":
		parent = 0x71
	case "Outputs":
		parent = 0x72
	case "Inputs / RF":
		parent = 0x73
	}
	model.input.SetValue(shell.Join([]string{
		"host-menu", "override", strconv.Itoa(int(entry.Page.ID)), entry.Page.Short,
		entry.Page.Name, fmt.Sprintf("0x%02X", parent),
	}))
	model.input.CursorEnd()
	model.setNotice("Press Enter to create this built-in online label override, then E again to edit fields")
	return model, nil, true
}

func (model Model) moveSelectedMenuToRank(rank int) (Model, tea.Cmd, bool) {
	entry, ok := model.selectedMenuConfiguration()
	if !ok {
		return model, nil, true
	}
	layout, err := control.MoveMenuPage(model.menuPageInfoValues(), model.menuLayoutStaged, entry.Page.ID, rank)
	if err != nil {
		model.menuLayoutError = err.Error()
		model.setNotice("Menu rank unchanged: " + err.Error())
		return model, nil, true
	}
	model.menuLayoutStaged = layout
	model.menuLayoutDirty = !sameMenuLayout(model.menuLayoutStaged, model.menuLayoutOriginal)
	model.menuLayoutError = ""
	return model, nil, true
}

func (model Model) toggleSelectedMenuVisibility() (Model, tea.Cmd, bool) {
	entry, ok := model.selectedMenuConfiguration()
	if !ok {
		return model, nil, true
	}
	hidingCurrent := entry.Visible && model.snapshot().Status.MenuPage == entry.Page.ID
	hidingDefault := entry.Visible && model.snapshot().Settings.DefaultPage == entry.Page.ID
	layout, err := control.SetMenuPageVisible(
		model.menuPageInfoValues(), model.menuLayoutStaged, entry.Page.ID, !entry.Visible,
	)
	if err != nil {
		model.menuLayoutError = err.Error()
		model.setNotice("Menu visibility unchanged: " + err.Error())
		return model, nil, true
	}
	model.menuLayoutStaged = layout
	model.menuLayoutDirty = !sameMenuLayout(model.menuLayoutStaged, model.menuLayoutOriginal)
	model.menuLayoutError = ""
	if hidingCurrent || hidingDefault {
		fallback := "first visible persisted rank"
		for _, id := range layout.Order {
			if layout.Visible(id) {
				fallback = fmt.Sprintf("stable ID %d (%s)", id, model.menuPageByID(id).Name)
				break
			}
		}
		model.setNotice("Warning: hiding current/default page; firmware will fall back to " + fallback + " after apply")
	}
	return model, nil, true
}

func (model Model) applyStagedMenuLayout() (Model, tea.Cmd, bool) {
	if !model.menuLayoutDirty {
		model.setNotice("Menu layout already matches board readback")
		return model, nil, true
	}
	if !model.menuLayoutStaged.Supported || !model.menuLayoutStaged.Persistent {
		model.setNotice("Apply disabled: firmware does not advertise persistent menu layout capability bit 23")
		return model, nil, true
	}
	parts := []string{"menu", "layout", "set", fmt.Sprintf("0x%04X", model.menuLayoutStaged.VisibleMask)}
	for _, id := range model.menuLayoutStaged.Order {
		parts = append(parts, strconv.Itoa(int(id)))
	}
	line := strings.Join(parts, " ")
	if model.preview != nil {
		model.menuLayout = cloneMenuLayout(model.menuLayoutStaged)
		model.menuLayoutOriginal = cloneMenuLayout(model.menuLayoutStaged)
		model.menuLayoutDirty = false
		model.setNotice("Preview EEPROM menu layout saved and read back")
		return model, nil, true
	}
	return model.dispatchLine(line)
}

func sameMenuLayout(left, right control.MenuLayout) bool {
	if left.VisibleMask != right.VisibleMask || len(left.Order) != len(right.Order) {
		return false
	}
	for index := range left.Order {
		if left.Order[index] != right.Order[index] {
			return false
		}
	}
	return true
}

func (model Model) frontPanelGesture(key int, phase string) (Model, tea.Cmd, bool) {
	if key < 1 || key > 4 {
		return model, nil, true
	}
	if model.hostMenus != nil && model.hostMenus.Snapshot().Active {
		return model, model.hostMenuKeyCommand(key, phase), true
	}
	if model.frontPanelKey != nil {
		return model, func() tea.Msg {
			err := model.frontPanelKey(key, phase)
			return commandResultMsg{line: fmt.Sprintf("front-panel K%d %s", key, phase), err: err}
		}, true
	}
	if phase == "release" {
		return model, nil, true
	}
	actions := []string{"menu prev", "menu next", "menu dec", "menu inc"}
	return model.dispatchLine(actions[key-1])
}

func (model *Model) applyCompletion(reverse bool) {
	candidates := completionCandidates(model.engine, model.input.Value())
	if len(candidates) == 0 {
		model.completion = nil
		return
	}
	model.completion = candidates
	if reverse {
		model.completionIndex--
		if model.completionIndex < 0 {
			model.completionIndex = len(candidates) - 1
		}
	} else {
		model.completionIndex = (model.completionIndex + 1) % len(candidates)
	}
	if len(candidates) == 1 {
		model.input.SetValue(candidates[0] + " ")
		model.input.CursorEnd()
		model.completion = nil
		return
	}
	common := commonCompletionPrefix(candidates)
	if len(common) > len(model.input.Value()) {
		model.input.SetValue(common)
		model.input.CursorEnd()
	}
}

func (model *Model) acceptRecommendedCompletion() {
	candidates := completionCandidates(model.engine, model.input.Value())
	if len(candidates) == 0 {
		return
	}
	index := model.completionIndex
	if index < 0 || index >= len(candidates) {
		index = 0
	}
	model.input.SetValue(candidates[index] + " ")
	model.input.CursorEnd()
	model.completion = nil
}

func (model Model) handleMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	if message.Action == tea.MouseActionRelease && model.page == PageMenus && !model.portPicker {
		contentY := 4 + strings.Count(model.tabBar(), "\n") + 1
		return model.handleFrontPanelMouse(message.Y-contentY, message.X, "release")
	}
	if message.Action != tea.MouseActionPress {
		return model, nil
	}
	if message.Button == tea.MouseButtonWheelUp {
		model.moveCursor(-1)
		return model, nil
	}
	if message.Button == tea.MouseButtonWheelDown {
		model.moveCursor(1)
		return model, nil
	}
	if message.Button != tea.MouseButtonLeft {
		return model, nil
	}
	// Header is row 0; bordered action buttons occupy rows 1..3.
	if message.Y >= 1 && message.Y <= 3 {
		return model.handleActionBarClick(message.X)
	}
	tabStart := 4
	if page, ok := model.tabAt(message.X, message.Y-tabStart); ok {
		model.switchPage(page)
		return model, nil
	}
	contentY := tabStart + strings.Count(model.tabBar(), "\n") + 1
	if message.Y < contentY {
		return model, nil
	}
	row := message.Y - contentY
	return model.handleContentClick(row, message.X)
}

func (model Model) handleActionBarClick(x int) (tea.Model, tea.Cmd) {
	buttons := []struct {
		width int
		run   func() (Model, tea.Cmd, bool)
	}{
		{13, model.showPortPicker},
		{10, model.openPort},
		{11, model.closePort},
		{16, func() (Model, tea.Cmd, bool) { return model.dispatchLine("reset app") }},
		{13, func() (Model, tea.Cmd, bool) { return model.dispatchLine("status") }},
	}
	position := 0
	for _, button := range buttons {
		if x >= position && x < position+button.width {
			updated, command, _ := button.run()
			return updated, command
		}
		position += button.width + 1
	}
	return model, nil
}

func (model Model) tabAt(x, row int) (Page, bool) {
	if row < 0 {
		return 0, false
	}
	currentRow, currentX := 0, 0
	for index, definition := range pageDefinitions {
		width := len(definition.Key+" "+definition.Short) + 2
		if currentX != 0 && currentX+1+width > model.width {
			currentRow++
			currentX = 0
		}
		if currentRow == row && x >= currentX && x < currentX+width {
			return Page(index), true
		}
		if currentX == 0 {
			currentX = width
		} else {
			currentX += 1 + width
		}
	}
	return 0, false
}

func (model Model) handleContentClick(row, x int) (tea.Model, tea.Cmd) {
	if model.portPicker {
		index := row - 2
		if index >= 0 && index < len(model.portCandidates) {
			model.portCursor = index
			selected := model.portCandidates[index]
			model.portPicker = false
			updated, command, _ := model.dispatchLine("open " + selected.Name)
			return updated, command
		}
		return model, nil
	}
	switch model.page {
	case PageOutputs:
		index := row - 2
		if index >= 0 && index < model.selectionCount() {
			model.cursor = index
			if index >= 15 && index <= 25 && x > 28 {
				value := (x - 28) * 4095 / 26
				if value < 0 {
					value = 0
				}
				if value > 4095 {
					value = 4095
				}
				updated, command, _ := model.setSelectedPWM(uint16(value))
				return updated, command
			}
			updated, command, _ := model.activateSelection()
			return updated, command
		}
	case PageMenus:
		if row >= 9 && row <= 11 {
			return model.handleFrontPanelMouse(row, x, "press")
		}
		index := row - 14
		if index >= 0 && index < len(model.activeMenuPages()) {
			model.cursor = index
			updated, command, _ := model.activateSelection()
			return updated, command
		}
	case PageBoardSettings, PageAppSettings:
		index := row - 2
		if index >= 0 && index < model.selectionCount() {
			model.cursor = index
			return model, nil
		}
	case PageRF:
		if row == 1 {
			switch {
			case x < 32:
				updated, command, _ := model.dispatchLine("rf learn indefinite multi")
				return updated, command
			case x < 46:
				updated, command, _ := model.dispatchLine("rf cancel")
				return updated, command
			case x < 67:
				updated, command, _ := model.dispatchLine("rf list")
				return updated, command
			}
		}
	}
	return model, nil
}

func (model Model) handleFrontPanelMouse(row, x int, phase string) (tea.Model, tea.Cmd) {
	if row < 9 || row > 11 {
		return model, nil
	}
	widths := []int{17, 13, 17, 17}
	position := 0
	for index, width := range widths {
		if x >= position && x < position+width {
			updated, command, _ := model.frontPanelGesture(index+1, phase)
			return updated, command
		}
		position += width + 1
	}
	return model, nil
}

func wrapByte(value byte, delta int, count int) byte {
	result := (int(value) + delta) % count
	if result < 0 {
		result += count
	}
	return byte(result)
}

func wrapInt(value, delta, count int) int {
	result := (value + delta) % count
	if result < 0 {
		result += count
	}
	return result
}

func deltaOrOne(delta int) int {
	if delta == 0 {
		return 1
	}
	return delta
}

func cycleInt(values []int, current, delta int) int {
	index := 0
	for candidate, value := range values {
		if value == current {
			index = candidate
			break
		}
	}
	return values[wrapInt(index, delta, len(values))]
}

func cycleUint16(values []uint16, current uint16, delta int) uint16 {
	index := 0
	for candidate, value := range values {
		if value == current {
			index = candidate
			break
		}
	}
	return values[wrapInt(index, delta, len(values))]
}

func (model *Model) persistUI(value appconfig.UI) error {
	if model.saveUI == nil {
		return nil
	}
	return model.saveUI(value)
}
