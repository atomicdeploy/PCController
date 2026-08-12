package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/portowner"
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
		PageOutputs:       "CONTROL",
		PageMenus:         "DISPLAY MENU MIRROR",
		PageBoardSettings: "BOARD EEPROM SETTINGS",
		PageAppSettings:   "HOST SETTINGS",
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

func TestDashboardConsumesHostPeripheralNamesForSensorsDisplaysAndPWM(t *testing.T) {
	model := readyModel(t, PageDashboard)
	model.uiValue.PeripheralNames = map[string]string{
		"sensor.supply-voltage": "Cabinet supply",
		"display.segment":       "Front readout",
		"display.lcd":           "Service LCD",
		"pwm.2":                 "Fan channel",
	}
	snapshot := RichPreviewSnapshot()
	snapshot.Status.PWMChannel = 2
	rendered := model.dashboardPage(snapshot)
	for _, expected := range []string{"Cabinet supply", "Front readout", "Service LCD", "Fan channel"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("dashboard did not consume custom peripheral name %q:\n%s", expected, rendered)
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
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "L Learn") || strings.Contains(plain, "Learn indefinite") || strings.Contains(plain, "Cancel learning") {
		t.Fatalf("idle RF learning controls are not simplified:\n%s", plain)
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

func TestGuidedRFWorkflowCapturesConfirmsAndKeepsFreshIdentityUnmapped(t *testing.T) {
	model := readyModel(t, PageRF)
	model = model.beginRFGuidedWorkflow()
	if !model.rfGuideActive || model.rfGuideStep != 0 || model.rfGuidePhase != "idle" {
		t.Fatalf("guide did not start at A: %#v", model)
	}

	updated, _, handled := model.handleRFGuidedKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated
	if !handled || model.rfGuidePhase != "capturing" {
		t.Fatalf("guided capture did not start: phase=%q handled=%t", model.rfGuidePhase, handled)
	}

	command := model.observeRFGuidedEvent(control.Event{
		Kind: "rf.learn.mapping-required", RFID: 0, HaveRFID: true,
	})
	if command == nil || model.rfGuidePhase != "resolving" || model.rfGuideAwaitID != 0 {
		t.Fatalf("capture did not request authoritative readback: phase=%q id=%d command=%v", model.rfGuidePhase, model.rfGuideAwaitID, command)
	}
	model.rfPending = false
	model.rfEntries = previewRFEntries()
	model.resetRFStage(model.rfEntries)
	model.resolveRFGuidedCandidate(model.rfEntries)
	if model.rfGuidePhase != "identity" || model.rfGuideCandidate == nil ||
		model.rfGuideCandidate.Code != 1_381_717 || model.rfGuideCandidate.Bits != 24 ||
		model.rfGuideCandidate.Protocol != 1 || model.rfGuideCandidate.PulseUS != 350 {
		t.Fatalf("exact identity was not presented: phase=%q candidate=%#v", model.rfGuidePhase, model.rfGuideCandidate)
	}

	updated, _, handled = model.handleRFGuidedKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated
	if !handled || !model.rfActionPicker || model.rfGuidePhase != "mapping" {
		t.Fatalf("identity confirmation did not open mapping review: phase=%q picker=%t", model.rfGuidePhase, model.rfActionPicker)
	}
	matches := model.filteredRFActions()
	if len(matches) != 1 || matches[0].Args != "none" {
		t.Fatalf("fresh capture did not default to Unmapped: %#v", matches)
	}

	updatedModel, executeCommand := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updatedModel.(Model)
	if executeCommand == nil || model.rfGuidePhase != "saving" || model.rfGuideMappingID != 0 {
		t.Fatalf("confirmed mapping was not dispatched: phase=%q id=%d command=%v", model.rfGuidePhase, model.rfGuideMappingID, executeCommand)
	}
	updatedModel, _ = model.Update(commandResultMsg{line: "rf map 0 none", output: "unmapped"})
	model = updatedModel.(Model)
	if model.rfGuideCaptures[0] == nil || model.rfGuideStep != 1 || model.rfGuidePhase != "idle" {
		t.Fatalf("guide did not advance from A to B: phase=%q step=%d captures=%#v", model.rfGuidePhase, model.rfGuideStep, model.rfGuideCaptures)
	}
}

func TestGuidedRFMappingPreservesOnlyAnExplicitBoardAssignment(t *testing.T) {
	model := readyModel(t, PageRF)
	existing := previewRFEntries()[1]
	model = model.beginRFGuidedWorkflow()
	model = model.beginRFGuidedMapping(existing, true)
	matches := model.filteredRFActions()
	if len(matches) != 1 || matches[0].Args != "side A up" {
		t.Fatalf("existing board mapping was not preserved: %#v", matches)
	}

	fresh := previewRFEntries()[0]
	model = model.beginRFGuidedMapping(fresh, true)
	matches = model.filteredRFActions()
	if len(matches) != 1 || matches[0].Args != "none" {
		t.Fatalf("fresh capture did not default to Unmapped: %#v", matches)
	}
	if !rfGuidedMappingCommandMatches("rf map 0 none", 0) {
		t.Fatal("the guided result matcher rejected the valid Unmapped command")
	}
}

func TestGuidedRFReviewFlagsUnmappedAndDuplicateRecords(t *testing.T) {
	entries := []native.RFEntry{
		{ID: 0, Code: 10, Bits: 24, Protocol: 1, ActionKind: native.RFActionKey},
		{ID: 1, Code: 10, Bits: 24, Protocol: 1, ActionKind: native.RFActionRelay},
		{ID: 2, Code: 20, Bits: 24, Protocol: 1, ActionKind: native.RFActionNone},
	}
	if rfGuidedRecordNeedsReview(entries[0], entries) ||
		!rfGuidedRecordNeedsReview(entries[1], entries) ||
		!rfGuidedRecordNeedsReview(entries[2], entries) {
		t.Fatalf("stale review classification is wrong: %#v", entries)
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
	for _, expected := range []string{"U Flash", "backup flash + EEPROM + metadata", "content-addressed SHA-256"} {
		if !strings.Contains(programming, expected) {
			t.Errorf("programming page missing %q:\n%s", expected, programming)
		}
	}
	for _, hidden := range []string{"Safe app reset", "Safe flash", "Advanced USBasp", "--method usbasp"} {
		if strings.Contains(programming, hidden) {
			t.Errorf("programming page exposed %q:\n%s", hidden, programming)
		}
	}
}

func TestHostedMenuPreviewAndLivePWMRemainBoardAuthoritative(t *testing.T) {
	rendered := PreviewFrame(PageMenus, 150, 48)
	for _, expected := range []string{"HOST MENUS", "ACTIVE · HOST / Host status", "physical / virtual host keys"} {
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
	model = updated.(Model)
	if got := model.input.Value(); got != "status" {
		t.Fatalf("right-arrow recall=%q", got)
	}
	model.page = PageOutputs
	model.terminalVisible = true
	model.input.SetValue("")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := updated.(Model).input.Value(); got != "status" {
		t.Fatalf("right-arrow recall from visible integrated terminal=%q", got)
	}
}

func TestCompletionViewDoesNotCorruptCandidates(t *testing.T) {
	model := readyModel(t, PageConsole)
	model.input.SetValue("relay ")
	model.applyCompletion(false)
	want := append([]string(nil), model.completion...)
	if len(want) < 2 || model.completionIndex != 0 {
		t.Fatalf("initial completion selection=%d candidates=%#v", model.completionIndex, want)
	}
	_ = model.completionView()
	_ = model.completionView()
	if !sameCompletionCandidates(model.completion, want) {
		t.Fatalf("rendering mutated completion candidates: got=%#v want=%#v", model.completion, want)
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

func TestControlPageAndTerminalVisibilityFollowNavigationContract(t *testing.T) {
	model := readyModel(t, PageOutputs)
	if pageDefinitions[PageOutputs].Short != "Control" || model.terminalIsVisible() {
		t.Fatalf("control page label/terminal state = %q/%t", pageDefinitions[PageOutputs].Short, model.terminalIsVisible())
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}})
	model = updated.(Model)
	if !model.terminalIsVisible() || !strings.Contains(ansi.Strip(model.View()), "Type a command") {
		t.Fatal("tilde did not reveal the integrated terminal")
	}
	model.input.SetValue("draft command")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}})
	model = updated.(Model)
	if model.terminalIsVisible() {
		t.Fatal("second tilde did not hide the integrated terminal")
	}
	beforeCursor := model.cursor
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if model.cursor == beforeCursor || model.input.Value() != "draft command" {
		t.Fatalf("hidden terminal blocked navigation or lost draft: cursor=%d input=%q", model.cursor, model.input.Value())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if got := updated.(Model).input.Value(); got != "draft command" {
		t.Fatalf("hidden terminal captured invisible input %q", got)
	}
}

func TestActionBarDefaultsToOnePortToggleAndShowsRebootProgress(t *testing.T) {
	model := readyModel(t, PageDashboard)
	plain := ansi.Strip(model.actionBar(model.snapshot()))
	if strings.Contains(plain, "O Open") || !strings.Contains(plain, "X Close") {
		t.Fatalf("connected default action bar did not use one Close toggle: %q", plain)
	}
	if strings.Contains(model.actionBar(model.snapshot()), buttonBadStyle.Render("R Reboot")) {
		t.Fatal("reboot action is permanently danger-colored")
	}

	model.uiValue.SeparatePortButtons = true
	plain = ansi.Strip(model.actionBar(model.snapshot()))
	if !strings.Contains(plain, "O Open") || !strings.Contains(plain, "X Close") {
		t.Fatalf("separate-port-buttons setting was ignored: %q", plain)
	}

	model.uiValue.SeparatePortButtons = false
	updated, command, _ := model.dispatchLine("reset app")
	model = updated
	if command == nil || !model.rebootPending || !strings.Contains(ansi.Strip(model.actionBar(model.snapshot())), "Rebooting") {
		t.Fatal("reboot did not enter visible in-transit state")
	}
	updatedModel, _ := model.Update(command())
	if updatedModel.(Model).rebootPending {
		t.Fatal("reboot progress did not clear after command completion")
	}
}

func TestF2PeripheralRenamePersistsInWatchedUIConfig(t *testing.T) {
	snapshot := RichPreviewSnapshot()
	ui := appconfig.Defaults().UI
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		Preview: &snapshot, DisableWelcome: true,
		UIConfig: func() appconfig.UI { return ui },
		SaveUI: func(value appconfig.UI) error {
			ui = value
			return nil
		},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 38})
	model = updated.(Model)
	model.page, model.cursor = PageOutputs, 4
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyF2})
	model = updated.(Model)
	if model.renameTarget != "relay.5" || !model.terminalIsVisible() {
		t.Fatalf("F2 editor state target=%q terminal=%t", model.renameTarget, model.terminalIsVisible())
	}
	model.input.SetValue("Workbench lamp")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if ui.PeripheralNames["relay.5"] != "Workbench lamp" || model.renameTarget != "" {
		t.Fatalf("saved names/editor state = %#v/%q", ui.PeripheralNames, model.renameTarget)
	}
	if rendered := ansi.Strip(model.outputsPage(model.snapshot())); !strings.Contains(rendered, "Workbench lamp") {
		t.Fatalf("renamed peripheral missing from Control page:\n%s", rendered)
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

func TestTargetedAndBoardAppPageActionsSelectOnlyTheIntendedTUI(t *testing.T) {
	model := readyModel(t, PageDashboard)
	model.instanceID = "host:tui"

	updated, _ := model.Update(appActionMsg(hostui.AppAction{
		Kind: "app.page", Value: "events", Target: "webui",
	}))
	model = updated.(Model)
	if model.page != PageDashboard {
		t.Fatalf("WebUI-targeted action changed TUI page to %v", model.page)
	}

	updated, _ = model.Update(runtimeEventMsg(control.Event{
		Kind: "app.page", Source: "board",
		Metadata: map[string]string{"page": "settings", "target_instance": "tui"},
	}))
	model = updated.(Model)
	if model.page != PageAppSettings {
		t.Fatalf("board TUI navigation selected %v", model.page)
	}
}

func TestTUIMessagePresentationHonorsTargetsAndExposesAction(t *testing.T) {
	model := readyModel(t, PageDashboard)
	updated, _ := model.Update(runtimeEventMsg(control.Event{
		Kind: "message", Text: "Inspect output 3", MessageType: "operator.prompt",
		Targets: []string{"native", "tui"}, Action: "relay off",
	}))
	model = updated.(Model)
	if !strings.Contains(model.notice, "Inspect output 3") ||
		!strings.Contains(model.notice, "action: relay off") {
		t.Fatalf("TUI notice=%q", model.notice)
	}
	previous := model.notice
	updated, _ = model.Update(runtimeEventMsg(control.Event{
		Kind: "message", Text: "Web only", Targets: []string{"web"},
	}))
	if got := updated.(Model).notice; got != previous {
		t.Fatalf("web-only message changed TUI notice to %q", got)
	}
}

func TestTUITerminalTitleAndOSCAppActions(t *testing.T) {
	snapshot := RichPreviewSnapshot()
	var payloads []string
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		Preview: &snapshot, DisableWelcome: true, InstanceID: "host:tui",
		WriteOSC: func(payload string) error {
			payloads = append(payloads, payload)
			return nil
		},
	})
	updated, _ := model.Update(appActionMsg(hostui.AppAction{
		Kind: "app.title", Value: "Bench controller", Target: "tui",
	}))
	model = updated.(Model)
	if got := model.terminalTitle(); got != "Bench controller" {
		t.Fatalf("terminal title=%q", got)
	}
	updated, command := model.Update(appActionMsg(hostui.AppAction{
		Kind: "app.progress", Value: "warning 73", Target: "host:tui",
	}))
	model = updated.(Model)
	if command == nil {
		t.Fatal("terminal progress did not emit a command")
	}
	message := command()
	if _, ok := message.(terminalOSCResultMsg); !ok {
		t.Fatalf("terminal progress command returned %T", message)
	}
	if len(payloads) != 1 || payloads[0] != "9;4;4;73" {
		t.Fatalf("terminal OSC payloads=%#v", payloads)
	}
}

func TestUpdateEventsOpenProgrammingPageAndTrackVisibleProgress(t *testing.T) {
	model := readyModel(t, PageDashboard)
	model.writeOSC = func(string) error { return nil }
	updated, command := model.Update(runtimeEventMsg(control.Event{
		Kind: "update.programming", Text: "verified write in progress", Time: time.Now(),
		Metadata: map[string]string{
			"operation_id": "op-test", "kind": "firmware", "state": "programming", "progress_percent": "40",
		},
	}))
	model = updated.(Model)
	if model.page != PageProgramming || model.update.Progress != 40 || model.update.OperationID != "op-test" {
		t.Fatalf("update presentation page=%v state=%#v", model.page, model.update)
	}
	if command == nil {
		t.Fatal("update event did not emit terminal presentation commands")
	}
	rendered := ansi.Strip(model.programmingPage(model.snapshot()))
	for _, expected := range []string{"op-test", "PROGRAMMING", "40%", "verified write in progress"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("programming page missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "Progress and hashes appear in Console") {
		t.Fatal("programming page retained the obsolete static progress hint")
	}
}

func TestStableCrossSurfacePageNamesSelectTUIPage(t *testing.T) {
	tests := map[string]Page{
		"dashboard": PageDashboard,
		"controls":  PageOutputs,
		"workbench": PageOutputs,
		"updates":   PageProgramming,
		"settings":  PageAppSettings,
		"events":    PageEvents,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := pageForName(name)
			if !ok || got != want {
				t.Fatalf("pageForName(%q)=(%v,%t), want (%v,true)", name, got, ok, want)
			}
		})
	}
}

func TestMouseOutputToggleUpdatesInjectedState(t *testing.T) {
	model := readyModel(t, PageOutputs)
	before := model.preview.Status.ActiveRelays
	updated, command := model.Update(tea.MouseMsg{
		X: 10, Y: 4 + strings.Count(model.tabBar(), "\n") + 1 + 4,
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

func TestMenuAndSettingsMouseHitTestingFollowRenderedGeometry(t *testing.T) {
	model := readyModel(t, PageMenus)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	model = updated.(Model)
	entries := model.menuConfigurationEntries()
	if len(entries) < 2 {
		t.Fatal("preview menu catalog is too small for hit-test coverage")
	}
	geometry := model.menuInteractionGeometry()
	contentY := 4 + strings.Count(model.tabBar(), "\n") + 1
	target := 1
	updated, command := model.Update(tea.MouseMsg{
		X: 6, Y: contentY + geometry.entriesStart + target,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if command == nil || model.cursor != target || model.preview.Status.MenuPage != entries[target].Page.ID {
		t.Fatalf("menu click drifted: cursor=%d page=%d want cursor=%d page=%d", model.cursor, model.preview.Status.MenuPage, target, entries[target].Page.ID)
	}

	model.page = PageBoardSettings
	settingTarget := 0
	for index, row := range model.boardSettingRows() {
		if row.Key == "illumination.off" {
			settingTarget = index
			break
		}
	}
	model.cursor = settingTarget
	start, _ := tableWindow(model.selectionCount(), tableBodyRows(model.contentHeight()), model.cursor)
	updated, _ = model.Update(tea.MouseMsg{
		X: model.width / 2, Y: contentY + 4 + settingTarget - start,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	model = updated.(Model)
	if model.settingEditor == nil || model.settingEditor.Key != "illumination.off" {
		t.Fatalf("settings row click did not open its modal: %#v", model.settingEditor)
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
	for index, row := range model.appSettingRows() {
		if row.Key == "measurement.visibility" {
			model.cursor = index
			break
		}
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.settingEditor == nil {
		t.Fatal("measurement visibility modal did not open")
	}
	model.settingEditor.Cursor = 2 // Load current.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(Model)
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

func TestAppearanceSettingsPersistThroughNativeEditor(t *testing.T) {
	ui := appconfig.Defaults().UI
	var saved []appconfig.UI
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		UIConfig: func() appconfig.UI { return ui },
		SaveUI: func(value appconfig.UI) error {
			saved = append(saved, value)
			ui = value
			return nil
		},
		Preview:        func() *control.Snapshot { value := RichPreviewSnapshot(); return &value }(),
		DisableWelcome: true,
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	model = updated.(Model)
	model.page = PageAppSettings

	open := func(key string) {
		t.Helper()
		for index, row := range model.appSettingRows() {
			if row.Key == key {
				model.cursor = index
				var ok bool
				model, ok = model.beginSettingEditor()
				if !ok || model.settingEditor == nil {
					t.Fatalf("editor %q did not open", key)
				}
				return
			}
		}
		t.Fatalf("setting row %q is missing", key)
	}

	open("appearance.identity")
	model.settingEditor.Fields[0].Value = 2 // dark
	model.settingEditor.Fields[1].Value = 1 // Persian
	model.settingEditor.Fields[2].Value = 2 // RTL
	model, _, _ = model.commitAppSettingEditor()

	open("appearance.accessibility")
	model.settingEditor.Fields[0].Value = 1
	model.settingEditor.Fields[1].Value = 1
	model, _, _ = model.commitAppSettingEditor()

	open("appearance.audio")
	model.settingEditor.Fields[0].Value = 1
	model.settingEditor.Fields[1].Value = 37
	model, _, _ = model.commitAppSettingEditor()

	appearance := model.uiValue.Appearance
	if appearance.Theme != "dark" || appearance.Locale != "fa" || appearance.Direction != "rtl" ||
		!appearance.ReduceMotion || !appearance.CompactNumbers || !appearance.AudioMuted || appearance.AudioVolume != 0.37 {
		t.Fatalf("appearance=%#v", appearance)
	}
	if len(saved) != 3 {
		t.Fatalf("save count=%d, want 3", len(saved))
	}
	rendered := ansi.Strip(model.appSettingsPage())
	for _, expected := range []string{"Dark · Persian · RTL", "REDUCED · COMPACT", "MUTED · 37%"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("appearance summary %q missing:\n%s", expected, rendered)
		}
	}
}

func TestConfiguredProductTitleAppearsAndHotReloadsInTUI(t *testing.T) {
	t.Setenv("APP_TITLE", "")
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

func TestAppTitlePromptDispatchesToWatchedConfigCommand(t *testing.T) {
	runtime := control.New(control.Options{})
	config := appconfig.Defaults()
	engine := control.NewCommandEngine(runtime, control.CommandOptions{
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	model := NewWithOptions(runtime, engine, Options{
		UIConfig: func() appconfig.UI { return config.UI }, DisableWelcome: true,
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	model = updated.(Model)
	model.page, model.cursor = PageAppSettings, 0
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.settingEditor == nil || !model.settingEditor.IsText {
		t.Fatal("title modal did not open")
	}
	model.settingEditor.Text = "Operations Console"
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("title config command was not dispatched")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if config.UI.AppTitle != "Operations Console" {
		t.Fatalf("title config command saved %q", config.UI.AppTitle)
	}
	model.syncUIConfig(config.UI)
	if !strings.Contains(ansi.Strip(model.View()), "◆ Operations Console") {
		t.Fatal("hot-reloaded application title was not rendered")
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
		t.Fatalf("preview did not use current Door page 0: settings=%d active=%d", model.preview.Settings.DefaultPage, model.preview.Status.MenuPage)
	}
}

func TestDashboardMapsProgramModeToHumanSubmode(t *testing.T) {
	rendered := PreviewFrame(PageDashboard, 132, 38)
	if !strings.Contains(rendered, "Door") || !strings.Contains(rendered, "Menu / Submode") {
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
			if page == PageRF && strings.Contains(line, "L Learn") && strings.Contains(line, "Refresh list") {
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
	for _, expected := range []string{"4-DIGIT DISPLAY", "2×16 LCD", "PCController", "K1 · previous", "active 0 · Door"} {
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

func TestFrontPanelRendersPushedStatusLEDInTrueColor(t *testing.T) {
	priorProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(priorProfile) })
	state := FrontPanelState{
		Segments: "1234", LCDLine1: "one", LCDLine2: "two", Exact: true,
		HaveStatusLED: true,
		StatusLED:     native.StatusLEDState{Red: 18, Green: 52, Blue: 86, Brightness: 200, Effect: native.StatusEffectCycle, Condition: 8},
	}
	rendered := renderFrontPanel(state)
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "#123456 · cycle · condition 8") {
		t.Fatalf("status LED preview missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "38;2;18;52;86") {
		t.Fatalf("status LED preview is not truecolor ANSI:\n%q", rendered)
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
		header := sectionHeader(width, "HOST SETTINGS", "saved in host JSON · never board EEPROM")
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
	for _, expected := range []string{"Motion allowed by door state", "Door open/close buzzer cues", "Relay on/off buzzer cues"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("board settings missing %q:\n%s", expected, rendered)
		}
	}
}

func TestSettingsTablesUseStableGroupedRowsAndModalDrafts(t *testing.T) {
	board := readyModel(t, PageBoardSettings)
	rendered := ansi.Strip(board.View())
	for _, expected := range []string{"GROUP", "SETTING", "VALUE", "Decimal places", "Voltage 2  ·  Current 2", "Direction dead-time"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("board table missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "Swap temperature") || strings.Contains(rendered, "palette") {
		t.Fatalf("build-only/internal settings leaked into the board table:\n%s", rendered)
	}

	for index, row := range board.boardSettingRows() {
		if row.Key == "illumination.off" {
			board.cursor = index
			break
		}
	}
	original := board.preview.Settings.OffBrightness
	updated, _ := board.Update(tea.KeyMsg{Type: tea.KeyEnter})
	board = updated.(Model)
	if board.settingEditor == nil || board.settingEditor.Key != "illumination.off" {
		t.Fatalf("stable row opened editor %#v", board.settingEditor)
	}
	updated, _ = board.Update(tea.KeyMsg{Type: tea.KeyRight})
	board = updated.(Model)
	if board.preview.Settings.OffBrightness != original {
		t.Fatal("modal draft changed board settings before confirmation")
	}
	updated, _ = board.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).preview.Settings.OffBrightness != original {
		t.Fatal("discarded modal changed the preview board")
	}
}

func TestControlAndDashboardExposePWMAsPercentOnly(t *testing.T) {
	for _, page := range []Page{PageDashboard, PageOutputs} {
		rendered := ansi.Strip(PreviewFrame(page, 132, 42))
		if !strings.Contains(rendered, "%") {
			t.Errorf("page %d has no PWM percentage:\n%s", page, rendered)
		}
		for _, forbidden := range []string{"/4095", "2816 ·"} {
			if strings.Contains(rendered, forbidden) {
				t.Errorf("page %d leaked raw PWM value %q:\n%s", page, forbidden, rendered)
			}
		}
	}
}

func TestStatusLEDStatesHaveIndependentFullEditors(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	for index, row := range model.appSettingRows() {
		if row.Key == "led.visual.door-opened" {
			model.cursor = index
			break
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.settingEditor == nil || model.settingEditor.Key != "led.visual.door-opened" {
		t.Fatalf("door-opened editor=%#v", model.settingEditor)
	}
	keys := make(map[string]bool)
	for _, field := range model.settingEditor.Fields {
		keys[field.Key] = true
	}
	for _, expected := range []string{"effect", "red", "green", "blue", "alt-red", "brightness", "minimum", "period"} {
		if !keys[expected] {
			t.Errorf("status LED editor missing %q: %#v", expected, model.settingEditor.Fields)
		}
	}
	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "live color preview") || !strings.Contains(rendered, "Enter save") {
		t.Fatalf("status LED modal lacks preview/confirmation:\n%s", rendered)
	}
}

func TestModalValueAdjustmentRollsOverAtBothBounds(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	model.settingEditor = &settingEditor{Fields: []settingEditorField{{Value: 0, Min: 0, Max: 255, Step: 5}}}
	model.adjustSettingEditor(-1)
	if got := model.settingEditor.Fields[0].Value; got != 255 {
		t.Fatalf("decrement rollover=%d, want 255", got)
	}
	model.adjustSettingEditor(1)
	if got := model.settingEditor.Fields[0].Value; got != 0 {
		t.Fatalf("increment rollover=%d, want 0", got)
	}
}

func TestModalAcceptsTypedSliderValuesBeforeExplicitSave(t *testing.T) {
	model := readyModel(t, PageBoardSettings)
	for index, row := range model.boardSettingRows() {
		if row.Key == "illumination.off" {
			model.cursor = index
			break
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	original := model.preview.Settings.OffBrightness
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("37")})
	model = updated.(Model)
	if model.settingEditor == nil || !model.settingEditor.NumberEditing {
		t.Fatal("typing did not enter direct numeric mode")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.settingEditor == nil || model.settingEditor.NumberEditing || editorField(model.settingEditor, "value") != 37 {
		t.Fatalf("typed number was not applied to the isolated draft: %#v", model.settingEditor)
	}
	if model.preview.Settings.OffBrightness != original {
		t.Fatal("typed draft changed board settings before explicit save")
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.settingEditor != nil || model.preview.Settings.OffBrightness != percentByte(37) {
		t.Fatalf("typed draft was not committed: value=%d command=%v editor=%#v", model.preview.Settings.OffBrightness, command, model.settingEditor)
	}

	temperature := settingEditorField{Min: 3000, Max: 12500, Unit: "°C"}
	if value, err := parseEditorNumber(temperature, "42.75"); err != nil || value != 4275 {
		t.Fatalf("typed Celsius conversion=(%d,%v)", value, err)
	}
}

func TestTableLayoutDefaultsCompactAndHotAppliesExpanded(t *testing.T) {
	ui := appconfig.Defaults().UI
	model := readyModel(t, PageAppSettings)
	model.uiValue = ui
	if got := model.presentationTableWidth(112); got != 112 {
		t.Fatalf("compact table width=%d", got)
	}
	ui.TableLayout = "expanded"
	model.syncUIConfig(ui)
	if got := model.presentationTableWidth(112); got != model.width {
		t.Fatalf("expanded table width=%d want %d", got, model.width)
	}
}

func TestEventsGraphsUseAlignedExpandableTable(t *testing.T) {
	model := readyModel(t, PageEvents)
	compact := ansi.Strip(model.View())
	for _, expected := range []string{"SIGNAL", "TREND", "MIN · MAX · LATEST", "E expand"} {
		if !strings.Contains(compact, expected) {
			t.Errorf("compact graph table missing %q:\n%s", expected, compact)
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(Model)
	if !model.eventsExpanded || !strings.Contains(ansi.Strip(model.View()), "E compact") {
		t.Fatal("events graph did not enter expanded mode")
	}
}

func TestDashboardAndRFAvoidImplementationHints(t *testing.T) {
	dashboard := ansi.Strip(PreviewFrame(PageDashboard, 132, 42))
	for _, forbidden := range []string{"HOST-owned", "HOST-controlled", "firmware-owned", "INA219 Bus Voltage"} {
		if strings.Contains(dashboard, forbidden) {
			t.Errorf("dashboard leaked %q:\n%s", forbidden, dashboard)
		}
	}
	rf := ansi.Strip(PreviewFrame(PageRF, 160, 46))
	if strings.Contains(rf, "Timer aliases") {
		t.Fatalf("RF page leaked parser aliases:\n%s", rf)
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
		{key: "o", command: "macro monitor"},
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

func TestAutomationMetadataShortcutsPrepareSelectedMacroCommands(t *testing.T) {
	for _, test := range []struct {
		key  string
		want string
	}{
		{key: "u", want: "macro rename 1 "},
		{key: "g", want: "macro category 1 "},
	} {
		t.Run(test.key, func(t *testing.T) {
			model := readyModel(t, PageAutomations)
			updated, command, handled := model.macroShortcut(test.key)
			if !handled || command != nil || updated.input.Value() != test.want {
				t.Fatalf("key %q handled=%v command=%v input=%q, want %q", test.key, handled, command, updated.input.Value(), test.want)
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

func TestVirtualFrontPanelPressDispatchesWithoutReleaseOrClickDelay(t *testing.T) {
	engine := shell.New(10)
	var calls []string
	if err := engine.Register(shell.Command{
		Name: "menu", Usage: "menu ACTION", Summary: "test",
		Run: func(_ context.Context, args []string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			return "ok", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	model := NewWithOptions(control.New(control.Options{}), engine, Options{DisableWelcome: true})
	model.page = PageMenus
	_, press, _ := model.frontPanelGesture(1, "press")
	if press == nil {
		t.Fatal("virtual front-panel press did not dispatch immediately")
	}
	_ = press()
	_, release, _ := model.frontPanelGesture(1, "release")
	if release != nil {
		t.Fatal("stateless virtual front-panel release dispatched a second action")
	}
	if got := strings.Join(calls, ","); got != "prev" {
		t.Fatalf("virtual front-panel calls=%q, want one immediate prev", got)
	}
}

type fakePortOwnerActions struct {
	calls []string
}

func (fake *fakePortOwnerActions) BringToForeground(context.Context, portowner.Owner) error {
	fake.calls = append(fake.calls, "foreground")
	return nil
}

func (fake *fakePortOwnerActions) RequestGracefulClose(context.Context, portowner.Owner) error {
	fake.calls = append(fake.calls, "close")
	return nil
}

func (fake *fakePortOwnerActions) Terminate(_ context.Context, owner portowner.Owner, confirmation string) error {
	fake.calls = append(fake.calls, "terminate:"+confirmation)
	return nil
}

func (*fakePortOwnerActions) TerminateConfirmation(owner portowner.Owner) string {
	return fmt.Sprintf("TERMINATE %d", owner.PID)
}

func TestSerialOwnerErrorExposesSafeTUIActionsAndDoubleTerminateConfirmation(t *testing.T) {
	actions := &fakePortOwnerActions{}
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		DisableWelcome: true, PortOwnerActions: actions,
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 180, Height: 40})
	model = updated.(Model)
	owner := portowner.Owner{
		PID: 321, Name: "terminal.exe", Executable: `C:\Apps\terminal.exe`,
		Window: portowner.Window{Title: "Serial monitor", Visible: true},
	}
	updated, _ = model.Update(connectResultMsg{err: &portowner.BusyError{
		Port: "COM7", Cause: errors.New("Serial port busy"), Owner: &owner,
	}})
	model = updated.(Model)
	if model.portOwner == nil || model.portOwner.PID != 321 {
		t.Fatalf("owner was not retained in view model: %#v", model.portOwner)
	}
	rendered := ansi.Strip(model.View())
	for _, expected := range []string{"BUSY", "terminal.exe", "PID 321", "Ask Close", "Terminate"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("owner UI missing %q:\n%s", expected, rendered)
		}
	}
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 88, Height: 30})
	model = updated.(Model)
	for lineNumber, line := range strings.Split(model.View(), "\n") {
		if cells := ansi.StringWidth(line); cells > 88 {
			t.Fatalf("serial-owner view line %d overflowed to %d cells: %q", lineNumber+1, cells, line)
		}
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	model = updated.(Model)
	if command == nil {
		t.Fatal("foreground action did not schedule a command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	model = updated.(Model)
	if command == nil {
		t.Fatal("graceful-close action did not schedule a command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)

	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(Model)
	if command != nil || model.ownerTerminateArmedUntil.IsZero() {
		t.Fatal("first terminate action did not arm an explicit confirmation")
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(Model)
	if command == nil {
		t.Fatal("confirmed terminate action did not schedule a command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if got := strings.Join(actions.calls, ","); got != "foreground,close,terminate:TERMINATE 321" {
		t.Fatalf("owner action calls=%q", got)
	}
	if model.portOwner != nil {
		t.Fatal("successful explicit termination left stale owner controls")
	}
}
