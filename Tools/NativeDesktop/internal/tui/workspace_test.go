package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/deviceconfig"
	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/model"
)

type workspaceTransport struct {
	*fakeTransport

	mu       sync.Mutex
	cfg      config.Config
	patches  []any
	commands []string
	results  map[string]json.RawMessage
	errors   map[string]error
	terminal deviceterminal.Snapshot
}

func (transport *workspaceTransport) GetConfig(
	context.Context,
) (config.Config, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.cfg, nil
}

func (transport *workspaceTransport) PatchConfig(
	_ context.Context,
	patch any,
) (config.Config, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.patches = append(transport.patches, patch)
	if root, ok := patch.(map[string]any); ok {
		if integration, ok := root["integration"].(map[string]any); ok {
			if enabled, ok := integration["local_api_enabled"].(bool); ok {
				transport.cfg.Integration.LocalAPIEnabled = enabled
			}
		}
	}
	return transport.cfg, nil
}

func (transport *workspaceTransport) ExecuteCommand(
	_ context.Context,
	method string,
	_ any,
) (json.RawMessage, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.commands = append(transport.commands, method)
	if err := transport.errors[method]; err != nil {
		return nil, err
	}
	if result := transport.results[method]; result != nil {
		return append(json.RawMessage(nil), result...), nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func (transport *workspaceTransport) TerminalStatus(
	context.Context,
) (deviceterminal.Snapshot, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.terminal, nil
}

func (transport *workspaceTransport) TerminalConnect(
	context.Context,
) (deviceterminal.Snapshot, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.terminal.State = deviceterminal.StateConnected
	return transport.terminal, nil
}

func (transport *workspaceTransport) TerminalDisconnect(
	context.Context,
) (deviceterminal.Snapshot, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.terminal.State = deviceterminal.StateDisconnected
	return transport.terminal, nil
}

func (transport *workspaceTransport) TerminalRead(
	context.Context,
	uint64,
	int,
	time.Duration,
) (deviceterminal.ReadResult, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return deviceterminal.ReadResult{Snapshot: transport.terminal}, nil
}

func (transport *workspaceTransport) TerminalWrite(
	_ context.Context,
	_ []byte,
) (deviceterminal.Snapshot, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.terminal, nil
}

func newWorkspaceModel(
	t *testing.T,
) (*Model, *workspaceTransport) {
	return newWorkspaceModelAt(t, ScreenDashboard, 110, 38)
}

func newWorkspaceModelAt(
	t *testing.T,
	screen InitialScreen,
	width int,
	height int,
) (*Model, *workspaceTransport) {
	t.Helper()
	base := &fakeTransport{snapshot: model.Snapshot{
		State:           model.StateOff,
		HTTPReachable:   true,
		WebSocketOnline: true,
		DeviceURL:       config.DefaultBaseURL,
	}}
	transport := &workspaceTransport{
		fakeTransport: base,
		cfg:           config.Default(),
		results:       make(map[string]json.RawMessage),
		errors:        make(map[string]error),
		terminal: deviceterminal.Snapshot{
			State: deviceterminal.StateDisconnected,
			Host:  "pccontroller-desktop.local",
			Port:  deviceterminal.DefaultPort,
		},
	}
	result, err := NewModel(context.Background(), transport, ModelOptions{
		Poll:          time.Second,
		Timeout:       time.Second,
		Theme:         ThemeDark,
		AltScreen:     true,
		Mouse:         true,
		InitialScreen: screen,
		InitialNow:    time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return result, transport
}

func TestInitialScreenRoutesAndRendersEveryWorkspace(t *testing.T) {
	tests := []struct {
		name       string
		screen     InitialScreen
		wantRender string
		assert     func(*testing.T, *Model)
	}{
		{
			name:       "dashboard",
			screen:     ScreenDashboard,
			wantRender: "PCController",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.settingsActive() ||
					model.commandsActive() ||
					model.deviceActive() ||
					model.diagnosticsActive() ||
					model.terminalActive() {
					t.Fatal("dashboard route opened an overlay")
				}
			},
		},
		{
			name:       "commands",
			screen:     ScreenCommands,
			wantRender: "COMMAND CENTER",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.commands.mode != commandsMenu {
					t.Fatalf("commands mode = %v", model.commands.mode)
				}
			},
		},
		{
			name:       "settings",
			screen:     ScreenSettings,
			wantRender: "LOADING APPLICATION SETTINGS",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.settings.mode != settingsLoading ||
					model.settings.requestSection != "" {
					t.Fatalf(
						"settings route = (%v, %q)",
						model.settings.mode,
						model.settings.requestSection,
					)
				}
			},
		},
		{
			name:       "device",
			screen:     ScreenDevice,
			wantRender: "LOADING DEVICE PARAMETERS",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.device.mode != deviceLoading {
					t.Fatalf("device mode = %v", model.device.mode)
				}
			},
		},
		{
			name:       "serving",
			screen:     ScreenServing,
			wantRender: "LOADING APPLICATION SETTINGS",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.settings.mode != settingsLoading ||
					model.settings.requestSection != settingsIntegration {
					t.Fatalf(
						"serving route = (%v, %q)",
						model.settings.mode,
						model.settings.requestSection,
					)
				}
			},
		},
		{
			name:       "diagnostics",
			screen:     ScreenDiagnostics,
			wantRender: "DIAGNOSTICS · FETCH",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if model.diagnostics.mode != diagnosticForm {
					t.Fatalf("diagnostics mode = %v", model.diagnostics.mode)
				}
			},
		},
		{
			name:       "terminal",
			screen:     ScreenTerminal,
			wantRender: "DEVICE TERMINAL",
			assert: func(t *testing.T, model *Model) {
				t.Helper()
				if !model.terminalActive() || !model.terminal.connecting {
					t.Fatalf(
						"terminal route = (active %v, connecting %v)",
						model.terminalActive(),
						model.terminal.connecting,
					)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tuiModel, _ := newWorkspaceModelAt(t, test.screen, 80, 24)
			if command := tuiModel.Init(); command == nil {
				t.Fatal("Init returned no startup command")
			}
			test.assert(t, tuiModel)
			content := tuiModel.View().Content
			if !strings.Contains(content, test.wantRender) {
				t.Fatalf(
					"%s view omitted %q:\n%s",
					test.screen,
					test.wantRender,
					content,
				)
			}
		})
	}
}

func TestSettingsPatchContainsOnlyChangedFields(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsDevice
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)
	tuiModel.settings.draft.cfg.Device.BaseURL = "http://device-office.local"

	patch, err := tuiModel.settingsPatch()
	if err != nil {
		t.Fatal(err)
	}
	device, ok := patch["device"].(map[string]any)
	if !ok {
		t.Fatalf("patch = %#v", patch)
	}
	if len(device) != 1 || device["base_url"] != "http://device-office.local" {
		t.Fatalf("device patch = %#v, want only base_url", device)
	}

	draft := newSettingsDraft(transport.cfg)
	if strings.Contains(draft.advancedJSON, transport.cfg.Integration.LocalAPIToken) {
		t.Fatal("advanced editor exposed the local API token")
	}
	if !strings.Contains(draft.advancedJSON, preservedTokenMarker) {
		t.Fatal("advanced editor omitted the preserved-token marker")
	}
}

func TestNotificationSettingsImportsCustomWAVBeforeApplyingSelection(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsNotifications
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)
	tuiModel.settings.draft.customSoundPath = `C:\Sounds\studio.wav`
	transport.results["audio.import"] = json.RawMessage(
		`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"studio.wav"}`,
	)

	command := tuiModel.beginSettingsSave()
	if command == nil || tuiModel.settings.mode != settingsSaving {
		t.Fatal("custom WAV import did not enter saving state")
	}
	message, ok := command().(configPatchedMsg)
	if !ok {
		t.Fatal("custom WAV import returned an unexpected message")
	}
	if message.err != nil || !message.configApplied {
		t.Fatalf("custom WAV import result = %#v", message)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.commands) != 1 ||
		transport.commands[0] != "audio.import" ||
		len(transport.patches) != 1 {
		t.Fatalf(
			"custom WAV operations = commands %#v, patches %#v",
			transport.commands,
			transport.patches,
		)
	}
	root, ok := transport.patches[0].(map[string]any)
	if !ok {
		t.Fatalf("custom WAV patch = %#v", transport.patches[0])
	}
	notifications, ok := root["notifications"].(map[string]any)
	if !ok ||
		notifications["sound"] != true ||
		notifications["sound_source"] != config.SoundSourceCustom ||
		notifications["sound_file"] != strings.Repeat("a", 64) ||
		notifications["sound_name"] != "studio.wav" {
		t.Fatalf("custom WAV notifications patch = %#v", notifications)
	}
}

func TestIntegrationSettingsQueriesRegistrationWithoutMutating(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	transport.results[commands.MethodIntegrationRegistrationStatus] =
		json.RawMessage(`{
			"executable":"C:\\Portable\\PCControllerDesktop.exe",
			"shell_registered":true,
			"com_registered":false
		}`)

	command := tuiModel.openSettingsEditor(settingsIntegration)
	if command == nil || tuiModel.settings.mode != settingsLoading {
		t.Fatal("integration editor did not enter registration status loading")
	}
	message, ok := command().(registrationLoadedMsg)
	if !ok {
		t.Fatal("registration status command returned an unexpected message")
	}
	if message.err != nil {
		t.Fatalf("registration status query failed: %v", message.err)
	}
	if command := tuiModel.acceptRegistrationLoaded(message); command == nil {
		t.Fatal("registration status did not open the integration editor")
	}
	if !tuiModel.settings.registrationKnown ||
		!tuiModel.settings.registration.ShellRegistered ||
		tuiModel.settings.registration.COMRegistered {
		t.Fatalf(
			"registration status = %+v",
			tuiModel.settings.registration,
		)
	}
	description := tuiModel.registrationStatusDescription()
	for _, expected := range []string{
		`C:\Portable\PCControllerDesktop.exe`,
		"Shell URI + Jump List",
		"COM automation",
	} {
		if !strings.Contains(description, expected) {
			t.Errorf("registration description omitted %q: %s", expected, description)
		}
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.commands) != 1 ||
		transport.commands[0] != commands.MethodIntegrationRegistrationStatus {
		t.Fatalf("read-only query commands = %#v", transport.commands)
	}
}

func TestIntegrationRegistrationActionIsExplicitAndRefreshesStatus(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsIntegration
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)
	tuiModel.settings.draft.registrationAction =
		commands.MethodIntegrationShellRegister
	transport.results[commands.MethodIntegrationRegistrationStatus] =
		json.RawMessage(`{
			"executable":"C:\\Portable\\PCControllerDesktop.exe",
			"shell_registered":true,
			"com_registered":false
		}`)

	command := tuiModel.beginSettingsSave()
	if command == nil || tuiModel.settings.mode != settingsSaving {
		t.Fatal("explicit registration action did not enter saving state")
	}
	message, ok := command().(configPatchedMsg)
	if !ok {
		t.Fatal("registration action returned an unexpected message")
	}
	if message.err != nil || !message.registrationLoaded ||
		!message.registration.ShellRegistered {
		t.Fatalf("registration action result = %#v", message)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.patches) != 0 {
		t.Fatalf("registration-only action persisted config: %#v", transport.patches)
	}
	want := []string{
		commands.MethodIntegrationShellRegister,
		commands.MethodIntegrationRegistrationStatus,
	}
	if len(transport.commands) != len(want) {
		t.Fatalf("registration commands = %#v", transport.commands)
	}
	for index := range want {
		if transport.commands[index] != want[index] {
			t.Fatalf("registration commands = %#v, want %#v", transport.commands, want)
		}
	}
}

func TestIntegrationUnregisterRequiresConfirmation(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsIntegration
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)
	tuiModel.settings.draft.registrationAction =
		commands.MethodIntegrationCOMUnregister

	if command := tuiModel.beginSettingsSave(); command != nil {
		t.Fatal("unconfirmed COM unregister returned a command")
	}
	if !strings.Contains(tuiModel.settings.err, "confirm") {
		t.Fatalf("unregister error = %q", tuiModel.settings.err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.commands) != 0 {
		t.Fatalf("unconfirmed unregister executed %#v", transport.commands)
	}
}

func TestRegistrationFailureStillRefreshesAuthoritativeStatus(t *testing.T) {
	_, transport := newWorkspaceModel(t)
	transport.errors[commands.MethodIntegrationShellRegister] =
		errors.New("partial: URI registered; Jump List failed")
	transport.results[commands.MethodIntegrationRegistrationStatus] =
		json.RawMessage(`{
			"executable":"C:\\Portable\\PCControllerDesktop.exe",
			"shell_registered":true,
			"com_registered":false
		}`)

	status, loaded, err := executeRegistrationAction(
		context.Background(),
		transport,
		commands.MethodIntegrationShellRegister,
	)
	if err == nil || !loaded || !status.ShellRegistered {
		t.Fatalf("partial action refresh = (%+v, %v, %v)", status, loaded, err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.commands) != 2 ||
		transport.commands[1] != commands.MethodIntegrationRegistrationStatus {
		t.Fatalf("partial action commands = %#v", transport.commands)
	}
}

func TestAutomationSettingsExposeLifecycleScriptsOnDedicatedPage(t *testing.T) {
	tuiModel, transport := newWorkspaceModelAt(
		t,
		ScreenDashboard,
		80,
		24,
	)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsAutomation
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)

	groups := tuiModel.automationSettingsGroups()
	if len(groups) != 3 {
		t.Fatalf("automation pages = %d, want 3", len(groups))
	}
	lifecyclePage := groups[1].Header() + "\n" + groups[1].Content()
	for _, title := range []string{
		"Windows lifecycle events",
		"Lock script",
		"Unlock script",
		"Suspend script",
		"Resume script",
		"Network-change script",
	} {
		if !strings.Contains(lifecyclePage, title) {
			t.Errorf("lifecycle page omitted %q:\n%s", title, lifecyclePage)
		}
	}

	tuiModel.settings.form = tuiModel.newSettingsForm(groups...)
	_ = tuiModel.settings.form.Init()
	_ = tuiModel.settings.form.NextGroup()
	view := tuiModel.renderSettingsOverlay()
	if width := lipgloss.Width(view.Content); width > tuiModel.width {
		t.Fatalf("automation overlay width = %d, terminal width = %d", width, tuiModel.width)
	}
	if height := lipgloss.Height(view.Content); height > tuiModel.height {
		t.Fatalf("automation overlay height = %d, terminal height = %d", height, tuiModel.height)
	}
}

func TestAutomationLifecycleScriptsEditAndApply(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsAutomation
	tuiModel.settings.cfg = transport.cfg
	tuiModel.settings.original = transport.cfg
	tuiModel.settings.draft = newSettingsDraft(transport.cfg)

	expected := map[string]string{
		"lock_script":           `C:\Automation\lock.ps1`,
		"unlock_script":         `C:\Automation\unlock.ps1`,
		"suspend_script":        `C:\Automation\suspend.ps1`,
		"resume_script":         `C:\Automation\resume.ps1`,
		"network_change_script": `C:\Automation\network-change.ps1`,
	}
	tuiModel.settings.draft.cfg.Automation.LockScript = expected["lock_script"]
	tuiModel.settings.draft.cfg.Automation.UnlockScript = expected["unlock_script"]
	tuiModel.settings.draft.cfg.Automation.SuspendScript = expected["suspend_script"]
	tuiModel.settings.draft.cfg.Automation.ResumeScript = expected["resume_script"]
	tuiModel.settings.draft.cfg.Automation.NetworkChangeScript =
		expected["network_change_script"]

	command := tuiModel.beginSettingsSave()
	if command == nil || tuiModel.settings.mode != settingsSaving {
		t.Fatal("automation Apply did not enter the saving state")
	}
	message, ok := command().(configPatchedMsg)
	if !ok {
		t.Fatalf("automation Apply returned an unexpected message")
	}
	if message.err != nil {
		t.Fatalf("automation Apply failed: %v", message.err)
	}
	transport.mu.Lock()
	if len(transport.patches) != 1 {
		transport.mu.Unlock()
		t.Fatalf("automation patch count = %d, want 1", len(transport.patches))
	}
	root, ok := transport.patches[0].(map[string]any)
	transport.mu.Unlock()
	if !ok {
		t.Fatalf("automation patch = %#v", transport.patches[0])
	}
	automation, ok := root["automation"].(map[string]any)
	if !ok {
		t.Fatalf("automation patch root = %#v", root)
	}
	if len(automation) != len(expected) {
		t.Fatalf("automation patch = %#v, want only lifecycle scripts", automation)
	}
	for field, want := range expected {
		if got := automation[field]; got != want {
			t.Errorf("automation.%s = %#v, want %q", field, got, want)
		}
	}
}

func TestAutomationApplyPreservesUntouchedLifecycleScripts(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	cfg := transport.cfg
	cfg.Automation.LockScript = `C:\Existing\lock.ps1`
	cfg.Automation.UnlockScript = `C:\Existing\unlock.ps1`
	cfg.Automation.SuspendScript = `C:\Existing\suspend.ps1`
	cfg.Automation.ResumeScript = `C:\Existing\resume.ps1`
	cfg.Automation.NetworkChangeScript = `C:\Existing\network.ps1`
	transport.cfg = cfg

	tuiModel.settings.mode = settingsEditing
	tuiModel.settings.section = settingsAutomation
	tuiModel.settings.cfg = cfg
	tuiModel.settings.original = cfg
	tuiModel.settings.draft = newSettingsDraft(cfg)
	tuiModel.settings.draft.cfg.Automation.LockScript = `C:\Updated\lock.ps1`

	patch, err := tuiModel.settingsPatch()
	if err != nil {
		t.Fatal(err)
	}
	automation, ok := patch["automation"].(map[string]any)
	if !ok {
		t.Fatalf("automation patch = %#v", patch)
	}
	if len(automation) != 1 ||
		automation["lock_script"] != `C:\Updated\lock.ps1` {
		t.Fatalf("automation patch = %#v, want only updated lock_script", automation)
	}
	for _, preserved := range []string{
		"unlock_script",
		"suspend_script",
		"resume_script",
		"network_change_script",
	} {
		if _, changed := automation[preserved]; changed {
			t.Errorf("untouched automation.%s was included in patch", preserved)
		}
	}
}

func TestAdvancedSettingsMasksTokenStructurally(t *testing.T) {
	cfg := config.Default()
	cfg.Integration.LocalAPIToken = "same-value-in-an-earlier-field"
	cfg.Automation.OnScript = cfg.Integration.LocalAPIToken

	draft := newSettingsDraft(cfg)
	var rendered map[string]any
	if err := json.Unmarshal([]byte(draft.advancedJSON), &rendered); err != nil {
		t.Fatal(err)
	}
	automation := rendered["automation"].(map[string]any)
	integration := rendered["integration"].(map[string]any)
	if automation["on_script"] != cfg.Automation.OnScript {
		t.Fatalf("unrelated matching value was masked: %#v", automation["on_script"])
	}
	if integration["local_api_token"] != preservedTokenMarker {
		t.Fatalf("integration token was not masked: %#v", integration["local_api_token"])
	}
}

func TestSettingsWorkspaceLoadsTypedConfiguration(t *testing.T) {
	tuiModel, _ := newWorkspaceModel(t)
	command := tuiModel.openSettingsAt(settingsDevice)
	if command == nil || tuiModel.settings.mode != settingsLoading {
		t.Fatal("settings workspace did not begin loading")
	}
	message := command()
	_, initCommand := tuiModel.Update(message)
	if initCommand == nil {
		t.Fatal("settings editor did not initialize its form")
	}
	if tuiModel.settings.mode != settingsEditing ||
		tuiModel.settings.section != settingsDevice {
		t.Fatalf(
			"settings state = (%v, %q)",
			tuiModel.settings.mode,
			tuiModel.settings.section,
		)
	}
	if content := tuiModel.View().Content; !strings.Contains(content, "DEVICE CONNECTION") {
		t.Fatalf("settings view omitted section title:\n%s", content)
	}
}

func TestCommandWorkspaceControlsAppAndServing(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	detail, quit, err := tuiModel.executePaletteCommand(
		context.Background(),
		commandAppShow,
	)
	if err != nil || quit || !strings.Contains(detail, "shown") {
		t.Fatalf("app show = (%q, %v, %v)", detail, quit, err)
	}
	if len(transport.commands) != 1 ||
		transport.commands[0] != "window.show" {
		t.Fatalf("commands = %v", transport.commands)
	}

	detail, quit, err = tuiModel.executePaletteCommand(
		context.Background(),
		commandServeDisable,
	)
	if err != nil || quit || !strings.Contains(detail, "disabled") {
		t.Fatalf("serve disable = (%q, %v, %v)", detail, quit, err)
	}
	if transport.cfg.Integration.LocalAPIEnabled {
		t.Fatal("serve disable did not patch coordinator configuration")
	}

	detail, quit, err = tuiModel.executePaletteCommand(
		context.Background(),
		commandAppAudio,
	)
	if err != nil || quit || !strings.Contains(detail, "Audible cue") {
		t.Fatalf("audio test = (%q, %v, %v)", detail, quit, err)
	}
	if got := transport.commands[len(transport.commands)-1]; got != "audio.play" {
		t.Fatalf("audio command = %q, want audio.play", got)
	}
}

func TestDeviceWorkspaceIgnoresUnknownCredentialFields(t *testing.T) {
	tuiModel, transport := newWorkspaceModel(t)
	snapshot := deviceconfig.Snapshot{
		DeviceURL:  config.DefaultBaseURL,
		DeviceName: "PCController",
		Hostname:   "pccontroller-desktop",
		Station: deviceconfig.Station{
			SSID: "Office",
			IP:   "192.0.2.20",
		},
		Capabilities: deviceconfig.Capabilities{HTTPRead: true},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown fields are ignored by the typed decoder. Even a compromised
	// peer cannot cause a credential-shaped extension to enter the view.
	data = append(data[:len(data)-1], []byte(`,"password":"NEVER-RENDER-ME"}`)...)
	transport.results["device.config.get"] = data

	command := tuiModel.openDeviceSettings()
	if command == nil {
		t.Fatal("device workspace did not begin loading")
	}
	_, initCommand := tuiModel.Update(command())
	if initCommand == nil || tuiModel.device.mode != deviceReady {
		t.Fatal("device workspace did not become ready")
	}
	content := tuiModel.View().Content
	if strings.Contains(content, "NEVER-RENDER-ME") {
		t.Fatalf("device view leaked a credential field:\n%s", content)
	}
	if !strings.Contains(content, "Office") ||
		!strings.Contains(content, "192.0.2.20") {
		t.Fatalf("device view omitted typed station values:\n%s", content)
	}
}

func TestDeviceWorkspaceIsCompleteAndScrollableAt80x24(t *testing.T) {
	tuiModel, transport := newWorkspaceModelAt(
		t,
		ScreenDashboard,
		80,
		24,
	)
	yes := true
	uptime := int64(7265)
	snapshot := deviceconfig.Snapshot{
		DeviceURL:  config.DefaultBaseURL,
		DeviceName: "Unit-80",
		Hostname:   "host-80",
		ModelName:  "Model-80",
		Station: deviceconfig.Station{
			SSID:      "Office-80",
			Status:    "associated-80",
			DHCP:      &yes,
			IP:        "192.0.2.80",
			Gateway:   "192.0.2.1",
			DNS:       "192.0.2.53",
			Connected: &yes,
		},
		AccessPoint: deviceconfig.AccessPoint{
			SSID:    "AP-80",
			Status:  "enabled-80",
			IP:      "10.10.10.1",
			Netmask: "255.255.255.0",
		},
		Firmware: deviceconfig.Firmware{
			Version:          "v80.0",
			Hash:             "hash-80",
			Build:            "build-80",
			UptimeSeconds:    &uptime,
			ProtocolRevision: "protocol-80",
		},
		Capabilities: deviceconfig.Capabilities{
			HTTPRead: true,
		},
		UpdatedAt: time.Date(2026, 7, 24, 8, 24, 0, 0, time.UTC),
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	transport.results["device.config.get"] = data

	command := tuiModel.openDeviceSettings()
	if command == nil {
		t.Fatal("device workspace did not begin loading")
	}
	_, initCommand := tuiModel.Update(command())
	if initCommand == nil || tuiModel.device.mode != deviceReady {
		t.Fatal("device workspace did not become ready")
	}

	var visited strings.Builder
	for page := 0; page < 16; page++ {
		content := tuiModel.View().Content
		visited.WriteString(content)
		visited.WriteByte('\n')
		if got := lipgloss.Width(content); got > 80 {
			t.Fatalf("device workspace width = %d, want <= 80", got)
		}
		if got := lipgloss.Height(content); got > 24 {
			t.Fatalf("device workspace height = %d, want <= 24", got)
		}
		if !strings.Contains(content, "Refresh all audited") {
			t.Fatalf("action selector disappeared on page %d:\n%s", page, content)
		}
		if tuiModel.device.viewport.AtBottom() {
			break
		}
		_, _ = tuiModel.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !tuiModel.device.viewport.AtBottom() {
		t.Fatal("Page Down could not reach the end of device information")
	}

	allPages := visited.String()
	for _, value := range []string{
		"Unit-80",
		"host-80",
		"Model-80",
		"Office-80",
		"associated-80",
		"192.0.2.80",
		"192.0.2.1",
		"AP-80",
		"enabled-80",
		"10.10.10.1",
		"v80.0",
		"hash-80",
		"build-80",
		"protocol-80",
	} {
		if !strings.Contains(allPages, value) {
			t.Errorf("scrollable 80x24 view never exposed %q", value)
		}
	}
	if content := tuiModel.View().Content; !strings.Contains(content, "Updated") {
		t.Fatalf("final metadata is not reachable at the bottom:\n%s", content)
	}
}
