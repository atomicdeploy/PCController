package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

type commandOverlayMode uint8

const (
	commandsClosed commandOverlayMode = iota
	commandsMenu
	commandsConfirm
	commandsRunning
	commandsResult
)

type commandID string

const (
	commandLampToggle         commandID = "lamp.toggle"
	commandLampOn             commandID = "lamp.on"
	commandLampOff            commandID = "lamp.off"
	commandLampRefresh        commandID = "lamp.refresh"
	commandLampReconnect      commandID = "lamp.reconnect"
	commandAppShow            commandID = "app.show"
	commandAppHide            commandID = "app.hide"
	commandAppNativeSettings  commandID = "app.native-settings"
	commandAppDiagnostics     commandID = "app.native-diagnostics"
	commandAppWeb             commandID = "app.web"
	commandAppNotify          commandID = "app.notify"
	commandAppAudio           commandID = "app.audio"
	commandAppQuit            commandID = "app.quit"
	commandConfig             commandID = "config"
	commandServing            commandID = "serving"
	commandServeStatus        commandID = "serve.status"
	commandServeEnable        commandID = "serve.enable"
	commandServeDisable       commandID = "serve.disable"
	commandDevice             commandID = "device"
	commandTerminal           commandID = "terminal"
	commandTerminalConnect    commandID = "terminal.connect"
	commandTerminalDisconnect commandID = "terminal.disconnect"
	commandDiagnostics        commandID = "diagnostics"
	commandDashboard          commandID = "dashboard"
)

type commandOverlay struct {
	mode      commandOverlayMode
	selected  commandID
	form      *huh.Form
	err       string
	detail    string
	requestID uint64
	confirm   bool
}

type commandResultMsg struct {
	requestID uint64
	command   commandID
	detail    string
	err       error
	quit      bool
}

type commandSpec struct {
	id          commandID
	label       string
	description string
}

func (m *Model) commandSpecs() []commandSpec {
	specs := []commandSpec{
		{commandLampToggle, "💡 Lamp · Toggle", "Toggle using the confirmed coordinator state"},
		{commandLampOn, "☀  Lamp · Turn on", "Request /lamp/on"},
		{commandLampOff, "🌙 Lamp · Turn off", "Request /lamp/off"},
		{commandLampRefresh, "↻  Lamp · Refresh state", "Read /lamp/get now"},
		{commandLampReconnect, "⟳  Lamp · Reconnect", "Restart WebSocket and HTTP recovery"},
		{commandAppShow, "▣  App · Show main window", "Bring the existing GUI forward"},
		{commandAppHide, "—  App · Hide main window", "Keep the coordinator in the tray"},
		{commandAppNativeSettings, "⚙  App · Open native settings", "Open the Win32 settings window"},
		{commandAppDiagnostics, "🩺 App · Open native diagnostics", "Open the native diagnostics window"},
		{commandAppWeb, "🌐 App · Open Web dashboard", "Open the active local dashboard"},
		{commandAppNotify, "🔔 App · Test notification", "Exercise configured toast/balloon delivery"},
		{commandAppAudio, "♪  App · Test audible cue", "Play audio independently of notification delivery"},
		{commandConfig, "🧰 Configure · Application settings", "Open the complete TUI-native editor"},
		{commandServing, "📡 Configure · Serving", "Local API, PWA, token, and Windows integration"},
		{commandServeStatus, "●  Serving · Status", "Show observed listener state when this TUI owns it"},
		{commandServeEnable, "▶  Serving · Enable", "Persist and start the loopback coordinator"},
		{commandServeDisable, "■  Serving · Disable", "Gracefully stop the loopback coordinator"},
		{commandDevice, "🔧 Device · Parameters", "Sanitized device values and capability-gated edits"},
		{commandTerminal, "⌨  Device · Open terminal", "Attach to the coordinator-owned Telnet session"},
		{commandTerminalConnect, "🔌 Device · Connect terminal", "Connect without opening the terminal screen"},
		{commandTerminalDisconnect, "⏏  Device · Disconnect terminal", "Close the shared Telnet session"},
		{commandDiagnostics, "🔎 Diagnostics · Fetch endpoint", "Open the allow-listed diagnostic reader"},
		{commandDashboard, "⌂  Navigation · Dashboard", "Return to lamp status and activity"},
		{commandAppQuit, "⏻  App · Quit primary", "Stop the active RayanLamp coordinator"},
	}
	for index := range specs {
		specs[index].label = m.text(specs[index].label)
		specs[index].description = m.text(specs[index].description)
	}
	return specs
}

func (m *Model) commandsActive() bool {
	return m.commands.mode != commandsClosed
}

func (m *Model) openCommands() tea.Cmd {
	m.closeSettings()
	m.commands.requestID++
	m.commands.mode = commandsMenu
	m.commands.err = ""
	m.commands.detail = ""
	specs := m.commandSpecs()
	options := make([]huh.Option[commandID], 0, len(specs))
	for _, spec := range specs {
		options = append(
			options,
			huh.NewOption(spec.label+"  ·  "+spec.description, spec.id),
		)
	}
	field := huh.NewSelect[commandID]().
		Title(m.text("Command palette")).
		Description(m.text("Search with / · every operation is routed through the active coordinator.")).
		Options(options...).
		Height(18).
		Value(&m.commands.selected)
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	m.commands.form = huh.NewForm(huh.NewGroup(field)).
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithShowHelp(false).
		WithShowErrors(true).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
	return m.commands.form.Init()
}

func (m *Model) closeCommands() {
	m.commands.requestID++
	m.commands = commandOverlay{requestID: m.commands.requestID}
}

func (m *Model) updateCommandsKey(message tea.KeyPressMsg) tea.Cmd {
	switch m.commands.mode {
	case commandsMenu:
		if message.String() == "esc" &&
			!formIsFiltering(m.commands.form) {
			m.closeCommands()
			return nil
		}
	case commandsConfirm:
		if message.String() == "esc" {
			return m.openCommands()
		}
	case commandsRunning:
		return nil
	case commandsResult:
		switch message.String() {
		case "esc", "enter":
			return m.openCommands()
		case "q":
			m.closeCommands()
			return nil
		}
		return nil
	}
	if m.commands.form == nil {
		return nil
	}
	return m.updateCommandsForm(message)
}

func (m *Model) updateCommandsForm(message tea.Msg) tea.Cmd {
	if m.commands.form == nil {
		return nil
	}
	updated, command := m.commands.form.Update(message)
	if form, ok := updated.(*huh.Form); ok {
		m.commands.form = form
	}
	switch m.commands.form.State {
	case huh.StateCompleted:
		if m.commands.mode == commandsConfirm {
			if !m.commands.confirm {
				return tea.Batch(command, m.openCommands())
			}
			return tea.Batch(command, m.beginPaletteCommand(commandAppQuit))
		}
		return tea.Batch(command, m.selectPaletteCommand(m.commands.selected))
	case huh.StateAborted:
		m.closeCommands()
		return nil
	default:
		return command
	}
}

func (m *Model) updateCommandsMessage(message tea.Msg) tea.Cmd {
	if m.commands.form == nil ||
		(m.commands.mode != commandsMenu && m.commands.mode != commandsConfirm) {
		return nil
	}
	return m.updateCommandsForm(message)
}

func (m *Model) selectPaletteCommand(command commandID) tea.Cmd {
	switch command {
	case commandDashboard:
		m.closeCommands()
		return nil
	case commandConfig:
		m.closeCommands()
		return m.openSettings()
	case commandServing:
		m.closeCommands()
		return m.openSettingsAt(settingsIntegration)
	case commandDevice:
		m.closeCommands()
		return m.openDeviceSettings()
	case commandTerminal:
		m.closeCommands()
		return m.openTerminal()
	case commandDiagnostics:
		m.closeCommands()
		return m.openDiagnostics()
	case commandLampToggle:
		m.closeCommands()
		_, result := m.startAction(ActionToggle)
		return result
	case commandLampOn:
		m.closeCommands()
		_, result := m.startAction(ActionOn)
		return result
	case commandLampOff:
		m.closeCommands()
		_, result := m.startAction(ActionOff)
		return result
	case commandLampRefresh:
		m.closeCommands()
		_, result := m.startAction(ActionRefresh)
		return result
	case commandLampReconnect:
		m.closeCommands()
		_, result := m.startAction(ActionReconnect)
		return result
	case commandAppQuit:
		return m.openQuitConfirmation()
	default:
		return m.beginPaletteCommand(command)
	}
}

func (m *Model) openQuitConfirmation() tea.Cmd {
	m.commands.mode = commandsConfirm
	m.commands.confirm = false
	m.commands.err = ""
	field := huh.NewConfirm().
		Title(m.text("Quit the active RayanLamp primary?")).
		Description(m.text("This stops the GUI/headless coordinator, WebSocket, local API, tray, and automations.")).
		Affirmative(m.text("Quit primary")).
		Negative(m.text("Cancel")).
		Value(&m.commands.confirm)
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	m.commands.form = huh.NewForm(huh.NewGroup(field)).
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithShowHelp(true).
		WithShowErrors(true).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
	return m.commands.form.Init()
}

func (m *Model) beginPaletteCommand(command commandID) tea.Cmd {
	m.commands.requestID++
	requestID := m.commands.requestID
	m.commands.mode = commandsRunning
	m.commands.selected = command
	m.commands.form = nil
	m.commands.err = ""
	m.commands.detail = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		detail, quit, err := m.executePaletteCommand(ctx, command)
		return commandResultMsg{
			requestID: requestID,
			command:   command,
			detail:    detail,
			err:       err,
			quit:      quit,
		}
	}
}

func (m *Model) executePaletteCommand(
	ctx context.Context,
	command commandID,
) (string, bool, error) {
	switch command {
	case commandServeStatus:
		if observer, ok := m.transport.(ServingStatusTransport); ok {
			status, err := observer.ServingStatus(ctx)
			if err != nil {
				return "", false, err
			}
			switch {
			case status.Running:
				return m.text("Local API/Web UI running at") + " " + status.URL, false, nil
			case status.Error != "":
				return m.text("Local API/Web UI configured but unavailable") + " · " + status.Error, false, nil
			case status.Enabled:
				return m.text("Local API/Web UI enabled; listener is starting"), false, nil
			default:
				return m.text("Local API/Web UI disabled"), false, nil
			}
		}
		transport, ok := m.transport.(ConfigTransport)
		if !ok {
			return "", false, fmt.Errorf("transport does not expose serving configuration")
		}
		cfg, err := transport.GetConfig(ctx)
		if err != nil {
			return "", false, err
		}
		if cfg.Integration.LocalAPIEnabled {
			return m.text("Local API/Web UI configured at") + " " + cfg.Integration.LocalAPIListen, false, nil
		}
		return m.text("Local API/Web UI disabled"), false, nil
	case commandServeEnable, commandServeDisable:
		transport, ok := m.transport.(ConfigTransport)
		if !ok {
			return "", false, fmt.Errorf("transport does not expose serving configuration")
		}
		enabled := command == commandServeEnable
		cfg, err := transport.PatchConfig(ctx, map[string]any{
			"integration": map[string]any{"local_api_enabled": enabled},
		})
		if err != nil {
			return "", false, err
		}
		if observer, ok := m.transport.(ServingStatusTransport); ok {
			status, statusErr := awaitServingState(ctx, observer, enabled)
			if statusErr != nil {
				return "", false, statusErr
			}
			if enabled && status.Running {
				return m.text("Local API/Web UI running at") + " " + status.URL, false, nil
			}
			if !enabled && !status.Running {
				return m.text("Local API/Web UI stopped gracefully"), false, nil
			}
		}
		if enabled {
			return m.text("Local API/Web UI enabled at") + " " + cfg.Integration.LocalAPIListen, false, nil
		}
		return m.text("Local API/Web UI disabled; active requests drain gracefully"), false, nil
	case commandTerminalConnect, commandTerminalDisconnect:
		transport, ok := m.transport.(TerminalTransport)
		if !ok {
			return "", false, fmt.Errorf("transport does not expose the device terminal")
		}
		if command == commandTerminalConnect {
			snapshot, err := transport.TerminalConnect(ctx)
			return m.text("Terminal") + " · " + string(snapshot.State), false, err
		}
		snapshot, err := transport.TerminalDisconnect(ctx)
		return m.text("Terminal") + " · " + string(snapshot.State), false, err
	}

	transport, ok := m.transport.(CommandTransport)
	if !ok {
		return "", false, fmt.Errorf("transport does not expose application commands")
	}
	method := ""
	var params any
	switch command {
	case commandAppShow:
		method = "window.show"
	case commandAppHide:
		method = "window.hide"
	case commandAppNativeSettings:
		method = "settings.show"
	case commandAppDiagnostics:
		method = "diagnostics.show"
	case commandAppWeb:
		method = "web.show"
	case commandAppNotify:
		method = "notifications.show"
		params = map[string]string{
			"title":   m.text("Rayan Lamp") + " · " + m.text("Terminal"),
			"message": m.text("Notification test from the command palette."),
		}
	case commandAppAudio:
		method = "audio.play"
	case commandAppQuit:
		method = "quit"
	default:
		return "", false, fmt.Errorf("unsupported command %q", command)
	}
	result, err := transport.ExecuteCommand(ctx, method, params)
	if err != nil {
		return "", false, err
	}
	detail := m.commandSuccessText(command)
	if len(result) != 0 && string(result) != "null" {
		var compact any
		if json.Unmarshal(result, &compact) == nil {
			if encoded, marshalErr := json.Marshal(compact); marshalErr == nil {
				detail += " · " + string(encoded)
			}
		}
	}
	return detail, command == commandAppQuit, nil
}

func awaitServingState(
	ctx context.Context,
	transport ServingStatusTransport,
	wantRunning bool,
) (ServingStatus, error) {
	var last ServingStatus
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := transport.ServingStatus(ctx)
		if err != nil {
			return status, err
		}
		last = status
		if status.Error != "" {
			return status, fmt.Errorf("local API/Web UI listener: %s", status.Error)
		}
		if status.Running == wantRunning {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return last, fmt.Errorf(
				"timed out waiting for local API/Web UI listener: %w",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (m *Model) commandSuccessText(command commandID) string {
	switch command {
	case commandAppShow:
		return m.text("Main window shown")
	case commandAppHide:
		return m.text("Main window hidden")
	case commandAppNativeSettings:
		return m.text("Native settings opened")
	case commandAppDiagnostics:
		return m.text("Native diagnostics opened")
	case commandAppWeb:
		return m.text("Web dashboard opened")
	case commandAppNotify:
		return m.text("Notification test requested")
	case commandAppAudio:
		return m.text("Audible cue requested")
	case commandAppQuit:
		return m.text("Primary is shutting down")
	default:
		return m.text("Command completed")
	}
}

func (m *Model) acceptCommandResult(message commandResultMsg) tea.Cmd {
	if message.requestID != m.commands.requestID ||
		m.commands.mode != commandsRunning {
		return nil
	}
	m.commands.mode = commandsResult
	m.commands.detail = message.detail
	if message.err != nil {
		m.commands.err = compactError(message.err)
		m.appendActivity(
			activityEvent("Command"),
			activityLevelError,
			string(message.command)+" · "+m.commands.err,
		)
	} else {
		m.commands.err = ""
		m.appendActivity(
			activityEvent("Command"),
			activityLevelSuccess,
			string(message.command)+" · "+message.detail,
		)
		m.setNotice(message.detail, false)
	}
	if message.quit {
		return tea.Quit
	}
	return nil
}

func (m *Model) resizeCommands() {
	if !m.commandsActive() || m.commands.form == nil {
		return
	}
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	m.commands.form.
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
}

func (m *Model) renderCommandsOverlay() tea.View {
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	title := m.text("COMMAND CENTER")
	subtitle := m.text("Lamp · app · serving · configuration · device")
	footer := m.text("↑/↓ choose · / filter · enter run · esc dashboard")
	var body string
	switch m.commands.mode {
	case commandsMenu, commandsConfirm:
		if m.commands.mode == commandsConfirm {
			title = m.text("CONFIRM DESTRUCTIVE COMMAND")
			footer = m.text("←/→ choose · enter confirm · esc cancel")
		}
		if m.commands.form != nil {
			body = m.commands.form.View()
		}
	case commandsRunning:
		title = m.text("RUNNING") + " · " + strings.ToUpper(string(m.commands.selected))
		footer = m.text("Please wait")
		transportLabel := sanitizeTerminalLine(m.transport.Label())
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.spinnerViewOn(m.styles.palette.surface)+
				styleOn(m.styles.title, m.styles.palette.surface).
					Render("  "+m.textf("Routing through %s…", transportLabel)),
		)
	case commandsResult:
		title = m.text("COMMAND RESULT")
		footer = m.text("enter/esc commands · q dashboard")
		style := m.styles.notice
		text := m.commands.detail
		if m.commands.err != "" {
			style = m.styles.errorNotice
			text = m.commands.err
		}
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			style.MaxWidth(layout.innerWidth).Render(text),
		)
	}
	body = lipgloss.NewStyle().
		Width(layout.innerWidth).
		Height(layout.bodyHeight).
		MaxWidth(layout.innerWidth).
		MaxHeight(layout.bodyHeight).
		Background(m.styles.palette.surface).
		Align(m.textPosition()).
		Render(body)
	panelTitle := styleOn(
		m.styles.panelTitle,
		m.styles.palette.surface,
	)
	panelMuted := styleOn(
		m.styles.muted,
		m.styles.palette.surface,
	)
	panel := m.styles.diagnosticPanel.
		Width(layout.width).
		Height(layout.height).
		MaxWidth(layout.width).
		MaxHeight(layout.height).
		Render(strings.Join([]string{
			panelTitle.Render(fitText(title, layout.innerWidth)),
			panelMuted.Render(fitText(subtitle, layout.innerWidth)),
			body,
			panelMuted.Render(fitText(footer, layout.innerWidth)),
		}, "\n"))
	content := lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceStyle(
			lipgloss.NewStyle().Background(m.styles.palette.background),
		),
	)
	return m.configuredView(content)
}
