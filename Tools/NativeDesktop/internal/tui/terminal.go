package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/terminal"
)

const (
	maxTerminalTranscriptBytes = 512 << 10
	maxTerminalQueuedInput     = 32 << 10
	terminalReadRetryDelay     = 500 * time.Millisecond
	terminalMaximumLongPoll    = 2 * time.Second
)

type terminalOverlay struct {
	active   bool
	viewport viewport.Model
	snapshot deviceterminal.Snapshot

	raw       []byte
	after     uint64
	session   uint64
	requestID uint64

	connecting   bool
	disconnect   bool
	readPending  bool
	readCancel   context.CancelFunc
	writePending bool
	inputQueue   [][]byte
	queuedBytes  int

	err    string
	notice string
}

type terminalConnectResultMsg struct {
	requestID uint64
	snapshot  deviceterminal.Snapshot
	err       error
	reconnect bool
}

type terminalDisconnectResultMsg struct {
	requestID uint64
	snapshot  deviceterminal.Snapshot
	err       error
}

type terminalReadResultMsg struct {
	requestID uint64
	session   uint64
	result    deviceterminal.ReadResult
	err       error
}

type terminalWriteResultMsg struct {
	requestID uint64
	snapshot  deviceterminal.Snapshot
	err       error
}

type terminalReadRetryMsg struct {
	requestID uint64
}

func newTerminalOverlay(
	width int,
	height int,
	mouse bool,
	style lipgloss.Style,
) terminalOverlay {
	view := viewport.New(
		viewport.WithWidth(max(1, width)),
		viewport.WithHeight(max(1, height)),
	)
	view.SoftWrap = true
	view.FillHeight = true
	view.MouseWheelEnabled = mouse
	view.Style = style
	return terminalOverlay{viewport: view}
}

func (m *Model) terminalActive() bool {
	return m.terminal.active
}

func (m *Model) openTerminal() tea.Cmd {
	transport, ok := m.transport.(TerminalTransport)
	if !ok {
		m.setNotice(m.text("This transport does not expose the device terminal"), true)
		return nil
	}
	if m.diagnosticsActive() {
		m.closeDiagnostics()
	}
	m.cancelTerminalRead()
	m.terminal.active = true
	m.terminal.requestID++
	m.terminal.connecting = true
	m.terminal.disconnect = false
	m.terminal.err = ""
	m.terminal.notice = m.text("Opening the app-owned terminal session…")
	m.resizeTerminal()
	requestID := m.terminal.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := transport.TerminalStatus(ctx)
		if err == nil && snapshot.State != deviceterminal.StateConnected {
			snapshot, err = transport.TerminalConnect(ctx)
		}
		return terminalConnectResultMsg{
			requestID: requestID,
			snapshot:  snapshot,
			err:       err,
		}
	}
}

func (m *Model) closeTerminal() {
	m.cancelTerminalRead()
	m.terminal.active = false
	m.terminal.requestID++
	m.terminal.connecting = false
	m.terminal.disconnect = false
	m.terminal.writePending = false
	m.terminal.inputQueue = nil
	m.terminal.queuedBytes = 0
	m.terminal.err = ""
	m.terminal.notice = ""
}

func (m *Model) reconnectTerminal() tea.Cmd {
	transport, ok := m.transport.(TerminalTransport)
	if !ok {
		m.terminal.err = m.text("This transport does not expose the device terminal")
		return nil
	}
	m.cancelTerminalRead()
	m.terminal.requestID++
	m.terminal.connecting = true
	m.terminal.disconnect = false
	m.terminal.writePending = false
	m.terminal.inputQueue = nil
	m.terminal.queuedBytes = 0
	m.terminal.err = ""
	m.terminal.notice = m.text("Reconnecting device terminal…")
	requestID := m.terminal.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		// Reconnect is deliberately explicit: Connect itself is idempotent.
		// Disconnect first so Ctrl+R always creates a fresh fenced session.
		_, _ = transport.TerminalDisconnect(ctx)
		snapshot, err := transport.TerminalConnect(ctx)
		return terminalConnectResultMsg{
			requestID: requestID,
			snapshot:  snapshot,
			err:       err,
			reconnect: true,
		}
	}
}

func (m *Model) disconnectTerminal() tea.Cmd {
	transport, ok := m.transport.(TerminalTransport)
	if !ok {
		m.terminal.err = m.text("This transport does not expose the device terminal")
		return nil
	}
	m.cancelTerminalRead()
	m.terminal.requestID++
	m.terminal.connecting = false
	m.terminal.disconnect = true
	m.terminal.writePending = false
	m.terminal.inputQueue = nil
	m.terminal.queuedBytes = 0
	m.terminal.err = ""
	m.terminal.notice = m.text("Disconnecting device terminal…")
	requestID := m.terminal.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := transport.TerminalDisconnect(ctx)
		return terminalDisconnectResultMsg{
			requestID: requestID,
			snapshot:  snapshot,
			err:       err,
		}
	}
}

func (m *Model) beginTerminalRead() tea.Cmd {
	transport, ok := m.transport.(TerminalTransport)
	if !ok || !m.terminal.active || m.terminal.readPending ||
		m.terminal.snapshot.State != deviceterminal.StateConnected {
		return nil
	}
	wait := m.options.Timeout - 250*time.Millisecond
	if wait < 0 {
		wait = 0
	}
	if wait > terminalMaximumLongPoll {
		wait = terminalMaximumLongPoll
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
	m.terminal.readCancel = cancel
	m.terminal.readPending = true
	requestID := m.terminal.requestID
	session := m.terminal.session
	after := m.terminal.after
	return func() tea.Msg {
		defer cancel()
		result, err := transport.TerminalRead(
			ctx,
			after,
			deviceterminal.DefaultReadBytes,
			wait,
		)
		return terminalReadResultMsg{
			requestID: requestID,
			session:   session,
			result:    result,
			err:       err,
		}
	}
}

func (m *Model) cancelTerminalRead() {
	if m.terminal.readCancel != nil {
		m.terminal.readCancel()
		m.terminal.readCancel = nil
	}
	m.terminal.readPending = false
}

func (m *Model) retryTerminalRead(requestID uint64) tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(terminalReadRetryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return terminalReadRetryMsg{requestID: requestID}
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *Model) queueTerminalInput(data []byte) tea.Cmd {
	if len(data) == 0 {
		return nil
	}
	if m.terminal.snapshot.ReadOnly {
		m.terminal.err = ""
		m.terminal.notice = m.text("ShellOnSerial · output only")
		return nil
	}
	if m.terminal.snapshot.State != deviceterminal.StateConnected {
		m.terminal.err = m.text("Device terminal is not connected · Ctrl+R reconnects")
		return nil
	}
	if len(data) > deviceterminal.MaxInputBytes ||
		m.terminal.queuedBytes+len(data) > maxTerminalQueuedInput {
		m.terminal.err = m.text("Terminal input queue is full")
		return nil
	}
	data = bytes.Clone(data)
	// Preserve the immutable in-flight head. Keystrokes arriving while it is
	// being written are coalesced into a bounded second batch.
	if m.terminal.writePending && len(m.terminal.inputQueue) > 1 {
		last := len(m.terminal.inputQueue) - 1
		if len(m.terminal.inputQueue[last])+len(data) <= deviceterminal.MaxInputBytes {
			m.terminal.inputQueue[last] = append(m.terminal.inputQueue[last], data...)
			m.terminal.queuedBytes += len(data)
			return nil
		}
	}
	m.terminal.inputQueue = append(m.terminal.inputQueue, data)
	m.terminal.queuedBytes += len(data)
	if m.terminal.writePending {
		return nil
	}
	return m.beginTerminalWrite()
}

func (m *Model) beginTerminalWrite() tea.Cmd {
	transport, ok := m.transport.(TerminalTransport)
	if !ok || !m.terminal.active || m.terminal.writePending ||
		len(m.terminal.inputQueue) == 0 ||
		m.terminal.snapshot.State != deviceterminal.StateConnected ||
		m.terminal.snapshot.ReadOnly {
		return nil
	}
	payload := bytes.Clone(m.terminal.inputQueue[0])
	m.terminal.writePending = true
	requestID := m.terminal.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		snapshot, err := transport.TerminalWrite(ctx, payload)
		return terminalWriteResultMsg{
			requestID: requestID,
			snapshot:  snapshot,
			err:       err,
		}
	}
}

func (m *Model) updateTerminalKey(message tea.KeyPressMsg) tea.Cmd {
	switch message.String() {
	case "esc", "ctrl+]":
		m.closeTerminal()
		return nil
	case "ctrl+d":
		return m.disconnectTerminal()
	case "ctrl+r", "f5":
		return m.reconnectTerminal()
	}
	data := terminalKeyBytes(message)
	return m.queueTerminalInput(data)
}

func terminalKeyBytes(message tea.KeyPressMsg) []byte {
	switch message.String() {
	case "ctrl+c":
		return []byte{0x03}
	case "ctrl+l":
		return []byte{0x0c}
	}
	key := message.Key()
	switch key.Code {
	case tea.KeyEnter, tea.KeyKpEnter:
		return []byte("\r\n")
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyBackspace:
		return []byte{0x08}
	case tea.KeyDelete, tea.KeyKpDelete:
		return []byte("\x1b[3~")
	case tea.KeyUp, tea.KeyKpUp:
		return []byte("\x1b[A")
	case tea.KeyDown, tea.KeyKpDown:
		return []byte("\x1b[B")
	case tea.KeyRight, tea.KeyKpRight:
		return []byte("\x1b[C")
	case tea.KeyLeft, tea.KeyKpLeft:
		return []byte("\x1b[D")
	case tea.KeyHome, tea.KeyKpHome:
		return []byte("\x1b[H")
	case tea.KeyEnd, tea.KeyKpEnd:
		return []byte("\x1b[F")
	}
	if key.Text != "" {
		return []byte(key.Text)
	}
	return nil
}

func (m *Model) acceptTerminalConnect(message terminalConnectResultMsg) tea.Cmd {
	if !m.terminal.active || message.requestID != m.terminal.requestID {
		return nil
	}
	m.terminal.connecting = false
	if message.err != nil {
		m.terminal.err = compactError(message.err)
		m.terminal.notice = m.text("Connection failed · Ctrl+R retries")
		return nil
	}
	m.acceptTerminalSnapshot(message.snapshot)
	if message.snapshot.State != deviceterminal.StateConnected {
		m.terminal.err = m.text("Device terminal did not enter the connected state")
		return nil
	}
	m.terminal.err = ""
	if message.reconnect {
		m.terminal.notice = m.text("Fresh terminal session connected")
	} else {
		m.terminal.notice = m.text("Interactive session attached")
	}
	return m.beginTerminalRead()
}

func (m *Model) acceptTerminalDisconnect(
	message terminalDisconnectResultMsg,
) tea.Cmd {
	if !m.terminal.active || message.requestID != m.terminal.requestID {
		return nil
	}
	m.terminal.disconnect = false
	if message.err != nil {
		m.terminal.err = compactError(message.err)
		m.terminal.notice = m.text("Disconnect failed")
		return nil
	}
	m.acceptTerminalSnapshot(message.snapshot)
	m.terminal.err = ""
	m.terminal.notice = m.text("Disconnected explicitly · Ctrl+R reconnects")
	return nil
}

func (m *Model) acceptTerminalRead(message terminalReadResultMsg) tea.Cmd {
	if !m.terminal.active || message.requestID != m.terminal.requestID {
		return nil
	}
	m.terminal.readPending = false
	m.terminal.readCancel = nil
	if message.err != nil {
		m.terminal.err = compactError(message.err)
		return m.retryTerminalRead(message.requestID)
	}

	result := message.result
	if message.session != 0 && result.Snapshot.Session != 0 &&
		message.session != result.Snapshot.Session {
		m.resetTerminalTranscript(m.text("Session changed · transcript fenced"))
	}
	if result.Reset {
		m.resetTerminalTranscript(m.text("Scrollback cursor expired · view resynchronized"))
	}
	m.acceptTerminalSnapshot(result.Snapshot)

	chunks := append([]deviceterminal.Chunk(nil), result.Chunks...)
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].Sequence < chunks[j].Sequence
	})
	wasAtBottom := m.terminal.viewport.AtBottom()
	for _, chunk := range chunks {
		if chunk.Sequence <= m.terminal.after {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
		if err != nil {
			m.terminal.err = m.text("Invalid terminal output received")
			continue
		}
		m.terminal.raw = append(m.terminal.raw, data...)
		m.terminal.after = chunk.Sequence
	}
	if len(m.terminal.raw) > maxTerminalTranscriptBytes {
		start := len(m.terminal.raw) - maxTerminalTranscriptBytes
		if newline := bytes.IndexByte(m.terminal.raw[start:], '\n'); newline >= 0 {
			start += newline + 1
		}
		m.terminal.raw = bytes.Clone(m.terminal.raw[start:])
		m.terminal.notice = m.text("Local display trimmed to the newest 512 KiB")
	}
	m.refreshTerminalView(wasAtBottom)
	if result.Snapshot.State != deviceterminal.StateConnected {
		m.terminal.notice = m.text("Device terminal disconnected · Ctrl+R reconnects")
		return nil
	}
	m.terminal.err = ""
	return m.beginTerminalRead()
}

func (m *Model) acceptTerminalWrite(message terminalWriteResultMsg) tea.Cmd {
	if !m.terminal.active || message.requestID != m.terminal.requestID {
		return nil
	}
	if len(m.terminal.inputQueue) > 0 {
		m.terminal.queuedBytes -= len(m.terminal.inputQueue[0])
		m.terminal.inputQueue = m.terminal.inputQueue[1:]
	}
	m.terminal.writePending = false
	if message.err != nil {
		m.terminal.err = compactError(message.err)
		m.terminal.inputQueue = nil
		m.terminal.queuedBytes = 0
		return nil
	}
	m.acceptTerminalSnapshot(message.snapshot)
	if message.snapshot.State != deviceterminal.StateConnected ||
		message.snapshot.ReadOnly {
		m.terminal.inputQueue = nil
		m.terminal.queuedBytes = 0
		if message.snapshot.ReadOnly {
			m.terminal.notice = m.text("ShellOnSerial · output only")
		} else {
			m.terminal.notice = m.text("Device terminal disconnected · input queue cleared")
		}
		return nil
	}
	return m.beginTerminalWrite()
}

func (m *Model) acceptTerminalSnapshot(snapshot deviceterminal.Snapshot) {
	if m.terminal.session != 0 && snapshot.Session != 0 &&
		m.terminal.session != snapshot.Session {
		m.resetTerminalTranscript(m.text("New terminal session · previous output cleared"))
	}
	m.terminal.snapshot = snapshot
	if snapshot.Session != 0 {
		m.terminal.session = snapshot.Session
	}
}

func (m *Model) resetTerminalTranscript(notice string) {
	m.terminal.raw = nil
	m.terminal.after = 0
	m.terminal.viewport.SetContent("")
	m.terminal.viewport.GotoTop()
	m.terminal.notice = notice
}

func (m *Model) refreshTerminalView(stickToBottom bool) {
	position := m.terminal.viewport.YOffset()
	m.terminal.viewport.SetContent(sanitizeDeviceTerminal(m.terminal.raw))
	if stickToBottom {
		m.terminal.viewport.GotoBottom()
	} else {
		m.terminal.viewport.SetYOffset(position)
	}
}

func (m *Model) resizeTerminal() {
	if !m.terminal.active {
		return
	}
	frameX := m.styles.terminalPanel.GetHorizontalFrameSize()
	frameY := m.styles.terminalPanel.GetVerticalFrameSize()
	innerWidth := max(1, m.width-frameX)
	innerHeight := max(1, m.height-frameY)
	// title, metadata, notice/status, and two footer rows
	bodyHeight := max(1, innerHeight-5)
	m.terminal.viewport.SetWidth(innerWidth)
	m.terminal.viewport.SetHeight(bodyHeight)
	m.terminal.viewport.SoftWrap = true
	m.terminal.viewport.FillHeight = true
	m.terminal.viewport.MouseWheelEnabled = m.options.Mouse
	m.terminal.viewport.Style = lipgloss.NewStyle().
		Foreground(m.styles.palette.text).
		Background(m.styles.palette.surface)
}

func (m *Model) renderTerminalOverlay() tea.View {
	frameX := m.styles.terminalPanel.GetHorizontalFrameSize()
	frameY := m.styles.terminalPanel.GetVerticalFrameSize()
	innerWidth := max(1, m.width-frameX)
	innerHeight := max(1, m.height-frameY)
	bodyHeight := max(1, innerHeight-5)

	state := m.terminal.snapshot.State
	stateText := strings.ToUpper(string(state))
	stateColor := m.styles.palette.bad
	switch {
	case m.terminal.connecting:
		stateText = m.text("CONNECTING")
		stateColor = m.styles.palette.accent
	case m.terminal.disconnect:
		stateText = m.text("DISCONNECTING")
		stateColor = m.styles.palette.warning
	case state == deviceterminal.StateConnected:
		stateText = m.text("Connected")
		stateColor = m.styles.palette.good
	case state == deviceterminal.StateConnecting:
		stateText = m.text("Connecting")
		stateColor = m.styles.palette.accent
	case stateText == "":
		stateText = m.text("DISCONNECTED")
	}
	pill := m.styles.pill.Copy().
		Foreground(m.styles.palette.brightText).
		Background(stateColor).
		Render("● " + stateText)
	background := m.styles.palette.surface
	title := styleOn(m.styles.title, background).
		Render("⌁  " + m.text("DEVICE TERMINAL"))
	gap := max(1, innerWidth-lipgloss.Width(title)-lipgloss.Width(pill))
	header := title +
		backgroundRun(background, strings.Repeat(" ", gap)) +
		pill
	if m.rtl() {
		header = pill +
			backgroundRun(background, strings.Repeat(" ", gap)) +
			title
	}
	if lipgloss.Width(header) > innerWidth {
		header = fitStyledLine(title, innerWidth, m.text("DEVICE TERMINAL"))
	}

	host := strings.TrimSpace(m.terminal.snapshot.Host)
	if host == "" {
		host = m.text("configured lamp")
	}
	port := m.terminal.snapshot.Port
	if port == 0 {
		port = deviceterminal.DefaultPort
	}
	metadata := fmt.Sprintf(
		"%s:%d  ·  %s %d  ·  %s",
		host,
		port,
		m.text("session"),
		m.terminal.snapshot.Session,
		m.transport.Label(),
	)
	if m.terminal.snapshot.ReadOnly {
		metadata += "  ·  " + m.text("read only")
	}

	body := m.terminal.viewport.View()
	if len(m.terminal.raw) == 0 {
		message := m.text("Waiting for device output…")
		if state != deviceterminal.StateConnected {
			message = m.text("No active session · press Ctrl+R to connect")
		}
		body = lipgloss.Place(
			innerWidth,
			bodyHeight,
			lipgloss.Center,
			lipgloss.Center,
			styleOn(m.styles.muted, background).Render(message),
			lipgloss.WithWhitespaceStyle(
				lipgloss.NewStyle().Background(background),
			),
		)
	}
	body = lipgloss.NewStyle().
		Width(innerWidth).
		Height(bodyHeight).
		MaxWidth(innerWidth).
		MaxHeight(bodyHeight).
		Foreground(m.styles.palette.text).
		Background(m.styles.palette.surface).
		Render(body)

	status := m.terminal.notice
	statusStyle := styleOn(m.styles.muted, background)
	if m.terminal.err != "" {
		status = m.terminal.err
		statusStyle = statusStyle.Foreground(m.styles.palette.bad).Bold(true)
	} else if m.terminal.snapshot.ReadOnly {
		status = m.text("ShellOnSerial · output only")
		statusStyle = statusStyle.Foreground(m.styles.palette.warning).Bold(true)
	} else if m.terminal.writePending || m.terminal.queuedBytes > 0 {
		status = m.text("Sending input") + " · " +
			fmt.Sprintf("%d", m.terminal.queuedBytes) + " " + m.text("bytes queued")
	}
	if status == "" {
		status = m.text("Raw keyboard input is sent to the lamp")
	}
	footer := m.text("Esc / Ctrl+] detach · Ctrl+C sends ETX · Ctrl+D disconnect · Ctrl+R / F5 reconnect")
	lines := []string{
		header,
		styleOn(m.styles.muted, background).
			Render(padRight(fitText(metadata, innerWidth), innerWidth)),
		body,
		statusStyle.Render(m.alignLine(status, innerWidth)),
		styleOn(m.styles.muted, background).
			Render(m.alignLine(footer, innerWidth)),
	}
	panel := m.styles.terminalPanel.
		Width(m.width).
		Height(m.height).
		MaxWidth(m.width).
		MaxHeight(m.height).
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
	view := m.configuredView(content)
	view.WindowTitle = m.text("Rayan Lamp") + " · " + m.text("DEVICE TERMINAL")
	if m.terminal.connecting || m.terminal.disconnect {
		view.ProgressBar = tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return view
}

// sanitizeDeviceTerminal preserves printable UTF-8, layout controls, and a
// conservative subset of SGR color/style sequences. Cursor movement, OSC
// (including clipboard/title controls), DCS/APC/PM/SOS, C0/C1 controls, and
// bidi overrides are removed before content reaches Bubble Tea.
func sanitizeDeviceTerminal(raw []byte) string {
	var safe bytes.Buffer
	safe.Grow(len(raw))
	retainedSGR := false
	for index := 0; index < len(raw); {
		switch raw[index] {
		case 0x1b:
			if index+1 >= len(raw) {
				index++
				continue
			}
			switch raw[index+1] {
			case '[':
				end := index + 2
				for end < len(raw) && end-index <= 96 {
					if raw[end] >= 0x40 && raw[end] <= 0x7e {
						break
					}
					end++
				}
				if end < len(raw) && raw[end] == 'm' &&
					safeSGRParameters(raw[index+2:end]) &&
					terminal.ColorRequested() {
					safe.Write(raw[index : end+1])
					retainedSGR = true
				}
				if end < len(raw) {
					index = end + 1
				} else {
					index = len(raw)
				}
			case ']', 'P', 'X', '^', '_':
				index += 2
				for index < len(raw) {
					if raw[index] == 0x07 {
						index++
						break
					}
					if raw[index] == 0x1b && index+1 < len(raw) &&
						raw[index+1] == '\\' {
						index += 2
						break
					}
					index++
				}
			default:
				// Two-byte escape controls and their optional intermediate
				// bytes are never meaningful in a scrollback viewport.
				index += 2
				for index < len(raw) && raw[index] >= 0x20 && raw[index] <= 0x2f {
					index++
				}
				if index < len(raw) && raw[index] >= 0x30 && raw[index] <= 0x7e {
					index++
				}
			}
		case '\r':
			if index+1 < len(raw) && raw[index+1] == '\n' {
				index++
			}
			safe.WriteByte('\n')
			index++
		case '\n', '\t':
			safe.WriteByte(raw[index])
			index++
		case '\b':
			removeLastPrintableRune(&safe)
			index++
		default:
			if raw[index] < 0x20 || raw[index] == 0x7f ||
				(raw[index] >= 0x80 && raw[index] <= 0x9f) {
				index++
				continue
			}
			_, size := utf8.DecodeRune(raw[index:])
			if size == 0 {
				index++
				continue
			}
			if size == 1 && raw[index] >= utf8.RuneSelf {
				safe.WriteRune('�')
				index++
				continue
			}
			character, _ := utf8.DecodeRune(raw[index : index+size])
			if unicode.IsControl(character) || unsafeBidiControl(character) {
				index += size
				continue
			}
			safe.Write(raw[index : index+size])
			index += size
		}
	}
	if retainedSGR {
		// Any retained SGR is locally terminated so remote output can never
		// style the overlay footer or the rest of the application.
		safe.WriteString(terminal.Reset)
	}
	return safe.String()
}

func safeSGRParameters(parameters []byte) bool {
	if len(parameters) > 64 {
		return false
	}
	for _, character := range parameters {
		if (character < '0' || character > '9') &&
			character != ';' && character != ':' {
			return false
		}
	}
	return true
}

func removeLastPrintableRune(buffer *bytes.Buffer) {
	data := buffer.Bytes()
	position := len(data)
	for position > 0 {
		if data[position-1] == 'm' {
			start := bytes.LastIndex(data[:position], []byte("\x1b["))
			if start >= 0 && safeSGRParameters(data[start+2:position-1]) {
				position = start
				continue
			}
		}
		character, size := utf8.DecodeLastRune(data[:position])
		if size <= 0 || character == '\n' {
			return
		}
		updated := make([]byte, 0, len(data)-size)
		updated = append(updated, data[:position-size]...)
		updated = append(updated, data[position:]...)
		buffer.Reset()
		_, _ = buffer.Write(updated)
		return
	}
}
