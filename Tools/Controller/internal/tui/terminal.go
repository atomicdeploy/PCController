package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
)

type updatePresentation struct {
	OperationID string
	Kind        string
	State       string
	Detail      string
	Progress    int
	UpdatedAt   time.Time
}

type terminalOSCResultMsg struct {
	kind string
	err  error
}

func (model Model) terminalTitle() string {
	if model.terminalTitleOverride != "" {
		return model.terminalTitleOverride
	}
	base := strings.TrimSpace(model.prefs.AppTitle)
	if base == "" {
		base = "PCController"
	}
	if model.update.State != "" && model.update.State != "completed" {
		if model.update.State == "failed" || model.update.State == "cancelled" {
			return fmt.Sprintf("%s — Update %s — %s", base, strings.ToUpper(model.update.State), pageDefinitions[model.page].Short)
		}
		return fmt.Sprintf("%s — Update %d%% — %s", base, model.update.Progress, pageDefinitions[model.page].Short)
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
		return model.navigationCursor.Accept(action, model.navigationGroup)
	}
	epoch, revision := model.navigationIdentity()
	return model.navigationCursor.AcceptFor(action, model.navigationGroup, epoch, revision)
}

func terminalOSCCommand(write func(string) error, payload, kind string) tea.Cmd {
	return func() tea.Msg {
		if write == nil {
			return terminalOSCResultMsg{kind: kind, err: fmt.Errorf("terminal OSC output is unavailable")}
		}
		return terminalOSCResultMsg{kind: kind, err: write(payload)}
	}
}

func (model *Model) observeUpdateEvent(event control.Event) tea.Cmd {
	if !strings.HasPrefix(strings.ToLower(event.Kind), "update.") {
		return nil
	}
	progress, err := strconv.Atoi(event.Metadata["progress_percent"])
	if err != nil {
		progress = model.update.Progress
	}
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}
	state := strings.TrimSpace(event.Metadata["state"])
	if state == "" {
		state = strings.TrimPrefix(strings.ToLower(event.Kind), "update.")
	}
	model.update = updatePresentation{
		OperationID: event.Metadata["operation_id"], Kind: event.Metadata["kind"],
		State: state, Detail: event.Text, Progress: progress, UpdatedAt: event.Time,
	}
	if model.page != PageProgramming {
		model.switchPage(PageProgramming)
	}
	model.terminalTitleDirty = true

	terminalState := 1
	switch state {
	case "completed", "downloaded":
		terminalState = 0
	case "failed", "cancelled":
		terminalState = 2
	case "queued":
		terminalState = 3
	case "verifying":
		terminalState = 4
	}
	payload, payloadErr := (hostui.TerminalProgress{State: terminalState, Percent: progress}).OSCPayload()
	if payloadErr != nil {
		return func() tea.Msg { return terminalOSCResultMsg{kind: "update progress", err: payloadErr} }
	}
	return terminalOSCCommand(model.writeOSC, payload, "update progress")
}

func (model Model) updateProgressLines() []string {
	if model.update.State == "" {
		return []string{
			kv("Update state", "idle"),
			kv("Update progress", strings.Repeat("─", 36)+"   0%"),
		}
	}
	width := 36
	filled := model.update.Progress * width / 100
	bar := strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
	identity := strings.TrimSpace(model.update.Kind)
	if model.update.OperationID != "" {
		identity += " · " + model.update.OperationID
	}
	lines := []string{
		kv("Update operation", strings.Trim(identity, " ·")),
		kv("Update state", strings.ToUpper(model.update.State)),
		kv("Update progress", fmt.Sprintf("%s %3d%%", bar, model.update.Progress)),
	}
	if model.update.Detail != "" {
		lines = append(lines, kv("Update detail", model.update.Detail))
	}
	return lines
}
