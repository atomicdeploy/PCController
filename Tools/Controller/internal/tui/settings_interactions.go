package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func (model Model) handleSettingEditorKey(message tea.KeyMsg) (Model, tea.Cmd, bool) {
	if model.settingEditor == nil {
		return model, nil, false
	}
	key := message.String()
	editor := model.settingEditor
	if editor.IsText {
		switch key {
		case "ctrl+c":
			return model, tea.Quit, true
		case "esc":
			model.settingEditor = nil
			model.setNotice("Edit discarded")
			return model, nil, true
		case "enter":
			return model.commitSettingEditor()
		case "ctrl+u":
			editor.Text = ""
			return model, nil, true
		case "backspace":
			runes := []rune(editor.Text)
			if len(runes) != 0 {
				editor.Text = string(runes[:len(runes)-1])
			}
			return model, nil, true
		default:
			if message.Type == tea.KeyRunes {
				editor.Text += string(message.Runes)
				return model, nil, true
			}
		}
		return model, nil, true
	}
	if editor.NumberEditing {
		switch key {
		case "ctrl+c":
			return model, tea.Quit, true
		case "esc":
			editor.NumberEditing = false
			editor.NumberText = ""
			model.setNotice("Typed value discarded; dialog draft retained")
			return model, nil, true
		case "enter":
			field := &editor.Fields[editor.Cursor]
			value, err := parseEditorNumber(*field, editor.NumberText)
			if err != nil {
				model.setNotice("Value not applied: " + err.Error())
				return model, nil, true
			}
			field.Value = value
			editor.NumberEditing = false
			editor.NumberText = ""
			model.setNotice("Typed value applied to draft; Enter again saves")
			return model, nil, true
		case "ctrl+u":
			editor.NumberText = ""
			return model, nil, true
		case "backspace":
			runes := []rune(editor.NumberText)
			if len(runes) != 0 {
				editor.NumberText = string(runes[:len(runes)-1])
			}
			return model, nil, true
		default:
			if message.Type == tea.KeyRunes && validNumberRunes(message.Runes) {
				editor.NumberText += string(message.Runes)
			}
			return model, nil, true
		}
	}

	switch key {
	case "ctrl+c":
		return model, tea.Quit, true
	case "esc":
		model.settingEditor = nil
		model.setNotice("Edit discarded")
		return model, nil, true
	case "enter":
		return model.commitSettingEditor()
	case "up":
		if len(editor.Fields) != 0 {
			editor.Cursor = wrapInt(editor.Cursor, -1, len(editor.Fields))
		}
		return model, nil, true
	case "down":
		if len(editor.Fields) != 0 {
			editor.Cursor = wrapInt(editor.Cursor, 1, len(editor.Fields))
		}
		return model, nil, true
	case "left":
		model.adjustSettingEditor(-1)
		return model, nil, true
	case "right", " ":
		model.adjustSettingEditor(1)
		return model, nil, true
	case "home":
		model.setSettingEditorBoundary(false)
		return model, nil, true
	case "end":
		model.setSettingEditorBoundary(true)
		return model, nil, true
	}
	if message.Type == tea.KeyRunes && len(editor.Fields) != 0 &&
		len(editor.Fields[editor.Cursor].Options) == 0 && validNumberRunes(message.Runes) {
		editor.NumberEditing = true
		editor.NumberText = string(message.Runes)
		return model, nil, true
	}
	return model, nil, true
}

func validNumberRunes(values []rune) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if (value < '0' || value > '9') && value != '.' && value != '-' && value != '+' {
			return false
		}
	}
	return true
}

func (model Model) quickAdjustSelectedSetting(delta int) (Model, tea.Cmd, bool) {
	updated, handled := model.beginSettingEditor()
	if !handled || updated.settingEditor == nil {
		return updated, nil, handled
	}
	if updated.settingEditor.IsText {
		updated.setNotice("Press Enter to edit text in the dialog")
		return updated, nil, true
	}
	updated.adjustSettingEditor(delta)
	return updated.commitSettingEditor()
}

func (model Model) commitSettingEditor() (Model, tea.Cmd, bool) {
	if model.settingEditor == nil {
		return model, nil, true
	}
	if model.settingEditor.Page == PageBoardSettings {
		return model.commitBoardSettingEditor()
	}
	return model.commitAppSettingEditor()
}

func (model Model) commitBoardSettingEditor() (Model, tea.Cmd, bool) {
	editor := model.settingEditor
	if editor.Key == "board.name" {
		name := editor.Text
		if err := native.ValidateBoardName(name); err != nil {
			model.setNotice("Board name was not saved: " + err.Error())
			return model, nil, true
		}
		if model.preview != nil {
			model.preview.BoardName = native.BoardName{Name: name}
			model.preview.HaveBoardName = true
		}
		model.settingEditor = nil
		if name == "" {
			return model.dispatchLine("board name clear")
		}
		return model.dispatchLine("board name set " + strconv.Quote(name))
	}
	settings := model.snapshot().Settings
	var valueErr error
	record := func(key string, err error) {
		if err != nil && valueErr == nil {
			valueErr = fmt.Errorf("%s: %w", key, err)
		}
	}
	byteField := func(key string) byte {
		value, err := checkedUint8(editorField(editor, key))
		record(key, err)
		return value
	}
	uint16Field := func(key string) uint16 {
		value, err := checkedUint16(editorField(editor, key))
		record(key, err)
		return value
	}
	switch editor.Key {
	case "sound.silent":
		setFlag(&settings.Flags, native.SettingsSilent, editorField(editor, "enabled") != 0)
	case "illumination.mode":
		settings.LightMode = byteField("mode")
	case "illumination.on":
		settings.OnBrightness = percentByte(editorField(editor, "value"))
	case "illumination.off":
		settings.OffBrightness = percentByte(editorField(editor, "value"))
	case "display.open":
		settings.DisplayBrightness = percentLevel(editorField(editor, "value"), 7)
	case "display.closed":
		settings.DisplayClosedBrightness = percentLevel(editorField(editor, "value"), 7)
	case "status.brightness":
		settings.StatusBrightness = percentByte(editorField(editor, "value"))
	case "status.color":
		record("color", settings.SetStatusColor(byteField("color")))
	case "output.persistence":
		settings.OutputPersistence = 0
		for _, item := range []struct {
			key  string
			mask byte
		}{{"motion", native.OutputPersistMotionDefault}, {"relays", native.OutputPersistUserRelays}, {"pwm", native.OutputPersistUserPWM}, {"direction", native.OutputPersistDirectionOnly}} {
			if editorField(editor, item.key) != 0 {
				settings.OutputPersistence |= item.mask
			}
		}
	case "relay.restore":
		settings.RelayRestoreMask = 0
		for relay := 1; relay <= 8; relay++ {
			if editorField(editor, fmt.Sprintf("r%d", relay)) != 0 {
				settings.RelayRestoreMask |= 1 << (relay - 1)
			}
		}
	case "stream.period":
		settings.StreamPeriodMS = uint16Field("period")
	case "measurement.decimals":
		record("voltage", settings.SetVoltageDecimals(byteField("voltage")))
		record("current", settings.SetCurrentDecimals(byteField("current")))
	case "menu.default":
		settings.DefaultPage = byteField("page")
	case "menu.remember":
		settings.SetSaveLastPage(editorField(editor, "enabled") != 0)
	case "motion.door":
		record("policy", settings.SetMotionDoorPolicy(byteField("policy")))
	case "motion.exit":
		settings.MotionExitHoldSeconds = byteField("seconds")
	case "motion.break":
		record("milliseconds", settings.SetMotionBreakMS(uint16Field("milliseconds")))
	case "audio.door":
		settings.SetDoorAudioEnabled(editorField(editor, "enabled") != 0)
	case "audio.relay":
		settings.SetRelayAudioEnabled(editorField(editor, "enabled") != 0)
	default:
		model.setNotice("This setting is read-only")
		return model, nil, true
	}
	if valueErr != nil {
		model.setNotice("Setting value is out of range: " + valueErr.Error())
		return model, nil, true
	}

	if model.preview != nil {
		model.preview.Settings = settings
		model.preview.HaveSettings = true
	}
	model.settingEditor = nil
	line := fmt.Sprintf(
		"settings set %d %d %d %d %d %d %d %d %d %d %t %d %d %d %d %d %d",
		settings.Flags, settings.LightMode, settings.OnBrightness, settings.OffBrightness,
		settings.DisplayBrightness, settings.DisplayClosedBrightness,
		settings.StatusBrightness, settings.OutputPersistence,
		settings.StreamPeriodMS, settings.DefaultPage, settings.SaveLastPage(),
		settings.StatusColor(), settings.VoltageDecimals(), settings.CurrentDecimals(),
		settings.MotionExitHoldSeconds, settings.MotionBreakMS(), settings.RelayRestoreMask,
	)
	return model.dispatchLine(line)
}

func (model Model) commitAppSettingEditor() (Model, tea.Cmd, bool) {
	editor := model.settingEditor
	if descriptor, ok := peripheralDescriptorForSettingKey(editor.Key); ok {
		updated, name, restored, err := model.savePeripheralName(descriptor, editor.Text)
		if err != nil {
			model.appendLog("error", "save peripheral name: "+err.Error())
			model.setNotice("Peripheral name was not saved: " + err.Error())
			return model, nil, true
		}
		updated.settingEditor = nil
		if restored {
			updated.setNotice(fmt.Sprintf("%s restored to %q in host settings", descriptor.Key, name))
		} else {
			updated.setNotice(fmt.Sprintf("%s renamed to %q in host settings", descriptor.Key, name))
		}
		return updated, nil, true
	}
	if strings.HasPrefix(editor.Key, "led.") {
		return model.commitStatusLEDSettingEditor()
	}
	if editor.Key == "buzzer.path" {
		paths := []string{"board", "host", "both", "none"}
		selected := editorField(editor, "path")
		if selected < 0 || selected >= len(paths) {
			model.setNotice("Unknown buzzer path")
			return model, nil, true
		}
		model.settingEditor = nil
		return model.dispatchLine("beep path " + paths[selected])
	}
	ui := model.uiValue
	switch editor.Key {
	case "app.title":
		title := strings.TrimSpace(editor.Text)
		if title == "" || len([]rune(title)) > 64 || strings.ContainsAny(title, "\r\n\t") {
			model.setNotice("Application title must be 1..64 printable characters")
			return model, nil, true
		}
		ui.AppTitle = title
		if model.saveUI == nil {
			model.uiValue = ui
			model.prefs = preferencesFromUI(ui)
			model.settingEditor = nil
			return model.dispatchLine("config set ui.app_title " + title)
		}
	case "app.tagline":
		tagline := strings.TrimSpace(editor.Text)
		if tagline == "" || len([]rune(tagline)) > 96 || strings.ContainsAny(tagline, "\r\n\t") {
			model.setNotice("Tagline must be 1..96 printable characters")
			return model, nil, true
		}
		ui.Tagline = tagline
		if model.saveUI == nil {
			model.uiValue = ui
			model.prefs = preferencesFromUI(ui)
			model.settingEditor = nil
			return model.dispatchLine("config set ui.tagline " + tagline)
		}
	case "appearance.identity":
		themes := []string{"system", "light", "dark"}
		locales := []string{"en", "fa"}
		directions := []string{"auto", "ltr", "rtl"}
		ui.Appearance.Theme = themes[editorField(editor, "theme")]
		ui.Appearance.Locale = locales[editorField(editor, "locale")]
		ui.Appearance.Direction = directions[editorField(editor, "direction")]
	case "appearance.accessibility":
		ui.Appearance.ReduceMotion = editorField(editor, "reduced-motion") != 0
		ui.Appearance.CompactNumbers = editorField(editor, "compact-numbers") != 0
	case "appearance.audio":
		ui.Appearance.AudioMuted = editorField(editor, "muted") != 0
		ui.Appearance.AudioVolume = float64(editorField(editor, "volume")) / 100
	case "layout.tables":
		ui.TableLayout = "compact"
		if editorField(editor, "layout") == 1 {
			ui.TableLayout = "expanded"
		}
	case "console.enabled":
		ui.TUIConsole.Enabled = editorField(editor, "enabled") != 0
	case "console.window":
		ui.TUIConsole.Columns = editorField(editor, "columns")
		ui.TUIConsole.Rows = editorField(editor, "rows")
	case "console.font":
		font := strings.TrimSpace(editor.Text)
		if font == "" || len([]rune(font)) > 31 || strings.ContainsAny(font, "\r\n\t") {
			model.setNotice("Console font must be 1..31 printable characters")
			return model, nil, true
		}
		ui.TUIConsole.FontFace = font
	case "console.font_size":
		ui.TUIConsole.FontSize = editorField(editor, "pixels")
	case "poll.active":
		ui.StatusIntervalMS = editorField(editor, "interval")
	case "history.retention":
		ui.HistoryHours = editorField(editor, "hours")
	case "display.decimals":
		ui.VoltageDecimals = editorField(editor, "voltage")
		ui.CurrentDecimals = editorField(editor, "current")
		ui.PowerDecimals = editorField(editor, "power")
		ui.TemperatureDecimals = editorField(editor, "temperature")
	case "measurement.visibility":
		ui.ShowSupplyVoltage = editorField(editor, "supply") != 0
		ui.ShowBusVoltage = editorField(editor, "bus") != 0
		ui.ShowCurrent = editorField(editor, "current") != 0
		ui.ShowPower = editorField(editor, "power") != 0
		ui.ShowTemperatureLED = editorField(editor, "temperature-led") != 0
		ui.ShowTemperatureBT = editorField(editor, "temperature-bt") != 0
	case "diagnostic.visibility":
		ui.ShowIO = editorField(editor, "io") != 0
		ui.ShowDiagnostics = editorField(editor, "diagnostics") != 0
		ui.ShowGraphs = editorField(editor, "graphs") != 0
	case "events.limit":
		ui.EventLogLimit = editorField(editor, "limit")
	case "lcd.services":
		ui.LCDServiceEnabled = editorField(editor, "service") != 0
		ui.MirrorPromptToLCD = editorField(editor, "mirror") != 0
	default:
		model.setNotice("No host-setting editor is available")
		return model, nil, true
	}
	ui.Appearance = appconfig.NormalizeAppearance(ui.Appearance)
	ui.SetupComplete = true
	if strings.HasPrefix(editor.Key, "console.") && model.applyTUIConsole != nil {
		if err := model.applyTUIConsole(ui.TUIConsole); err != nil {
			model.appendLog("error", "apply local console settings: "+err.Error())
			model.setNotice("Local console setting was not applied or saved: " + err.Error())
			return model, nil, true
		}
	}
	if model.saveUI != nil {
		if err := model.saveUI(ui); err != nil {
			model.appendLog("error", "save host settings: "+err.Error())
			model.setNotice("Setting was not saved: " + err.Error())
			return model, nil, true
		}
	}
	model.uiValue = ui
	model.prefs = preferencesFromUI(ui)
	model.lcdMirror = ui.MirrorPromptToLCD
	model.settingEditor = nil
	model.setNotice("Host setting saved and hot-applied")
	return model, nil, true
}

func (model Model) commitStatusLEDSettingEditor() (Model, tea.Cmd, bool) {
	editor := model.settingEditor
	value := model.hostIntegrationValue
	policy := value.StatusLED
	var valueErr error
	byteField := func(key string) byte {
		converted, err := checkedUint8(editorField(editor, key))
		if err != nil && valueErr == nil {
			valueErr = fmt.Errorf("%s: %w", key, err)
		}
		return converted
	}
	switch editor.Key {
	case "led.enabled":
		policy.Enabled = editorField(editor, "enabled") != 0
	case "led.transition":
		policy.TransitionMS = editorField(editor, "milliseconds")
	case "led.rf_hold":
		policy.RFHoldMS = editorField(editor, "milliseconds")
	case "led.door_hold":
		policy.DoorCueHoldMS = editorField(editor, "milliseconds")
	case "led.hot":
		converted, err := checkedInt16(editorField(editor, "centi"))
		if err != nil {
			valueErr = fmt.Errorf("centi: %w", err)
		}
		policy.HotThresholdCentiC = converted
	default:
		if !strings.HasPrefix(editor.Key, "led.visual.") {
			return model, nil, true
		}
		visual := statusVisualByKey(&policy, strings.TrimPrefix(editor.Key, "led.visual."))
		if visual == nil {
			model.setNotice("Unknown status LED state")
			return model, nil, true
		}
		effects := []string{"steady", "flash", "breathe", "crossfade"}
		effect := editorField(editor, "effect")
		if effect < 0 || effect >= len(effects) {
			effect = 0
		}
		visual.Effect = effects[effect]
		visual.Color.Red = byteField("red")
		visual.Color.Green = byteField("green")
		visual.Color.Blue = byteField("blue")
		visual.AlternateColor.Red = byteField("alt-red")
		visual.AlternateColor.Green = byteField("alt-green")
		visual.AlternateColor.Blue = byteField("alt-blue")
		visual.Brightness = percentByte(editorField(editor, "brightness"))
		visual.MinimumBrightness = percentByte(editorField(editor, "minimum"))
		if visual.MinimumBrightness > visual.Brightness {
			visual.MinimumBrightness = visual.Brightness
		}
		visual.PeriodMS = editorField(editor, "period")
		switch visual.Effect {
		case "steady":
			visual.PeriodMS = 0
		case "flash":
			if visual.PeriodMS < int(native.StatusEffectMinimumPeriodMS) {
				visual.PeriodMS = int(native.StatusEffectMinimumPeriodMS)
			}
		default:
			if visual.PeriodMS < int(native.StatusEffectMinimumPeriodMS) {
				visual.PeriodMS = 1000
			}
		}
	}
	if valueErr != nil {
		model.setNotice("Setting value is out of range: " + valueErr.Error())
		return model, nil, true
	}
	value.StatusLED = policy
	if model.saveHostIntegrations != nil {
		if err := model.saveHostIntegrations(value); err != nil {
			model.appendLog("error", "save status LED settings: "+err.Error())
			model.setNotice("Status LED setting was not saved: " + err.Error())
			return model, nil, true
		}
	}
	model.hostIntegrationValue = value
	model.settingEditor = nil
	model.setNotice("Status LED setting saved and hot-applied")
	return model, nil, true
}

func setFlag(flags *byte, mask byte, enabled bool) {
	if enabled {
		*flags |= mask
	} else {
		*flags &^= mask
	}
}

func checkedUint8(value int) (uint8, error) {
	if value < 0 || value > 255 {
		return 0, fmt.Errorf("%d is outside 0..255", value)
	}
	return uint8(value), nil
}

func checkedUint16(value int) (uint16, error) {
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("%d is outside 0..65535", value)
	}
	return uint16(value), nil
}

func checkedInt16(value int) (int16, error) {
	if value < -32768 || value > 32767 {
		return 0, fmt.Errorf("%d is outside -32768..32767", value)
	}
	return int16(value), nil
}
