package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"rayanlamp.local/app/internal/deviceconfig"
)

type deviceOverlayMode uint8

const (
	deviceClosed deviceOverlayMode = iota
	deviceLoading
	deviceReady
	deviceRunning
)

type deviceAction string

const (
	deviceActionRefresh  deviceAction = "refresh"
	deviceActionConnect  deviceAction = "connect"
	deviceActionTerminal deviceAction = "terminal"
	deviceActionLamp     deviceAction = "lamp-settings"
	deviceActionBack     deviceAction = "back"
)

type deviceOverlay struct {
	mode      deviceOverlayMode
	snapshot  deviceconfig.Snapshot
	action    deviceAction
	form      *huh.Form
	viewport  viewport.Model
	err       string
	detail    string
	requestID uint64
}

type deviceConfigResultMsg struct {
	requestID uint64
	snapshot  deviceconfig.Snapshot
	err       error
}

type deviceActionResultMsg struct {
	requestID uint64
	action    deviceAction
	detail    string
	err       error
}

func (m *Model) deviceActive() bool {
	return m.device.mode != deviceClosed
}

func (m *Model) openDeviceSettings() tea.Cmd {
	m.closeCommands()
	m.closeSettings()
	m.device.requestID++
	requestID := m.device.requestID
	m.device.mode = deviceLoading
	m.device.form = nil
	m.device.err = ""
	m.device.detail = ""
	m.device.viewport.GotoTop()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := fetchDeviceConfig(ctx, m.transport)
		return deviceConfigResultMsg{
			requestID: requestID,
			snapshot:  snapshot,
			err:       err,
		}
	}
}

func (m *Model) closeDeviceSettings() {
	requestID := m.device.requestID + 1
	information := m.device.viewport
	m.device = deviceOverlay{
		requestID: requestID,
		viewport:  information,
	}
}

func fetchDeviceConfig(
	ctx context.Context,
	transport Transport,
) (deviceconfig.Snapshot, error) {
	controller, ok := transport.(CommandTransport)
	if !ok {
		return deviceconfig.Snapshot{}, fmt.Errorf(
			"transport does not expose typed device information",
		)
	}
	result, err := controller.ExecuteCommand(ctx, "device.config.get", nil)
	if err != nil {
		return deviceconfig.Snapshot{}, err
	}
	var snapshot deviceconfig.Snapshot
	if err := json.Unmarshal(result, &snapshot); err != nil {
		return deviceconfig.Snapshot{}, fmt.Errorf("decode typed device information: %w", err)
	}
	return snapshot, nil
}

func (m *Model) acceptDeviceConfig(message deviceConfigResultMsg) tea.Cmd {
	if message.requestID != m.device.requestID ||
		m.device.mode != deviceLoading {
		return nil
	}
	if message.err != nil {
		m.device.err = compactError(message.err)
		m.device.mode = deviceReady
		return m.buildDeviceActions()
	}
	m.device.snapshot = message.snapshot
	m.device.err = ""
	m.device.mode = deviceReady
	m.device.viewport.GotoTop()
	return m.buildDeviceActions()
}

func (m *Model) buildDeviceActions() tea.Cmd {
	m.device.mode = deviceReady
	options := []huh.Option[deviceAction]{
		huh.NewOption(m.text("↻ Refresh all audited device parameters"), deviceActionRefresh),
		huh.NewOption(m.text("🔌 Connect / probe firmware terminal"), deviceActionConnect),
		huh.NewOption(m.text("⌨ Open expert device terminal"), deviceActionTerminal),
		huh.NewOption(m.text("💡 Edit coordinator lamp connection"), deviceActionLamp),
		huh.NewOption(m.text("← Return to dashboard"), deviceActionBack),
	}
	field := huh.NewSelect[deviceAction]().
		Title(m.text("Device actions")).
		Description(m.deviceCapabilitySummary(m.device.snapshot)).
		Options(options...).
		Height(7).
		Value(&m.device.action)
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	m.device.form = huh.NewForm(huh.NewGroup(field)).
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithShowHelp(false).
		WithShowErrors(true).
		WithWidth(layout.innerWidth).
		WithHeight(9)
	m.resizeDevice()
	return m.device.form.Init()
}

func (m *Model) updateDeviceKey(message tea.KeyPressMsg) tea.Cmd {
	switch m.device.mode {
	case deviceLoading, deviceRunning:
		if message.String() == "esc" {
			m.closeDeviceSettings()
		}
		return nil
	case deviceReady:
		// A select filter owns printable input. Do not turn the "r", "x", or
		// "q" in a search query into destructive workspace shortcuts.
		if !formIsFiltering(m.device.form) {
			switch message.String() {
			case "esc", "q":
				m.closeDeviceSettings()
				return nil
			case "r":
				return m.beginDeviceAction(deviceActionRefresh)
			case "x":
				m.closeDeviceSettings()
				return m.openTerminal()
			case "pgup":
				m.device.viewport.PageUp()
				return nil
			case "pgdown":
				m.device.viewport.PageDown()
				return nil
			case "ctrl+u":
				m.device.viewport.HalfPageUp()
				return nil
			case "ctrl+d":
				m.device.viewport.HalfPageDown()
				return nil
			case "home":
				m.device.viewport.GotoTop()
				return nil
			case "end":
				m.device.viewport.GotoBottom()
				return nil
			}
		}
	}
	if m.device.form == nil {
		return nil
	}
	return m.updateDeviceForm(message)
}

func (m *Model) updateDeviceForm(message tea.Msg) tea.Cmd {
	if m.device.form == nil {
		return nil
	}
	updated, command := m.device.form.Update(message)
	if form, ok := updated.(*huh.Form); ok {
		m.device.form = form
	}
	switch m.device.form.State {
	case huh.StateCompleted:
		return tea.Batch(command, m.selectDeviceAction(m.device.action))
	case huh.StateAborted:
		m.closeDeviceSettings()
		return nil
	default:
		return command
	}
}

func (m *Model) updateDeviceMessage(message tea.Msg) tea.Cmd {
	if m.device.mode != deviceReady {
		return nil
	}
	if _, ok := message.(tea.MouseWheelMsg); ok {
		var command tea.Cmd
		m.device.viewport, command = m.device.viewport.Update(message)
		return command
	}
	if m.device.form == nil {
		return nil
	}
	return m.updateDeviceForm(message)
}

func (m *Model) selectDeviceAction(action deviceAction) tea.Cmd {
	switch action {
	case deviceActionBack:
		m.closeDeviceSettings()
		return nil
	case deviceActionTerminal:
		m.closeDeviceSettings()
		return m.openTerminal()
	case deviceActionLamp:
		m.closeDeviceSettings()
		return m.openSettingsAt(settingsLamp)
	default:
		return m.beginDeviceAction(action)
	}
}

func (m *Model) beginDeviceAction(action deviceAction) tea.Cmd {
	m.device.requestID++
	requestID := m.device.requestID
	m.device.mode = deviceRunning
	m.device.action = action
	m.device.form = nil
	m.device.err = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		switch action {
		case deviceActionRefresh:
			controller, ok := m.transport.(CommandTransport)
			if !ok {
				return deviceActionResultMsg{
					requestID: requestID,
					action:    action,
					err:       fmt.Errorf("transport does not expose device refresh"),
				}
			}
			_, err := controller.ExecuteCommand(ctx, "device.config.refresh", nil)
			return deviceActionResultMsg{
				requestID: requestID,
				action:    action,
				detail:    m.text("Audited device parameters refreshed"),
				err:       err,
			}
		case deviceActionConnect:
			terminal, ok := m.transport.(TerminalTransport)
			if !ok {
				return deviceActionResultMsg{
					requestID: requestID,
					action:    action,
					err:       fmt.Errorf("transport does not expose the device terminal"),
				}
			}
			snapshot, err := terminal.TerminalConnect(ctx)
			detail := m.text("Terminal") + " · " + string(snapshot.State)
			if snapshot.ReadOnly {
				detail += " · " + m.text("firmware shell is on Serial (read only over network)")
			}
			return deviceActionResultMsg{
				requestID: requestID,
				action:    action,
				detail:    detail,
				err:       err,
			}
		default:
			return deviceActionResultMsg{
				requestID: requestID,
				action:    action,
				err:       fmt.Errorf("unsupported device action"),
			}
		}
	}
}

func (m *Model) acceptDeviceAction(message deviceActionResultMsg) tea.Cmd {
	if message.requestID != m.device.requestID ||
		m.device.mode != deviceRunning {
		return nil
	}
	if message.err != nil {
		m.device.err = compactError(message.err)
		m.device.detail = ""
	} else {
		m.device.err = ""
		m.device.detail = message.detail
		m.setNotice(message.detail, false)
	}
	// Always fetch a new typed projection after a refresh or terminal probe so
	// capability flags and timestamps cannot lag behind the action.
	return m.openDeviceSettings()
}

func (m *Model) deviceCapabilitySummary(snapshot deviceconfig.Snapshot) string {
	capabilities := snapshot.Capabilities
	switch {
	case capabilities.WriteAvailable:
		return m.text("Writable shell detected; structured mutation remains locked until credential-safe firmware support is present.")
	case capabilities.TerminalReadOnly:
		return m.text("Read only: this firmware routes its shell to Serial. HTTP parameters remain safely viewable.")
	case capabilities.TerminalReachable:
		return m.text("Terminal connected; waiting for a capability banner before allowing writes.")
	case capabilities.HTTPRead:
		return m.text("HTTP diagnostics available. Connect the terminal to probe optional parameter writes.")
	default:
		return m.text("Device is offline; cached sanitized values may be shown.")
	}
}

func (m *Model) resizeDevice() {
	if !m.deviceActive() {
		return
	}
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	actionHeight := deviceActionPaneHeight(layout.bodyHeight)
	if m.device.form != nil && actionHeight > 0 {
		m.device.form.
			WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
			WithWidth(layout.innerWidth).
			WithHeight(actionHeight)
	}
	informationHeight := max(1, layout.bodyHeight-actionHeight)
	m.device.viewport.SetWidth(layout.innerWidth)
	m.device.viewport.SetHeight(informationHeight)
	m.device.viewport.SoftWrap = true
	m.device.viewport.FillHeight = true
	m.device.viewport.MouseWheelEnabled = m.options.Mouse
	m.device.viewport.Style = lipgloss.NewStyle().
		Foreground(m.styles.palette.text).
		Background(m.styles.palette.surfaceAlt).
		Padding(0, 1)
	m.device.viewport.SetContent(
		m.renderDeviceInformation(
			max(
				1,
				layout.innerWidth-
					m.device.viewport.Style.GetHorizontalFrameSize(),
			),
		),
	)
}

func deviceActionPaneHeight(bodyHeight int) int {
	const (
		preferredActionHeight = 9
		minimumInfoHeight     = 3
	)
	return min(
		preferredActionHeight,
		max(0, bodyHeight-minimumInfoHeight),
	)
}

func (m *Model) renderDeviceOverlay() tea.View {
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	title := m.text("DEVICE SETTINGS")
	subtitle := m.text("Typed, sanitized projection · passwords and raw /config bodies never enter this screen")
	footer := m.text("↑/↓ choose · pgup/pgdn info · enter run · r refresh · x terminal · esc")
	var body string
	switch m.device.mode {
	case deviceLoading:
		title = m.text("LOADING DEVICE PARAMETERS")
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.spinnerViewOn(m.styles.palette.surface)+
				styleOn(m.styles.title, m.styles.palette.surface).
					Render("  "+m.text("Reading sanitized coordinator cache…")),
		)
	case deviceRunning:
		title = m.text("Device") + " · " + strings.ToUpper(string(m.device.action))
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.spinnerViewOn(m.styles.palette.surface)+
				styleOn(m.styles.title, m.styles.palette.surface).
					Render("  "+m.text("Coordinating device operation…")),
		)
	default:
		info := m.device.viewport.View()
		actions := ""
		if m.device.form != nil &&
			deviceActionPaneHeight(layout.bodyHeight) > 0 {
			actions = m.device.form.View()
		}
		body = lipgloss.JoinVertical(m.textPosition(), info, actions)
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

func (m *Model) renderDeviceInformation(width int) string {
	snapshot := m.device.snapshot
	value := func(text string) string {
		if strings.TrimSpace(text) == "" {
			return m.text("Not reported")
		}
		return text
	}
	boolValue := func(flag *bool) string {
		if flag == nil {
			return m.text("Not reported")
		}
		if *flag {
			return m.text("Yes")
		}
		return m.text("No")
	}
	uptime := m.text("Not reported")
	if snapshot.Firmware.UptimeSeconds != nil {
		uptime = friendlyDuration(
			time.Duration(*snapshot.Firmware.UptimeSeconds) * time.Second,
		)
	}
	background := m.styles.palette.surfaceAlt
	lines := []string{
		m.renderKVOn(m.text("Device"), value(snapshot.DeviceName), width, background),
		m.renderKVOn(m.text("Hostname"), value(snapshot.Hostname), width, background),
		m.renderKVOn(m.text("Model"), value(snapshot.ModelName), width, background),
		m.renderKVOn(m.text("Station SSID"), value(snapshot.Station.SSID), width, background),
		m.renderKVOn(m.text("Wi-Fi mode"), m.text("Not reported by audited HTTP firmware"), width, background),
		m.renderKVOn(m.text("Station status"), value(snapshot.Station.Status), width, background),
		m.renderKVOn(m.text("Connected"), boolValue(snapshot.Station.Connected), width, background),
		m.renderKVOn(m.text("DHCP"), boolValue(snapshot.Station.DHCP), width, background),
		m.renderKVOn(m.text("Station IP"), value(snapshot.Station.IP), width, background),
		m.renderKVOn(m.text("Gateway / DNS"), value(strings.TrimSpace(snapshot.Station.Gateway+"  "+snapshot.Station.DNS)), width, background),
		m.renderKVOn(m.text("Access point"), value(snapshot.AccessPoint.SSID), width, background),
		m.renderKVOn(m.text("AP status"), value(snapshot.AccessPoint.Status), width, background),
		m.renderKVOn(m.text("AP address"), value(strings.TrimSpace(snapshot.AccessPoint.IP+"  "+snapshot.AccessPoint.Netmask)), width, background),
		m.renderKVOn(m.text("Firmware"), value(snapshot.Firmware.Version), width, background),
		m.renderKVOn(m.text("Firmware hash"), value(snapshot.Firmware.Hash), width, background),
		m.renderKVOn(m.text("Build"), value(snapshot.Firmware.Build), width, background),
		m.renderKVOn(m.text("Uptime"), uptime, width, background),
		m.renderKVOn(m.text("Protocol"), value(snapshot.Firmware.ProtocolRevision), width, background),
		m.renderKVOn(m.text("Updated"), formatTime(snapshot.UpdatedAt), width, background),
	}
	if m.device.err != "" {
		lines = append(
			[]string{
				m.styles.errorNotice.
					MaxWidth(width).
					Render(m.device.err),
				"",
			},
			lines...,
		)
	}
	return strings.Join(lines, "\n")
}
