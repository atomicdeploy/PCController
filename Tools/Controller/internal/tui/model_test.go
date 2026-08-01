package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func readyModel(t *testing.T, page Page) Model {
	t.Helper()
	model := RichPreviewModel(false)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 38})
	model = updated.(Model)
	model.page = page
	return model
}

func TestPreviewFramesCoverEveryDomainPage(t *testing.T) {
	expected := map[Page]string{
		PageDashboard:     "LIVE MEASUREMENTS",
		PageOutputs:       "OUTPUT CONTROL",
		PageMenus:         "DISPLAY MENU MIRROR",
		PageBoardSettings: "BOARD EEPROM SETTINGS",
		PageAppSettings:   "PC HOST SETTINGS",
		PageRF:            "433 MHz RF",
		PageProgramming:   "PROGRAMMING",
		PageAutomations:   "AUTOMATIONS & MACROS",
		PageEvents:        "24-HOUR HISTORY",
		PageConsole:       "command console",
	}
	for page, needle := range expected {
		rendered := PreviewFrame(page, 132, 38)
		if !strings.Contains(rendered, needle) {
			t.Errorf("page %d missing %q:\n%s", page, needle, rendered)
		}
		if !strings.Contains(rendered, "PREVIEW") {
			t.Errorf("page %d does not identify safe injected preview", page)
		}
	}
}

func TestDashboardUsesExpandedNamesAndAdaptiveUnits(t *testing.T) {
	rendered := PreviewFrame(PageDashboard, 132, 38)
	for _, expected := range []string{
		"Supply Voltage", "12.22 V", "Load Current", "286.0 mA",
		"Load Power", "3.49 W", "Temperature · Illumination LED",
		"Temperature · BT Audio", "BT Audio · disconnected /",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("dashboard missing %q:\n%s", expected, rendered)
		}
	}
}

func TestRFPageUsesOneRadixStableMetadataAndFixedPalette(t *testing.T) {
	model := readyModel(t, PageRF)
	model.rfValue = appconfig.RFConfig{
		DisplayRadix: "decimal",
		Categories:   []appconfig.RFCategory{{Name: "Motion", Color: "red"}},
		Metadata: []appconfig.RFMetadata{{
			Key:  appconfig.RFCodeKey{Code: 1_381_717, Bits: 24, Protocol: 1},
			Name: "Side A Up", Category: "Motion",
		}},
	}
	rendered := model.View()
	for _, expected := range []string{
		"1381717", "Side A Up", "Motion", "(code, bits, protocol)",
		"red · blue · violet/purple · green · white",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("RF page missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "0x00151555") {
		t.Fatalf("RF page mixed hexadecimal into decimal presentation:\n%s", rendered)
	}
}

func TestRFActionPickerSearchesAndMapsSelectedID(t *testing.T) {
	model := readyModel(t, PageRF)
	model.beginRFActionPicker()
	model.rfActionQuery = "motion side b down"
	matches := model.filteredRFActions()
	if len(matches) != 1 || matches[0].Args != "side B down" {
		t.Fatalf("filtered actions=%#v", matches)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("action picker did not dispatch mapping")
	}
	found := false
	for _, line := range model.logs {
		if strings.Contains(line, "rf map 0 side B down") {
			found = true
		}
	}
	if !found {
		t.Fatalf("mapping command missing from log: %#v", model.logs)
	}
}

func TestRFReorderIsStagedSortedAndCapabilityGated(t *testing.T) {
	model := readyModel(t, PageRF)
	model.rfApplyOrder, model.rfFetch = nil, nil
	model.rfReplaceSupport, model.rfProbeReplace = nil, nil
	originalCode := model.rfStaged[0].Code
	model.moveRFStage(1)
	if !model.rfStageDirty || model.rfStaged[1].Code != originalCode {
		t.Fatalf("stage not moved: %#v", model.rfStaged)
	}
	for index, entry := range model.rfStaged {
		if entry.ID != byte(index) {
			t.Fatalf("staged list not ID-sorted at %d: %#v", index, model.rfStaged)
		}
	}
	model.rfReview = true
	updated, command, handled := model.pageShortcut("g")
	if !handled || command != nil || !strings.Contains(updated.notice, "opcode is unavailable") {
		t.Fatalf("unsupported apply was not visibly gated: handled=%t command=%v notice=%q", handled, command, updated.notice)
	}
}

func TestRFNameAndRadixPersistPCSideOnly(t *testing.T) {
	snapshot := RichPreviewSnapshot()
	config := appconfig.DefaultRFConfig()
	var saved appconfig.RFConfig
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		Preview: &snapshot, DisableWelcome: true,
		RFConfig: func() appconfig.RFConfig { return config },
		SaveRF:   func(value appconfig.RFConfig) error { saved = value; config = value; return nil },
	})
	model.beginRFNameEdit()
	model.input.SetValue("Main remote A")
	_, handled := model.finishRFEdit()
	if !handled || len(saved.Metadata) != 1 || saved.Metadata[0].Name != "Main remote A" {
		t.Fatalf("saved metadata=%#v", saved.Metadata)
	}
	model.toggleRFRadix()
	if saved.DisplayRadix != "decimal" {
		t.Fatalf("saved radix=%q", saved.DisplayRadix)
	}
	key := saved.Metadata[0].Key
	if key.Code != 1_381_717 || key.Bits != 24 || key.Protocol != 1 {
		t.Fatalf("metadata key drifted from stable tuple: %#v", key)
	}
}

func TestDashboardAndProgrammingExposeUptimeAndGuardedFlash(t *testing.T) {
	dashboard := PreviewFrame(PageDashboard, 132, 38)
	if !strings.Contains(dashboard, "Device Uptime") || !strings.Contains(dashboard, "1h13m12s") {
		t.Fatalf("polished uptime missing:\n%s", dashboard)
	}
	programming := PreviewFrame(PageProgramming, 160, 46)
	for _, expected := range []string{"U Safe flash", "backup flash + EEPROM + metadata", "content-addressed SHA-256", "USBasp is hidden troubleshooting"} {
		if !strings.Contains(programming, expected) {
			t.Errorf("programming page missing %q:\n%s", expected, programming)
		}
	}
}

func TestHostedMenuPreviewAndLivePWMRemainBoardAuthoritative(t *testing.T) {
	rendered := PreviewFrame(PageMenus, 150, 48)
	for _, expected := range []string{"PC-OWNED MENUS", "ACTIVE · PC Host / Host status", "physical / virtual host keys"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("hosted menu preview missing %q:\n%s", expected, rendered)
		}
	}

	model := New(control.New(control.Options{}), shell.New(10))
	model.page, model.cursor = PageOutputs, 15
	model.pwmValues[0] = 123
	updated, _, _ := model.setSelectedPWM(2048)
	if updated.pwmValues[0] != 123 {
		t.Fatalf("live PWM changed optimistically to %d", updated.pwmValues[0])
	}
	if !outputCommandNeedsReadback("pwm set 0 2048") || outputCommandNeedsReadback("pwm get") {
		t.Fatal("PWM readback trigger is incorrect")
	}
}

func TestNestedTabAndRightArrowCompletion(t *testing.T) {
	model := readyModel(t, PageConsole)
	model.input.SetValue("silent sta")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if got := model.input.Value(); got != "silent status " {
		t.Fatalf("nested Tab completion=%q", got)
	}
	for _, line := range model.logs {
		if strings.Contains(line, "completion:") {
			t.Fatalf("completion leaked into transcript: %q", line)
		}
	}

	model.input.SetValue("relay side l")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
	if got := model.input.Value(); got != "relay side left " {
		t.Fatalf("right completion=%q", got)
	}
}

func TestRightArrowRecallsPlaceholderCommand(t *testing.T) {
	engine := shell.New(10)
	if err := engine.Register(shell.Command{
		Name: "status", Usage: "status", Summary: "test",
		Run: func(context.Context, []string) (string, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), "status"); err != nil {
		t.Fatal(err)
	}
	model := NewPreview(engine, RichPreviewSnapshot(), false)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.page = PageConsole
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := updated.(Model).input.Value(); got != "status" {
		t.Fatalf("right-arrow recall=%q", got)
	}
}

func TestKeyboardAndMousePageNavigation(t *testing.T) {
	model := readyModel(t, PageDashboard)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	model = updated.(Model)
	if model.page != PageOutputs {
		t.Fatalf("numeric navigation page=%d", model.page)
	}

	model.page = PageDashboard
	// Header is row 0, action buttons rows 1..3, tabs start at row 4.
	firstTabWidth := len("1 Dashboard") + 2
	updated, _ = model.Update(tea.MouseMsg{
		X: firstTabWidth + 2, Y: 4,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if got := updated.(Model).page; got != PageOutputs {
		t.Fatalf("mouse tab navigation page=%d", got)
	}
}

func TestExternalAppPageActionSelectsTUIPage(t *testing.T) {
	model := readyModel(t, PageDashboard)
	updated, _ := model.Update(appActionMsg(hostui.AppAction{
		Kind: "app.page", Value: "events", Source: "test",
	}))
	if got := updated.(Model).page; got != PageEvents {
		t.Fatalf("external app.page selected %v, want events", got)
	}
}

func TestMouseOutputToggleUpdatesInjectedState(t *testing.T) {
	model := readyModel(t, PageOutputs)
	before := model.preview.Status.ActiveRelays
	updated, command := model.Update(tea.MouseMsg{
		X: 10, Y: 4 + strings.Count(model.tabBar(), "\n") + 1 + 2,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.preview.Status.ActiveRelays == before {
		t.Fatal("relay mouse click did not toggle injected state")
	}
	if command == nil {
		t.Fatal("relay mouse click did not produce command result")
	}
}

func TestAppSettingsPersistThroughSaveHook(t *testing.T) {
	defaults := appconfig.Defaults().UI
	saved := make(chan appconfig.UI, 1)
	runtime := control.New(control.Options{})
	model := NewWithOptions(runtime, shell.New(10), Options{
		UIConfig:       func() appconfig.UI { return defaults },
		SaveUI:         func(value appconfig.UI) error { saved <- value; return nil },
		Preview:        func() *control.Snapshot { value := RichPreviewSnapshot(); return &value }(),
		DisableWelcome: true,
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	model = updated.(Model)
	model.page = PageAppSettings
	model.cursor = 9 // Show current
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	select {
	case value := <-saved:
		if value.ShowCurrent == defaults.ShowCurrent {
			t.Fatal("visibility was not toggled in persisted UI config")
		}
	case <-time.After(time.Second):
		t.Fatal("save hook was not called")
	}
}

func TestConfiguredProductTitleAppearsAndHotReloadsInTUI(t *testing.T) {
	t.Setenv("PCCONTROLLER_APP_TITLE", "")
	ui := appconfig.Defaults().UI
	ui.AppTitle = "Workshop Controller"
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		UIConfig: func() appconfig.UI { return ui }, DisableWelcome: true,
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	model = updated.(Model)
	if rendered := ansi.Strip(model.View()); !strings.Contains(rendered, "◆ Workshop Controller") {
		t.Fatalf("configured title missing from TUI header:\n%s", rendered)
	}
	ui.AppTitle = "Live Control Desk"
	model.syncUIConfig(ui)
	if rendered := ansi.Strip(model.View()); !strings.Contains(rendered, "◆ Live Control Desk") {
		t.Fatalf("hot-reloaded title missing from TUI header:\n%s", rendered)
	}
}

func TestRawHelloFramesHiddenOutsideDebug(t *testing.T) {
	model := readyModel(t, PageConsole)
	before := len(model.logs)
	updated, _ := model.Update(runtimeEventMsg(control.Event{
		Kind: "rx", Text: "HELLO seq=9 payload=01 02 03",
		Frame: structFrameHello(),
	}))
	model = updated.(Model)
	if len(model.logs) != before {
		t.Fatalf("HELLO raw payload appended outside debug: %#v", model.logs[before:])
	}
}

func TestFirstRunSetupCannotBeSkippedBeforeHandshake(t *testing.T) {
	model := RichPreviewModel(true)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	if !strings.Contains(model.View(), "One host. Every board surface.") {
		t.Fatal("first-run animation frame missing")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.(Model).welcome {
		t.Fatal("setup was skipped before controller/audio readiness")
	}
}

func TestApplicationWelcomePersistsCanonicalSetupComplete(t *testing.T) {
	ui := appconfig.Defaults().UI
	ui.SetupComplete = false
	saved := make(chan appconfig.UI, 1)
	model := NewApplication(
		control.New(control.Options{}),
		shell.New(10),
		func() appconfig.UI { return ui },
		func(value appconfig.UI) error { saved <- value; return nil },
	)
	if !model.welcome {
		t.Fatal("new setup did not show welcome")
	}
	model.welcomeCanContinue = true // bounded timeout/offline acknowledgement
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(Model).welcome {
		t.Fatal("welcome did not finish")
	}
	select {
	case value := <-saved:
		if !value.SetupComplete {
			t.Fatal("setup_complete was not persisted")
		}
	case <-time.After(time.Second):
		t.Fatal("welcome completion did not call config saver")
	}
}

func TestApplicationWithOptionsRetainsHostIntegrations(t *testing.T) {
	ui := appconfig.Defaults().UI
	ui.SetupComplete = true
	notifier := hostui.NewNotifier(hostui.NotifierOptions{AppID: "PCController.Tests"})
	want := hostui.IntegrationStatus{
		Messaging: hostui.ServiceStatus{Name: "Text messaging", Enabled: true, State: "ready"},
	}
	customMirrorCalls := 0
	model := NewApplicationWithOptions(
		control.New(control.Options{}),
		shell.New(10),
		Options{
			UIConfig:     func() appconfig.UI { return ui },
			Integrations: func() hostui.IntegrationStatus { return want },
			Notifier:     notifier,
			MirrorLCD: func(_, _ string) error {
				customMirrorCalls++
				return nil
			},
		},
	)
	if model.welcome {
		t.Fatal("completed setup unexpectedly showed welcome")
	}
	if model.integrations == nil || model.integrations().Messaging.State != "ready" {
		t.Fatal("integration status provider was not retained")
	}
	if model.notifier != notifier {
		t.Fatal("notification adapter was not retained")
	}
	if err := model.mirrorLCD("line one", "line two"); err != nil {
		t.Fatalf("custom LCD mirror: %v", err)
	}
	if customMirrorCalls != 1 {
		t.Fatalf("custom LCD mirror calls=%d", customMirrorCalls)
	}
}

func TestClearCommandClearsTranscript(t *testing.T) {
	model := readyModel(t, PageConsole)
	model.input.SetValue("clear")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := len(updated.(Model).logs); got != 0 {
		t.Fatalf("clear left %d transcript entries", got)
	}
}

func TestPreviewUsesCurrentConfigDefaults(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	if model.prefs.CurrentDecimals != 1 || model.prefs.EventLogLimit != 2000 {
		t.Fatalf("preview defaults current decimals=%d event limit=%d", model.prefs.CurrentDecimals, model.prefs.EventLogLimit)
	}
	if model.preview.Settings.DefaultPage != 0 || model.preview.Status.MenuPage != 0 {
		t.Fatalf("preview did not use current Status page 0: settings=%d active=%d", model.preview.Settings.DefaultPage, model.preview.Status.MenuPage)
	}
}

func TestDashboardMapsProgramModeToHumanSubmode(t *testing.T) {
	rendered := PreviewFrame(PageDashboard, 132, 38)
	if !strings.Contains(rendered, "Status") || !strings.Contains(rendered, "Menu / Submode") {
		t.Fatalf("human submode missing:\n%s", rendered)
	}
}

func TestBorderedPageButtonsShareHorizontalRow(t *testing.T) {
	for _, page := range []Page{PageMenus, PageRF, PageProgramming, PageAutomations} {
		rendered := PreviewFrame(page, 160, 44)
		lines := strings.Split(rendered, "\n")
		found := false
		for _, line := range lines {
			if page == PageMenus && strings.Contains(line, "K1 · previous") && strings.Contains(line, "K4 · increase") {
				found = true
			}
			if page == PageRF && strings.Contains(line, "Learn indefinite") && strings.Contains(line, "Refresh list") {
				found = true
			}
			if page == PageProgramming && strings.Contains(line, "Urclock probe") && strings.Contains(line, "Metadata") {
				found = true
			}
			if page == PageAutomations && strings.Contains(line, "N New") && strings.Contains(line, "P Play") {
				found = true
			}
		}
		if !found {
			t.Errorf("page %d buttons are not a clean horizontal group:\n%s", page, rendered)
		}
	}
}

func TestPortPickerIsVisibleAndNeverEnumeratesInPreview(t *testing.T) {
	model := readyModel(t, PageDashboard)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	model = updated.(Model)
	if command != nil {
		t.Fatal("preview port picker unexpectedly scheduled OS enumeration")
	}
	rendered := model.View()
	if !strings.Contains(rendered, "SELECT SERIAL DEVICE") || !strings.Contains(rendered, "VIRTUAL") {
		t.Fatalf("port picker missing:\n%s", rendered)
	}
}

func TestFrontPanelPreviewAndOfflineLCD(t *testing.T) {
	rendered := PreviewFrame(PageMenus, 160, 46)
	for _, expected := range []string{"4-DIGIT DISPLAY", "2×16 LCD", "PCController", "K1 · previous", "active 0 · Status"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("front panel missing %q:\n%s", expected, rendered)
		}
	}
	snapshot := RichPreviewSnapshot()
	snapshot.Connected = false
	model := NewPreview(shell.New(10), snapshot, false)
	state := model.currentFrontPanel(snapshot)
	if !strings.Contains(state.LCDLine1, "PC offline") || !strings.Contains(state.LCDLine2, "Connect USB toPC") {
		t.Fatalf("offline LCD=%q/%q", state.LCDLine1, state.LCDLine2)
	}
	if page := model.dashboardPage(snapshot); !strings.Contains(page, "offline · physical contents unverified") {
		t.Fatalf("dashboard claimed a live LCD while disconnected:\n%s", page)
	}
}

func TestFrontPanelRawSegmentsOverrideTextGlyphs(t *testing.T) {
	state := FrontPanelState{
		Segments: "8888", RawSegments: [4]byte{segG, segG | 0x80, segA, segD},
		HasRawSegments: true, LCDLine1: "one", LCDLine2: "two", Exact: true,
	}
	rendered := renderFrontPanel(state)
	if !strings.Contains(rendered, "●") {
		t.Fatalf("raw decimal bit not rendered:\n%s", rendered)
	}
	if strings.Count(rendered, "┃") != 0 {
		t.Fatalf("text glyphs overrode raw masks:\n%s", rendered)
	}
}

func TestPreviewHeaderAndFrontPanelFit160Columns(t *testing.T) {
	for _, page := range []Page{PageDashboard, PageMenus, PageAppSettings, PageRF, PageProgramming} {
		rendered := PreviewFrame(page, 160, 46)
		for index, line := range strings.Split(rendered, "\n") {
			if width := lipgloss.Width(line); width > 160 {
				t.Errorf("page %d line %d width=%d:\n%s", page, index, width, line)
			}
		}
	}
	menu := PreviewFrame(PageMenus, 160, 46)
	if !strings.Contains(menu, "K4 · increase") {
		t.Fatalf("K4 card clipped:\n%s", menu)
	}
}

func TestPrimaryTablesFitRepresentativeNarrowAndWideWidths(t *testing.T) {
	for _, width := range []int{88, 120, 132, 160} {
		for _, page := range []Page{PageDashboard, PageOutputs, PageBoardSettings, PageAppSettings} {
			rendered := PreviewFrame(page, width, 46)
			for index, line := range strings.Split(rendered, "\n") {
				if actual := lipgloss.Width(line); actual > width {
					t.Errorf("page %d width %d line %d overflowed to %d cells:\n%s", page, width, index, actual, line)
				}
			}
		}
	}

	dashboard := PreviewFrame(PageDashboard, 132, 38)
	foundPair := false
	for _, line := range strings.Split(dashboard, "\n") {
		if strings.Count(line, "╭") == 2 {
			foundPair = true
			if width := lipgloss.Width(line); width > 132 {
				t.Fatalf("dashboard card pair width=%d:\n%s", width, line)
			}
		}
	}
	if !foundPair {
		t.Fatalf("wide dashboard did not render a two-card row:\n%s", dashboard)
	}
}

func TestDashboardLongValuesWrapInsideTheirValueColumn(t *testing.T) {
	row := ansi.Strip(kvCard(55, 22, "Bluetooth", "BT Audio · disconnected / pairing (blinking indicator)"))
	lines := strings.Split(row, "\n")
	if len(lines) < 2 {
		t.Fatalf("representative long value did not wrap: %q", row)
	}
	for index, line := range lines[1:] {
		leading := lipgloss.Width(line) - lipgloss.Width(strings.TrimLeft(line, " "))
		if leading != 23 {
			t.Fatalf("wrapped continuation %d starts at cell %d, want 23: %q", index, leading, line)
		}
		if width := lipgloss.Width(line); width != 55 {
			t.Fatalf("wrapped continuation %d width=%d, want 55: %q", index, width, line)
		}
	}
}

func TestSectionHeadersAreCenteredAtNarrowAndWideWidths(t *testing.T) {
	for _, width := range []int{72, 128} {
		header := sectionHeader(width, "PC HOST SETTINGS", "saved in host JSON · never board EEPROM")
		if actual := lipgloss.Width(header); actual != width {
			t.Fatalf("header width=%d, want %d", actual, width)
		}
		left := len(header) - len(strings.TrimLeft(header, " "))
		right := len(header) - len(strings.TrimRight(header, " "))
		if difference := left - right; difference < -1 || difference > 1 {
			t.Fatalf("header not centered at width %d: left=%d right=%d", width, left, right)
		}
	}
}

func TestTableColumnsUseVisibleCellPadding(t *testing.T) {
	rows := []string{
		ansi.Strip(kv("A", "first")),
		ansi.Strip(kv("Temperature · Illumination LED", "second")),
		settingsRow("Short", "third"),
		settingsRow("Motion allowed by door state", "fourth"),
	}
	for index, row := range rows[:2] {
		value := []string{"first", "second"}[index]
		if column := lipgloss.Width(row[:strings.Index(row, value)]); column != 34 {
			t.Fatalf("key/value row %d starts value at column %d, want 34: %q", index, column, row)
		}
	}
	for index, row := range rows[2:] {
		value := []string{"third", "fourth"}[index]
		if column := lipgloss.Width(row[:strings.Index(row, value)]); column != 39 {
			t.Fatalf("settings row %d starts value at column %d, want 39: %q", index, column, row)
		}
	}
}

func TestBoardPageShowsSafetyAndAudioCueSettings(t *testing.T) {
	rendered := PreviewFrame(PageBoardSettings, 132, 42)
	for _, expected := range []string{"Motion allowed by door state", "Door open/close audio cues", "Relay on/off audio cues"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("board settings missing %q:\n%s", expected, rendered)
		}
	}
}

func TestAutomationPageShowsHostPlatformAndBridgeStatus(t *testing.T) {
	rendered := PreviewFrame(PageAutomations, 160, 46)
	for _, expected := range []string{"HOST PLATFORM & BRIDGES", "Global hotkeys", "Desktop toasts", "Text messaging", "Device discovery", "Webhooks", "Socket.IO", "Ctrl+Alt+P"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("integration status missing %q:\n%s", expected, rendered)
		}
	}
	if !strings.Contains(rendered, "actions require registered") {
		t.Fatal("toast activation limitation is not visible")
	}
}

func TestAutomationPageProvidesCompleteMacroWorkspace(t *testing.T) {
	rendered := PreviewFrame(PageAutomations, 160, 46)
	for _, expected := range []string{
		"MACRO LIBRARY", "output-demo", "door-notify", "PLAYBACK",
		"Elapsed / Duration", "buffer 42/127 B", "accepted 95 B",
		"last +267 µs", "faithful pending", "RECORDING",
		"N New", "R Record", "C Cancel off", "K Cancel keep",
		"HOST PLATFORM & BRIDGES",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("macro workspace missing %q:\n%s", expected, rendered)
		}
	}
	for _, width := range []int{88, 120, 160} {
		rendered = PreviewFrame(PageAutomations, width, 46)
		for lineNumber, line := range strings.Split(rendered, "\n") {
			if cells := ansi.StringWidth(line); cells > width {
				t.Errorf("width %d line %d uses %d cells: %q", width, lineNumber+1, cells, line)
			}
		}
	}
}

func TestAutomationTableHeadersAreCenteredInExactDataColumns(t *testing.T) {
	for _, width := range []int{88, 120, 160} {
		nameWidth, categoryWidth := macroColumnWidths(width)
		plain := ansi.Strip(macroTableHeader(width))
		columns := []struct {
			label string
			start int
			width int
		}{
			{label: "ID", start: 2, width: 3},
			{label: "NAME", start: 6, width: nameWidth},
			{label: "CATEGORY", start: 7 + nameWidth, width: categoryWidth},
			{label: "COLOR", start: 8 + nameWidth + categoryWidth, width: 8},
			{label: "STEPS", start: 17 + nameWidth + categoryWidth, width: 5},
			{label: "DURATION", start: 23 + nameWidth + categoryWidth, width: 10},
		}
		for _, column := range columns {
			end := column.start + column.width
			if end > len(plain) {
				t.Fatalf("width %d column %s exceeds header %q", width, column.label, plain)
			}
			cell := plain[column.start:end]
			labelAt := strings.Index(cell, column.label)
			if labelAt < 0 {
				t.Fatalf("width %d column %s absent from exact cell %q in %q", width, column.label, cell, plain)
			}
			left := lipgloss.Width(cell[:labelAt])
			right := column.width - left - lipgloss.Width(column.label)
			if difference := left - right; difference < 0 || difference > 1 {
				t.Errorf("width %d column %s is not visibly centered: left=%d right=%d cell=%q", width, column.label, left, right, cell)
			}
		}
		expectedWidth := 33 + nameWidth + categoryWidth
		if actual := lipgloss.Width(plain); actual != expectedWidth {
			t.Errorf("width %d header consumes %d cells, want exact row geometry %d: %q", width, actual, expectedWidth, plain)
		}
	}
}

func TestAutomationSearchAndKeyboardLifecycle(t *testing.T) {
	model := readyModel(t, PageAutomations)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	if !model.macroSearchEditing {
		t.Fatal("slash did not enter macro search")
	}
	for _, value := range "door" {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
		model = updated.(Model)
	}
	matches := model.filteredMacros(model.macroLibrary())
	if len(matches) != 1 || matches[0].Name != "door-notify" {
		t.Fatalf("search matches=%#v", matches)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.macroSearchEditing {
		t.Fatal("enter did not finish macro search")
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = updated.(Model)
	if command == nil || !logsContain(model.logs, "macro play 2") {
		t.Fatalf("play did not dispatch selected filtered macro: logs=%#v", model.logs)
	}

	model.macroSearch = ""
	model.cursor = 0
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	if got := model.input.Value(); got != "macro create 0 " {
		t.Fatalf("new macro prompt=%q", got)
	}
	model.input.SetValue("")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if got := model.input.Value(); got != "macro record start " {
		t.Fatalf("record prompt=%q", got)
	}
}

func TestAutomationDeleteRequiresTwoExplicitPresses(t *testing.T) {
	model := readyModel(t, PageAutomations)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(Model)
	if command != nil || !model.macroDeleteArmed || model.macroDeleteReference != "1/output-demo" {
		t.Fatalf("first delete press state=%+v command=%v", model, command)
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(Model)
	if command == nil || model.macroDeleteArmed || !logsContain(model.logs, "macro delete 1") {
		t.Fatalf("second delete press did not dispatch: armed=%v logs=%#v", model.macroDeleteArmed, model.logs)
	}
}

func TestAutomationDeleteConfirmationIsCancelledByAnotherAction(t *testing.T) {
	model := readyModel(t, PageAutomations)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(Model)
	if !model.macroDeleteArmed {
		t.Fatal("delete was not armed")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.macroDeleteArmed || model.macroDeleteReference != "" {
		t.Fatalf("navigation did not cancel delete confirmation: %+v", model)
	}
}

func TestAutomationLifecycleButtonsDispatchEveryRecorderAndCancelPolicy(t *testing.T) {
	tests := []struct {
		key       string
		command   string
		recording bool
		playing   bool
	}{
		{key: "s", command: "macro record save", recording: true},
		{key: "d", command: "macro record discard", recording: true},
		{key: "c", command: "macro cancel", playing: true},
		{key: "k", command: "macro cancel keep", playing: true},
		{key: "i", command: "macro show 1"},
		{key: "a", command: "automation list"},
		{key: "m", command: "macro list"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			model := readyModel(t, PageAutomations)
			model.previewMacroRecording.Active = test.recording
			model.previewMacroState.Running = test.playing
			updated, command, handled := model.macroShortcut(test.key)
			if !handled || command == nil || !logsContain(updated.logs, test.command) {
				t.Fatalf("key %q handled=%v command=%v logs=%#v", test.key, handled, command, updated.logs)
			}
		})
	}
}

func TestAutomationMouseButtonsAndLibrarySelection(t *testing.T) {
	model := readyModel(t, PageAutomations)
	recordX := lipgloss.Width(buttonStyle.Render("N New")) + 1
	updated, command := model.handleContentClick(1, recordX)
	model = updated.(Model)
	if command != nil || model.input.Value() != "macro record start " {
		t.Fatalf("record mouse action command=%v input=%q", command, model.input.Value())
	}
	model.input.SetValue("")
	updated, _ = model.handleContentClick(macroLibraryFirstRow+2, 4)
	model = updated.(Model)
	if model.cursor != 2 {
		t.Fatalf("library mouse selection cursor=%d", model.cursor)
	}
}

func TestTUIUsesCommandEngineMacroRunner(t *testing.T) {
	runtime := control.New(control.Options{})
	macros := []appconfig.Macro{{ID: 9, Name: "shared-runner", Steps: []appconfig.MacroStep{{Kind: "relay", Target: 5, Value: 1}}}}
	config := appconfig.Defaults()
	config.Macros = macros
	engine := control.NewCommandEngine(runtime, control.CommandOptions{
		Macros:     func() []appconfig.Macro { return append([]appconfig.Macro(nil), macros...) },
		HostConfig: func() appconfig.Config { return config },
	})
	model := New(runtime, engine)
	model.page = PageAutomations
	if runtime.MacroRunner() == nil {
		t.Fatal("command engine did not register its macro runner")
	}
	library := model.macroLibrary()
	if len(library) != 1 || library[0].ID != 9 || library[0].Name != "shared-runner" {
		t.Fatalf("TUI did not read command engine macro runner: %#v", library)
	}
}

func logsContain(logs []string, expected string) bool {
	for _, line := range logs {
		if strings.Contains(line, expected) {
			return true
		}
	}
	return false
}

func TestFrontPanelPressAndHoldUseBackendCallback(t *testing.T) {
	var gestures []string
	snapshot := RichPreviewSnapshot()
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		Preview: &snapshot, DisableWelcome: true,
		FrontPanelKey: func(key int, phase string) error {
			gestures = append(gestures, fmt.Sprintf("K%d:%s", key, phase))
			return nil
		},
	})
	model.page = PageMenus
	_, press, _ := model.frontPanelGesture(1, "press")
	_, hold, _ := model.frontPanelGesture(1, "hold")
	_ = press()
	_ = hold()
	if got := strings.Join(gestures, ","); got != "K1:press,K1:hold" {
		t.Fatalf("front panel gestures=%q", got)
	}
}
