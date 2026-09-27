package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

const (
	macroLibraryVisibleRows = 5
	macroLibraryFirstRow    = 19
)

type macroButtonTone byte

const (
	macroButtonNormal macroButtonTone = iota
	macroButtonGood
	macroButtonDanger
)

type macroButtonDefinition struct {
	key   string
	label string
	tone  macroButtonTone
}

var macroPrimaryButtons = []macroButtonDefinition{
	{key: "n", label: "N New"},
	{key: "r", label: "R Record", tone: macroButtonGood},
	{key: "s", label: "S Save", tone: macroButtonGood},
	{key: "d", label: "D Discard", tone: macroButtonDanger},
	{key: "p", label: "P Play", tone: macroButtonGood},
}

var macroSecondaryButtons = []macroButtonDefinition{
	{key: "c", label: "C Cancel off", tone: macroButtonDanger},
	{key: "k", label: "K Cancel keep"},
	{key: "/", label: "/ Find"},
	{key: "i", label: "I Info"},
	{key: "x", label: "X Delete", tone: macroButtonDanger},
	{key: "a", label: "A Rules"},
}

func (model Model) automationsPage() string {
	state := model.macroState()
	recording := model.macroRecordingState()
	allMacros := model.macroLibrary()
	filtered := model.filteredMacros(allMacros)
	search := "all macros"
	if model.macroSearch != "" {
		search = model.macroSearch
	}
	if model.macroSearchEditing {
		search = "✎ " + search + "▏"
	}
	searchLine := labelStyle.Render("/ search · Ctrl+U clear · ↑/↓ select · Enter/P play · source is watched HOST configuration") + "  " + valueStyle.Render(search)
	if model.macroDeleteArmed {
		searchLine = errorStyle.Copy().Bold(true).Render("DELETE ARMED · press X again to delete macro " + model.macroDeleteReference + " · any other action cancels")
	}

	lines := []string{
		sectionHeader(model.width, "AUTOMATIONS & MACROS", "searchable PC library · MCU-timed queue · exact acknowledgement deltas"),
		renderMacroButtonRow(macroPrimaryButtons),
		renderMacroButtonRow(macroSecondaryButtons),
		ansi.Truncate(searchLine, model.width, "…"),
		sectionHeader(model.width, "PLAYBACK", macroLifecycleLabel(state)),
		model.macroKV("Active / Last Macro", macroIdentity(state)),
		model.macroKV("Elapsed / Duration", macroElapsedSummary(state, time.Now())),
		model.macroKV("Steps / Queue", macroProgressSummary(state, model.width)),
		model.macroKV("Timing", macroTimingSummary(state)),
		model.macroKV("Result", macroResultSummary(state)),
		sectionHeader(model.width, "RECORDING", boolWord(recording.Active, "ACTIVE · MCU acknowledgements are authoritative", "idle")),
		model.macroKV("Recorder", macroRecordingSummary(recording, time.Now())),
		ansi.Truncate(macroRecordingHelp(recording), model.width, "…"),
		sectionHeader(model.width, "MACRO LIBRARY", fmt.Sprintf("%d of %d match · ID-sorted · metadata stays on PC", len(filtered), len(allMacros))),
		macroTableHeader(model.width),
	}

	start, visible := macroWindow(filtered, model.cursor, macroLibraryVisibleRows)
	for index := 0; index < macroLibraryVisibleRows; index++ {
		if index >= len(visible) {
			lines = append(lines, "")
			continue
		}
		macro := visible[index]
		line := model.macroTableRow(macro, state, recording)
		lines = append(lines, model.selectionLine(start+index, line))
	}
	if len(filtered) == 0 {
		lines[len(lines)-macroLibraryVisibleRows] = warnStyle.Render("  No macros match. Press / to change the search or N to create a draft.")
	}

	lines = append(lines, "", titleStyle.Render("HOST PLATFORM & BRIDGES"))
	if model.width < 100 {
		lines = append(lines, labelStyle.Render("Widen the terminal to inspect hotkeys, toasts, discovery, webhooks, messaging, and Socket.IO."))
	} else {
		for _, line := range model.integrationStatusLines() {
			lines = append(lines, ansi.Truncate(line, model.width, "…"))
		}
	}
	return strings.Join(lines, "\n")
}

func (model Model) macroLibrary() []appconfig.Macro {
	if model.preview != nil {
		result := append([]appconfig.Macro(nil), model.previewMacros...)
		for index := range result {
			result[index].Steps = append([]appconfig.MacroStep(nil), result[index].Steps...)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
		return result
	}
	if runner := model.runtime.MacroRunner(); runner != nil {
		return runner.List()
	}
	return nil
}

func (model Model) macroState() control.MacroState {
	if model.preview != nil {
		return model.previewMacroState
	}
	if runner := model.runtime.MacroRunner(); runner != nil {
		return runner.State()
	}
	return control.MacroState{}
}

func (model Model) macroRecordingState() control.MacroRecordingState {
	if model.preview != nil {
		return model.previewMacroRecording
	}
	if runner := model.runtime.MacroRunner(); runner != nil {
		return runner.RecordingState()
	}
	return control.MacroRecordingState{}
}

func (model Model) filteredMacros(source []appconfig.Macro) []appconfig.Macro {
	query := strings.ToLower(strings.TrimSpace(model.macroSearch))
	if query == "" {
		return source
	}
	result := make([]appconfig.Macro, 0, len(source))
	for _, macro := range source {
		fields := []string{
			strconv.Itoa(int(macro.ID)), fmt.Sprintf("0x%02x", macro.ID), macro.Name,
			macro.Category, macro.Color, macro.Label, macro.LCDMessage,
		}
		for _, step := range macro.Steps {
			fields = append(fields, step.Kind, step.Text, step.Destination)
		}
		if strings.Contains(strings.ToLower(strings.Join(fields, " ")), query) {
			result = append(result, macro)
		}
	}
	return result
}

func (model Model) selectedMacro() (appconfig.Macro, bool) {
	values := model.filteredMacros(model.macroLibrary())
	if len(values) == 0 {
		return appconfig.Macro{}, false
	}
	index := model.cursor
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index], true
}

func macroWindow(values []appconfig.Macro, cursor, limit int) (int, []appconfig.Macro) {
	if len(values) == 0 || limit <= 0 {
		return 0, nil
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(values) {
		cursor = len(values) - 1
	}
	start := cursor - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(values) {
		start = max(0, len(values)-limit)
	}
	end := min(len(values), start+limit)
	return start, values[start:end]
}

func renderMacroButtonRow(definitions []macroButtonDefinition) string {
	values := make([]string, 0, len(definitions)*2-1)
	for index, definition := range definitions {
		if index != 0 {
			values = append(values, " ")
		}
		style := buttonStyle
		switch definition.tone {
		case macroButtonGood:
			style = buttonGoodStyle
		case macroButtonDanger:
			style = buttonBadStyle
		}
		values = append(values, style.Render(definition.label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, values...)
}

func macroButtonKeyAt(definitions []macroButtonDefinition, x int) (string, bool) {
	position := 0
	for _, definition := range definitions {
		width := lipgloss.Width(buttonStyle.Render(definition.label))
		if x >= position && x < position+width {
			return definition.key, true
		}
		position += width + 1
	}
	return "", false
}

func (model Model) macroShortcut(key string) (Model, tea.Cmd, bool) {
	// Delete is intentionally a two-key action. Every other macro action safely
	// disarms a pending deletion so an unrelated key can never confirm it.
	if key != "x" {
		model.macroDeleteArmed = false
		model.macroDeleteReference = ""
	}
	switch key {
	case "/":
		model.macroSearchEditing = true
		model.setNotice("Type to filter macro ID, name, category, color, label, LCD text, or step kind")
		return model, nil, true
	case "a":
		return model.dispatchLine("automation list")
	case "m":
		return model.dispatchLine("macro list")
	case "n":
		id := model.nextMacroID()
		model.input.SetValue(fmt.Sprintf("macro create %d ", id))
		model.input.CursorEnd()
		model.revealTerminal()
		model.setNotice("Complete NAME [CATEGORY [COLOR]]; colors: red, blue, violet, green, white")
		return model, nil, true
	case "r":
		if recording := model.macroRecordingState(); recording.Active {
			model.setNotice(fmt.Sprintf("Already recording %d/%s with %d steps", recording.ID, recording.Name, recording.Steps))
			return model, nil, true
		}
		model.input.SetValue("macro record start ")
		model.input.CursorEnd()
		model.revealTerminal()
		model.setNotice("Complete NAME [CATEGORY [COLOR]], then operate relays/PWM/etc.; MCU ACK deltas set timing")
		return model, nil, true
	case "s":
		if !model.macroRecordingState().Active {
			model.setNotice("No active recording to save")
			return model, nil, true
		}
		return model.dispatchLine("macro record save")
	case "d":
		if !model.macroRecordingState().Active {
			model.setNotice("No active recording to discard")
			return model, nil, true
		}
		return model.dispatchLine("macro record discard")
	case "p":
		return model.playSelectedMacro()
	case "c":
		if !model.macroState().Running {
			model.setNotice("No macro is currently playing")
			return model, nil, true
		}
		return model.dispatchLine("macro cancel")
	case "k":
		if !model.macroState().Running {
			model.setNotice("No macro is currently playing")
			return model, nil, true
		}
		return model.dispatchLine("macro cancel keep")
	case "i":
		macro, ok := model.selectedMacro()
		if !ok {
			model.setNotice("No macro selected")
			return model, nil, true
		}
		return model.dispatchLine(fmt.Sprintf("macro show %d", macro.ID))
	case "x":
		return model.deleteSelectedMacro()
	}
	return model, nil, false
}

func (model Model) playSelectedMacro() (Model, tea.Cmd, bool) {
	macro, ok := model.selectedMacro()
	if !ok {
		model.setNotice("No macro selected")
		return model, nil, true
	}
	if len(macro.Steps) == 0 {
		model.setNotice(fmt.Sprintf("Macro %d/%s is an empty draft; record or add steps before playback", macro.ID, macro.Name))
		return model, nil, true
	}
	return model.dispatchLine(fmt.Sprintf("macro play %d", macro.ID))
}

func (model Model) deleteSelectedMacro() (Model, tea.Cmd, bool) {
	macro, ok := model.selectedMacro()
	if !ok {
		model.setNotice("No macro selected")
		return model, nil, true
	}
	reference := fmt.Sprintf("%d/%s", macro.ID, macro.Name)
	if !model.macroDeleteArmed || model.macroDeleteReference != reference {
		model.macroDeleteArmed = true
		model.macroDeleteReference = reference
		model.setNotice("Press X again to permanently delete HOST macro " + reference)
		return model, nil, true
	}
	model.macroDeleteArmed = false
	model.macroDeleteReference = ""
	return model.dispatchLine(fmt.Sprintf("macro delete %d", macro.ID))
}

func (model Model) nextMacroID() byte {
	used := make(map[byte]bool)
	for _, macro := range model.macroLibrary() {
		used[macro.ID] = true
	}
	for candidate := 0; candidate <= 0xFF; candidate++ {
		if !used[byte(candidate)] {
			return byte(candidate)
		}
	}
	return 0xFF
}

func (model Model) macroKV(label, value string) string {
	labelWidth := 22
	if model.width < 72 {
		labelWidth = 17
	}
	available := max(1, model.width-labelWidth-1)
	return labelStyle.Render(padRightVisible(label, labelWidth)) + " " + valueStyle.Render(truncateDisplayText(value, available))
}

func macroIdentity(state control.MacroState) string {
	if state.Name == "" && state.StepCount == 0 {
		return "none this session"
	}
	metadata := fmt.Sprintf("%d · %s", state.ID, state.Name)
	if state.Category != "" {
		metadata += " · " + state.Category
	}
	if state.Color != "" {
		metadata += " · " + state.Color
	}
	return metadata
}

func macroLifecycleLabel(state control.MacroState) string {
	if state.Lifecycle == "" {
		return "not started"
	}
	return strings.ToUpper(state.Lifecycle)
}

func macroElapsedSummary(state control.MacroState, now time.Time) string {
	duration := time.Duration(state.DurationUS) * time.Microsecond
	if state.StartedAt.IsZero() {
		return "— / " + formatMacroDuration(duration)
	}
	end := now
	if !state.Running && !state.FinishedAt.IsZero() {
		end = state.FinishedAt
	}
	elapsed := end.Sub(state.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	return formatMacroDuration(elapsed) + " / " + formatMacroDuration(duration)
}

func macroProgressSummary(state control.MacroState, width int) string {
	barWidth := max(10, min(28, width-57))
	return fmt.Sprintf("%s %d/%d · buffer %d/%d B · accepted %d B",
		macroProgressBar(state.Step, state.StepCount, barWidth),
		state.Step, state.StepCount, state.BufferFill, native.MacroQueueCapacity, state.AcceptedBytes)
}

func macroProgressBar(current, total, width int) string {
	filled := 0
	if total > 0 {
		filled = current * width / total
	}
	filled = max(0, min(width, filled))
	return "[" + lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("━", filled)) +
		labelStyle.Render(strings.Repeat("─", width-filled)) + "]"
}

func macroTimingSummary(state control.MacroState) string {
	return fmt.Sprintf("last %s · max %s · tolerance %s · violations %d",
		formatSignedMicros(state.LastTimingDeltaUS), formatMicros(state.MaximumTimingErrorUS),
		formatMicros(state.TimingToleranceUS), state.TimingViolations)
}

func macroResultSummary(state control.MacroState) string {
	faithful := "pending"
	if !state.Running && state.Lifecycle != "" {
		faithful = boolWord(state.Faithful, "YES", "NO")
	}
	result := fmt.Sprintf("lifecycle %s · faithful %s · underruns %d · dispatch errors %d",
		macroLifecycleLabel(state), faithful, state.Underruns, state.DispatchErrors)
	if state.LastError != "" {
		result += " · " + state.LastError
	}
	return result
}

func macroRecordingSummary(state control.MacroRecordingState, now time.Time) string {
	if !state.Active {
		if state.Name == "" {
			return "idle · no recording this session"
		}
		return fmt.Sprintf("idle · last %d/%s · %d steps", state.ID, state.Name, state.Steps)
	}
	elapsed := now.Sub(state.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
<<<<<<< HEAD
	return fmt.Sprintf("%d · %s · %s · %d steps · %s", state.ID, state.Name, state.Category, state.Steps, formatMacroDuration(elapsed))
=======
	return fmt.Sprintf("%d · %s · %s · %d steps (host %d · panel %d · RF %d) · last at %dµs, delta %dµs · %s", state.ID, state.Name, state.Category, state.Steps, state.HostSteps, state.PanelSteps, state.RFSteps, state.LastAtUS, state.LastDeltaUS, formatMacroDuration(elapsed))
>>>>>>> e2958b6 (feat(macros): recover timed capture and safe schema migration)
}

func macroRecordingHelp(state control.MacroRecordingState) string {
	if state.LastError != "" {
		return errorStyle.Render("Recorder error: " + state.LastError)
	}
	if state.Active {
		return warnStyle.Render("Operate any queueable relay, motion, PWM, buzzer, display, RF, RGB, LED, or menu command; S saves, D discards.")
	}
	return labelStyle.Render("R starts a named recording; exact MCU acknowledgement timestamps become step offsets. N creates an editable empty draft.")
}

func macroTableHeader(width int) string {
	nameWidth, categoryWidth := macroColumnWidths(width)
	return titleStyle.Render(strings.Join([]string{
		" ", centerMacroHeader("ID", 3), centerMacroHeader("NAME", nameWidth),
		centerMacroHeader("CATEGORY", categoryWidth), centerMacroHeader("COLOR", 8),
		centerMacroHeader("STEPS", 5), centerMacroHeader("DURATION", 10),
	}, " "))
}

// centerMacroHeader centers a label by terminal cells inside the exact width
// used by its data column; the extra cell, when odd, is placed on the left.
func centerMacroHeader(value string, width int) string {
	value = truncateDisplayText(value, width)
	padding := max(0, width-lipgloss.Width(value))
	left := (padding + 1) / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", padding-left)
}

func (model Model) macroTableRow(macro appconfig.Macro, state control.MacroState, recording control.MacroRecordingState) string {
	nameWidth, categoryWidth := macroColumnWidths(model.width)
	name := macro.Name
	marker := " "
	if state.Running && state.ID == macro.ID {
		marker = "▶"
	} else if recording.Active && recording.ID == macro.ID {
		marker = "●"
	}
	category := macro.Category
	if category == "" {
		category = "—"
	}
	color := macro.Color
	if color == "" {
		color = "—"
	}
	duration := macroDefinitionDuration(macro)
	durationText := formatMacroDuration(duration)
	if len(macro.Steps) == 0 {
		durationText = "draft"
	}
	return fmt.Sprintf("%s %-3d %-*s %-*s %-8s %-5d %-10s",
		marker, macro.ID,
		nameWidth, truncateDisplayText(name, nameWidth),
		categoryWidth, truncateDisplayText(category, categoryWidth),
		truncateDisplayText(color, 8), len(macro.Steps), durationText)
}

func macroColumnWidths(width int) (int, int) {
	nameWidth, categoryWidth := 24, 16
	if width < 112 {
		nameWidth, categoryWidth = 18, 12
	}
	if width < 82 {
		nameWidth, categoryWidth = 14, 10
	}
	return nameWidth, categoryWidth
}

func macroDefinitionDuration(macro appconfig.Macro) time.Duration {
	var maximum uint32
	for _, step := range macro.Steps {
		due := step.AtUS
		if due > maximum {
			maximum = due
		}
	}
	return time.Duration(maximum) * time.Microsecond
}

func formatMacroDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	if value < time.Second {
		return value.Round(time.Millisecond).String()
	}
	return value.Round(10 * time.Millisecond).String()
}

func formatSignedMicros(value int32) string {
	return fmt.Sprintf("%+d µs", value)
}

func formatMicros(value uint32) string {
	return fmt.Sprintf("%d µs", value)
}
