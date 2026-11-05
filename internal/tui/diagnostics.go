package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

const (
	defaultDiagnosticEndpoint = "/info"
	maxDiagnosticPanelWidth   = 100
	maxDiagnosticPanelHeight  = 32
	maxDiagnosticDisplayBytes = 256 << 10
)

type diagnosticOverlayMode uint8

const (
	diagnosticClosed diagnosticOverlayMode = iota
	diagnosticForm
	diagnosticLoading
	diagnosticResult
)

type diagnosticOverlay struct {
	mode       diagnosticOverlayMode
	endpoint   string
	form       *huh.Form
	viewport   viewport.Model
	result     DiagnosticResult
	err        string
	requestID  uint64
	renderedAt diagnosticRenderKey
}

type diagnosticRenderKey struct {
	width  int
	isDark bool
	result DiagnosticResult
}

type diagnosticResultMsg struct {
	requestID uint64
	result    DiagnosticResult
	err       error
}

type diagnosticOverlayLayout struct {
	width        int
	height       int
	innerWidth   int
	innerHeight  int
	bodyHeight   int
	showSubtitle bool
}

func calculateDiagnosticOverlayLayout(
	terminalWidth int,
	terminalHeight int,
) diagnosticOverlayLayout {
	terminalWidth = max(1, terminalWidth)
	terminalHeight = max(1, terminalHeight)

	width := terminalWidth
	switch {
	case terminalWidth >= 64:
		width = min(maxDiagnosticPanelWidth, terminalWidth-6)
	case terminalWidth >= 28:
		width = terminalWidth - 2
	}
	height := terminalHeight
	switch {
	case terminalHeight >= 22:
		height = min(maxDiagnosticPanelHeight, terminalHeight-4)
	case terminalHeight >= 10:
		height = terminalHeight - 2
	}

	// The diagnostics panel has one border cell plus two padding cells on each
	// horizontal edge, and one border plus one padding cell vertically.
	innerWidth := max(1, width-6)
	innerHeight := max(1, height-4)
	showSubtitle := innerHeight >= 5
	chromeHeight := 2 // title and footer
	if showSubtitle {
		chromeHeight++
	}
	return diagnosticOverlayLayout{
		width:        width,
		height:       height,
		innerWidth:   innerWidth,
		innerHeight:  innerHeight,
		bodyHeight:   max(1, innerHeight-chromeHeight),
		showSubtitle: showSubtitle,
	}
}

func (m *Model) diagnosticsActive() bool {
	return m.diagnostics.mode != diagnosticClosed
}

func (m *Model) openDiagnostics() tea.Cmd {
	if _, ok := m.transport.(DiagnosticsTransport); !ok {
		m.setNotice(m.text("This transport does not expose diagnostics"), true)
		return nil
	}
	m.diagnostics.requestID++
	m.diagnostics.mode = diagnosticForm
	m.diagnostics.err = ""
	m.diagnostics.result = DiagnosticResult{}
	m.diagnostics.renderedAt = diagnosticRenderKey{}
	if m.diagnostics.endpoint == "" {
		m.diagnostics.endpoint = defaultDiagnosticEndpoint
	}

	layout := calculateDiagnosticOverlayLayout(m.width, m.height)
	input := huh.NewInput().
		Title(m.text("Endpoint")).
		Inline(true).
		Placeholder(defaultDiagnosticEndpoint).
		Suggestions([]string{"/info"}).
		CharLimit(180).
		Value(&m.diagnostics.endpoint).
		Validate(validateDiagnosticEndpoint)
	m.diagnostics.form = huh.NewForm(huh.NewGroup(input)).
		WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
		WithShowHelp(false).
		WithShowErrors(true).
		WithWidth(layout.innerWidth).
		WithHeight(layout.bodyHeight)
	return m.diagnostics.form.Init()
}

func (m *Model) closeDiagnostics() {
	m.diagnostics.requestID++
	m.diagnostics.mode = diagnosticClosed
	m.diagnostics.form = nil
	m.diagnostics.err = ""
	m.diagnostics.result = DiagnosticResult{}
	m.diagnostics.renderedAt = diagnosticRenderKey{}
}

func (m *Model) updateDiagnosticForm(message tea.Msg) tea.Cmd {
	if m.diagnostics.form == nil {
		m.closeDiagnostics()
		return nil
	}
	updated, command := m.diagnostics.form.Update(message)
	form, ok := updated.(*huh.Form)
	if ok {
		m.diagnostics.form = form
	}
	switch m.diagnostics.form.State {
	case huh.StateCompleted:
		return tea.Batch(command, m.beginDiagnosticFetch())
	case huh.StateAborted:
		m.closeDiagnostics()
		return nil
	default:
		return command
	}
}

func (m *Model) beginDiagnosticFetch() tea.Cmd {
	endpoint := m.diagnostics.endpoint
	if err := validateDiagnosticEndpoint(endpoint); err != nil {
		m.diagnostics.err = err.Error()
		return m.openDiagnostics()
	}
	fetcher, ok := m.transport.(DiagnosticsTransport)
	if !ok {
		m.diagnostics.mode = diagnosticResult
		m.diagnostics.err = m.text("This transport does not expose diagnostics.")
		return nil
	}

	m.diagnostics.requestID++
	requestID := m.diagnostics.requestID
	m.diagnostics.mode = diagnosticLoading
	m.diagnostics.form = nil
	m.diagnostics.err = ""
	m.diagnostics.result = DiagnosticResult{}
	m.diagnostics.renderedAt = diagnosticRenderKey{}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		result, err := fetcher.FetchDiagnostics(ctx, endpoint)
		return diagnosticResultMsg{
			requestID: requestID,
			result:    result,
			err:       err,
		}
	}
}

func (m *Model) acceptDiagnosticResult(message diagnosticResultMsg) {
	if message.requestID != m.diagnostics.requestID ||
		m.diagnostics.mode != diagnosticLoading {
		return
	}
	m.diagnostics.mode = diagnosticResult
	if message.err != nil {
		m.diagnostics.err = compactError(message.err)
		m.appendActivity(
			activityDiagnosticsError,
			activityLevelError,
			m.diagnostics.err,
		)
		return
	}
	m.diagnostics.result = message.result
	m.diagnostics.endpoint = message.result.Endpoint
	m.diagnostics.err = ""
	m.diagnostics.renderedAt = diagnosticRenderKey{}
	m.resizeDiagnostics()
	m.diagnostics.viewport.GotoTop()
	m.appendActivity(
		activityDiagnosticsLoaded,
		activityLevelSuccess,
		fmt.Sprintf("%s · %d %s", message.result.Endpoint, message.result.Bytes, m.text("bytes")),
	)
}

func (m *Model) resizeDiagnostics() {
	if !m.diagnosticsActive() {
		return
	}
	layout := calculateDiagnosticOverlayLayout(m.width, m.height)
	if m.diagnostics.form != nil {
		m.diagnostics.form.
			WithTheme(diagnosticHuhTheme(m.isDark, m.styles.palette, m.rtl())).
			WithWidth(layout.innerWidth).
			WithHeight(layout.bodyHeight)
	}
	if m.diagnostics.mode != diagnosticResult || m.diagnostics.err != "" {
		return
	}

	m.diagnostics.viewport.SetWidth(layout.innerWidth)
	m.diagnostics.viewport.SetHeight(layout.bodyHeight)
	m.diagnostics.viewport.SoftWrap = true
	m.diagnostics.viewport.FillHeight = true
	m.diagnostics.viewport.MouseWheelEnabled = m.options.Mouse
	m.diagnostics.viewport.Style = lipgloss.NewStyle().
		Foreground(m.styles.palette.text).
		Background(m.styles.palette.surface)

	key := diagnosticRenderKey{
		width:  layout.innerWidth,
		isDark: m.isDark,
		result: m.diagnostics.result,
	}
	if key == m.diagnostics.renderedAt {
		return
	}
	position := m.diagnostics.viewport.YOffset()
	rendered, err := renderDiagnosticDocument(
		m.diagnostics.result.Content,
		layout.innerWidth,
		m.isDark,
	)
	if err != nil {
		m.diagnostics.err = compactError(err)
		return
	}
	m.diagnostics.viewport.SetContent(rendered)
	m.diagnostics.viewport.SetYOffset(position)
	m.diagnostics.renderedAt = key
}

func renderDiagnosticDocument(
	content string,
	width int,
	isDark bool,
) (string, error) {
	content = strings.TrimSpace(content)
	colorEnabled := !noColorRequested()
	p := themePalette(isDark)
	if !colorEnabled {
		p = noColorPalette()
	}

	if content == "" {
		empty := "The endpoint returned an empty response."
		if colorEnabled {
			empty = lipgloss.NewStyle().
				Foreground(p.muted).
				Italic(true).
				Render(empty)
		}
		return lipgloss.Wrap(empty, max(8, width), ""), nil
	}

	content = strings.ToValidUTF8(content, "�")
	truncated := len(content) > maxDiagnosticDisplayBytes
	if truncated {
		end := maxDiagnosticDisplayBytes
		for end > 0 && !utf8.RuneStart(content[end]) {
			end--
		}
		content = content[:end] +
			"\n\n… response display truncated at 256 KiB …"
	}

	isJSON := false
	var value any
	if !truncated && json.Unmarshal([]byte(content), &value) == nil {
		if indented, err := json.MarshalIndent(value, "", "  "); err == nil {
			content = string(indented)
			isJSON = true
		}
	}
	content = sanitizeDiagnosticText(content)

	var rendered string
	if isJSON {
		rendered = renderDiagnosticJSON(content, p, colorEnabled)
	} else {
		rendered = renderDiagnosticText(content, p)
	}
	return lipgloss.Wrap(rendered, max(8, width), ""), nil
}

func renderDiagnosticText(content string, p palette) string {
	style := lipgloss.NewStyle().Foreground(p.text)
	var result strings.Builder
	result.Grow(len(content) + 32)
	first := true
	for line := range strings.SplitSeq(content, "\n") {
		if !first {
			result.WriteByte('\n')
		}
		first = false
		result.WriteString(style.Render(line))
	}
	return result.String()
}

func renderDiagnosticJSON(
	content string,
	p palette,
	colorEnabled bool,
) string {
	if !colorEnabled {
		return content
	}

	keyStyle := lipgloss.NewStyle().
		Foreground(p.accentAlt).
		Bold(true)
	stringStyle := lipgloss.NewStyle().Foreground(p.good)
	numberStyle := lipgloss.NewStyle().Foreground(p.warning)
	literalStyle := lipgloss.NewStyle().Foreground(p.accent)
	nullStyle := lipgloss.NewStyle().
		Foreground(p.faint).
		Italic(true)

	var result strings.Builder
	result.Grow(len(content) + len(content)/8)
	for index := 0; index < len(content); {
		switch character := content[index]; {
		case character == '"':
			end := diagnosticJSONStringEnd(content, index)
			style := stringStyle
			if diagnosticJSONStringIsKey(content, end) {
				style = keyStyle
			}
			result.WriteString(style.Render(content[index:end]))
			index = end
		case character == '-' || character >= '0' && character <= '9':
			end := diagnosticJSONNumberEnd(content, index)
			result.WriteString(numberStyle.Render(content[index:end]))
			index = end
		case strings.HasPrefix(content[index:], "true"):
			result.WriteString(literalStyle.Render("true"))
			index += len("true")
		case strings.HasPrefix(content[index:], "false"):
			result.WriteString(literalStyle.Render("false"))
			index += len("false")
		case strings.HasPrefix(content[index:], "null"):
			result.WriteString(nullStyle.Render("null"))
			index += len("null")
		default:
			result.WriteByte(character)
			index++
		}
	}
	return result.String()
}

func diagnosticJSONStringEnd(content string, start int) int {
	escaped := false
	for index := start + 1; index < len(content); index++ {
		switch {
		case escaped:
			escaped = false
		case content[index] == '\\':
			escaped = true
		case content[index] == '"':
			return index + 1
		}
	}
	return len(content)
}

func diagnosticJSONStringIsKey(content string, end int) bool {
	for end < len(content) {
		switch content[end] {
		case ' ', '\t', '\r', '\n':
			end++
		default:
			return content[end] == ':'
		}
	}
	return false
}

func diagnosticJSONNumberEnd(content string, start int) int {
	end := start + 1
	for end < len(content) {
		character := content[end]
		if character != '+' &&
			character != '-' &&
			character != '.' &&
			character != 'e' &&
			character != 'E' &&
			(character < '0' || character > '9') {
			break
		}
		end++
	}
	return end
}

func sanitizeDiagnosticText(content string) string {
	var result strings.Builder
	result.Grow(len(content))
	for _, character := range content {
		switch {
		case character == '\n' || character == '\t':
			result.WriteRune(character)
		case character == 0x1b:
			result.WriteString(`\x1b`)
		case character < 0x20 || character == 0x7f:
			_, _ = fmt.Fprintf(&result, `\x%02x`, character)
		case character >= 0x80 && character <= 0x9f:
			_, _ = fmt.Fprintf(&result, `\u%04x`, character)
		case unsafeBidiControl(character):
			_, _ = fmt.Fprintf(&result, `\u%04x`, character)
		default:
			result.WriteRune(character)
		}
	}
	return result.String()
}

func diagnosticHuhTheme(isDark bool, p palette, rtlValues ...bool) huh.Theme {
	rtl := len(rtlValues) != 0 && rtlValues[0]
	return huh.ThemeFunc(func(bool) *huh.Styles {
		theme := huh.ThemeBase(isDark)
		surface := lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface)
		activeSurface := lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surfaceAlt)
		theme.Form.Base = surface
		theme.Group.Base = surface
		theme.Group.Title = surface.
			Foreground(p.accentAlt).
			Bold(true)
		theme.Group.Description = surface.Foreground(p.muted)
		theme.FieldSeparator = surface.SetString("\n\n")

		theme.Focused.Base = lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surfaceAlt).
			BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).
			BorderForeground(p.accent).
			PaddingLeft(1)
		theme.Focused.Card = theme.Focused.Base
		theme.Focused.Title = activeSurface.
			Foreground(p.accentAlt).
			Bold(true).
			PaddingRight(1)
		theme.Focused.NoteTitle = theme.Focused.Title
		theme.Focused.Description = activeSurface.Foreground(p.muted)
		theme.Focused.ErrorIndicator = activeSurface.
			Foreground(p.bad).
			SetString(" !")
		theme.Focused.ErrorMessage = activeSurface.
			Foreground(p.bad).
			Bold(true).
			SetString(" !")
		theme.Focused.SelectSelector = activeSurface.
			Foreground(p.accent).
			Bold(true).
			SetString("› ")
		theme.Focused.Option = activeSurface.Foreground(p.text)
		theme.Focused.NextIndicator = activeSurface.
			Foreground(p.accent).
			SetString("→")
		theme.Focused.PrevIndicator = activeSurface.
			Foreground(p.accent).
			SetString("←")
		theme.Focused.MultiSelectSelector = theme.Focused.SelectSelector
		theme.Focused.SelectedOption = activeSurface.
			Foreground(p.good).
			Bold(true)
		theme.Focused.SelectedPrefix = activeSurface.
			Foreground(p.good).
			SetString("✓ ")
		theme.Focused.UnselectedOption = activeSurface.Foreground(p.text)
		theme.Focused.UnselectedPrefix = activeSurface.
			Foreground(p.faint).
			SetString("○ ")
		theme.Focused.FocusedButton = lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.accent).
			Bold(true).
			Padding(0, 2)
		theme.Focused.BlurredButton = lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface).
			Padding(0, 2)
		theme.Focused.Next = theme.Focused.FocusedButton
		theme.Focused.Directory = activeSurface.Foreground(p.accentAlt)
		theme.Focused.File = activeSurface.Foreground(p.text)
		theme.Focused.TextInput.Prompt = activeSurface.
			Foreground(p.accent).
			Bold(true)
		theme.Focused.TextInput.Cursor = activeSurface.Foreground(p.accent)
		theme.Focused.TextInput.CursorText = lipgloss.NewStyle().
			Foreground(p.background).
			Background(p.accent)
		theme.Focused.TextInput.Text = activeSurface.
			Foreground(p.text).
			Background(p.surfaceAlt)
		theme.Focused.TextInput.Placeholder = activeSurface.
			Foreground(p.faint).
			Background(p.surfaceAlt)

		theme.Blurred = theme.Focused
		theme.Blurred.Base = surface.
			PaddingLeft(1).
			BorderStyle(lipgloss.HiddenBorder())
		theme.Blurred.Card = theme.Blurred.Base
		theme.Blurred.Title = surface.
			Foreground(p.text).
			Bold(true).
			PaddingRight(1)
		theme.Blurred.NoteTitle = theme.Blurred.Title
		theme.Blurred.Description = surface.Foreground(p.faint)
		theme.Blurred.SelectSelector = surface.SetString("  ")
		theme.Blurred.Option = surface.Foreground(p.muted)
		theme.Blurred.NextIndicator = surface
		theme.Blurred.PrevIndicator = surface
		theme.Blurred.MultiSelectSelector = surface.SetString("  ")
		theme.Blurred.SelectedOption = surface.Foreground(p.good)
		theme.Blurred.SelectedPrefix = surface.
			Foreground(p.good).
			SetString("✓ ")
		theme.Blurred.UnselectedOption = surface.Foreground(p.muted)
		theme.Blurred.UnselectedPrefix = surface.
			Foreground(p.faint).
			SetString("○ ")
		theme.Blurred.FocusedButton = lipgloss.NewStyle().
			Foreground(p.brightText).
			Background(p.accent).
			Bold(true).
			Padding(0, 2)
		theme.Blurred.BlurredButton = lipgloss.NewStyle().
			Foreground(p.muted).
			Background(p.surfaceAlt).
			Padding(0, 2)
		theme.Blurred.Next = theme.Blurred.FocusedButton
		theme.Blurred.Directory = surface.Foreground(p.muted)
		theme.Blurred.File = surface.Foreground(p.muted)
		theme.Blurred.TextInput.Prompt = surface.Foreground(p.faint)
		theme.Blurred.TextInput.Cursor = surface.Foreground(p.faint)
		theme.Blurred.TextInput.CursorText = lipgloss.NewStyle().
			Foreground(p.text).
			Background(p.surface)
		theme.Blurred.TextInput.Text = surface.Foreground(p.text)
		theme.Blurred.TextInput.Placeholder = surface.Foreground(p.faint)
		theme.Help = helpStylesOn(isDark, p, p.surface)
		if rtl {
			theme.Group.Title = theme.Group.Title.Align(lipgloss.Right)
			theme.Group.Description = theme.Group.Description.Align(lipgloss.Right)
			theme.Focused.Base = theme.Focused.Base.
				BorderLeft(false).
				BorderRight(true).
				PaddingLeft(0).
				PaddingRight(1)
			theme.Focused.Title = theme.Focused.Title.Align(lipgloss.Right)
			theme.Focused.NoteTitle = theme.Focused.NoteTitle.Align(lipgloss.Right)
			theme.Focused.Description = theme.Focused.Description.Align(lipgloss.Right)
			theme.Focused.Option = theme.Focused.Option.Align(lipgloss.Right)
			theme.Focused.SelectedOption = theme.Focused.SelectedOption.Align(lipgloss.Right)
			theme.Focused.UnselectedOption = theme.Focused.UnselectedOption.Align(lipgloss.Right)
			theme.Blurred.Base = theme.Blurred.Base.
				PaddingLeft(0).
				PaddingRight(1)
			theme.Blurred.Title = theme.Blurred.Title.Align(lipgloss.Right)
			theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Align(lipgloss.Right)
			theme.Blurred.Description = theme.Blurred.Description.Align(lipgloss.Right)
			theme.Blurred.Option = theme.Blurred.Option.Align(lipgloss.Right)
			theme.Blurred.SelectedOption = theme.Blurred.SelectedOption.Align(lipgloss.Right)
			theme.Blurred.UnselectedOption = theme.Blurred.UnselectedOption.Align(lipgloss.Right)
		}
		return theme
	})
}

func (m *Model) renderDiagnosticsOverlay() tea.View {
	layout := calculateDiagnosticOverlayLayout(m.width, m.height)

	title := m.text("DIAGNOSTICS · READ ONLY")
	subtitle := m.textf("Routed through %s · allow-list enforced", m.transport.Label())
	footer := m.text("enter fetch  ·  esc close")
	var body string
	switch m.diagnostics.mode {
	case diagnosticForm:
		title = m.text("DIAGNOSTICS · FETCH")
		if m.diagnostics.form != nil {
			body = m.diagnostics.form.View()
		}
	case diagnosticLoading:
		title = m.text("FETCHING") + " " + m.diagnostics.endpoint
		body = lipgloss.Place(
			layout.innerWidth,
			layout.bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			m.spinnerViewOn(m.styles.palette.surface)+
				styleOn(m.styles.title, m.styles.palette.surface).
					Render("  "+m.text("Contacting active transport…")),
		)
	case diagnosticResult:
		title = fmt.Sprintf(
			"%s · %d %s",
			m.diagnostics.result.Endpoint,
			m.diagnostics.result.Bytes,
			m.text("BYTES"),
		)
		footer = m.text("↑/↓/pgup/pgdn scroll  ·  d another  ·  esc close")
		if m.diagnostics.err != "" {
			title = m.text("DIAGNOSTICS · COULD NOT FETCH")
			body = lipgloss.Place(
				layout.innerWidth,
				layout.bodyHeight,
				lipgloss.Left,
				lipgloss.Center,
				m.styles.errorNotice.
					MaxWidth(layout.innerWidth).
					Render(m.diagnostics.err),
			)
		} else {
			body = m.diagnostics.viewport.View()
		}
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
	lines := []string{
		panelTitle.Render(fitText(title, layout.innerWidth)),
	}
	if layout.showSubtitle {
		lines = append(
			lines,
			panelMuted.Render(fitText(subtitle, layout.innerWidth)),
		)
	}
	lines = append(
		lines,
		body,
		panelMuted.Render(fitText(footer, layout.innerWidth)),
	)
	panel := m.styles.diagnosticPanel.
		Width(layout.width).
		Height(layout.height).
		MaxWidth(layout.width).
		MaxHeight(layout.height).
		Render(strings.Join(lines, "\n"))

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
