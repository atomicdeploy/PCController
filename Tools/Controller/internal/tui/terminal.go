package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
)

type updatePresentation struct {
	OperationID   string
	Kind          string
	State         string
	Detail        string
	Progress      int
	ProgressKnown bool
	BytesDone     int64
	BytesTotal    int64
	Stage         string
	ErrorCode     string
	StartedAt     time.Time
	Scroll        int
	UpdatedAt     time.Time
}

type terminalOSCResultMsg struct {
	kind string
	ack  *hostui.ActionAck
	err  error
}

type appActionAckResultMsg struct {
	ack     hostui.ActionAck
	attempt int
	err     error
}

type appActionAckRetryMsg struct {
	ack     hostui.ActionAck
	attempt int
}

const (
	maximumAppActionAckAttempts = 3
	appActionAckRetryDelay      = 100 * time.Millisecond
)

func (model Model) terminalTitle() string {
	if model.terminalTitleOverride != "" {
		return model.terminalTitleOverride
	}
	base := strings.TrimSpace(model.prefs.AppTitle)
	if base == "" {
		base = "PCController"
	}
	if (hostui.UpdateProgress{State: model.update.State}).Active() || model.update.State == "failed" || model.update.State == "cancelled" {
		if model.update.State == "failed" || model.update.State == "cancelled" {
			return fmt.Sprintf("%s — Update %s — %s", base, strings.ToUpper(model.update.State), pageDefinitions[model.page].Short)
		}
		if model.update.ProgressKnown {
			return fmt.Sprintf("%s — %s %d%% — %s", base, model.update.Stage, model.update.Progress, pageDefinitions[model.page].Short)
		}
		return fmt.Sprintf("%s — %s — %s", base, model.update.Stage, pageDefinitions[model.page].Short)
	}
	return fmt.Sprintf("%s — %s", base, pageDefinitions[model.page].Short)
}

func (model *Model) reportInstance() {
	page := pageInstanceName(model.page)
	title := model.terminalTitle()
	if model.reportTerminalAsync != nil {
		model.reportTerminalAsync(page, title)
		return
	}
	if model.reportTerminal != nil {
		if err := model.reportTerminal(page, title); err != nil {
			model.appendLog("warn", "app instance report failed: "+err.Error())
		}
		return
	}
	if model.reportPage != nil {
		if err := model.reportPage(page); err != nil {
			model.appendLog("warn", "app instance report failed: "+err.Error())
		}
	}
}

func (model *Model) acceptNavigationAction(action hostui.AppAction) (string, bool) {
	if model.navigationIdentity == nil {
		return "", false
	}
	epoch, revision := model.navigationIdentity()
	return model.navigationCursor.AcceptFor(action, model.navigationGroup, epoch, revision)
}

func terminalOSCCommand(
	write func(string) error,
	payload, kind string,
	ack *hostui.ActionAck,
) tea.Cmd {
	return func() tea.Msg {
		if write == nil {
			return terminalOSCResultMsg{kind: kind, ack: ack, err: fmt.Errorf("terminal OSC output is unavailable")}
		}
		return terminalOSCResultMsg{kind: kind, ack: ack, err: write(payload)}
	}
}

func acknowledgeAppAction(
	acknowledge func(hostui.ActionAck) error,
	ack hostui.ActionAck,
	attempts ...int,
) tea.Cmd {
	attempt := 1
	if len(attempts) > 0 && attempts[0] > 1 {
		attempt = attempts[0]
	}
	return func() tea.Msg {
		if acknowledge == nil {
			return appActionAckResultMsg{
				ack: ack, attempt: attempt,
				err: fmt.Errorf("app action acknowledgement is unavailable"),
			}
		}
		return appActionAckResultMsg{ack: ack, attempt: attempt, err: acknowledge(ack)}
	}
}

func retryAppActionAcknowledgement(ack hostui.ActionAck, attempt int) tea.Cmd {
	delay := time.Duration(attempt-1) * appActionAckRetryDelay
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return appActionAckRetryMsg{ack: ack, attempt: attempt}
	})
}

func (model *Model) observeUpdateEvent(event control.Event) tea.Cmd {
	kind := strings.ToLower(event.Kind)
	if !strings.HasPrefix(kind, "update.") && !strings.HasPrefix(kind, "peer-update.") {
		return nil
	}
	value := hostui.ParseUpdateProgress(event.Kind, event.Text, event.Metadata, event.Time)
	state := value.State
	if strings.EqualFold(state, "idle") {
		model.update = updatePresentation{}
		model.terminalTitleDirty = true
		return func() tea.Msg {
			return terminalOSCResultMsg{kind: "update progress", err: hostui.PresentUpdateProgress(value, model.writeOSC)}
		}
	}
	scroll := 0
	if model.update.OperationID == value.OperationID {
		scroll = model.update.Scroll
	}
	model.update = updatePresentation{
		OperationID: value.OperationID, Kind: value.Kind,
		State: state, Detail: value.Detail, Progress: value.Percent, ProgressKnown: value.Known,
		BytesDone: value.BytesDone, BytesTotal: value.BytesTotal,
		Stage: value.Stage, ErrorCode: value.ErrorCode, StartedAt: value.StartedAt, UpdatedAt: value.UpdatedAt,
		Scroll: scroll,
	}
	model.terminalTitleDirty = true
	return func() tea.Msg {
		return terminalOSCResultMsg{kind: "update progress", err: hostui.PresentUpdateProgress(value, model.writeOSC)}
	}
}

func (model Model) updateProgressLines() []string {
	if model.update.State == "" || strings.EqualFold(model.update.State, "idle") {
		return nil
	}
	width := max(24, min(76, model.width-8))
	progress := max(0, min(100, model.update.Progress))
	state, stage := model.update.State, strings.ReplaceAll(model.update.Stage, "-", " ")
	if stage == "" {
		stage = strings.ReplaceAll(state, "-", " ")
	}
	color, symbol := colorAccent, model.spinnerView()
	active := (hostui.UpdateProgress{State: state}).Active()
	switch state {
	case "failed":
		color, symbol = colorBad, "✕"
	case "completed":
		color, symbol = colorGood, "✓"
	case "cancelled":
		color, symbol = colorWarn, "■"
	default:
		if !active {
			symbol = "✓"
		}
	}
	heading := strings.ToUpper(stage)
	if state == "failed" {
		heading = "UPDATE FAILED"
	}
	if state == "completed" {
		heading = "UPDATE COMPLETE"
	}
	if state == "cancelled" {
		heading = "UPDATE CANCELLED"
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(color).Render(symbol + "  " + heading)}
	if state == "failed" {
		lines = append(lines, labelStyle.Render("Last stage: "+stage))
	}
	if active && model.update.ProgressKnown {
		barWidth := max(12, width-10)
		filled := progress * barWidth / 100
		bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("━", filled)) + labelStyle.Render(strings.Repeat("─", barWidth-filled))
		lines = append(lines, fmt.Sprintf("%s  %3d%%", bar, progress), labelStyle.Render("Current stage"))
	}
	if model.update.BytesTotal > 0 {
		lines = append(lines, labelStyle.Render("Transferred ")+valueStyle.Render(hostui.FormatByteProgress(model.update.BytesDone, model.update.BytesTotal)))
	}
	if !model.update.StartedAt.IsZero() {
		end := model.update.UpdatedAt
		if active {
			end = time.Now()
		}
		if end.Before(model.update.StartedAt) {
			end = model.update.StartedAt
		}
		lines = append(lines, labelStyle.Render("Elapsed "+end.Sub(model.update.StartedAt).Round(time.Second).String()))
	}
	if model.update.Detail != "" {
		lines = append(lines, "", lipgloss.NewStyle().Width(width-4).Render(model.update.Detail))
	}
	if model.update.ErrorCode != "" {
		lines = append(lines, errorStyle.Render("Error: "+model.update.ErrorCode))
	}
	if model.update.OperationID != "" {
		lines = append(lines, "", labelStyle.Render("Operation "+model.update.OperationID))
	}
	panel := cardStyle.Copy().BorderForeground(color).Width(width).Render(strings.Join(lines, "\n"))
	return strings.Split(panel, "\n")
}
