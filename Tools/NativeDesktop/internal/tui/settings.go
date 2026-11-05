package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/i18n"
)

const (
	maxSettingsPanelWidth  = 108
	maxSettingsPanelHeight = 38
	preservedTokenMarker   = "(preserved — enter a replacement to rotate)"
)

type settingsOverlayMode uint8

const (
	settingsClosed settingsOverlayMode = iota
	settingsLoading
	settingsMenu
	settingsEditing
	settingsSaving
)

type settingsSection string

const (
	settingsDevice        settingsSection = "device"
	settingsAppearance    settingsSection = "appearance"
	settingsWindow        settingsSection = "window"
	settingsNotifications settingsSection = "notifications"
	settingsHotkeys       settingsSection = "hotkeys"
	settingsAutomation    settingsSection = "automation"
	settingsIntegration   settingsSection = "integration"
	settingsDiagnostics   settingsSection = "diagnostics"
	settingsAdvanced      settingsSection = "advanced"
	settingsDone          settingsSection = "done"
)

type settingsOverlay struct {
	mode                    settingsOverlayMode
	section                 settingsSection
	requestSection          settingsSection
	form                    *huh.Form
	cfg                     config.Config
	original                config.Config
	draft                   settingsDraft
	registration            commands.RegistrationStatus
	registrationKnown       bool
	registrationStatusError string
	err                     string
	requestID               uint64
}

type settingsDraft struct {
	cfg config.Config

	requestTimeout    string
	reconnectMin      string
	reconnectMax      string
	httpPoll          string
	telnetPort        string
	settingsTab       string
	snapDistance      string
	windowX           string
	windowY           string
	windowWidth       string
	windowHeight      string
	automationTimeout string
	localAPIToken     string
	safeEndpoints     string
	expertEndpoints   string
	advancedJSON      string
	customSoundPath   string

	registrationAction    string
	registrationConfirmed bool
}

type configLoadedMsg struct {
	requestID uint64
	cfg       config.Config
	err       error
}

type configPatchedMsg struct {
	requestID          uint64
	section            settingsSection
	cfg                config.Config
	configApplied      bool
	registration       commands.RegistrationStatus
	registrationLoaded bool
	registrationAction string
	err                error
}

type registrationLoadedMsg struct {
	requestID    uint64
	registration commands.RegistrationStatus
	err          error
}

type settingsOverlayLayout struct {
	width       int
	height      int
	innerWidth  int
	innerHeight int
	bodyHeight  int
}

func calculateSettingsOverlayLayout(width, height int) settingsOverlayLayout {
	width = max(1, width)
	height = max(1, height)
	panelWidth := width
	switch {
	case width >= 72:
		panelWidth = min(maxSettingsPanelWidth, width-6)
	case width >= 28:
		panelWidth = width - 2
	}
	panelHeight := height
	switch {
	case height >= 24:
		panelHeight = min(maxSettingsPanelHeight, height-4)
	case height >= 10:
		panelHeight = height - 2
	}
	innerWidth := max(1, panelWidth-6)
	innerHeight := max(1, panelHeight-4)
	return settingsOverlayLayout{
		width:       panelWidth,
		height:      panelHeight,
		innerWidth:  innerWidth,
		innerHeight: innerHeight,
		bodyHeight:  max(1, innerHeight-3),
	}
}

func (m *Model) settingsActive() bool {
	return m.settings.mode != settingsClosed
}

func (m *Model) openSettings() tea.Cmd {
	return m.openSettingsAt("")
}

func (m *Model) openSettingsAt(section settingsSection) tea.Cmd {
	if _, ok := m.transport.(ConfigTransport); !ok {
		m.setNotice("This transport does not expose application configuration", true)
		return nil
	}
	m.closeCommands()
	m.settings.requestID++
	requestID := m.settings.requestID
	m.settings.mode = settingsLoading
	m.settings.requestSection = section
	m.settings.form = nil
	m.settings.err = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		cfg, err := m.transport.(ConfigTransport).GetConfig(ctx)
		return configLoadedMsg{requestID: requestID, cfg: cfg, err: err}
	}
}

func (m *Model) closeSettings() {
	m.settings.requestID++
	m.settings = settingsOverlay{requestID: m.settings.requestID}
}

func (m *Model) acceptConfigLoaded(message configLoadedMsg) tea.Cmd {
	if message.requestID != m.settings.requestID ||
		m.settings.mode != settingsLoading {
		return nil
	}
	if message.err != nil {
		m.settings.err = compactError(message.err)
		return nil
	}
	m.localeRequestID++
	m.applyLocalePreference(message.cfg.UI.Locale)
	m.settings.cfg = message.cfg
	m.settings.original = message.cfg
	if section := m.settings.requestSection; section != "" {
		m.settings.requestSection = ""
		return m.openSettingsEditor(section)
	}
	return m.openSettingsMenu()
}

func (m *Model) openSettingsMenu() tea.Cmd {
	if m.settings.cfg.UI.Locale != "" {
		m.applyLocalePreference(m.settings.cfg.UI.Locale)
	}
	m.settings.mode = settingsMenu
	m.settings.section = ""
	m.settings.err = ""
	selectField := huh.NewSelect[settingsSection]().
		Title(m.text("Choose a configuration workspace")).
		Description(m.text("Each Apply is validated, saved atomically, and synchronized live.")).
		Options(
			huh.NewOption(m.text("💡 Device connection & recovery"), settingsDevice),
			huh.NewOption(m.text("🎨 Appearance & rendering"), settingsAppearance),
			huh.NewOption(m.text("🪟 Window behavior & placement"), settingsWindow),
			huh.NewOption(m.text("🔔 Notifications & sound"), settingsNotifications),
			huh.NewOption(m.text("⌨  Global hotkeys"), settingsHotkeys),
			huh.NewOption(m.text("⚙  Automation scripts"), settingsAutomation),
			huh.NewOption(m.text("🌐 Serving & Windows integration"), settingsIntegration),
			huh.NewOption(m.text("🩺 Diagnostics policy (read only)"), settingsDiagnostics),
			huh.NewOption(m.text("🧰 Complete JSON editor"), settingsAdvanced),
			huh.NewOption(m.text("✓  Return to dashboard"), settingsDone),
		).
		Height(13).
		Value(&m.settings.section)
	m.settings.form = m.newSettingsForm(huh.NewGroup(selectField))
	return m.settings.form.Init()
}

func (m *Model) openSettingsEditor(section settingsSection) tea.Cmd {
	if section == settingsIntegration {
		return m.loadIntegrationRegistration()
	}
	return m.openSettingsEditorReady(section)
}

func (m *Model) loadIntegrationRegistration() tea.Cmd {
	transport, ok := m.transport.(CommandTransport)
	if !ok {
		m.settings.registrationKnown = false
		m.settings.registrationStatusError =
			"This transport does not expose Windows registration controls."
		return m.openSettingsEditorReady(settingsIntegration)
	}
	m.settings.requestID++
	requestID := m.settings.requestID
	m.settings.mode = settingsLoading
	m.settings.section = settingsIntegration
	m.settings.requestSection = settingsIntegration
	m.settings.form = nil
	m.settings.err = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		result, err := transport.ExecuteCommand(
			ctx,
			commands.MethodIntegrationRegistrationStatus,
			nil,
		)
		var status commands.RegistrationStatus
		if err == nil {
			status, err = decodeRegistrationStatus(result)
		}
		return registrationLoadedMsg{
			requestID:    requestID,
			registration: status,
			err:          err,
		}
	}
}

func (m *Model) acceptRegistrationLoaded(
	message registrationLoadedMsg,
) tea.Cmd {
	if message.requestID != m.settings.requestID ||
		m.settings.mode != settingsLoading ||
		m.settings.section != settingsIntegration {
		return nil
	}
	m.settings.requestSection = ""
	if message.err != nil {
		m.settings.registrationKnown = false
		m.settings.registrationStatusError = compactError(message.err)
	} else {
		m.settings.registration = message.registration
		m.settings.registrationKnown = true
		m.settings.registrationStatusError = ""
	}
	return m.openSettingsEditorReady(settingsIntegration)
}

func (m *Model) openSettingsEditorReady(section settingsSection) tea.Cmd {
	m.settings.mode = settingsEditing
	m.settings.section = section
	m.settings.err = ""
	m.settings.original = m.settings.cfg
	m.settings.draft = newSettingsDraft(m.settings.cfg)

	groups := m.settingsEditorGroups(section)
	if groups == nil {
		if section == settingsDone {
			m.closeSettings()
			return nil
		}
		return m.openSettingsMenu()
	}
	m.settings.form = m.newSettingsForm(groups...)
	return m.settings.form.Init()
}

func (m *Model) settingsEditorGroups(
	section settingsSection,
) []*huh.Group {
	switch section {
	case settingsDevice:
		return m.deviceSettingsGroups()
	case settingsAppearance:
		return m.appearanceSettingsGroups()
	case settingsWindow:
		return m.windowSettingsGroups()
	case settingsNotifications:
		return m.notificationSettingsGroups()
	case settingsHotkeys:
		return m.hotkeySettingsGroups()
	case settingsAutomation:
		return m.automationSettingsGroups()
	case settingsIntegration:
		return m.integrationSettingsGroups()
	case settingsDiagnostics:
		return m.diagnosticSettingsGroups()
	case settingsAdvanced:
		return m.advancedSettingsGroups()
	default:
		return nil
	}
}

func newSettingsDraft(cfg config.Config) settingsDraft {
	// Mask the token in the typed field before serialization. Replacing the
	// first matching JSON string could mask an unrelated script/path that
	// happens to equal the token and leave the real credential visible.
	masked := cfg
	if masked.Integration.LocalAPIToken != "" {
		masked.Integration.LocalAPIToken = preservedTokenMarker
	}
	data, _ := json.MarshalIndent(masked, "", "  ")
	advanced := string(data)
	return settingsDraft{
		cfg:               cfg,
		requestTimeout:    cfg.Device.RequestTimeout.String(),
		reconnectMin:      cfg.Device.ReconnectMin.String(),
		reconnectMax:      cfg.Device.ReconnectMax.String(),
		httpPoll:          cfg.Device.HTTPPoll.String(),
		telnetPort:        strconv.Itoa(cfg.Device.TelnetPort),
		settingsTab:       strconv.Itoa(cfg.UI.SettingsTab),
		snapDistance:      strconv.Itoa(cfg.UI.MagneticSnapDistance),
		windowX:           strconv.Itoa(cfg.UI.WindowBounds.X),
		windowY:           strconv.Itoa(cfg.UI.WindowBounds.Y),
		windowWidth:       strconv.Itoa(cfg.UI.WindowBounds.Width),
		windowHeight:      strconv.Itoa(cfg.UI.WindowBounds.Height),
		automationTimeout: cfg.Automation.Timeout.String(),
		localAPIToken:     "",
		safeEndpoints:     strings.Join(cfg.Diagnostics.SafeEndpoints, "\n"),
		expertEndpoints:   strings.Join(cfg.Diagnostics.ExpertEndpoints, "\n"),
		advancedJSON:      advanced,
	}
}

func (m *Model) newSettingsForm(groups ...*huh.Group) *huh.Form {
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	return huh.NewForm(groups...).
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithShowHelp(true).
		WithShowErrors(true).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
}

func settingsInput(
	title string,
	description string,
	value *string,
	validator func(string) error,
) *huh.Input {
	field := huh.NewInput().
		Title(title).
		Description(description).
		CharLimit(512).
		Value(value)
	if validator != nil {
		field.Validate(validator)
	}
	return field
}

func (m *Model) settingsBool(title, description string, value *bool) *huh.Confirm {
	return huh.NewConfirm().
		Title(title).
		Description(description).
		Affirmative(m.text("Enabled")).
		Negative(m.text("Disabled")).
		Inline(true).
		Value(value)
}

func (m *Model) settingsSelect(
	title string,
	description string,
	value *string,
	options ...string,
) *huh.Select[string] {
	items := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		items = append(items, huh.NewOption(m.text(option), option))
	}
	return huh.NewSelect[string]().
		Title(title).
		Description(description).
		Options(items...).
		Inline(true).
		Value(value)
}

func (m *Model) deviceSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	return []*huh.Group{
		huh.NewGroup(
			settingsInput(
				m.text("Device base URL"),
				m.text("HTTP(S) origin; `.local` is resolved and cached by the coordinator."),
				&d.cfg.Device.BaseURL,
				validateDeviceURL,
			),
			settingsInput(m.text("Request timeout"), m.text("100ms–5m"), &d.requestTimeout, durationRange(100*time.Millisecond, 5*time.Minute, false)),
			m.settingsBool(m.text("WebSocket events"), m.text("Listen on /ws and reconnect with backoff."), &d.cfg.Device.WebSocket),
			settingsInput(m.text("Reconnect minimum"), m.text("100ms–5m"), &d.reconnectMin, durationRange(100*time.Millisecond, 5*time.Minute, false)),
			settingsInput(m.text("Reconnect maximum"), m.text("At least the minimum; at most 15m."), &d.reconnectMax, durationRange(100*time.Millisecond, 15*time.Minute, false)),
			settingsInput(m.text("HTTP fallback poll"), m.text("0 disables polling; otherwise 1s–24h."), &d.httpPoll, durationRange(time.Second, 24*time.Hour, true)),
			settingsInput(m.text("Telnet port"), m.text("Device terminal port, 1–65535."), &d.telnetPort, integerRange(1, 65535)),
		).Title(m.text("Device connection")).Description(m.text("Ctrl+S applies this section immediately · Esc discards")),
	}
}

func (m *Model) appearanceSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	language := huh.NewSelect[string]().
		Key("ui.locale").
		Title(m.text("Language")).
		Description(m.text("Choose the language used by every PCControllerDesktop surface.")).
		Options(
			huh.NewOption(m.text("System default"), config.LocaleAuto),
			huh.NewOption(m.text("English"), config.LocaleEnglish),
			huh.NewOption(m.text("Persian"), config.LocalePersian),
		).
		Inline(true).
		Value(&d.cfg.UI.Locale)
	return []*huh.Group{
		huh.NewGroup(
			language,
			m.settingsSelect(m.text("View"), m.text("Compact device or larger dashboard."), &d.cfg.UI.ViewMode, config.ViewCompact, config.ViewDashboard),
			m.settingsSelect(m.text("Theme"), m.text("Follow Windows or force a palette."), &d.cfg.UI.Theme, "system", "light", "dark"),
			m.settingsSelect(
				m.text("Backdrop"),
				m.text("Windows composition material."),
				&d.cfg.UI.Backdrop,
				config.BackdropAuto,
				config.BackdropSolid,
				config.BackdropTransparent,
				config.BackdropBlur,
				config.BackdropAcrylic,
				config.BackdropMica,
				config.BackdropTabbed,
			),
			m.settingsSelect(
				m.text("Rendering backend"),
				m.text("Automatic prefers D3D11/DirectComposition. Compatibility retains every UI feature while using less private memory."),
				&d.cfg.UI.RenderBackend,
				config.RenderBackendAuto,
				config.RenderBackendDirectX,
				config.RenderBackendLegacy,
			),
			m.settingsBool(m.text("Rounded corners"), m.text("Use the platform corner preference."), &d.cfg.UI.RoundedCorners),
			m.settingsBool(m.text("Layered foreground"), m.text("Alpha-composited glass styling; independent of the presentation backend."), &d.cfg.UI.LayeredForeground),
			m.settingsSelect(m.text("Title bar"), m.text("None, macOS-style custom, or native."), &d.cfg.UI.TitleBarMode, config.TitleBarNone, config.TitleBarCustom, config.TitleBarNative),
			m.settingsSelect(m.text("Border"), m.text("Automatic native frame or no border."), &d.cfg.UI.BorderMode, config.BorderAuto, config.BorderNone),
			m.settingsBool(m.text("Animations"), m.text("Transitions, button feedback, and fades."), &d.cfg.UI.Animations),
		).Title(m.text("Appearance")).Description(m.text("Ctrl+S applies this section immediately · Esc discards")),
	}
}

func (m *Model) windowSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	return []*huh.Group{
		huh.NewGroup(
			m.settingsBool(m.text("Always on top"), m.text("Keep the device above ordinary windows."), &d.cfg.UI.AlwaysOnTop),
			m.settingsBool(m.text("Close to tray"), m.text("Close hides the window while the coordinator remains active."), &d.cfg.UI.CloseToTray),
			m.settingsBool(m.text("Prewarm tray menu"), m.text("Keep the acrylic menu ready to avoid first-open flash."), &d.cfg.UI.PrewarmTrayMenu),
			m.settingsBool(m.text("Start minimized"), m.text("Begin in the notification area."), &d.cfg.UI.StartMinimized),
			m.settingsBool(m.text("Window visible"), m.text("Restore the last visible/hidden state."), &d.cfg.UI.WindowVisible),
			m.settingsSelect(m.text("Saved show state"), m.text("State restored on the next GUI launch."), &d.cfg.UI.WindowState, config.WindowStateNormal, config.WindowStateMinimized, config.WindowStateMaximized),
			m.settingsBool(m.text("Show in taskbar"), m.text("Expose the main window in the Windows taskbar."), &d.cfg.UI.ShowTaskbar),
			m.settingsBool(m.text("Magnetic snap"), m.text("Snap near monitor work-area edges; Ctrl bypasses it."), &d.cfg.UI.MagneticSnap),
			settingsInput(m.text("Snap distance"), m.text("1–128 physical-independent pixels."), &d.snapDistance, integerRange(1, 128)),
			settingsInput(m.text("Last settings tab"), m.text("Native settings page index, 0–5."), &d.settingsTab, integerRange(0, config.SettingsTabCount-1)),
		).Title(m.text("Window behavior")),
		huh.NewGroup(
			settingsInput("X", m.text("-1 asks Windows to center."), &d.windowX, integerRange(-100000, 100000)),
			settingsInput("Y", m.text("-1 asks Windows to center."), &d.windowY, integerRange(-100000, 100000)),
			settingsInput(m.text("Width"), m.text("Validated against the selected view mode."), &d.windowWidth, integerRange(1, 4096)),
			settingsInput(m.text("Height"), m.text("Validated against the selected view mode."), &d.windowHeight, integerRange(1, 4096)),
		).Title(m.text("Saved bounds")).Description(m.text("Ctrl+S applies all pages in this section · Esc discards")),
	}
}

func (m *Model) notificationSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	soundAssetID := func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if len(value) != 64 {
			return errors.New("sound asset ID must be 64 lowercase hex characters")
		}
		for _, character := range value {
			if (character < '0' || character > '9') &&
				(character < 'a' || character > 'f') {
				return errors.New("sound asset ID must be lowercase SHA-256 hex")
			}
		}
		return nil
	}
	return []*huh.Group{
		huh.NewGroup(
			m.settingsBool(m.text("Notifications"), m.text("Master notification switch."), &d.cfg.Notifications.Enabled),
			m.settingsBool(m.text("State changes"), m.text("Notify when device state changes."), &d.cfg.Notifications.StateChanges),
			m.settingsBool(m.text("Initial sync"), m.text("Notify after the first confirmed state."), &d.cfg.Notifications.InitialSync),
			m.settingsBool(m.text("Custom messages"), m.text("Render `message:` device events."), &d.cfg.Notifications.CustomMessage),
			m.settingsBool(m.text("Audible cues"), m.text("Play cues independently of toast delivery."), &d.cfg.Notifications.Sound),
			m.settingsSelect(m.text("Sound source"), m.text("Use the shared bundled catalog or a content-addressed custom WAV."), &d.cfg.Notifications.SoundSource, config.SoundSourceBundled, config.SoundSourceCustom),
			settingsInput(m.text("Import custom sound"), m.text("WAV, MP3 or FLAC path"), &d.customSoundPath, cleanSingleLine),
			settingsInput(m.text("Custom sound asset"), m.text("SHA-256 ID imported by the native or Web settings surface."), &d.cfg.Notifications.SoundFile, soundAssetID),
			settingsInput(m.text("Custom sound name"), m.text("Reader-facing name shared by native and Web settings."), &d.cfg.Notifications.SoundName, nil),
			m.settingsSelect(m.text("Delivery"), m.text("Automatic modern toast with legacy balloon fallback."), &d.cfg.Notifications.Delivery, "auto", "toast", "balloon"),
		).Title(m.text("Notifications & sound")).Description(m.text("Ctrl+S applies this section immediately · Esc discards")),
	}
}

func (m *Model) hotkeySettingsGroups() []*huh.Group {
	d := &m.settings.draft
	chord := func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if strings.ContainsAny(value, "\r\n\t") {
			return errors.New("hotkey must be one chord")
		}
		return nil
	}
	return []*huh.Group{
		huh.NewGroup(
			settingsInput(m.text("Toggle"), m.text("Example: Ctrl+Alt+L; blank disables."), &d.cfg.Hotkeys.Toggle, chord),
			settingsInput(m.text("On"), m.text("Example: Ctrl+Alt+Shift+O; blank disables."), &d.cfg.Hotkeys.On, chord),
			settingsInput(m.text("Off"), m.text("Example: Ctrl+Alt+Shift+F; blank disables."), &d.cfg.Hotkeys.Off, chord),
			settingsInput(m.text("Show / hide"), m.text("Example: Ctrl+Alt+Shift+L; blank disables."), &d.cfg.Hotkeys.Show, chord),
		).Title(m.text("Global hotkeys")).Description(m.text("Native settings also offers live key recording · Ctrl+S applies")),
	}
}

func (m *Model) automationSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	return []*huh.Group{
		huh.NewGroup(
			m.settingsBool(m.text("Automation"), m.text("Run scripts after device and Windows lifecycle events."), &d.cfg.Automation.Enabled),
			settingsInput(m.text("On script"), m.text("Executable/script invoked after ON."), &d.cfg.Automation.OnScript, cleanSingleLine),
			settingsInput(m.text("Off script"), m.text("Executable/script invoked after OFF."), &d.cfg.Automation.OffScript, cleanSingleLine),
			settingsInput(m.text("Message script"), m.text("Executable/script invoked for custom messages."), &d.cfg.Automation.MessageScript, cleanSingleLine),
		).Title(m.text("Device & message events")).Description(m.text("Page 1 of 3 · Ctrl+S applies every automation page · Esc discards")),
		huh.NewGroup(
			settingsInput(m.text("Lock script"), m.text("Invoked when Windows locks the session."), &d.cfg.Automation.LockScript, cleanSingleLine),
			settingsInput(m.text("Unlock script"), m.text("Invoked when Windows unlocks the session."), &d.cfg.Automation.UnlockScript, cleanSingleLine),
			settingsInput(m.text("Suspend script"), m.text("Invoked when Windows is entering suspend."), &d.cfg.Automation.SuspendScript, cleanSingleLine),
			settingsInput(m.text("Resume script"), m.text("Invoked after Windows resumes."), &d.cfg.Automation.ResumeScript, cleanSingleLine),
			settingsInput(m.text("Network-change script"), m.text("Invoked after a host network change."), &d.cfg.Automation.NetworkChangeScript, cleanSingleLine),
		).Title(m.text("Windows lifecycle events")).Description(m.text("Page 2 of 3 · Blank disables an individual event · Ctrl+S applies all pages")),
		huh.NewGroup(
			settingsInput(m.text("Working directory"), m.text("Blank inherits the application directory."), &d.cfg.Automation.WorkingDirectory, cleanSingleLine),
			settingsInput(m.text("Timeout"), m.text("1s–10m per invocation."), &d.automationTimeout, durationRange(time.Second, 10*time.Minute, false)),
			m.settingsBool(m.text("Capture output"), m.text("Capture and colorize child stdout/stderr."), &d.cfg.Automation.CaptureOutput),
		).Title(m.text("Execution policy")).Description(m.text("Page 3 of 3 · Ctrl+S applies every automation page · Esc discards")),
	}
}

func (m *Model) integrationSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	token := settingsInput(
		m.text("Rotate local API token"),
		m.text("Leave blank to preserve the current secret."),
		&d.localAPIToken,
		validateOptionalToken,
	).EchoMode(huh.EchoModePassword)
	registrationDescription := m.registrationStatusDescription()
	registrationAction := huh.NewSelect[string]().
		Title(m.text("One-time registration action")).
		Description(
			m.text(
				"Portable mode stays unregistered unless you explicitly choose an action. "+
					"Nothing here is replayed on startup.",
			),
		).
		Options(
			huh.NewOption(m.text("No registration change"), ""),
			huh.NewOption(
				m.text("Register URI scheme + Jump List for this executable"),
				commands.MethodIntegrationShellRegister,
			),
			huh.NewOption(
				m.text("Unregister URI scheme + Jump List owned by this executable"),
				commands.MethodIntegrationShellUnregister,
			),
			huh.NewOption(
				m.text("Register COM automation for this executable"),
				commands.MethodIntegrationCOMRegister,
			),
			huh.NewOption(
				m.text("Unregister COM automation owned by this executable"),
				commands.MethodIntegrationCOMUnregister,
			),
		).
		Value(&d.registrationAction)
	return []*huh.Group{
		huh.NewGroup(
			m.settingsBool(m.text("Start with Windows"), m.text("Register the unified executable for the current user."), &d.cfg.Integration.StartWithWindows),
			m.settingsBool(m.text("Local API / Web UI"), m.text("Serve the coordinator and bundled dashboard on loopback."), &d.cfg.Integration.LocalAPIEnabled),
			settingsInput(m.text("Listen address"), m.text("Explicit loopback host:port."), &d.cfg.Integration.LocalAPIListen, validateLoopbackListen),
			token,
		).Title(m.text("Serving & integration")).Description(m.text("Listener changes migrate after Apply · Ctrl+S applies")),
		huh.NewGroup(
			huh.NewNote().
				Title(m.text("Current executable registration")).
				Description(registrationDescription).
				Next(true).
				NextLabel(m.text("Choose action")),
			registrationAction,
			huh.NewConfirm().
				Title(m.text("Confirm ownership-safe unregistration")).
				Description(
					m.text(
						"Required only when unregistering. PCControllerDesktop rechecks every "+
							"owned registry view before removing shared entries.",
					),
				).
				Affirmative(m.text("I understand")).
				Negative(m.text("Not confirmed")).
				Inline(true).
				Value(&d.registrationConfirmed),
		).Title(m.text("Windows shell & automation registration")).
			Description(m.text("Page 2 of 2 · actions target the exact executable shown above")),
	}
}

func (m *Model) registrationStatusDescription() string {
	if m.settings.registrationStatusError != "" {
		return m.text("Status unavailable:") + " " + m.settings.registrationStatusError
	}
	if !m.settings.registrationKnown {
		return m.text("Status has not been queried.")
	}
	status := m.settings.registration
	return fmt.Sprintf(
		m.text("Executable")+"\n%s\n\n"+
			m.text("Shell URI + Jump List")+"\n%s\n\n"+
			m.text("COM automation")+"\n%s",
		status.Executable,
		m.registrationStateLabel(status.ShellRegistered),
		m.registrationStateLabel(status.COMRegistered),
	)
}

func (m *Model) registrationStateLabel(registered bool) string {
	if registered {
		return m.text("✓ Registered for this exact executable")
	}
	return m.text("○ Not registered for this exact executable")
}

func (m *Model) diagnosticSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	description := m.text("Safe automatic endpoints") + "\n" +
		d.safeEndpoints +
		"\n\n" + m.text("Expert endpoint policy") + "\n" +
		d.expertEndpoints +
		"\n\n" + m.text(
		"Sensitive /config projections are sanitized before retention. "+
			"Non-device ASA routes remain excluded by compiled policy.",
	)
	return []*huh.Group{
		huh.NewGroup(
			huh.NewNote().
				Title(m.text("Compiled diagnostics policy")).
				Description(description).
				Height(18).
				Next(true).
				NextLabel(m.text("Back to categories")),
		).Title(m.text("Diagnostics policy")).Description(m.text("This screen is intentionally read only")),
	}
}

func (m *Model) advancedSettingsGroups() []*huh.Group {
	d := &m.settings.draft
	return []*huh.Group{
		huh.NewGroup(
			huh.NewText().
				Title(m.text("Complete PCControllerDesktop configuration")).
				Description(m.text("JSON object · token is masked · Ctrl+S validates and atomically applies.")).
				Value(&d.advancedJSON).
				Lines(22).
				Validate(validateConfigJSON),
		).Title(m.text("Advanced JSON editor")),
	}
}

func (m *Model) updateSettingsKey(message tea.KeyPressMsg) tea.Cmd {
	switch m.settings.mode {
	case settingsLoading:
		if message.String() == "esc" {
			m.closeSettings()
			return nil
		}
		if m.settings.err != "" && (message.String() == "enter" || message.String() == "r") {
			return m.openSettingsAt(m.settings.requestSection)
		}
		return nil
	case settingsMenu:
		if message.String() == "esc" &&
			!formIsFiltering(m.settings.form) {
			m.closeSettings()
			return nil
		}
	case settingsEditing:
		if message.String() == "esc" {
			return m.openSettingsMenu()
		}
		if message.String() == "ctrl+s" && m.settings.section != settingsDiagnostics {
			return m.beginSettingsSave()
		}
	case settingsSaving:
		return nil
	}
	if m.settings.form == nil {
		return nil
	}
	return m.updateSettingsForm(message)
}

func (m *Model) updateSettingsForm(message tea.Msg) tea.Cmd {
	if m.settings.form == nil {
		return nil
	}
	updated, command := m.settings.form.Update(message)
	if form, ok := updated.(*huh.Form); ok {
		m.settings.form = form
	}
	if m.settings.mode == settingsEditing &&
		m.settings.section == settingsAppearance {
		if preference, err := i18n.ParsePreference(
			m.settings.draft.cfg.UI.Locale,
		); err == nil && preference != m.localePreference {
			m.applyLocalePreference(string(preference))
			groups := m.settingsEditorGroups(settingsAppearance)
			m.settings.form = m.newSettingsForm(groups...)
			return tea.Batch(command, m.settings.form.Init())
		}
	}
	switch m.settings.form.State {
	case huh.StateCompleted:
		if m.settings.mode == settingsMenu {
			return tea.Batch(command, m.openSettingsEditor(m.settings.section))
		}
		if m.settings.section == settingsDiagnostics {
			return tea.Batch(command, m.openSettingsMenu())
		}
		return tea.Batch(command, m.beginSettingsSave())
	case huh.StateAborted:
		if m.settings.mode == settingsMenu {
			m.closeSettings()
			return nil
		}
		return m.openSettingsMenu()
	default:
		return command
	}
}

func (m *Model) updateSettingsMessage(message tea.Msg) tea.Cmd {
	if m.settings.form == nil ||
		(m.settings.mode != settingsMenu && m.settings.mode != settingsEditing) {
		return nil
	}
	return m.updateSettingsForm(message)
}

func (m *Model) beginSettingsSave() tea.Cmd {
	transport, ok := m.transport.(ConfigTransport)
	if !ok {
		m.settings.err = "This transport does not expose application configuration."
		return nil
	}
	patch, err := m.settingsPatch()
	if err != nil {
		m.settings.err = compactError(err)
		return nil
	}
	registrationAction, err := m.selectedRegistrationAction()
	if err != nil {
		m.settings.err = compactError(err)
		return nil
	}
	customSoundPath := strings.TrimSpace(m.settings.draft.customSoundPath)
	if len(patch) == 0 && registrationAction == "" && customSoundPath == "" {
		m.setNotice("No configuration changes to apply", false)
		return m.openSettingsEditor(m.settings.section)
	}
	commandTransport, hasCommandTransport := m.transport.(CommandTransport)
	if (registrationAction != "" || customSoundPath != "") &&
		!hasCommandTransport {
		m.settings.err = "This transport does not expose the requested application command."
		return nil
	}
	m.settings.requestID++
	requestID := m.settings.requestID
	section := m.settings.section
	m.settings.mode = settingsSaving
	m.settings.form = nil
	m.settings.err = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		cfg := m.settings.cfg
		configApplied := false
		var applyErr error
		if customSoundPath != "" {
			var imported commands.AudioImportResult
			raw, importErr := commandTransport.ExecuteCommand(
				ctx,
				"audio.import",
				map[string]string{"path": customSoundPath},
			)
			if importErr == nil {
				importErr = json.Unmarshal(raw, &imported)
			}
			if importErr == nil &&
				(strings.TrimSpace(imported.ID) == "" ||
					strings.TrimSpace(imported.Name) == "") {
				importErr = errors.New("audio importer returned incomplete metadata")
			}
			if importErr != nil {
				applyErr = fmt.Errorf("import custom WAV: %w", importErr)
			} else {
				if patch == nil {
					patch = make(map[string]any)
				}
				notifications, ok := patch["notifications"].(map[string]any)
				if !ok {
					notifications = make(map[string]any)
					patch["notifications"] = notifications
				}
				notifications["sound"] = true
				notifications["sound_source"] = config.SoundSourceCustom
				notifications["sound_file"] = imported.ID
				notifications["sound_name"] = imported.Name
			}
		}
		if applyErr == nil && len(patch) != 0 {
			cfg, applyErr = transport.PatchConfig(ctx, patch)
			configApplied = applyErr == nil
		}
		var (
			registration       commands.RegistrationStatus
			registrationLoaded bool
		)
		if applyErr == nil && registrationAction != "" {
			registration, registrationLoaded, applyErr =
				executeRegistrationAction(
					ctx,
					commandTransport,
					registrationAction,
				)
		}
		return configPatchedMsg{
			requestID:          requestID,
			section:            section,
			cfg:                cfg,
			configApplied:      configApplied,
			registration:       registration,
			registrationLoaded: registrationLoaded,
			registrationAction: registrationAction,
			err:                applyErr,
		}
	}
}

func (m *Model) acceptConfigPatched(message configPatchedMsg) tea.Cmd {
	if message.requestID != m.settings.requestID ||
		m.settings.mode != settingsSaving {
		return nil
	}
	if message.configApplied {
		m.localeRequestID++
		m.applyLocalePreference(message.cfg.UI.Locale)
		m.settings.cfg = message.cfg
		m.settings.original = message.cfg
		m.settings.draft = newSettingsDraft(message.cfg)
	}
	if message.registrationLoaded {
		m.settings.registration = message.registration
		m.settings.registrationKnown = true
		m.settings.registrationStatusError = ""
	} else if message.registrationAction != "" && message.err != nil {
		m.settings.registrationKnown = false
		m.settings.registrationStatusError =
			"Unable to confirm the current registration state: " +
				compactError(message.err)
	}
	if message.err != nil {
		m.settings.mode = settingsEditing
		m.settings.err = compactError(message.err)
		m.settings.draft.registrationAction = ""
		m.settings.draft.registrationConfirmed = false
		return m.openSettingsEditorWithError(message.section, m.settings.err)
	}
	m.settings.cfg = message.cfg
	m.settings.original = message.cfg
	notice := m.text("Configuration applied live") + " · " +
		m.settingsSectionTitle(message.section)
	if message.registrationAction != "" {
		notice = m.text("Windows registration updated for the current executable")
	}
	m.setNotice(notice, false)
	m.appendActivity(
		activityEvent("Configuration"),
		activityLevelSuccess,
		m.text("Applied")+" "+m.settingsSectionTitle(message.section),
	)
	if message.registrationAction != "" {
		return m.openSettingsEditorReady(message.section)
	}
	return m.openSettingsEditor(message.section)
}

func (m *Model) selectedRegistrationAction() (string, error) {
	if m.settings.section != settingsIntegration {
		return "", nil
	}
	method := strings.TrimSpace(m.settings.draft.registrationAction)
	switch method {
	case "":
		return "", nil
	case commands.MethodIntegrationShellRegister,
		commands.MethodIntegrationCOMRegister:
		return method, nil
	case commands.MethodIntegrationShellUnregister,
		commands.MethodIntegrationCOMUnregister:
		if !m.settings.draft.registrationConfirmed {
			return "", errors.New(
				"confirm ownership-safe unregistration before applying",
			)
		}
		return method, nil
	default:
		return "", errors.New("unsupported Windows registration action")
	}
}

func executeRegistrationAction(
	ctx context.Context,
	transport CommandTransport,
	method string,
) (commands.RegistrationStatus, bool, error) {
	_, actionErr := transport.ExecuteCommand(ctx, method, nil)
	rawStatus, statusErr := transport.ExecuteCommand(
		ctx,
		commands.MethodIntegrationRegistrationStatus,
		nil,
	)
	var status commands.RegistrationStatus
	if statusErr == nil {
		status, statusErr = decodeRegistrationStatus(rawStatus)
	}
	return status, statusErr == nil, errors.Join(actionErr, statusErr)
}

func decodeRegistrationStatus(
	result json.RawMessage,
) (commands.RegistrationStatus, error) {
	var status commands.RegistrationStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return status, fmt.Errorf("decode Windows registration status: %w", err)
	}
	if strings.TrimSpace(status.Executable) == "" {
		return status, errors.New(
			"decode Windows registration status: executable is empty",
		)
	}
	return status, nil
}

func (m *Model) openSettingsEditorWithError(
	section settingsSection,
	message string,
) tea.Cmd {
	m.settings.mode = settingsEditing
	m.settings.section = section
	m.settings.err = message
	groups := m.settingsEditorGroups(section)
	if groups == nil {
		return m.openSettingsMenu()
	}
	m.settings.form = m.newSettingsForm(groups...)
	return m.settings.form.Init()
}

func (m *Model) settingsPatch() (map[string]any, error) {
	if m.settings.section == settingsAdvanced {
		return m.advancedSettingsPatch()
	}
	d := &m.settings.draft
	candidate := d.cfg
	var err error
	switch m.settings.section {
	case settingsDevice:
		if candidate.Device.RequestTimeout, err = parseConfigDuration(d.requestTimeout); err != nil {
			return nil, fmt.Errorf("request timeout: %w", err)
		}
		if candidate.Device.ReconnectMin, err = parseConfigDuration(d.reconnectMin); err != nil {
			return nil, fmt.Errorf("reconnect minimum: %w", err)
		}
		if candidate.Device.ReconnectMax, err = parseConfigDuration(d.reconnectMax); err != nil {
			return nil, fmt.Errorf("reconnect maximum: %w", err)
		}
		if candidate.Device.HTTPPoll, err = parseConfigDuration(d.httpPoll); err != nil {
			return nil, fmt.Errorf("HTTP poll: %w", err)
		}
		if candidate.Device.TelnetPort, err = strconv.Atoi(strings.TrimSpace(d.telnetPort)); err != nil {
			return nil, errors.New("telnet port must be an integer")
		}
	case settingsWindow:
		if candidate.UI.SettingsTab, err = strconv.Atoi(strings.TrimSpace(d.settingsTab)); err != nil {
			return nil, errors.New("settings tab must be an integer")
		}
		if candidate.UI.MagneticSnapDistance, err = strconv.Atoi(strings.TrimSpace(d.snapDistance)); err != nil {
			return nil, errors.New("snap distance must be an integer")
		}
		values := []*int{
			&candidate.UI.WindowBounds.X,
			&candidate.UI.WindowBounds.Y,
			&candidate.UI.WindowBounds.Width,
			&candidate.UI.WindowBounds.Height,
		}
		raw := []string{d.windowX, d.windowY, d.windowWidth, d.windowHeight}
		for index := range values {
			*values[index], err = strconv.Atoi(strings.TrimSpace(raw[index]))
			if err != nil {
				return nil, errors.New("window bounds must be integers")
			}
		}
	case settingsAutomation:
		if candidate.Automation.Timeout, err = parseConfigDuration(d.automationTimeout); err != nil {
			return nil, fmt.Errorf("automation timeout: %w", err)
		}
	case settingsIntegration:
		if token := strings.TrimSpace(d.localAPIToken); token != "" {
			candidate.Integration.LocalAPIToken = token
		}
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	return diffSettingsSection(m.settings.section, m.settings.original, candidate), nil
}

func (m *Model) advancedSettingsPatch() (map[string]any, error) {
	var candidate config.Config
	decoder := json.NewDecoder(strings.NewReader(m.settings.draft.advancedJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return nil, fmt.Errorf("invalid configuration JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("configuration JSON must contain exactly one object")
	}
	if candidate.Integration.LocalAPIToken == preservedTokenMarker {
		candidate.Integration.LocalAPIToken = m.settings.original.Integration.LocalAPIToken
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	var patch map[string]any
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, err
	}
	if integration, ok := patch["integration"].(map[string]any); ok &&
		candidate.Integration.LocalAPIToken == m.settings.original.Integration.LocalAPIToken {
		delete(integration, "local_api_token")
	}
	return patch, nil
}

func diffSettingsSection(
	section settingsSection,
	before config.Config,
	after config.Config,
) map[string]any {
	var key string
	var oldValue, newValue any
	switch section {
	case settingsDevice:
		key, oldValue, newValue = "device", before.Device, after.Device
	case settingsAppearance, settingsWindow:
		key, oldValue, newValue = "ui", before.UI, after.UI
	case settingsNotifications:
		key, oldValue, newValue = "notifications", before.Notifications, after.Notifications
	case settingsHotkeys:
		key, oldValue, newValue = "hotkeys", before.Hotkeys, after.Hotkeys
	case settingsAutomation:
		key, oldValue, newValue = "automation", before.Automation, after.Automation
	case settingsIntegration:
		key, oldValue, newValue = "integration", before.Integration, after.Integration
	default:
		return nil
	}
	oldMap := structMap(oldValue)
	newMap := structMap(newValue)
	diff := map[string]any{}
	for field, value := range newMap {
		old, exists := oldMap[field]
		if !exists || !jsonValuesEqual(old, value) {
			diff[field] = value
		}
	}
	if len(diff) == 0 {
		return nil
	}
	return map[string]any{key: diff}
}

func structMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

func jsonValuesEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func parseConfigDuration(value string) (config.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return config.Duration(duration), nil
}

func durationRange(
	minimum time.Duration,
	maximum time.Duration,
	allowZero bool,
) func(string) error {
	return func(value string) error {
		duration, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil {
			return errors.New("enter a duration such as 750ms, 5s, or 1m")
		}
		if allowZero && duration == 0 {
			return nil
		}
		if duration < minimum || duration > maximum {
			return fmt.Errorf("must be between %s and %s", minimum, maximum)
		}
		return nil
	}
}

func integerRange(minimum, maximum int) func(string) error {
	return func(value string) error {
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return errors.New("enter a whole number")
		}
		if number < minimum || number > maximum {
			return fmt.Errorf("must be between %d and %d", minimum, maximum)
		}
		return nil
	}
}

func validateDeviceURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return errors.New("enter an HTTP(S) origin without credentials, query, or fragment")
	}
	return nil
}

func cleanSingleLine(value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("must be a single clean line")
	}
	return nil
}

func validateOptionalToken(value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t ") {
		return errors.New("token must not contain whitespace")
	}
	if len(value) < 24 {
		return errors.New("use at least 24 characters")
	}
	return nil
}

func validateLoopbackListen(value string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return errors.New("enter loopback host:port, for example 127.0.0.1:47831")
	}
	if host == "" || port == "" {
		return errors.New("host and port are required")
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("listen host must be loopback")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func validateConfigJSON(value string) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var candidate config.Config
	if err := decoder.Decode(&candidate); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("configuration must contain exactly one JSON object")
	}
	if candidate.Integration.LocalAPIToken == preservedTokenMarker {
		candidate.Integration.LocalAPIToken = strings.Repeat("x", 32)
	}
	return candidate.Validate()
}

func (m *Model) settingsSectionTitle(section settingsSection) string {
	switch section {
	case settingsDevice:
		return m.text("Device connection")
	case settingsAppearance:
		return m.text("Appearance")
	case settingsWindow:
		return m.text("Window behavior")
	case settingsNotifications:
		return m.text("Notifications")
	case settingsHotkeys:
		return m.text("Global hotkeys")
	case settingsAutomation:
		return m.text("Automation")
	case settingsIntegration:
		return m.text("Serving & integration")
	case settingsDiagnostics:
		return m.text("Diagnostics policy")
	case settingsAdvanced:
		return m.text("Complete JSON")
	default:
		return m.text("Settings")
	}
}

// settingsSectionTitle retains the deterministic English helper used by
// interaction tests and integrations that identify a settings workspace.
func settingsSectionTitle(section settingsSection) string {
	model := Model{localizer: i18n.New(i18n.English)}
	return model.settingsSectionTitle(section)
}

func (m *Model) resizeSettings() {
	if !m.settingsActive() || m.settings.form == nil {
		return
	}
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	m.settings.form.
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
}

func (m *Model) renderSettingsOverlay() tea.View {
	layout := calculateSettingsOverlayLayout(m.width, m.height)
	title := m.text("APPLICATION SETTINGS")
	subtitle := m.text("TUI-native workspace") + " · " + m.transport.Label()
	footer := m.text("esc close")
	var body string
	switch m.settings.mode {
	case settingsLoading:
		title = m.text("LOADING APPLICATION SETTINGS")
		if m.settings.err != "" {
			body = m.styles.errorNotice.
				MaxWidth(layout.innerWidth).
				Render(m.settings.err)
			footer = m.text("enter/r retry · esc close")
		} else {
			body = lipgloss.Place(
				layout.innerWidth,
				layout.bodyHeight,
				lipgloss.Center,
				lipgloss.Center,
				m.spinnerViewOn(m.styles.palette.surface)+
					styleOn(m.styles.title, m.styles.palette.surface).
						Render("  "+m.text("Reading authoritative configuration…")),
			)
		}
	case settingsSaving:
		title = m.text("APPLYING") + " · " +
			strings.ToUpper(m.settingsSectionTitle(m.settings.section))
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.spinnerViewOn(m.styles.palette.surface)+
				styleOn(m.styles.title, m.styles.palette.surface).
					Render("  "+m.text("Validating, saving, and synchronizing…")),
		)
		footer = m.text("Please wait")
	case settingsMenu:
		footer = m.text("↑/↓ choose · / filter · enter open · esc close")
		if m.settings.form != nil {
			body = m.settings.form.View()
		}
	case settingsEditing:
		title = strings.ToUpper(m.settingsSectionTitle(m.settings.section))
		footer = m.text("tab/enter next · shift+tab back · ctrl+s apply · esc categories")
		if m.settings.section == settingsDiagnostics {
			footer = m.text("scroll/read · esc categories")
		}
		if m.settings.form != nil {
			body = m.settings.form.View()
		}
	}
	if m.settings.err != "" && m.settings.mode == settingsEditing {
		body = m.styles.errorNotice.
			MaxWidth(layout.innerWidth).
			Render(m.settings.err) + "\n" + body
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
