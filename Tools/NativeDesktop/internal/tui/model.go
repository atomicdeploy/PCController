package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/model"
)

const maxActivityEntries = 80

type ModelOptions struct {
	Poll          time.Duration
	Timeout       time.Duration
	Theme         ThemeMode
	Locale        i18n.Locale
	AltScreen     bool
	Mouse         bool
	InitialScreen InitialScreen
	OpenTerminal  bool
	InitialNow    time.Time
}

type Model struct {
	ctx       context.Context
	transport Transport
	options   ModelOptions

	width  int
	height int

	snapshot        model.Snapshot
	haveState       bool
	busy            bool
	busyAction      Action
	lastError       string
	controllerError string
	notice          string
	noticeError     bool
	noticeUntil     time.Time
	noticeRevision  uint64
	noticeScheduled uint64
	activity        []activityEntry
	pulse           int

	statusPending   bool
	statusRequestID uint64
	stateRevision   uint64

	themeMode        ThemeMode
	isDark           bool
	styles           styles
	localePreference i18n.Locale
	localizer        i18n.Localizer
	localeRequestID  uint64
	keys             keyMap
	help             help.Model
	spinner          spinner.Model
	spinnerRunning   bool
	viewport         viewport.Model

	diagnostics diagnosticOverlay
	terminal    terminalOverlay
	settings    settingsOverlay
	commands    commandOverlay
	device      deviceOverlay

	deviceRegion  region
	buttonRegions []actionRegion
}

type activityEntry struct {
	at     time.Time
	event  activityEvent
	level  activityLevel
	detail string
}

type region struct {
	x, y, width, height int
}

func (r region) contains(x, y int) bool {
	return x >= r.x && y >= r.y && x < r.x+r.width && y < r.y+r.height
}

type actionRegion struct {
	region
	action Action
}

type statusResultMsg struct {
	snapshot     model.Snapshot
	err          error
	reason       string
	requestID    uint64
	baseRevision uint64
}

type actionResultMsg struct {
	action       Action
	snapshot     model.Snapshot
	err          error
	elapsed      time.Duration
	baseRevision uint64
}

type pollTickMsg time.Time
type noticeExpiredMsg struct {
	revision uint64
	at       time.Time
}
type transportEventMsg struct {
	event model.Event
	ok    bool
}

func NewModel(
	ctx context.Context,
	transport Transport,
	options ModelOptions,
) (*Model, error) {
	if transport == nil {
		return nil, fmt.Errorf("TUI transport is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Poll == 0 {
		options.Poll = time.Second
	}
	if options.Poll < 250*time.Millisecond || options.Poll > time.Minute {
		return nil, fmt.Errorf("TUI poll interval must be between 250ms and 1m")
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	if options.Timeout < 100*time.Millisecond || options.Timeout > time.Minute {
		return nil, fmt.Errorf("TUI timeout must be between 100ms and 1m")
	}
	if options.Theme == "" {
		options.Theme = ThemeAuto
	}
	if _, err := ParseThemeMode(string(options.Theme)); err != nil {
		return nil, err
	}
	if options.Locale == "" {
		options.Locale = i18n.Auto
	}
	if _, err := i18n.ParsePreference(string(options.Locale)); err != nil {
		return nil, err
	}

	isDark := initialThemeIsDark(options.Theme)
	localizer := i18n.New(options.Locale)
	themeStyles := newStyles(isDark)
	helpModel := help.New()
	helpModel.Styles = helpStyles(isDark, themeStyles.palette)
	spin := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(themeStyles.title),
	)
	events := viewport.New(viewport.WithWidth(40), viewport.WithHeight(4))
	events.SoftWrap = true
	events.FillHeight = true
	events.MouseWheelEnabled = options.Mouse
	events.Style = styleOn(
		themeStyles.muted,
		themeStyles.palette.surface,
	)
	diagnosticView := viewport.New(
		viewport.WithWidth(40),
		viewport.WithHeight(8),
	)
	diagnosticView.SoftWrap = true
	diagnosticView.FillHeight = true
	diagnosticView.MouseWheelEnabled = options.Mouse
	diagnosticView.Style = lipgloss.NewStyle().
		Foreground(themeStyles.palette.text).
		Background(themeStyles.palette.surface)
	deviceView := viewport.New(
		viewport.WithWidth(40),
		viewport.WithHeight(4),
	)
	deviceView.SoftWrap = true
	deviceView.FillHeight = true
	deviceView.MouseWheelEnabled = options.Mouse
	deviceView.Style = lipgloss.NewStyle().
		Foreground(themeStyles.palette.text).
		Background(themeStyles.palette.surfaceAlt).
		Padding(0, 1)

	now := options.InitialNow
	if now.IsZero() {
		now = time.Now()
	}
	result := &Model{
		ctx:              ctx,
		transport:        transport,
		options:          options,
		width:            80,
		height:           24,
		themeMode:        options.Theme,
		isDark:           isDark,
		styles:           themeStyles,
		localePreference: options.Locale,
		localizer:        localizer,
		keys:             defaultKeyMap(localizer),
		help:             helpModel,
		spinner:          spin,
		viewport:         events,
		diagnostics: diagnosticOverlay{
			endpoint: defaultDiagnosticEndpoint,
			viewport: diagnosticView,
		},
		device: deviceOverlay{
			viewport: deviceView,
		},
		terminal: newTerminalOverlay(
			40,
			12,
			options.Mouse,
			lipgloss.NewStyle().
				Foreground(themeStyles.palette.text).
				Background(themeStyles.palette.surface),
		),
		activity: []activityEntry{
			{
				at:    now,
				event: activityAppStart,
				level: activityLevelInfo,
				detail: localizer.Choose(
					"Terminal UI ready",
					persianTUIText["Terminal UI ready"],
				),
			},
			{
				at:    now,
				event: activityTransportReady,
				level: activityLevelSuccess,
				detail: sanitizeTerminalLine(
					string(transport.Mode()) + " · " + transport.Label(),
				),
			},
		},
	}
	result.refreshActivityView()
	return result, nil
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{
		m.beginStatus("initial"),
		m.beginLocaleLoad(),
		m.pollAfter(),
		backgroundColorRequest(m.themeMode),
	}
	if events := m.transport.Events(); events != nil {
		commands = append(commands, waitForTransportEvent(m.ctx, events))
	}
	if m.options.OpenTerminal {
		commands = append(commands, m.openTerminal())
	} else {
		switch m.options.InitialScreen {
		case ScreenCommands:
			commands = append(commands, m.openCommands())
		case ScreenSettings:
			commands = append(commands, m.openSettings())
		case ScreenDevice:
			commands = append(commands, m.openDeviceSettings())
		case ScreenServing:
			commands = append(commands, m.openSettingsAt(settingsIntegration))
		case ScreenDiagnostics:
			commands = append(commands, m.openDiagnostics())
		case ScreenTerminal:
			commands = append(commands, m.openTerminal())
		}
	}
	return tea.Batch(commands...)
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.resizeDiagnostics()
		m.resizeTerminal()
		m.resizeSettings()
		m.resizeCommands()
		m.resizeDevice()
		var command tea.Cmd
		switch {
		case m.settingsActive():
			command = m.updateSettingsMessage(msg)
		case m.commandsActive():
			command = m.updateCommandsMessage(msg)
		case m.deviceActive():
			command = m.updateDeviceMessage(msg)
		}
		return m, command

	case tea.BackgroundColorMsg:
		if m.themeMode == ThemeAuto {
			m.applyTheme(msg.IsDark())
		}
		return m, nil

	case localeConfigMsg:
		return m, m.acceptLocaleConfig(msg)

	case tea.KeyPressMsg:
		if m.terminalActive() {
			return m, m.updateTerminalKey(msg)
		}
		// Ctrl+C is the application-level emergency exit everywhere except
		// the raw device terminal, where it intentionally means ETX.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.settingsActive() {
			return m, m.updateSettingsKey(msg)
		}
		if m.commandsActive() {
			return m, m.updateCommandsKey(msg)
		}
		if m.deviceActive() {
			return m, m.updateDeviceKey(msg)
		}
		if m.diagnosticsActive() {
			switch {
			case key.Matches(msg, m.keys.Close):
				m.closeDiagnostics()
				return m, nil
			case m.diagnostics.mode == diagnosticForm:
				return m, m.updateDiagnosticForm(msg)
			case key.Matches(msg, m.keys.Diagnostics):
				return m, m.openDiagnostics()
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			default:
				var command tea.Cmd
				m.diagnostics.viewport, command = m.diagnostics.viewport.Update(msg)
				return m, command
			}
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		case key.Matches(msg, m.keys.Theme):
			return m, m.cycleTheme()
		case key.Matches(msg, m.keys.Toggle):
			return m.startAction(ActionToggle)
		case key.Matches(msg, m.keys.On):
			return m.startAction(ActionOn)
		case key.Matches(msg, m.keys.Off):
			return m.startAction(ActionOff)
		case key.Matches(msg, m.keys.Refresh):
			return m.startAction(ActionRefresh)
		case key.Matches(msg, m.keys.Reconnect):
			return m.startAction(ActionReconnect)
		case key.Matches(msg, m.keys.Diagnostics):
			return m, m.openDiagnostics()
		case key.Matches(msg, m.keys.Terminal):
			return m, m.openTerminal()
		case key.Matches(msg, m.keys.Commands):
			return m, m.openCommands()
		case key.Matches(msg, m.keys.Settings):
			return m, m.openSettings()
		case key.Matches(msg, m.keys.Device):
			return m, m.openDeviceSettings()
		case key.Matches(msg, m.keys.Serving):
			return m, m.openSettingsAt(settingsIntegration)
		}

	case tea.MouseClickMsg:
		if m.settingsActive() {
			return m, m.updateSettingsMessage(msg)
		}
		if m.commandsActive() {
			return m, m.updateCommandsMessage(msg)
		}
		if m.deviceActive() {
			return m, m.updateDeviceMessage(msg)
		}
		if m.diagnosticsActive() || m.terminalActive() {
			return m, nil
		}
		if m.options.Mouse && msg.Button == tea.MouseLeft {
			for _, item := range m.buttonRegions {
				if item.contains(msg.X, msg.Y) {
					return m.startAction(item.action)
				}
			}
			if m.deviceRegion.contains(msg.X, msg.Y) {
				return m.startAction(ActionToggle)
			}
		}

	case statusResultMsg:
		if msg.requestID != m.statusRequestID {
			return m, nil
		}
		m.statusPending = false
		// A command or WebSocket snapshot accepted after this request began is
		// newer than a response that may have been captured before it.
		if msg.baseRevision != m.stateRevision {
			return m, m.beginStatus("superseded")
		}
		if msg.err != nil {
			m.controllerError = compactError(msg.err)
			m.recordError(activityTransportError, msg.err)
		} else {
			m.acceptSnapshot(msg.snapshot, msg.reason)
		}
		return m, nil

	case actionResultMsg:
		m.busy = false
		m.busyAction = ""
		if msg.err != nil {
			var confirmationError *ConfirmationError
			if errors.As(msg.err, &confirmationError) {
				m.controllerError = compactError(confirmationError.Cause)
				m.appendActivity(
					activityDeviceCommand,
					activityLevelWarn,
					m.actionTitle(msg.action)+" · "+
						m.text("accepted; awaiting state confirmation"),
				)
				m.setNotice(
					m.actionTitle(msg.action)+" "+
						m.text("sent · awaiting confirmed state"),
					false,
				)
				return m, m.beginStatus("confirmation")
			}
			if m.transport.Mode() != ModeStandalone {
				m.controllerError = compactError(msg.err)
			}
			m.appendActivity(
				activityDeviceCommand,
				activityLevelError,
				m.actionTitle(msg.action)+" "+m.text("failed")+" · "+compactError(msg.err),
			)
			m.setNotice(
				fmt.Sprintf(
					"%s %s · %s",
					m.actionTitle(msg.action),
					m.text("not confirmed"),
					compactError(msg.err),
				),
				true,
			)
		} else {
			if msg.baseRevision == m.stateRevision {
				m.acceptSnapshot(msg.snapshot, string(msg.action))
			}
			m.appendActivity(
				activityDeviceCommand,
				activityLevelSuccess,
				fmt.Sprintf(
					"%s · %s %s",
					m.actionTitle(msg.action),
					m.text("completed in"),
					friendlyDuration(msg.elapsed),
				),
			)
			m.setNotice(m.actionSuccess(msg.action, m.snapshot), false)
		}
		return m, nil

	case pollTickMsg:
		statusCommand := m.beginStatus("poll")
		if statusCommand == nil {
			return m, m.pollAfter()
		}
		return m, tea.Batch(statusCommand, m.pollAfter())

	case noticeExpiredMsg:
		if msg.revision != m.noticeRevision {
			return m, nil
		}
		if msg.at.Before(m.noticeUntil) {
			// A timer can fire fractionally early on some hosts. Let the
			// runtime wrapper schedule the small remainder exactly once.
			m.noticeScheduled = 0
			return m, nil
		}
		m.notice = ""
		m.noticeError = false
		m.noticeUntil = time.Time{}
		return m, nil

	case transportEventMsg:
		if !msg.ok {
			return m, nil
		}
		m.acceptEvent(msg.event)
		return m, waitForTransportEvent(m.ctx, m.transport.Events())

	case diagnosticResultMsg:
		m.acceptDiagnosticResult(msg)
		return m, nil

	case configLoadedMsg:
		return m, m.acceptConfigLoaded(msg)

	case configPatchedMsg:
		return m, m.acceptConfigPatched(msg)

	case registrationLoadedMsg:
		return m, m.acceptRegistrationLoaded(msg)

	case commandResultMsg:
		return m, m.acceptCommandResult(msg)

	case deviceConfigResultMsg:
		return m, m.acceptDeviceConfig(msg)

	case deviceActionResultMsg:
		return m, m.acceptDeviceAction(msg)

	case terminalConnectResultMsg:
		return m, m.acceptTerminalConnect(msg)

	case terminalDisconnectResultMsg:
		return m, m.acceptTerminalDisconnect(msg)

	case terminalReadResultMsg:
		return m, m.acceptTerminalRead(msg)

	case terminalWriteResultMsg:
		return m, m.acceptTerminalWrite(msg)

	case terminalReadRetryMsg:
		if !m.terminalActive() || msg.requestID != m.terminal.requestID {
			return m, nil
		}
		return m, m.beginTerminalRead()

	case spinner.TickMsg:
		if !m.spinnerRunning {
			return m, nil
		}
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(msg)
		m.pulse++
		if !m.spinnerActive() {
			m.spinnerRunning = false
			return m, nil
		}
		return m, command
	}

	if m.terminalActive() {
		var command tea.Cmd
		m.terminal.viewport, command = m.terminal.viewport.Update(message)
		return m, command
	}
	if m.settingsActive() {
		return m, m.updateSettingsMessage(message)
	}
	if m.commandsActive() {
		return m, m.updateCommandsMessage(message)
	}
	if m.deviceActive() {
		return m, m.updateDeviceMessage(message)
	}
	if m.diagnostics.mode == diagnosticForm {
		return m, m.updateDiagnosticForm(message)
	}
	if m.diagnostics.mode == diagnosticResult {
		var command tea.Cmd
		m.diagnostics.viewport, command = m.diagnostics.viewport.Update(message)
		return m, command
	}
	var command tea.Cmd
	m.viewport, command = m.viewport.Update(message)
	return m, command
}

func (m *Model) startAction(action Action) (tea.Model, tea.Cmd) {
	if m.busy {
		m.setNotice(m.text("One device command is already in flight"), true)
		return m, nil
	}
	m.busy = true
	m.busyAction = action
	// Invalidate any status request that began before this operation. In
	// particular, an old OFF response must not overwrite a successful ON.
	m.stateRevision++
	m.lastError = ""
	m.appendActivity(
		activityDeviceCommand,
		activityLevelInfo,
		m.actionTitle(action)+" · "+m.text("sending…"),
	)
	return m, m.executeActionCmd(action, m.stateRevision)
}

func (m *Model) spinnerActive() bool {
	return m.busy ||
		m.diagnostics.mode == diagnosticLoading ||
		m.terminal.connecting ||
		m.terminal.disconnect ||
		m.settings.mode == settingsLoading ||
		m.settings.mode == settingsSaving ||
		m.commands.mode == commandsRunning ||
		m.device.mode == deviceLoading ||
		m.device.mode == deviceRunning
}

func (m *Model) beginStatus(reason string) tea.Cmd {
	if m.statusPending || m.busy {
		return nil
	}
	m.statusPending = true
	m.statusRequestID++
	requestID := m.statusRequestID
	baseRevision := m.stateRevision
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := m.transport.Status(ctx)
		return statusResultMsg{
			snapshot:     snapshot,
			err:          err,
			reason:       reason,
			requestID:    requestID,
			baseRevision: baseRevision,
		}
	}
}

func (m *Model) executeActionCmd(action Action, baseRevision uint64) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := m.transport.Execute(ctx, action)
		return actionResultMsg{
			action:       action,
			snapshot:     snapshot,
			err:          err,
			elapsed:      time.Since(started),
			baseRevision: baseRevision,
		}
	}
}

func (m *Model) pollAfter() tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(m.options.Poll)
		defer timer.Stop()
		select {
		case at := <-timer.C:
			return pollTickMsg(at)
		case <-m.ctx.Done():
			return nil
		}
	}
}

func waitForTransportEvent(
	ctx context.Context,
	events <-chan model.Event,
) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return transportEventMsg{}
		case event, ok := <-events:
			return transportEventMsg{event: event, ok: ok}
		}
	}
}

func (m *Model) acceptEvent(event model.Event) {
	switch event.Kind {
	case model.EventMessage:
		detail := strings.TrimSpace(event.Message)
		if detail == "" {
			detail = strings.TrimSpace(event.Raw)
		}
		m.appendActivity(
			activityMessageReceived,
			activityLevelInfo,
			detail,
		)
		if detail != "" {
			m.setNotice(detail, false)
		}
	case model.EventBeep:
		m.appendActivity(
			activityAudioCue,
			activityLevelInfo,
			m.textf("%d pulse(s)", event.BeepCount),
		)
	case model.EventRaw:
		m.appendActivity(
			activityDeviceEvent,
			activityLevelDebug,
			m.textf(
				"Unclassified device frame received · %d bytes",
				len([]byte(event.Raw)),
			),
		)
	default:
		m.acceptSnapshot(event.Snapshot, "websocket")
	}
}

func (m *Model) acceptSnapshot(snapshot model.Snapshot, reason string) {
	m.stateRevision++
	m.controllerError = ""
	previous := m.snapshot
	hadState := m.haveState
	m.snapshot = snapshot
	m.haveState = true
	m.lastError = snapshot.LastError

	if !hadState || previous.State != snapshot.State {
		detail := m.deviceStateText()
		if source := strings.TrimSpace(snapshot.LastSource); source != "" {
			detail += " " + m.text("via") + " " + source
		} else if reason != "" {
			detail += " " + m.text("via") + " " + reason
		}
		m.appendActivity(
			activityOutputState,
			activityLevelSuccess,
			detail,
		)
	}
	if hadState && previous.WebSocketOnline != snapshot.WebSocketOnline {
		event := activityWebSocketDisconnected
		level := activityLevelWarn
		status := m.text("Disconnected")
		if snapshot.HTTPReachable {
			status += " · " + m.text("HTTP polling active")
		}
		if snapshot.WebSocketOnline {
			event = activityWebSocketConnected
			level = activityLevelSuccess
			status = m.text("Connected")
		}
		m.appendActivity(event, level, status)
	}
}

func (m *Model) recordError(event activityEvent, err error) {
	message := compactError(err)
	m.lastError = message
	// Polling an unavailable remote should not flood the activity viewport.
	if len(m.activity) == 0 ||
		m.activity[len(m.activity)-1].detail != message ||
		m.activity[len(m.activity)-1].level != activityLevelError ||
		m.activity[len(m.activity)-1].event != event {
		m.appendActivity(event, activityLevelError, message)
	}
}

func (m *Model) appendActivity(
	event activityEvent,
	level activityLevel,
	detail string,
) {
	event = activityEvent(truncateTerminalLine(string(event), 64))
	level = normalizeActivityLevel(level)
	detail = truncateTerminalLine(detail, 2048)
	if detail == "" {
		detail = "—"
	}
	m.activity = append(m.activity, activityEntry{
		at:     time.Now(),
		event:  event,
		level:  level,
		detail: detail,
	})
	if len(m.activity) > maxActivityEntries {
		copy(m.activity, m.activity[len(m.activity)-maxActivityEntries:])
		m.activity = m.activity[:maxActivityEntries]
	}
	m.refreshActivityView()
	m.viewport.GotoBottom()
}

func (m *Model) refreshActivityView() {
	var lines []string
	gap := backgroundRun(m.styles.palette.surface, "  ")
	contentWidth := max(
		0,
		m.viewport.Width()-m.viewport.Style.GetHorizontalFrameSize(),
	)
	for _, entry := range m.activity {
		timeText := styleOn(
			m.styles.logTime,
			m.styles.palette.surface,
		).Render(m.localizer.ShapeDigits(entry.at.Format("15:04:05")))
		levelColor := m.activityLevelColor(entry.level)
		level := m.styles.logLevel.Copy().
			Foreground(levelColor).
			Background(m.styles.palette.surface).
			Bold(true).
			Render(padRight(formatActivityLevel(entry.level), 7))
		event := m.styles.logEvent.Copy().
			Foreground(levelColor).
			Background(m.styles.palette.surface).
			Bold(true).
			Render(activitySymbol(entry.event) + " " + string(entry.event))
		detail := styleOn(
			m.styles.logDetail,
			m.styles.palette.surface,
		)
		if entry.level == activityLevelError {
			detail = detail.Foreground(levelColor)
		}
		line := fmt.Sprintf(
			"%s%s%s%s%s%s%s",
			timeText,
			gap,
			level,
			gap,
			event,
			gap,
			detail.Render(entry.detail),
		)
		if padding := contentWidth - lipgloss.Width(line); padding > 0 {
			// viewport.View pads with an unstyled Lip Gloss run. Explicitly
			// own the rest of each row so a terminal's default black/white
			// background cannot leak through after a short activity detail.
			line += backgroundRun(
				m.styles.palette.surface,
				strings.Repeat(" ", padding),
			)
		}
		lines = append(lines, line)
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
}

func (m *Model) spinnerViewOn(background color.Color) string {
	spin := m.spinner
	spin.Style = styleOn(m.styles.title, background)
	return spin.View()
}

func (m *Model) activityLevelColor(level activityLevel) color.Color {
	switch normalizeActivityLevel(level) {
	case activityLevelDebug:
		return m.styles.palette.faint
	case activityLevelSuccess:
		return m.styles.palette.good
	case activityLevelWarn:
		return m.styles.palette.warning
	case activityLevelError:
		return m.styles.palette.bad
	default:
		return m.styles.palette.accent
	}
}

func (m *Model) setNotice(message string, isError bool) {
	m.notice = truncateTerminalLine(message, 512)
	m.noticeError = isError
	m.noticeUntil = time.Now().Add(4 * time.Second)
	m.noticeRevision++
}

func (m *Model) applyTheme(isDark bool) {
	if m.isDark == isDark {
		return
	}
	m.isDark = isDark
	m.styles = newStyles(isDark)
	m.help.Styles = helpStyles(isDark, m.styles.palette)
	m.spinner.Style = m.styles.title
	m.viewport.Style = styleOn(
		m.styles.muted,
		m.styles.palette.surface,
	)
	m.refreshActivityView()
	m.resizeDiagnostics()
	m.resizeTerminal()
	m.resizeSettings()
	m.resizeCommands()
	m.resizeDevice()
}

func (m *Model) cycleTheme() tea.Cmd {
	var command tea.Cmd
	switch {
	case m.themeMode == ThemeAuto:
		m.themeMode = ThemeDark
		m.applyTheme(true)
	case m.themeMode == ThemeDark:
		m.themeMode = ThemeLight
		m.applyTheme(false)
	default:
		m.themeMode = ThemeAuto
		m.applyTheme(systemThemeFallbackDark())
		command = backgroundColorRequest(m.themeMode)
	}
	m.setNotice(m.text("Terminal theme")+" · "+m.text(string(m.themeMode)), false)
	return command
}

func (m *Model) actionTitle(action Action) string {
	return m.text(actionTitle(action))
}

func actionTitle(action Action) string {
	switch action {
	case ActionOn:
		return "Turn on"
	case ActionOff:
		return "Turn off"
	case ActionReconnect:
		return "Reconnect"
	case ActionRefresh:
		return "Refresh"
	default:
		return "Toggle"
	}
}

func (m *Model) actionSuccess(action Action, snapshot model.Snapshot) string {
	if action == ActionReconnect {
		return m.text("Reconnect requested")
	}
	state := m.text("Unknown")
	switch snapshot.State {
	case model.StateOn:
		state = m.text("On")
	case model.StateOff:
		state = m.text("Off")
	}
	if action == ActionRefresh {
		return m.text("State refreshed") + " · " + state
	}
	return m.text("Device is") + " " + state
}

func actionSuccess(action Action, snapshot model.Snapshot) string {
	if action == ActionReconnect {
		return "Reconnect requested"
	}
	if action == ActionRefresh {
		return "State refreshed · " + snapshot.State.String()
	}
	return "Device is " + snapshot.State.String()
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	return truncateTerminalLine(err.Error(), 180)
}

func friendlyDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return "<1ms"
	}
	if duration < time.Second {
		return duration.Round(time.Millisecond).String()
	}
	return duration.Round(100 * time.Millisecond).String()
}
