package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pccontroller.local/desktop/internal/model"
)

const (
	maximumViewWidth = 116
	wideLayoutWidth  = 80
	mainPanelHeight  = 16
)

func (m *Model) View() tea.View {
	m.buttonRegions = m.buttonRegions[:0]
	m.deviceRegion = region{}
	if m.terminalActive() {
		return m.renderTerminalOverlay()
	}
	if m.width < 24 || m.height < 8 {
		return m.renderMicroView(m.width, m.height)
	}
	if m.settingsActive() {
		return m.renderSettingsOverlay()
	}
	if m.commandsActive() {
		return m.renderCommandsOverlay()
	}
	if m.deviceActive() {
		return m.renderDeviceOverlay()
	}
	if m.diagnosticsActive() {
		return m.renderDiagnosticsOverlay()
	}
	outerWidth := min(maximumViewWidth, max(1, m.width))
	offsetX := max(0, (m.width-outerWidth)/2)
	if m.height < 18 || outerWidth < 34 || (m.help.ShowAll && m.height < 23) {
		return m.renderUltraCompactView(outerWidth, offsetX)
	}

	header := m.renderHeader(outerWidth)
	headerHeight := lipgloss.Height(header)
	helpText := m.help.View(m.keys)
	helpHeight := max(1, lipgloss.Height(helpText))
	noticeHeight := 0
	if m.notice != "" {
		noticeHeight = 1
	}

	var sections []string
	sections = append(sections, header)
	mainY := headerHeight
	mainBudget := m.height - headerHeight - helpHeight - 2
	if noticeHeight != 0 {
		mainBudget -= 2
	}
	if outerWidth >= wideLayoutWidth && mainBudget >= 15 {
		panelHeight := min(mainPanelHeight, mainBudget)
		leftWidth := max(31, outerWidth*2/5)
		rightWidth := outerWidth - leftWidth - 1
		device := m.renderDevicePanel(leftWidth, panelHeight)
		controlX := offsetX + leftWidth + 1
		deviceX := offsetX
		if m.rtl() {
			controlX = offsetX
			deviceX = offsetX + rightWidth + 1
		}
		controls, regions := m.renderControlPanel(rightWidth, panelHeight, controlX, mainY)
		m.buttonRegions = append(m.buttonRegions, regions...)
		m.deviceRegion = region{
			x: deviceX, y: mainY,
			width: leftWidth, height: panelHeight,
		}
		joined := lipgloss.JoinHorizontal(lipgloss.Top, device, " ", controls)
		if m.rtl() {
			joined = lipgloss.JoinHorizontal(lipgloss.Top, controls, " ", device)
		}
		sections = append(sections, joined)
	} else {
		available := max(
			10,
			m.height-headerHeight-helpHeight-noticeHeight-3,
		)
		compact, regions, deviceRegion := m.renderCompactPanel(
			outerWidth,
			available,
			offsetX,
			mainY,
		)
		m.buttonRegions = append(m.buttonRegions, regions...)
		m.deviceRegion = deviceRegion
		sections = append(sections, compact)
	}

	usedHeight := 0
	for _, section := range sections {
		usedHeight += lipgloss.Height(section)
	}
	usedHeight += len(sections) - 1
	remaining := m.height - usedHeight - helpHeight - noticeHeight - 2
	if remaining >= 6 {
		activity := m.renderActivityPanel(outerWidth, remaining)
		sections = append(sections, activity)
	}
	if m.notice != "" {
		noticeStyle := m.styles.notice
		if m.noticeError {
			noticeStyle = m.styles.errorNotice
		}
		sections = append(
			sections,
			noticeStyle.Width(outerWidth).Render(fitText(m.notice, outerWidth-2)),
		)
	}

	m.help.SetWidth(outerWidth)
	sections = append(sections, lipgloss.PlaceHorizontal(
		outerWidth,
		lipgloss.Center,
		m.help.View(m.keys),
	))
	content := strings.Join(sections, "\n")
	if offsetX > 0 {
		content = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
	}
	content = m.styles.app.Render(content)

	return m.configuredView(content)
}

func (m *Model) configuredView(content string) tea.View {
	view := tea.NewView(content)
	view.AltScreen = m.options.AltScreen
	if m.options.Mouse {
		view.MouseMode = tea.MouseModeCellMotion
	}
	view.WindowTitle = m.text("PCController") + " · " + m.text("Terminal")
	view.ForegroundColor = m.styles.palette.text
	view.BackgroundColor = m.styles.palette.background
	if m.settingsActive() ||
		m.commandsActive() ||
		m.deviceActive() ||
		m.diagnosticsActive() ||
		m.terminalActive() {
		// Huh and viewport components intentionally leave some padding cells
		// unstyled. Their containing overlay is a surface, so make that the
		// terminal's compositing default while an overlay is active. Each
		// overlay explicitly paints its outer margin with the canvas color.
		view.BackgroundColor = m.styles.palette.surface
	}
	if m.busy ||
		(m.settings.mode == settingsLoading && m.settings.err == "") ||
		m.settings.mode == settingsSaving ||
		m.commands.mode == commandsRunning ||
		m.device.mode == deviceLoading ||
		m.device.mode == deviceRunning {
		view.ProgressBar = tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	} else if m.lastError != "" {
		view.ProgressBar = tea.NewProgressBar(tea.ProgressBarError, 0)
	} else {
		view.ProgressBar = tea.NewProgressBar(tea.ProgressBarNone, 0)
	}
	return view
}

func (m *Model) renderMicroView(width, height int) tea.View {
	width = max(1, width)
	height = max(1, height)

	if m.diagnosticsActive() {
		lines := []string{microIdentity(m.text("DIAGNOSTICS"), width)}
		if height >= 2 {
			lines = append(lines, fitText(m.diagnostics.endpoint, width))
		}
		if height >= 3 {
			lines = append(lines, fitText(m.text("esc close"), width))
		}
		for len(lines) > height {
			lines = lines[:len(lines)-1]
		}
		return m.configuredView(strings.Join(lines, "\n"))
	}
	if m.settingsActive() || m.commandsActive() || m.deviceActive() {
		identity := m.text("SETTINGS")
		detail := m.text("esc close")
		animated := false
		switch {
		case m.commandsActive():
			identity = m.text("COMMANDS")
			switch m.commands.mode {
			case commandsRunning:
				animated = true
				detail = m.text("running") + " " + string(m.commands.selected)
			case commandsResult:
				detail = m.text("result · enter commands")
			case commandsConfirm:
				detail = m.text("confirm quit · esc cancel")
			default:
				detail = m.text("selected") + " " + string(m.commands.selected)
			}
		case m.deviceActive():
			identity = m.text("DEVICE")
			switch m.device.mode {
			case deviceLoading:
				animated = true
				detail = m.text("loading parameters")
			case deviceRunning:
				animated = true
				detail = m.text("running") + " " + string(m.device.action)
			default:
				detail = m.text("selected") + " " + string(m.device.action)
			}
		default:
			switch m.settings.mode {
			case settingsLoading:
				animated = m.settings.err == ""
				if m.settings.err != "" {
					detail = m.text("load failed · enter retry")
				} else {
					detail = m.text("loading configuration")
				}
			case settingsSaving:
				animated = true
				detail = m.text("applying") + " " +
					m.settingsSectionTitle(m.settings.section)
			case settingsEditing:
				detail = m.settingsSectionTitle(m.settings.section) +
					" · " + m.text("ctrl+s apply")
			default:
				if m.settings.section != "" {
					detail = m.text("selected") + " " +
						m.settingsSectionTitle(m.settings.section)
				} else {
					detail = m.text("choose category")
				}
			}
		}
		lines := []string{microIdentity(identity, width)}
		if height >= 2 {
			if animated {
				detail = ansi.Strip(m.spinner.View()) + " " + detail
			}
			lines = append(lines, fitText(detail, width))
		}
		for len(lines) > height {
			lines = lines[:len(lines)-1]
		}
		return m.configuredView(strings.Join(lines, "\n"))
	}

	lines := []string{microIdentity(m.text("PCController"), width)}
	if height >= 2 {
		state := m.deviceStateText()
		if m.controllerError != "" {
			state = m.text("Offline")
		}
		lines = append(lines, fitText(state, width))
		m.deviceRegion = region{x: 0, y: 1, width: width, height: 1}
	}
	if height >= 3 {
		lines = append(lines, fitText(m.text("t toggle · o on · f off"), width))
	}
	if height >= 4 {
		lines = append(lines, fitText(m.text(": commands · s settings · q quit"), width))
	}
	if height >= 5 && m.notice != "" {
		lines = append(lines, fitText(m.notice, width))
	}
	for len(lines) > height {
		lines = lines[:len(lines)-1]
	}
	return m.configuredView(strings.Join(lines, "\n"))
}

func microIdentity(identity string, width int) string {
	if width <= 0 {
		return ""
	}
	identity = sanitizeTerminalLine(identity)
	if ansi.StringWidth(identity) <= width {
		return identity
	}
	// At one-cell terminal widths an ellipsis erases the product identity
	// altogether. Keep the first display cell instead; ansi.Truncate avoids
	// slicing a multi-byte Persian rune.
	return ansi.Truncate(identity, width, "")
}

func (m *Model) renderUltraCompactView(width, offsetX int) tea.View {
	background := m.styles.palette.background
	state, stateColor := m.connectionState()
	stateTextColor := m.styles.palette.brightText
	statePill := m.styles.pill.Copy().
		Foreground(stateTextColor).
		Background(stateColor).
		Render(strings.ToUpper(state))
	title := styleOn(m.styles.title, background).Render(m.text("PCController"))
	gap := max(1, width-lipgloss.Width(title)-lipgloss.Width(statePill))
	first := title + backgroundRun(background, strings.Repeat(" ", gap)) + statePill
	if m.rtl() {
		first = statePill + backgroundRun(background, strings.Repeat(" ", gap)) + title
	}
	if lipgloss.Width(first) > width {
		first = title
	}

	device := m.renderDeviceStatusOn(background)
	deviceLine := placeHorizontalOn(
		width,
		lipgloss.Center,
		styleOn(m.styles.value, background).Render("◉  ")+device,
		background,
	)
	m.deviceRegion = region{x: offsetX, y: 2, width: width, height: 1}

	specs := []buttonSpec{
		{ActionToggle, m.text("T TOGGLE"), m.styles.buttonHot},
		{ActionOn, m.text("O ON"), m.styles.buttonOn},
		{ActionOff, m.text("F OFF"), m.styles.buttonOff},
		{ActionRefresh, "R", m.styles.button},
		{ActionReconnect, "C", m.styles.button},
	}
	buttons, regions := renderButtonRow(specs, 3, width, background, m.rtl())
	for index := range regions {
		regions[index].x += offsetX
	}
	m.buttonRegions = append(m.buttonRegions, regions...)

	lines := []string{
		fitStyledLine(first, width, m.text("PCController")),
		styleOn(m.styles.muted, background).
			Render(fitText(m.transport.Label(), width)),
		deviceLine,
		buttons,
	}
	if m.height >= 6 {
		device := emptyDash(m.snapshot.DeviceURL)
		if m.lastError != "" {
			device = m.text("error") + " · " + m.lastError
		}
		lines = append(
			lines,
			styleOn(m.styles.muted, background).
				Render(fitText(device, width)),
		)
	}
	if m.height >= 7 && m.notice != "" {
		noticeStyle := m.styles.notice
		if m.noticeError {
			noticeStyle = m.styles.errorNotice
		}
		lines = append(lines, noticeStyle.Render(fitText(m.notice, width-2)))
	}
	if m.height >= 8 {
		lines = append(
			lines,
			styleOn(m.styles.muted, background).Render(
				fitText(m.text("space toggle · d info · x terminal · ? help · q quit"), width),
			),
		)
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	content := strings.Join(lines, "\n")
	if offsetX > 0 {
		content = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
	}
	return m.configuredView(m.styles.app.Render(content))
}

func (m *Model) renderHeader(width int) string {
	background := m.styles.palette.surface
	state, stateColor := m.connectionState()
	stateTextColor := m.styles.palette.brightText
	connection := m.styles.pill.Copy().
		Foreground(stateTextColor).
		Background(stateColor).
		Render("● " + strings.ToUpper(state))
	title := styleOn(m.styles.title, background).Render("◉  " + m.text("PCController"))

	innerWidth := max(8, width-m.styles.header.GetHorizontalFrameSize())
	gap := max(1, innerWidth-lipgloss.Width(title)-lipgloss.Width(connection))
	firstLine := title +
		backgroundRun(background, strings.Repeat(" ", gap)) +
		connection
	if m.rtl() {
		firstLine = connection +
			backgroundRun(background, strings.Repeat(" ", gap)) +
			title
	}
	if lipgloss.Width(firstLine) > innerWidth {
		firstLine = styleOn(m.styles.title, background).Render(m.text("PCController")) +
			backgroundRun(background, "  ") +
			connection
	}

	detail := m.transport.Label()
	if m.snapshot.DeviceURL != "" {
		detail += "  ·  " + m.snapshot.DeviceURL
	}
	secondLine := styleOn(m.styles.subtitle, background).
		Render(fitText(detail, innerWidth))
	return m.styles.header.Width(width).Render(firstLine + "\n" + secondLine)
}

func (m *Model) renderDevicePanel(width, height int) string {
	background := m.styles.palette.surfaceAlt
	innerWidth := max(8, width-m.styles.panelHot.GetHorizontalFrameSize())
	title := styleOn(m.styles.panelTitle, background).Render(m.text("DEVICE"))
	art := placeHorizontalOn(
		innerWidth,
		lipgloss.Center,
		m.renderDeviceArt(false),
		background,
	)
	status := placeHorizontalOn(
		innerWidth,
		lipgloss.Center,
		m.renderDeviceStatusOn(background),
		background,
	)
	hint := placeHorizontalOn(
		innerWidth,
		lipgloss.Center,
		styleOn(m.styles.muted, background).
			Render(m.text("click or press space")),
		background,
	)
	content := strings.Join([]string{title, art, status, hint}, "\n")
	return m.styles.panelHot.Width(width).Height(height).Render(content)
}

func (m *Model) renderControlPanel(
	width, height, absoluteX, absoluteY int,
) (string, []actionRegion) {
	background := m.styles.palette.surface
	innerWidth := max(8, width-m.styles.panel.GetHorizontalFrameSize())
	lines, local := m.renderControlContent(innerWidth, background)
	rendered := m.styles.panel.Width(width).Height(height).Render(strings.Join(lines, "\n"))

	contentX := absoluteX + 3 // one border cell and two padding cells
	contentY := absoluteY + 2 // one border cell and one padding cell
	regions := make([]actionRegion, 0, len(local))
	for _, item := range local {
		item.x += contentX
		item.y += contentY
		regions = append(regions, item)
	}
	return rendered, regions
}

func (m *Model) renderCompactPanel(
	width, height, absoluteX, absoluteY int,
) (string, []actionRegion, region) {
	background := m.styles.palette.surfaceAlt
	style := m.styles.panelHot.Width(width).Height(height)
	innerWidth := max(8, width-style.GetHorizontalFrameSize())
	title := styleOn(m.styles.panelTitle, background).
		Render(m.text("DEVICE CONTROL"))

	var lines []string
	lines = append(lines, title)
	if innerWidth >= 48 && height >= 12 {
		art := m.renderDeviceArt(true)
		info, local := m.renderControlContent(
			max(26, innerWidth-17),
			background,
		)
		infoText := strings.Join(info, "\n")
		joined := lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(15).Render(art),
			backgroundRun(background, "  "),
			infoText,
		)
		lines = append(lines, joined)

		contentX := absoluteX + 3 + 17
		contentY := absoluteY + 2 + 1
		for index := range local {
			local[index].x += contentX
			local[index].y += contentY
		}
		device := region{
			x:      absoluteX + 3,
			y:      absoluteY + 3,
			width:  min(15, innerWidth),
			height: min(5, max(1, height-4)),
		}
		return style.Render(strings.Join(lines, "\n")), local, device
	}

	lines = append(
		lines,
		placeHorizontalOn(
			innerWidth,
			lipgloss.Center,
			m.renderDeviceStatusOn(background),
			background,
		),
	)
	state, _ := m.connectionState()
	lines = append(
		lines,
		m.renderKVOn(m.text("Connection"), state, innerWidth, background),
	)
	lines = append(
		lines,
		m.renderKVOn(
			m.text("Transport"),
			string(m.transport.Mode()),
			innerWidth,
			background,
		),
	)
	buttonLines, local := m.renderButtons(
		innerWidth,
		len(lines),
		background,
	)
	lines = append(lines, buttonLines...)
	contentX := absoluteX + 3
	contentY := absoluteY + 2
	for index := range local {
		local[index].x += contentX
		local[index].y += contentY
	}
	device := region{
		x:      absoluteX + 3,
		y:      absoluteY + 3,
		width:  innerWidth,
		height: 1,
	}
	return style.Render(strings.Join(lines, "\n")), local, device
}

func (m *Model) renderControlContent(
	innerWidth int,
	background color.Color,
) ([]string, []actionRegion) {
	state, _ := m.connectionState()
	source := strings.TrimSpace(m.snapshot.LastSource)
	if source == "" {
		source = string(m.transport.Mode())
	}
	lastChanged := "—"
	if !m.snapshot.LastChanged.IsZero() {
		lastChanged = m.snapshot.LastChanged.Local().Format("15:04:05")
	}
	activity := m.text("ready")
	switch {
	case m.busy:
		activity = m.spinner.View() + " " + m.actionTitle(m.busyAction)
	case m.snapshot.PendingCommand != "":
		activity = m.text("pending") + " · " + m.snapshot.PendingCommand
	case m.lastError != "":
		activity = fitText(m.lastError, max(8, innerWidth-11))
	}
	lines := []string{
		styleOn(m.styles.panelTitle, background).
			Render(m.text("STATUS & CONTROL")),
		m.renderKVOn(m.text("Connection"), state, innerWidth, background),
		m.renderKVOn(
			m.text("Device"),
			emptyDash(m.snapshot.DeviceURL),
			innerWidth,
			background,
		),
		m.renderKVOn(m.text("Source"), source, innerWidth, background),
		m.renderKVOn(m.text("Changed"), lastChanged, innerWidth, background),
		m.renderKVOn(m.text("Activity"), activity, innerWidth, background),
		backgroundRun(background, strings.Repeat(" ", innerWidth)),
	}
	buttonLines, regions := m.renderButtons(
		innerWidth,
		len(lines),
		background,
	)
	lines = append(lines, buttonLines...)
	return lines, regions
}

func (m *Model) renderButtons(
	innerWidth int,
	startY int,
	background color.Color,
) ([]string, []actionRegion) {
	rowOneItems := []buttonSpec{
		{ActionToggle, m.text("SPACE  TOGGLE"), m.styles.buttonHot},
		{ActionOn, m.text("O  ON"), m.styles.buttonOn},
		{ActionOff, m.text("F  OFF"), m.styles.buttonOff},
	}
	rowTwoItems := []buttonSpec{
		{ActionRefresh, m.text("R  REFRESH"), m.styles.button},
		{ActionReconnect, m.text("C  RECONNECT"), m.styles.button},
	}
	rowOne, regionsOne := renderButtonRow(
		rowOneItems,
		startY,
		innerWidth,
		background,
		m.rtl(),
	)
	rowTwo, regionsTwo := renderButtonRow(
		rowTwoItems,
		startY+1,
		innerWidth,
		background,
		m.rtl(),
	)
	return []string{rowOne, rowTwo}, append(regionsOne, regionsTwo...)
}

type buttonSpec struct {
	action Action
	label  string
	style  lipgloss.Style
}

func renderButtonRow(
	items []buttonSpec,
	y int,
	maxWidth int,
	background color.Color,
	rtl ...bool,
) (string, []actionRegion) {
	if len(rtl) != 0 && rtl[0] {
		items = append([]buttonSpec(nil), items...)
		for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
			items[left], items[right] = items[right], items[left]
		}
	}
	var rendered []string
	var regions []actionRegion
	x := 0
	for _, item := range items {
		button := item.style.Render(item.label)
		buttonWidth := lipgloss.Width(button)
		added := buttonWidth
		if len(rendered) != 0 {
			added++
		}
		if x+added > maxWidth {
			break
		}
		if len(rendered) != 0 {
			rendered = append(
				rendered,
				backgroundRun(background, " "),
			)
			x++
		}
		regions = append(regions, actionRegion{
			region: region{x: x, y: y, width: buttonWidth, height: 1},
			action: item.action,
		})
		rendered = append(rendered, button)
		x += buttonWidth
	}
	return strings.Join(rendered, ""), regions
}

func (m *Model) renderActivityPanel(width, height int) string {
	innerHeight := max(1, height-m.styles.panel.GetVerticalFrameSize()-1)
	innerWidth := max(8, width-m.styles.panel.GetHorizontalFrameSize())
	if m.viewport.Width() != innerWidth {
		m.viewport.SetWidth(innerWidth)
		m.refreshActivityView()
	}
	m.viewport.SetHeight(innerHeight)
	content := styleOn(
		m.styles.panelTitle,
		m.styles.palette.surface,
	).Render(m.text("RECENT ACTIVITY")) + "\n" + m.viewport.View()
	return m.styles.panel.Width(width).Height(height).Render(content)
}

func (m *Model) renderDeviceArt(compact bool) string {
	symbol := "?"
	color := m.styles.palette.bad
	glow := "·"
	switch m.snapshot.State {
	case model.StateOn:
		symbol = "✦"
		color = m.styles.palette.on
		if m.pulse%4 < 2 {
			glow = "✧"
		} else {
			glow = "·"
		}
	case model.StateOff:
		symbol = "·"
		color = m.styles.palette.off
		glow = " "
	}
	if m.busy {
		// The art owns its color and background; retaining the spinner's
		// nested SGR reset would punch a default-background rectangle into it.
		symbol = ansi.Strip(m.spinner.View())
		color = m.styles.palette.accent
	}
	if compact {
		art := fmt.Sprintf(
			"   %s   \n"+
				" ╭───╮ \n"+
				"│  %s  │\n"+
				" ╰─┬─╯ \n"+
				"   ╧   ",
			glow,
			symbol,
		)
		return lipgloss.NewStyle().
			Foreground(color).
			Background(m.styles.palette.surfaceAlt).
			Bold(true).
			Render(art)
	}
	art := fmt.Sprintf(
		"   %s       %s   \n"+
			"    ╭──────╮    \n"+
			"  ╱          ╲  \n"+
			" │     %s      │ \n"+
			"  ╲          ╱  \n"+
			"    ╰─┬──┬─╯    \n"+
			"      ╞══╡      \n"+
			"      ╰──╯      ",
		glow,
		glow,
		symbol,
	)
	return lipgloss.NewStyle().
		Foreground(color).
		Background(m.styles.palette.surfaceAlt).
		Bold(true).
		Render(art)
}

func (m *Model) renderDeviceStatus() string {
	return m.renderDeviceStatusOn(m.styles.palette.background)
}

func (m *Model) renderDeviceStatusOn(background color.Color) string {
	label := m.deviceStateText()
	if m.controllerError != "" {
		label += " · " + m.text("LAST KNOWN")
	}
	style := m.styles.pill.Copy().Background(background)
	switch m.snapshot.State {
	case model.StateOn:
		style = style.
			Foreground(m.styles.palette.brightText).
			Background(m.styles.palette.on)
	case model.StateOff:
		style = style.
			Foreground(m.styles.palette.text).
			Background(m.styles.palette.off)
	default:
		style = style.
			Foreground(m.styles.palette.brightText).
			Background(m.styles.palette.bad)
	}
	if m.busy {
		label = m.actionTitle(m.busyAction) + "…"
		style = style.
			Foreground(m.styles.palette.brightText).
			Background(m.styles.palette.accent)
	}
	return style.Render(" " + label + " ")
}

func (m *Model) connectionState() (string, anyColor) {
	switch {
	case m.controllerError != "":
		return m.text("Controller offline"), m.styles.palette.bad
	case m.snapshot.WebSocketOnline:
		return "WebSocket", m.styles.palette.good
	case m.snapshot.HTTPReachable:
		return m.text("HTTP fallback"), m.styles.palette.warning
	case m.lastError != "" || m.snapshot.LastError != "":
		return m.text("Offline"), m.styles.palette.bad
	default:
		return m.text("Connecting"), m.styles.palette.accent
	}
}

// anyColor is an alias kept local to this view so helpers can return any
// concrete Lip Gloss color without coupling the palette to a color model.
type anyColor = interface {
	RGBA() (r, g, b, a uint32)
}

func (m *Model) renderKV(label, value string, width int) string {
	return m.renderKVOn(
		label,
		value,
		width,
		m.styles.palette.background,
	)
}

func (m *Model) renderKVOn(
	label string,
	value string,
	width int,
	background color.Color,
) string {
	if width <= 2 {
		return styleOn(m.styles.value, background).
			Render(padRight(fitText(value, max(0, width)), max(0, width)))
	}
	labelWidth := min(11, width-2)
	if m.rtl() {
		labelWidth = min(16, width-2)
	}
	label = fitText(label, labelWidth)
	valueWidth := width - labelWidth - 1
	value = padRight(fitText(value, valueWidth), valueWidth)
	if m.rtl() {
		return styleOn(m.styles.value, background).
			Render(value) +
			backgroundRun(background, " ") +
			styleOn(m.styles.label, background).
				Render(padLeft(label, labelWidth))
	}
	return styleOn(m.styles.label, background).
		Render(padRight(label, labelWidth)) +
		backgroundRun(background, " ") +
		styleOn(m.styles.value, background).
			Render(value)
}

func (m *Model) deviceStateText() string {
	switch m.snapshot.State {
	case model.StateOn:
		return m.text("On")
	case model.StateOff:
		return m.text("Off")
	default:
		return m.text("Unknown")
	}
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	return value
}

func fitText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = sanitizeTerminalLine(value)
	if ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "…")
}

func fitStyledLine(value string, width int, fallback ...string) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	// Styled lines are only used in the tiny header. Falling back to the plain
	// product name avoids slicing ANSI escape sequences.
	plain := "PCController"
	if len(fallback) != 0 {
		plain = fallback[0]
	}
	return fitText(plain, width)
}

func padRight(value string, width int) string {
	missing := width - ansi.StringWidth(value)
	if missing <= 0 {
		return value
	}
	return value + strings.Repeat(" ", missing)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("15:04:05")
}
