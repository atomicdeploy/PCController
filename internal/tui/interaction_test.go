package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestSingleEnterCompletesHuhMenusWithoutASecondKeypress(t *testing.T) {
	t.Run("command action", func(t *testing.T) {
		model, transport := newWorkspaceModel(t)
		_ = model.openCommands()
		_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		runTUICommand(t, model, command)

		if model.commandsActive() {
			t.Fatal("command menu remained open after selecting Toggle once")
		}
		transport.mutex.Lock()
		actions := append([]Action(nil), transport.actions...)
		transport.mutex.Unlock()
		if !reflect.DeepEqual(actions, []Action{ActionToggle}) {
			t.Fatalf("single Enter actions = %v, want [toggle]", actions)
		}
	})

	t.Run("settings category", func(t *testing.T) {
		model, _ := newWorkspaceModel(t)
		load := model.openSettings()
		_, initCommand := model.Update(load())
		_ = initCommand
		if model.settings.mode != settingsMenu {
			t.Fatalf("settings mode = %v, want menu", model.settings.mode)
		}

		_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		runTUICommand(t, model, command)
		if model.settings.mode != settingsEditing ||
			model.settings.section != settingsLamp {
			t.Fatalf(
				"single Enter settings state = (%v, %q), want editing/lamp",
				model.settings.mode,
				model.settings.section,
			)
		}
	})

	t.Run("device refresh", func(t *testing.T) {
		model, transport := newWorkspaceModel(t)
		load := model.openDeviceSettings()
		_, _ = model.Update(load())
		_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		runTUICommand(t, model, command)

		transport.mu.Lock()
		commands := append([]string(nil), transport.commands...)
		transport.mu.Unlock()
		if !containsString(commands, "device.config.refresh") {
			t.Fatalf("single Enter device commands = %v", commands)
		}
		if model.device.mode != deviceReady {
			t.Fatalf("device mode = %v after refresh, want ready", model.device.mode)
		}
	})
}

func TestCommandMenuRunsServingControlAndQuitConfirmation(t *testing.T) {
	t.Run("serving", func(t *testing.T) {
		model, transport := newWorkspaceModel(t)
		model.commands.selected = commandServeDisable
		_ = model.openCommands()
		_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		runTUICommand(t, model, command)

		if model.commands.mode != commandsResult {
			t.Fatalf("serving command mode = %v, want result", model.commands.mode)
		}
		if transport.cfg.Integration.LocalAPIEnabled {
			t.Fatal("serving command did not disable the coordinator")
		}
		if !strings.Contains(model.commands.detail, "disabled") {
			t.Fatalf("serving result = %q", model.commands.detail)
		}
	})

	t.Run("quit requires confirmation", func(t *testing.T) {
		model, transport := newWorkspaceModel(t)
		model.commands.selected = commandAppQuit
		_ = model.openCommands()
		_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		runTUICommand(t, model, command)
		if model.commands.mode != commandsConfirm {
			t.Fatalf("quit selection mode = %v, want confirmation", model.commands.mode)
		}
		if containsString(transport.commands, "quit") {
			t.Fatal("quit ran before explicit confirmation")
		}

		_, command = model.Update(keyMessage('y', "y"))
		runTUICommand(t, model, command)
		if !containsString(transport.commands, "quit") {
			t.Fatal("confirmed quit was not routed to the coordinator")
		}
	})
}

func TestSelectFilteringOwnsPrintableShortcutsAndEscape(t *testing.T) {
	t.Run("commands", func(t *testing.T) {
		model, _ := newWorkspaceModel(t)
		if command := model.openCommands(); command == nil {
			t.Fatal("command center returned no initialization command")
		}

		_, _ = model.Update(keyMessage('/', "/"))
		if !formIsFiltering(model.commands.form) {
			t.Fatal("slash did not activate command filtering")
		}
		_, _ = model.Update(keyMessage('r', "r"))
		if model.commands.mode != commandsMenu {
			t.Fatal("printable filter input escaped the command menu")
		}

		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !model.commandsActive() {
			t.Fatal("first Escape closed commands instead of accepting the filter")
		}
		if formIsFiltering(model.commands.form) {
			t.Fatal("first Escape did not leave filter-entry mode")
		}
		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.commandsActive() {
			t.Fatal("second Escape did not close the command center")
		}
	})

	t.Run("settings", func(t *testing.T) {
		model, _ := newWorkspaceModel(t)
		load := model.openSettings()
		if load == nil {
			t.Fatal("settings returned no load command")
		}
		_, _ = model.Update(load())
		if model.settings.mode != settingsMenu {
			t.Fatalf("settings mode = %v, want menu", model.settings.mode)
		}

		_, _ = model.Update(keyMessage('/', "/"))
		if !formIsFiltering(model.settings.form) {
			t.Fatal("slash did not activate settings filtering")
		}
		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !model.settingsActive() {
			t.Fatal("first Escape closed settings instead of accepting the filter")
		}
		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.settingsActive() {
			t.Fatal("second Escape did not close settings")
		}
	})

	t.Run("device shortcuts", func(t *testing.T) {
		model, transport := newWorkspaceModel(t)
		load := model.openDeviceSettings()
		if load == nil {
			t.Fatal("device workspace returned no load command")
		}
		_, _ = model.Update(load())
		if model.device.mode != deviceReady {
			t.Fatalf("device mode = %v, want ready", model.device.mode)
		}
		before := append([]string(nil), transport.commands...)

		_, _ = model.Update(keyMessage('/', "/"))
		if !formIsFiltering(model.device.form) {
			t.Fatal("slash did not activate device-action filtering")
		}
		for _, character := range []rune{'r', 'x', 'q'} {
			_, _ = model.Update(keyMessage(character, string(character)))
			if model.device.mode != deviceReady || !model.deviceActive() {
				t.Fatalf("%q filter character closed the device workspace", character)
			}
		}
		if !reflect.DeepEqual(transport.commands, before) {
			t.Fatalf(
				"device filter invoked commands: before=%v after=%v",
				before,
				transport.commands,
			)
		}

		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !model.deviceActive() {
			t.Fatal("first Escape closed device workspace during filtering")
		}
		_, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.deviceActive() {
			t.Fatal("second Escape did not close device workspace")
		}
	})
}

func TestCtrlCQuitsEveryWorkspaceExceptRawTerminal(t *testing.T) {
	tests := []struct {
		name string
		open func(*Model) tea.Cmd
	}{
		{name: "dashboard", open: func(*Model) tea.Cmd { return nil }},
		{name: "commands", open: (*Model).openCommands},
		{name: "settings", open: func(model *Model) tea.Cmd {
			load := model.openSettings()
			_, _ = model.Update(load())
			return nil
		}},
		{name: "device", open: func(model *Model) tea.Cmd {
			load := model.openDeviceSettings()
			_, _ = model.Update(load())
			return nil
		}},
		{name: "diagnostics", open: (*Model).openDiagnostics},
	}
	controlC := tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model, _ := newWorkspaceModel(t)
			_ = test.open(model)
			_, command := model.Update(controlC)
			if command == nil {
				t.Fatal("Ctrl+C returned no quit command")
			}
			message := command()
			if _, ok := message.(tea.QuitMsg); !ok {
				t.Fatalf("Ctrl+C command returned %T, want tea.QuitMsg", message)
			}
		})
	}

	model, transport := newWorkspaceModel(t)
	model.terminal.active = true
	model.terminal.snapshot = transport.terminal
	model.terminal.snapshot.State = "connected"
	_, command := model.Update(controlC)
	if command == nil {
		t.Fatal("terminal Ctrl+C was not forwarded as ETX")
	}
	message := command()
	write, ok := message.(terminalWriteResultMsg)
	if !ok || write.err != nil {
		t.Fatalf("terminal Ctrl+C result = %#v", message)
	}
}

func TestEverySettingsWorkspaceFitsResponsiveTerminal(t *testing.T) {
	sections := []settingsSection{
		settingsLamp,
		settingsAppearance,
		settingsWindow,
		settingsNotifications,
		settingsHotkeys,
		settingsAutomation,
		settingsIntegration,
		settingsDiagnostics,
		settingsAdvanced,
	}
	sizes := []tea.WindowSizeMsg{
		{Width: 24, Height: 8},
		{Width: 40, Height: 12},
		{Width: 80, Height: 24},
		{Width: 120, Height: 40},
	}
	for _, section := range sections {
		for _, size := range sizes {
			t.Run(
				fmt.Sprintf("%s/%dx%d", section, size.Width, size.Height),
				func(t *testing.T) {
					model, transport := newWorkspaceModelAt(
						t,
						ScreenDashboard,
						size.Width,
						size.Height,
					)
					model.settings.cfg = transport.cfg
					model.settings.original = transport.cfg
					_ = model.openSettingsEditorReady(section)
					view := model.View()
					if got := lipgloss.Width(view.Content); got > size.Width {
						t.Fatalf(
							"%s width = %d, terminal width = %d",
							section,
							got,
							size.Width,
						)
					}
					if got := lipgloss.Height(view.Content); got > size.Height {
						t.Fatalf(
							"%s height = %d, terminal height = %d",
							section,
							got,
							size.Height,
						)
					}
					if !strings.Contains(
						view.Content,
						strings.ToUpper(
							strings.Fields(settingsSectionTitle(section))[0],
						),
					) && size.Width >= 24 && size.Height >= 8 {
						t.Fatalf(
							"%s view omitted its identity:\n%s",
							section,
							view.Content,
						)
					}
				},
			)
		}
	}
}

func TestFailedSettingsLoadDoesNotAdvertiseBusyProgress(t *testing.T) {
	model, _ := newWorkspaceModel(t)
	model.settings.mode = settingsLoading
	model.settings.err = "coordinator unavailable"
	view := model.configuredView("error")
	if view.ProgressBar == nil ||
		view.ProgressBar.State != tea.ProgressBarNone {
		t.Fatalf("failed load progress = %#v, want none", view.ProgressBar)
	}
}

func runTUICommand(t *testing.T, model *Model, initial tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{initial}
	for steps := 0; len(queue) != 0; steps++ {
		if steps > 128 {
			t.Fatal("TUI command simulation exceeded 128 messages")
		}
		command := queue[0]
		queue = queue[1:]
		if command == nil {
			continue
		}
		message := command()
		if message == nil {
			continue
		}
		if batch, ok := message.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		// Huh initialization uses Bubble Tea's private sequence message. The
		// form has already focused its first field synchronously, and the
		// runtime-only cursor/window-size sequence is irrelevant to this
		// state-machine simulation.
		if reflect.TypeOf(message).String() == "tea.sequenceMsg" {
			continue
		}
		_, next := model.Update(message)
		queue = append(queue, next)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
