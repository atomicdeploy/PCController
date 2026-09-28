package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

type rfActionOption struct {
	Label string
	Args  string
}

var rfActionCatalog = buildRFActionCatalog()

func buildRFActionCatalog() []rfActionOption {
	options := []rfActionOption{{Label: "Unmapped / no board action", Args: "none"}}
	for key := 1; key <= 4; key++ {
		for _, behavior := range []string{"press", "toggle", "momentary"} {
			options = append(options, rfActionOption{
				Label: fmt.Sprintf("Physical key K%d · %s", key, behavior),
				Args:  fmt.Sprintf("key %d %s", key, behavior),
			})
		}
	}
	for _, action := range []string{"prev", "next", "dec", "inc"} {
		options = append(options, rfActionOption{
			Label: "Menu navigation · " + action,
			Args:  "menu " + action,
		})
	}
	for relay := 5; relay <= 8; relay++ {
		for _, behavior := range []string{"toggle", "momentary", "press"} {
			options = append(options, rfActionOption{
				Label: fmt.Sprintf("User relay R%d · %s", relay, behavior),
				Args:  fmt.Sprintf("relay %d %s", relay, behavior),
			})
		}
	}
	for _, side := range []string{"A", "B"} {
		for _, motion := range []string{"up", "down", "stop"} {
			options = append(options, rfActionOption{
				Label: fmt.Sprintf("Motion side %s · %s", side, motion),
				Args:  fmt.Sprintf("side %s %s", side, motion),
			})
		}
	}
	for channel := 0; channel <= 10; channel++ {
		for _, behavior := range []string{"press", "toggle", "momentary"} {
			options = append(options, rfActionOption{
				Label: fmt.Sprintf("User PWM %d · %s", channel, behavior),
				Args:  fmt.Sprintf("pwm %d %s", channel, behavior),
			})
		}
	}
	return options
}

func previewRFEntries() []native.RFEntry {
	return []native.RFEntry{
		{ID: 0, Code: 1_381_717, Bits: 24, Protocol: 1, PulseUS: 350},
		{ID: 1, Code: 1_381_721, Bits: 24, Protocol: 1, PulseUS: 350, ActionKind: native.RFActionSide, ActionValue: 0, Behavior: native.RFBehaviorUp},
		{ID: 2, Code: 1_381_729, Bits: 24, Protocol: 1, PulseUS: 350, ActionKind: native.RFActionRelay, ActionValue: 4, Behavior: native.RFBehaviorToggle},
		{ID: 3, Code: 0x00ABCDEF, Bits: 24, Protocol: 2, PulseUS: 420, ActionKind: native.RFActionKey, ActionValue: 1, Behavior: native.RFBehaviorPress},
	}
}

func sortedRFEntries(entries []native.RFEntry) []native.RFEntry {
	result := append([]native.RFEntry(nil), entries...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (model *Model) resetRFStage(entries []native.RFEntry) {
	model.rfOriginal = sortedRFEntries(entries)
	model.rfStaged = append([]native.RFEntry(nil), model.rfOriginal...)
	model.rfStageDirty = false
	model.rfReview = false
	if len(model.rfStaged) == 0 {
		model.cursor = 0
	} else if model.cursor >= len(model.rfStaged) {
		model.cursor = len(model.rfStaged) - 1
	}
}

func (model Model) fetchRFEntriesCommand() tea.Cmd {
	fetch := model.rfFetch
	if fetch == nil {
		if model.remote != nil {
			return func() tea.Msg {
				return rfEntriesResultMsg{err: errors.New("remote RF list endpoint is unavailable")}
			}
		}
		fetch = func(ctx context.Context) ([]native.RFEntry, error) {
			cursor := byte(0)
			var entries []native.RFEntry
			for page := 0; page < 86; page++ {
				frame, err := model.runtime.Request(ctx, native.OpRFLearnList, []byte{cursor}, native.OpRFEntries)
				if err != nil {
					return nil, err
				}
				parsed, err := native.ParseRFEntries(frame.Payload)
				if err != nil {
					return nil, err
				}
				entries = append(entries, parsed.Entries...)
				if parsed.NextCursor == 0xFF {
					return sortedRFEntries(entries), nil
				}
				if parsed.NextCursor == cursor {
					return nil, fmt.Errorf("RF list cursor did not advance from %d", cursor)
				}
				cursor = parsed.NextCursor
			}
			return nil, fmt.Errorf("RF list exceeded pagination safety limit")
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		entries, err := fetch(ctx)
		return rfEntriesResultMsg{entries: entries, err: err}
	}
}

func (model Model) selectedRFEntry() (native.RFEntry, bool) {
	if model.cursor < 0 || model.cursor >= len(model.rfStaged) {
		return native.RFEntry{}, false
	}
	return model.rfStaged[model.cursor], true
}

func (model *Model) moveRFStage(delta int) {
	if len(model.rfStaged) < 2 || model.cursor < 0 || model.cursor >= len(model.rfStaged) {
		return
	}
	target := model.cursor + delta
	if target < 0 || target >= len(model.rfStaged) {
		model.setNotice("RF ID is already at the edge")
		return
	}
	model.rfStaged[model.cursor], model.rfStaged[target] = model.rfStaged[target], model.rfStaged[model.cursor]
	for index := range model.rfStaged {
		model.rfStaged[index].ID = byte(index)
	}
	model.cursor = target
	model.rfStageDirty = !sameRFOrder(model.rfStaged, model.rfOriginal)
	model.rfReview = false
	model.setNotice("RF ID change staged locally; review before applying")
}

func sameRFOrder(left, right []native.RFEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID ||
			left[index].Code != right[index].Code ||
			left[index].Bits != right[index].Bits ||
			left[index].Protocol != right[index].Protocol {
			return false
		}
	}
	return true
}

func (model Model) applyRFOrderCommand() tea.Cmd {
	apply := model.rfApplyOrder
	fetch := model.rfFetch
	desired := append([]native.RFEntry(nil), model.rfStaged...)
	original := append([]native.RFEntry(nil), model.rfOriginal...)
	return func() tea.Msg {
		if apply == nil || fetch == nil {
			return rfOrderResultMsg{err: fmt.Errorf("firmware does not advertise a transactional RF reorder opcode")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := apply(ctx, desired); err != nil {
			return rfOrderResultMsg{err: err}
		}
		readback, err := fetch(ctx)
		readback = sortedRFEntries(readback)
		if err == nil && sameRFOrder(readback, desired) {
			return rfOrderResultMsg{entries: readback}
		}
		failure := err
		if failure == nil {
			failure = fmt.Errorf("device readback differs from staged RF order")
		}
		rollbackErr := apply(ctx, original)
		rolledBack := false
		rollbackEntries := original
		if rollbackErr == nil {
			rollbackEntries, rollbackErr = fetch(ctx)
			rollbackEntries = sortedRFEntries(rollbackEntries)
			rolledBack = rollbackErr == nil && sameRFOrder(rollbackEntries, original)
		}
		if rollbackErr != nil {
			failure = fmt.Errorf("%v; rollback failed: %w", failure, rollbackErr)
		} else if !rolledBack {
			failure = fmt.Errorf("%v; rollback readback did not match original order", failure)
		}
		return rfOrderResultMsg{entries: rollbackEntries, rolledBack: rolledBack, err: failure}
	}
}

func (model Model) probeRFReplaceCommand() tea.Cmd {
	probe := model.rfProbeReplace
	return func() tea.Msg {
		if probe == nil {
			return rfProbeResultMsg{err: fmt.Errorf("safe RF replace probe is unavailable")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		support, err := probe(ctx)
		return rfProbeResultMsg{support: support, err: err}
	}
}

func (model Model) currentRFReplaceSupport() control.RFReplaceSupport {
	if model.rfReplaceSupport == nil {
		return control.RFReplaceSupport{Known: true, Reason: "firmware reorder opcode is unavailable"}
	}
	return model.rfReplaceSupport()
}

func (model Model) filteredRFActions() []rfActionOption {
	query := strings.ToLower(strings.TrimSpace(model.rfActionQuery))
	if query == "" {
		return append([]rfActionOption(nil), rfActionCatalog...)
	}
	words := strings.Fields(query)
	var matches []rfActionOption
	for _, option := range rfActionCatalog {
		haystack := strings.ToLower(option.Label + " " + option.Args)
		matched := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, option)
		}
	}
	return matches
}

func (model *Model) beginRFActionPicker() {
	if _, ok := model.selectedRFEntry(); !ok {
		model.setNotice("No learned RF code is selected")
		return
	}
	if model.rfStageDirty {
		model.setNotice("Apply or roll back staged IDs before changing a mapping")
		return
	}
	model.rfActionPicker = true
	model.rfActionQuery = ""
	model.rfActionCursor = 0
	model.input.Blur()
}

func (model *Model) beginRFNameEdit() {
	if model.remote != nil && model.saveRF == nil {
		model.setNotice("Remote RF presentation editing is unavailable; board RF commands remain available")
		return
	}
	entry, ok := model.selectedRFEntry()
	if !ok {
		return
	}
	metadata, _ := model.rfValue.MetadataFor(appconfig.RFCodeKey{Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol})
	model.rfEditMode = "name"
	model.input.Prompt = "RF name › "
	model.input.SetValue(metadata.Name)
	model.input.CursorEnd()
	model.revealTerminal()
}

func (model *Model) beginRFCategoryPicker() {
	if model.remote != nil && model.saveRF == nil {
		model.setNotice("Remote RF presentation editing is unavailable; board RF commands remain available")
		return
	}
	if _, ok := model.selectedRFEntry(); !ok {
		return
	}
	model.rfCategoryPicker = true
	model.rfCategoryCursor = 0
	model.input.Blur()
}

func (model *Model) beginRFCategoryCreate() {
	if model.remote != nil && model.saveRF == nil {
		model.setNotice("Remote RF presentation editing is unavailable; board RF commands remain available")
		return
	}
	model.rfCategoryPicker = false
	model.rfEditMode = "category-name"
	model.input.Prompt = "Category name › "
	model.input.SetValue("")
	model.revealTerminal()
}

func (model *Model) finishRFEdit() (tea.Cmd, bool) {
	value := strings.TrimSpace(model.input.Value())
	switch model.rfEditMode {
	case "name":
		entry, ok := model.selectedRFEntry()
		if !ok {
			model.cancelRFModal()
			return nil, true
		}
		key := appconfig.RFCodeKey{Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol}
		model.updateRFMetadata(key, func(metadata *appconfig.RFMetadata) { metadata.Name = value })
		model.cancelRFModal()
		model.persistRFConfig("RF code name saved")
		return nil, true
	case "category-name":
		if value == "" {
			model.setNotice("Category name cannot be empty")
			return nil, true
		}
		for _, category := range model.rfValue.Categories {
			if strings.EqualFold(category.Name, value) {
				model.setNotice("Category name already exists")
				return nil, true
			}
		}
		model.rfCategoryDraft = value
		model.rfEditMode = "category-color"
		model.rfCategoryCursor = 0
		model.input.SetValue("")
		model.input.Blur()
		return nil, true
	}
	return nil, false
}

func (model *Model) finishRFCategoryColor() {
	if model.rfCategoryCursor < 0 || model.rfCategoryCursor >= len(appconfig.RFCategoryPalette) {
		return
	}
	model.rfValue.Categories = append(model.rfValue.Categories, appconfig.RFCategory{
		Name: model.rfCategoryDraft, Color: appconfig.RFCategoryPalette[model.rfCategoryCursor],
	})
	name := model.rfCategoryDraft
	model.cancelRFModal()
	model.persistRFConfig("Created RF category " + name)
}

func (model *Model) assignRFCategory(index int) {
	entry, ok := model.selectedRFEntry()
	if !ok {
		return
	}
	category := ""
	if index > 0 && index <= len(model.rfValue.Categories) {
		category = model.rfValue.Categories[index-1].Name
	}
	key := appconfig.RFCodeKey{Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol}
	model.updateRFMetadata(key, func(metadata *appconfig.RFMetadata) { metadata.Category = category })
	model.cancelRFModal()
	model.persistRFConfig("RF category assignment saved")
}

func (model *Model) updateRFMetadata(key appconfig.RFCodeKey, update func(*appconfig.RFMetadata)) {
	index := -1
	for candidate := range model.rfValue.Metadata {
		if model.rfValue.Metadata[candidate].Key.StableKey() == key.StableKey() {
			index = candidate
			break
		}
	}
	if index < 0 {
		model.rfValue.Metadata = append(model.rfValue.Metadata, appconfig.RFMetadata{Key: key})
		index = len(model.rfValue.Metadata) - 1
	}
	update(&model.rfValue.Metadata[index])
	if model.rfValue.Metadata[index].Name == "" && model.rfValue.Metadata[index].Category == "" {
		model.rfValue.Metadata = append(model.rfValue.Metadata[:index], model.rfValue.Metadata[index+1:]...)
	}
}

func (model *Model) toggleRFRadix() {
	if model.remote != nil && model.saveRF == nil {
		model.setNotice("Remote RF presentation editing is unavailable; board RF commands remain available")
		return
	}
	if strings.EqualFold(model.rfValue.DisplayRadix, "decimal") {
		model.rfValue.DisplayRadix = "hex"
	} else {
		model.rfValue.DisplayRadix = "decimal"
	}
	model.persistRFConfig("RF code view changed to " + strings.ToUpper(model.rfValue.DisplayRadix))
}

func (model *Model) persistRFConfig(success string) {
	if model.saveRF == nil {
		if model.remote != nil {
			model.setNotice("Remote RF presentation editing is unavailable; board RF commands remain available")
			return
		}
		model.setNotice(success + " for this session; persistence hook unavailable")
		return
	}
	if err := model.saveRF(model.rfValue); err != nil {
		model.appendLog("error", "save RF presentation: "+err.Error())
		return
	}
	model.setNotice(success)
}

func (model *Model) cancelRFModal() {
	model.rfActionPicker = false
	model.rfCategoryPicker = false
	model.rfEditMode = ""
	model.rfCategoryDraft = ""
	model.input.Prompt = "❯ "
	model.input.SetValue("")
	model.input.Focus()
	model.terminalVisible = false
}

func (model Model) rfActionPickerPage() string {
	entry, _ := model.selectedRFEntry()
	matches := model.filteredRFActions()
	lines := []string{
		sectionHeader(model.width, "SEARCH RF ACTION", "type to filter · ↑/↓ select · Enter map · Esc cancel"),
		kv("Selected code", fmt.Sprintf("ID %d · %s · %d bits · protocol %d", entry.ID, appconfig.FormatRFCode(entry.Code, model.rfValue.DisplayRadix), entry.Bits, entry.Protocol)),
		kv("Search", model.rfActionQuery+"▏"),
		"",
	}
	if len(matches) == 0 {
		return strings.Join(append(lines, warnStyle.Render("No actions match this search.")), "\n")
	}
	if model.rfActionCursor >= len(matches) {
		model.rfActionCursor = len(matches) - 1
	}
	start := model.rfActionCursor - 8
	if start < 0 {
		start = 0
	}
	end := start + 18
	if end > len(matches) {
		end = len(matches)
	}
	for index := start; index < end; index++ {
		line := fmt.Sprintf("%-42s  %s", matches[index].Label, labelStyle.Render(matches[index].Args))
		if index == model.rfActionCursor {
			line = selectedStyle.Copy().Width(model.width - 2).Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", labelStyle.Render(fmt.Sprintf("%d of %d actions match", len(matches), len(rfActionCatalog))))
	return strings.Join(lines, "\n")
}

func (model Model) rfCategoryPickerPage() string {
	entry, _ := model.selectedRFEntry()
	lines := []string{
		sectionHeader(model.width, "RF CATEGORY", "↑/↓ select · Enter assign · C create named category · Esc cancel"),
		kv("Selected code", fmt.Sprintf("ID %d · %s", entry.ID, appconfig.FormatRFCode(entry.Code, model.rfValue.DisplayRadix))),
		"",
	}
	values := []appconfig.RFCategory{{Name: "Unassigned", Color: "white"}}
	values = append(values, model.rfValue.Categories...)
	for index, category := range values {
		line := categorySwatch(category.Color) + " " + category.Name + "  " + labelStyle.Render(category.Color)
		if index == model.rfCategoryCursor {
			line = selectedStyle.Copy().Width(model.width - 2).Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", labelStyle.Render("Fixed palette order: red · blue · violet/purple · green · white"))
	return strings.Join(lines, "\n")
}

func (model Model) rfCategoryColorPage() string {
	lines := []string{
		sectionHeader(model.width, "CATEGORY COLOR", "fixed ordered palette · ←/→ or ↑/↓ · Enter save · Esc cancel"),
		kv("Category", model.rfCategoryDraft),
		"",
	}
	for index, color := range appconfig.RFCategoryPalette {
		line := categorySwatch(color) + " " + color
		if index == model.rfCategoryCursor {
			line = selectedStyle.Copy().Width(model.width - 2).Render("› " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func categorySwatch(color string) string {
	palette := map[string]lipgloss.Color{
		"red": "#F7768E", "blue": "#7AA2F7", "violet": "#BB9AF7",
		"purple": "#BB9AF7", "green": "#9ECE6A", "white": "#C0CAF5",
	}
	selected := palette[strings.ToLower(color)]
	if selected == "" {
		selected = colorMuted
	}
	return lipgloss.NewStyle().Foreground(selected).Render("●")
}

func (model Model) rfCategoryColor(name string) string {
	for _, category := range model.rfValue.Categories {
		if strings.EqualFold(category.Name, name) {
			return category.Color
		}
	}
	return "white"
}

func formatRFMappingUI(entry native.RFEntry) string {
	kinds := []string{"unmapped", "key", "menu", "relay", "side", "pwm"}
	behaviors := []string{"press", "toggle", "momentary", "up", "down", "stop"}
	kind := fmt.Sprintf("kind-%d", entry.ActionKind)
	if int(entry.ActionKind) < len(kinds) {
		kind = kinds[entry.ActionKind]
	}
	if entry.ActionKind == native.RFActionNone {
		return kind
	}
	behavior := fmt.Sprintf("behavior-%d", entry.Behavior)
	if int(entry.Behavior) < len(behaviors) {
		behavior = behaviors[entry.Behavior]
	}
	value := entry.ActionValue
	if entry.ActionKind == native.RFActionKey || entry.ActionKind == native.RFActionRelay {
		value++
	}
	return fmt.Sprintf("%s:%d/%s", kind, value, behavior)
}

func normalizeRFCodeTokens(text, radix string) string {
	fields := strings.Fields(text)
	for index, field := range fields {
		prefix := "code="
		position := strings.Index(strings.ToLower(field), prefix)
		if position < 0 {
			continue
		}
		value := field[position+len(prefix):]
		suffix := ""
		for len(value) > 0 {
			last := value[len(value)-1]
			if (last >= '0' && last <= '9') || (last >= 'a' && last <= 'f') ||
				(last >= 'A' && last <= 'F') || last == 'x' || last == 'X' {
				break
			}
			suffix = string(last) + suffix
			value = value[:len(value)-1]
		}
		base := 10
		if strings.HasPrefix(strings.ToLower(value), "0x") {
			base = 0
		}
		parsed, err := strconv.ParseUint(value, base, 32)
		if err == nil {
			fields[index] = field[:position] + prefix + appconfig.FormatRFCode(uint32(parsed), radix) + suffix
		}
	}
	return strings.Join(fields, " ")
}
