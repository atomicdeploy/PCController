package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// runtimeModel adds scheduling concerns that should not change Model's
// deterministic command contract. This keeps direct model tests and embedders
// able to execute one concrete command/result at a time, while the real Bubble
// Tea runtime can independently animate active work and expire notices.
type runtimeModel struct {
	model *Model
}

func newRuntimeModel(model *Model) *runtimeModel {
	return &runtimeModel{model: model}
}

func (runtime *runtimeModel) Init() tea.Cmd {
	return runtime.model.runtimeCommands(runtime.model.Init())
}

func (runtime *runtimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := runtime.model.Update(message)
	if model, ok := updated.(*Model); ok {
		runtime.model = model
	}
	return runtime, runtime.model.runtimeCommands(command)
}

func (runtime *runtimeModel) View() tea.View {
	return runtime.model.View()
}

func (m *Model) runtimeCommands(command tea.Cmd) tea.Cmd {
	commands := make([]tea.Cmd, 0, 3)
	if command != nil {
		commands = append(commands, command)
	}
	if m.spinnerActive() && !m.spinnerRunning {
		m.spinnerRunning = true
		commands = append(commands, m.spinner.Tick)
	}
	if m.notice != "" &&
		!m.noticeUntil.IsZero() &&
		m.noticeScheduled != m.noticeRevision {
		revision := m.noticeRevision
		deadline := m.noticeUntil
		m.noticeScheduled = revision
		delay := time.Until(deadline)
		if delay < 0 {
			delay = 0
		}
		commands = append(commands, tea.Tick(delay, func(at time.Time) tea.Msg {
			return noticeExpiredMsg{revision: revision, at: at}
		}))
	}
	switch len(commands) {
	case 0:
		return nil
	case 1:
		return commands[0]
	default:
		return tea.Batch(commands...)
	}
}
