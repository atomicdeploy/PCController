//go:build windows

package ui

import (
	"context"
	"fmt"
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"math"
	"pccontroller.local/desktop/internal/dialogs"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
	"strconv"
	"strings"
	"time"
)

type controllerTable struct {
	walk.TableModelBase
	rows          [][]string
	receipts      map[string]presentation.RFReceipt
	muted, active walk.Color
	highContrast  bool
}

func (m *controllerTable) StyleCell(style *walk.CellStyle) {
	if m.highContrast || style.Row() < 0 || style.Row() >= len(m.rows) || len(m.rows[style.Row()]) == 0 {
		return
	}
	row := m.rows[style.Row()]
	if receipt, ok := m.receipts[row[0]]; ok {
		if !receipt.Learned {
			style.TextColor = m.muted
		}
		if style.Col() == len(row)-2 && row[len(row)-2] == "●" {
			style.TextColor = m.active
		}
	}
}

func (m *controllerTable) RowCount() int { return len(m.rows) }
func (m *controllerTable) Value(row, column int) any {
	if row < 0 || row >= len(m.rows) || column < 0 || column >= len(m.rows[row]) {
		return ""
	}
	if column == 0 && len(m.rows[row]) > 0 {
		if receipt, ok := m.receipts[m.rows[row][0]]; ok && !receipt.Learned {
			return "+"
		}
	}
	return m.rows[row][column]
}

// Typed native fields, not an editable JSON dump. Requests run outside the
// window thread, errors retain drafts, and modal dismissal cannot lose edits.
func (s *Shell) openControllerForm(key string) {
	s.openControllerFormObserved(key, nil)
}

func (s *Shell) openControllerFormObserved(key string, observe func(*walk.Dialog)) {
	if s.controller.LoadPage == nil {
		return
	}
	var dialog *walk.Dialog
	var body *walk.Composite
	var actionsBody *walk.ScrollView
	var heading *walk.Label
	var headingIcon *walk.Label
	var status *walk.Label
	var closeButton, refreshButton *walk.PushButton
	busy, dirty, closed := false, false, false
	getters := map[string]func() string{}
	setters := map[string]func(string){}
	baseline := map[string]string{}
	var saveButtons []**walk.PushButton
	actionButtons := map[string]**walk.PushButton{}
	building := false
	var formRows []*controllerFormRow
	footer := &controllerFormFooter{}
	reflow := func() {
		if dialog == nil || dialog.IsDisposed() {
			return
		}
		width := dialog.ClientBounds().Width - 64
		for _, row := range formRows {
			row.reflow(width)
		}
		footer.reflow(dialog.ClientBounds().Width - 40)
	}
	styleForm := func() {
		appearance, _ := winfx.ResolveAppearance(s.Config().UI.Theme)
		if appearance.HighContrast {
			return
		}
		colors := controllerPalette(appearance.Dark)
		for _, row := range formRows {
			if row.label != nil {
				row.label.SetTextColor(colors.secondary)
			}
			if key == "connection" && row.root != nil && row.root.Name() == "form.row.state" && row.editor.Children().Len() > 0 {
				if value, ok := row.editor.Children().At(0).(*walk.Label); ok {
					color := colors.secondary
					if value.Text() == "Connected" {
						color = colors.success
					}
					value.SetTextColor(color)
				}
			}
		}
		if headingIcon != nil {
			headingIcon.SetTextColor(colors.accent)
		}
	}
	type conditionalRow struct {
		field  presentation.Field
		widget **walk.Composite
	}
	var conditionalRows []conditionalRow
	var timeline *walk.CustomWidget
	var refreshActionState func()
	changed := func() {
		if building {
			return
		}
		dirty = false
		for key, get := range getters {
			if get() != baseline[key] {
				dirty = true
				break
			}
		}
		values := map[string]string{}
		for key, get := range getters {
			values[key] = get()
		}
		for _, row := range conditionalRows {
			if *row.widget != nil {
				(*row.widget).SetVisible(controllerFormFieldVisible(row.field, values))
			}
		}
		for _, button := range saveButtons {
			if *button != nil {
				(*button).SetEnabled(dirty && !busy)
			}
		}
		if refreshActionState != nil {
			refreshActionState()
		}
		if timeline != nil {
			timeline.Invalidate()
		}
	}
	var table *walk.TableView
	var tableModel *controllerTable
	var rfSession controllerRFSession
	var renderVersion, lastPageVersion uint64
	liveBusy, suppressSelection := false, false
	var page presentation.Page
	refreshActionState = func() {
		selected := table != nil && table.CurrentIndex() >= 0 && table.CurrentIndex() < len(page.Rows)
		index := -1
		if selected {
			index = table.CurrentIndex()
		}
		for _, action := range page.Actions {
			if button := actionButtons[controllerFormActionSlot(key, action.ID)]; button != nil && *button != nil {
				(*button).SetEnabled(!busy && action.Enabled && (!action.SelectionRequired || selected) && controllerFormAdjacentSelection(action, index, len(page.Rows)) && (!controllerFormRequiresChanges(action) || dirty))
			}
		}
	}
	var theme *dialogs.ThemeSession
	var load func()
	var render func(presentation.Page)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	askDiscard := func() bool {
		return !dirty || walk.MsgBox(dialog, s.nativeText("Unsaved changes"), s.nativeText("Discard changes to this form?"), walk.MsgBoxYesNo|walk.MsgBoxIconWarning) == walk.DlgCmdYes
	}
	render = func(next presentation.Page) {
		renderVersion++
		if key == "rf" {
			rfSession.decorate(&next, time.Now())
		}
		building = true
		dirty = false
		defer func() { building = false; changed() }()
		selected := ""
		activeSection := ""
		if tabs := findControllerFormTabs(body); tabs != nil && tabs.CurrentIndex() >= 0 {
			activeSection = tabs.Pages().At(tabs.CurrentIndex()).Title()
		}
		if table != nil && !next.ClearSelection {
			i := table.CurrentIndex()
			if i >= 0 && i < len(page.Rows) && len(page.Rows[i]) > 0 {
				selected = page.Rows[i][0]
			}
		}
		if next.SelectedRow != "" {
			selected = next.SelectedRow
		}
		for body.Children().Len() > 0 {
			body.Children().At(0).Dispose()
		}
		for actionsBody.Children().Len() > 0 {
			actionsBody.Children().At(0).Dispose()
		}
		page = next
		getters = map[string]func() string{}
		setters = map[string]func(string){}
		baseline = map[string]string{}
		saveButtons = nil
		conditionalRows = nil
		formRows = nil
		actionButtons = map[string]**walk.PushButton{}
		table = nil
		timeline = nil
		_ = dialog.SetTitle(s.nativeText(next.Title) + " — " + s.controller.Title)
		_ = heading.SetText(strings.ReplaceAll(s.nativeText(next.Title), "&", "&&"))
		children := []d.Widget{}
		if len(next.Columns) > 0 {
			appearance, _ := winfx.ResolveAppearance(s.Config().UI.Theme)
			colors := controllerPalette(appearance.Dark)
			tableModel = &controllerTable{rows: next.Rows, receipts: next.Receipts, muted: colors.secondary, active: colors.success, highContrast: appearance.HighContrast}
			columns := []d.TableViewColumn{}
			for index, name := range next.Columns {
				width := 130
				if key == "rf" && index < 6 {
					width = []int{45, 160, 90, 185, 60, 90}[index]
				}
				if key == "sequence" && len(next.Timeline) > 0 && index < 6 {
					width = []int{45, 120, 95, 70, 170, 95}[index]
				}
				columns = append(columns, d.TableViewColumn{Title: s.nativeText(name), Width: width})
			}
			maximumHeight := 0
			minimumHeight := 140
			if len(next.Fields) > 0 {
				maximumHeight = max(80, min(180, 36+len(next.Rows)*24))
				minimumHeight = min(100, maximumHeight)
			}
			if len(next.Timeline) > 0 {
				minimumHeight, maximumHeight = 100, 140
			}
			children = append(children, d.TableView{AssignTo: &table, Columns: columns, Model: tableModel, LastColumnStretched: true, ColumnsSizable: true, MinSize: d.Size{Height: minimumHeight}, MaxSize: d.Size{Height: maximumHeight}, StretchFactor: 1})
		}
		if len(next.Timeline) > 0 {
			selectedKey := func() string {
				if table != nil {
					if i := table.CurrentIndex(); i >= 0 && i < len(page.Rows) && len(page.Rows[i]) > 0 {
						return page.Rows[i][0]
					}
				}
				return ""
			}
			at := func() string {
				if get := getters["step.at_us"]; get != nil {
					return get()
				}
				return ""
			}
			setAt := func(value string) {
				if set := setters["step.at_us"]; set != nil {
					set(value)
				}
			}
			selectKey := func(key string) {
				if table != nil {
					for i, row := range page.Rows {
						if len(row) > 0 && row[0] == key {
							table.SetCurrentIndex(i)
							return
						}
					}
				}
			}
			children = append(children, s.controllerTimelineWidget(next.Timeline, selectedKey, selectKey, at, setAt, &timeline))
		}
		section := ""
		var sectionRows []d.Widget
		var sections []d.TabPage
		var sectionKeys []string
		var fieldTabs *walk.TabWidget
		type sectionAction struct {
			button  **walk.PushButton
			section string
		}
		var sectionActions []sectionAction
		var buttonGrid *walk.Composite
		showActions := func() {
			active := ""
			if len(sectionKeys) == 1 {
				active = sectionKeys[0]
			}
			if fieldTabs != nil && fieldTabs.CurrentIndex() >= 0 && fieldTabs.CurrentIndex() < len(sections) {
				active = sectionKeys[fieldTabs.CurrentIndex()]
			}
			for _, item := range sectionActions {
				if *item.button != nil {
					(*item.button).SetVisible(item.section == "" || item.section == active)
				}
			}
			if buttonGrid != nil {
				buttonGrid.RequestLayout()
			}
			footer.reflow(dialog.ClientBounds().Width - 40)
		}
		flushSection := func() {
			if len(sectionRows) == 0 {
				return
			}
			title := section
			if title == "" {
				title = "Options"
			}
			sectionKeys = append(sectionKeys, section)
			sections = append(sections, d.TabPage{Title: s.nativeText(title), Layout: d.VBox{MarginsZero: true}, Children: []d.Widget{d.ScrollView{Name: "form.viewport", Layout: d.VBox{Margins: d.Margins{Left: 4, Top: 12, Right: 4, Bottom: 12}, Spacing: 14, Alignment: d.AlignHNearVNear}, Children: sectionRows, StretchFactor: 1}}})
			sectionRows = nil
		}
		for _, item := range controllerFormOrderedFields(next.Fields) {
			field := item
			if field.Hidden {
				value := field.Value
				baseline[field.Key] = field.Value
				getters[field.Key] = func() string { return value }
				setters[field.Key] = func(next string) { value = next }
				continue
			}
			if field.Section != section {
				flushSection()
				section = field.Section
			}
			baseline[field.Key] = field.Value
			row := []d.Widget{}
			if field.Multiline {
				var input *walk.TextEdit
				row = append(row, d.TextEdit{AssignTo: &input, Text: field.Value, ReadOnly: field.ReadOnly, VScroll: true, MinSize: d.Size{Height: 130}, StretchFactor: 1, OnTextChanged: changed})
				if !field.ReadOnly {
					getters[field.Key] = func() string { return input.Text() }
					setters[field.Key] = func(value string) { _ = input.SetText(value) }
				}
			} else if field.ReadOnly {
				row = append(row, d.Label{Name: "form.value." + field.Key, Text: field.Value, TextAlignment: d.AlignNear, Alignment: d.AlignHNearVCenter, RightToLeftReading: s.isRTL(), EllipsisMode: d.EllipsisEnd, MinSize: d.Size{Height: 32}, StretchFactor: 1})
				row = append(row, d.HSpacer{})
			} else if field.Numeric {
				var input *walk.LineEdit
				// Keep the literal draft. NumberEdit.Value retains the previous
				// valid number while its native edit contains invalid text.
				row = append(row, d.LineEdit{Name: "form.value." + field.Key, AssignTo: &input, Text: field.Value, MinSize: d.Size{Width: 80, Height: 32}, MaxSize: d.Size{Height: 32}, StretchFactor: 1, OnTextChanged: changed})
				getters[field.Key] = func() string {
					if input == nil {
						return field.Value
					}
					return input.Text()
				}
				setters[field.Key] = func(value string) { _ = input.SetText(value) }
			} else if len(field.Choices) == 2 && field.Choices[0] == "false" && field.Choices[1] == "true" {
				var input *walk.CheckBox
				row = append(row, d.CheckBox{Name: "form.value." + field.Key, AssignTo: &input, Checked: field.Value == "true", Alignment: d.AlignHNearVCenter, MinSize: d.Size{Width: 32, Height: 32}, MaxSize: d.Size{Width: 32, Height: 32}, OnCheckedChanged: changed}, d.HSpacer{})
				getters[field.Key] = func() string {
					if input == nil {
						return field.Value
					}
					return strconv.FormatBool(input.Checked())
				}
				setters[field.Key] = func(value string) { input.SetChecked(value == "true") }
			} else if len(field.Choices) > 0 {
				var combo *walk.ComboBox
				index := -1
				for i, value := range field.Choices {
					if value == field.Value {
						index = i
					}
				}
				if index < 0 {
					field.Choices = append([]string{field.Value}, field.Choices...)
					if len(field.ChoiceLabels) > 0 {
						field.ChoiceLabels = append([]string{field.Value}, field.ChoiceLabels...)
					}
					index = 0
				}
				labels := field.Choices
				if len(field.ChoiceLabels) == len(field.Choices) {
					labels = make([]string, len(field.ChoiceLabels))
					for i, label := range field.ChoiceLabels {
						labels[i] = s.nativeText(label)
					}
				}
				row = append(row, d.ComboBox{Name: "form.value." + field.Key, AssignTo: &combo, Model: labels, CurrentIndex: index, MinSize: d.Size{Height: 32}, MaxSize: d.Size{Height: 32}, StretchFactor: 1, OnCurrentIndexChanged: changed})
				getters[field.Key] = func() string {
					if combo == nil {
						return field.Value
					}
					if index := combo.CurrentIndex(); index >= 0 && index < len(field.Choices) {
						return field.Choices[index]
					}
					return field.Value
				}
				setters[field.Key] = func(value string) {
					for index, choice := range field.Choices {
						if choice == value {
							_ = combo.SetCurrentIndex(index)
							return
						}
					}
					// A selected row may carry a newer/legacy enum value not in
					// this host's advertised editor choices. Preserve it verbatim.
					field.Choices = append(field.Choices, value)
					labels := field.Choices
					if len(field.ChoiceLabels) > 0 {
						field.ChoiceLabels = append(field.ChoiceLabels, value)
						labels = make([]string, len(field.ChoiceLabels))
						for i, label := range field.ChoiceLabels {
							labels[i] = s.nativeText(label)
						}
					}
					_ = combo.SetModel(labels)
					_ = combo.SetCurrentIndex(len(field.Choices) - 1)
				}
			} else {
				var edit *walk.LineEdit
				row = append(row, d.LineEdit{Name: "form.value." + field.Key, AssignTo: &edit, Text: field.Value, MinSize: d.Size{Height: 32}, MaxSize: d.Size{Height: 32}, StretchFactor: 1, OnTextChanged: changed})
				getters[field.Key] = func() string {
					if edit == nil {
						return field.Value
					}
					return edit.Text()
				}
				setters[field.Key] = func(value string) { _ = edit.SetText(value) }
				if field.IconPicker {
					row = append(row, d.PushButton{Text: s.nativeText("Choose icon"), MaxSize: d.Size{Width: 110}, OnClicked: func() {
						if value, ok := s.chooseControllerIconOwned(dialog, edit.Text()); ok {
							_ = edit.SetText(value)
						}
					}})
				}
				if field.FileFilter != "" {
					row = append(row, d.PushButton{Text: "Browse…", MaxSize: d.Size{Width: 100}, OnClicked: func() {
						picker := walk.FileDialog{Title: field.Label, Filter: field.FileFilter, FilePath: edit.Text()}
						show := picker.ShowOpen
						if field.SaveFile {
							show = picker.ShowSave
						}
						if ok, err := show(dialog); err != nil {
							status.SetText(err.Error())
						} else if ok {
							_ = edit.SetText(picker.FilePath)
						}
					}})
				}
			}
			rowLayout := &controllerFormRow{multiline: field.Multiline}
			sectionRows = append(sectionRows, d.Composite{Name: "form.row." + field.Key, AssignTo: &rowLayout.root, Layout: d.HBox{MarginsZero: true, Spacing: 10, Alignment: d.AlignHNearVCenter}, OnSizeChanged: func() { rowLayout.reflow(dialog.ClientBounds().Width - 64) }, Children: []d.Widget{
				d.Label{Name: "form.label." + field.Key, AssignTo: &rowLayout.label, Text: s.nativeText(field.Label), ToolTipText: s.nativeText(field.Label), RightToLeftReading: s.isRTL(), EllipsisMode: d.EllipsisEnd, Alignment: d.AlignHNearVCenter, MinSize: d.Size{Width: 160, Height: 32}, MaxSize: d.Size{Width: 160, Height: 32}},
				d.Composite{AssignTo: &rowLayout.editor, Layout: d.HBox{MarginsZero: true, Spacing: 8, Alignment: d.AlignHNearVCenter}, Children: row, StretchFactor: 4},
			}})
			formRows = append(formRows, rowLayout)
			if field.WhenKey != "" {
				conditionalRows = append(conditionalRows, conditionalRow{field, &rowLayout.root})
			}
		}
		flushSection()
		if len(sections) == 1 {
			children = append(children, sections[0].Children...)
		} else if len(sections) > 1 {
			children = append(children, d.TabWidget{AssignTo: &fieldTabs, Pages: sections, StretchFactor: 1, OnCurrentIndexChanged: showActions})
		}
		if next.Output != "" {
			children = append(children, d.TextEdit{Text: next.Output, ReadOnly: true, VScroll: true, MinSize: d.Size{Height: 160}, StretchFactor: 1})
		}
		buttons := []d.Widget{d.HSpacer{}}
		for _, item := range next.Actions {
			action := item
			var button *walk.PushButton
			slot := controllerFormActionSlot(key, action.ID)
			actionButtons[slot] = &button
			sectionActions = append(sectionActions, sectionAction{&button, action.Section})
			if controllerFormRequiresChanges(action) && action.Enabled {
				saveButtons = append(saveButtons, &button)
			}
			buttons = append(buttons, d.PushButton{AssignTo: &button, Column: len(buttons), Text: s.nativeText(action.Label), Enabled: action.Enabled, MinSize: d.Size{Width: 96, Height: 32}, MaxSize: d.Size{Width: 240, Height: 32}, OnClicked: func() {
				action := action
				for _, current := range page.Actions {
					if controllerFormActionSlot(key, current.ID) == slot {
						action = current
						break
					}
				}
				if !action.Enabled {
					return
				}
				if busy || s.controller.SubmitPage == nil {
					return
				}
				if controllerFormRequiresChanges(action) && !dirty {
					return
				}
				if (action.ID == "new" || action.DiscardChanges) && !askDiscard() {
					return
				}
				values := map[string]string{}
				for name, get := range getters {
					values[name] = get()
				}
				if err := validateControllerFormAction(page.Fields, action, values); err != nil && action.ID != "new" && !action.DiscardChanges {
					status.SetText(err.Error())
					return
				}
				if table != nil {
					index := table.CurrentIndex()
					if index >= 0 && index < len(page.Rows) && len(page.Rows[index]) > 0 {
						values["selected"] = page.Rows[index][0]
					}
				}
				if action.SelectionRequired && values["selected"] == "" {
					status.SetText("Select an item first.")
					return
				}
				if action.Confirm {
					message := controllerFormConfirmation(key, action, values)
					if key == "firmware" {
						message = action.Label + " using " + values["method"] + "? This interrupts board control during programming."
						if action.ID == "upload" {
							if values["selected"] == "" {
								status.SetText("Select a stored firmware image first.")
								return
							}
							message += "\n\nSHA-256: " + values["selected"] + "\nEEPROM preservation remains enabled."
						}
					}
					if walk.MsgBox(dialog, action.Label, message, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
						return
					}
				}
				busy = true
				renderVersion++
				body.SetEnabled(false)
				actionsBody.SetEnabled(false)
				refreshButton.SetEnabled(false)
				status.SetText("")
				go func() {
					requestCtx, done := context.WithTimeout(ctx, time.Duration(max(15, min(300, action.TimeoutSeconds)))*time.Second)
					defer done()
					updated, err := s.controller.SubmitPage(requestCtx, key, action.ID, values)
					dialog.Synchronize(func() {
						if closed {
							return
						}
						busy = false
						body.SetEnabled(true)
						actionsBody.SetEnabled(true)
						refreshButton.SetEnabled(true)
						if err != nil {
							status.SetText(err.Error())
							return
						}
						dirty = false
						render(updated)
					})
				}()
			}})
		}
		if len(buttons) > 1 {
			// Reserve caption-sized cells; the footer reflows the same controls.
			if err := (d.Composite{AssignTo: &buttonGrid, Layout: d.Grid{Columns: len(buttons), Spacing: 8, MarginsZero: true, Alignment: d.AlignHNearVCenter}, Children: buttons}).Create(d.NewBuilder(actionsBody)); err != nil {
				status.SetText(err.Error())
			}
		}
		if err := (d.Composite{Layout: d.VBox{MarginsZero: true, Spacing: 12}, Children: children}).Create(d.NewBuilder(body)); err != nil {
			status.SetText(err.Error())
		}
		showActions()
		if fieldTabs != nil && activeSection != "" {
			for index, item := range sections {
				if item.Title == activeSection {
					_ = fieldTabs.SetCurrentIndex(index)
					break
				}
			}
		}
		if table != nil && selected != "" {
			for i, row := range next.Rows {
				if len(row) > 0 && row[0] == selected {
					_ = table.SetCurrentIndex(i)
					break
				}
			}
		}
		if table != nil && len(next.RowValues) > 0 {
			previousIndex, selecting := table.CurrentIndex(), false
			applySelection := func() {
				if selecting || suppressSelection {
					return
				}
				index := table.CurrentIndex()
				if index < 0 || index >= len(page.Rows) || len(page.Rows[index]) == 0 {
					return
				}
				if dirty && !askDiscard() {
					selecting = true
					_ = table.SetCurrentIndex(previousIndex)
					selecting = false
					return
				}
				previousIndex = index
				values := page.RowValues[page.Rows[index][0]]
				if values == nil {
					return
				}
				wasBuilding := building
				building = true
				for key, value := range values {
					if set := setters[key]; set != nil {
						set(value)
						baseline[key] = value
					}
				}
				building = wasBuilding
				changed()
			}
			table.CurrentIndexChanged().Attach(applySelection)
			applySelection()
		}
		if table != nil {
			table.CurrentIndexChanged().Attach(refreshActionState)
		}
		for name, value := range next.DraftBaseline {
			baseline[name] = value
		}
		dirty = false
		reflow()
		theme.Refresh(s.Config().UI.Theme, s.motionEnabled)
		styleForm()
		if observe != nil {
			observe(dialog)
		}
	}
	load = func() {
		if busy || !askDiscard() {
			return
		}
		busy = true
		body.SetEnabled(false)
		actionsBody.SetEnabled(false)
		refreshButton.SetEnabled(false)
		status.SetText(s.nativeText("Loading…"))
		version := uint64(0)
		if s.controller.PageVersion != nil {
			version = s.controller.PageVersion(key)
		}
		go func() {
			requestCtx, done := context.WithTimeout(ctx, 10*time.Second)
			defer done()
			next, err := s.controller.LoadPage(requestCtx, key)
			dialog.Synchronize(func() {
				if closed {
					return
				}
				busy = false
				body.SetEnabled(true)
				actionsBody.SetEnabled(true)
				refreshButton.SetEnabled(true)
				status.SetText("")
				if err != nil {
					status.SetText(err.Error())
					return
				}
				render(next)
				lastPageVersion = version
			})
		}()
	}
	spec := d.Dialog{AssignTo: &dialog, Title: "PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 420, Height: 300}, Size: d.Size{Width: 720, Height: controllerFormWindowHeight(key)}, Layout: d.VBox{Margins: d.Margins{Left: 20, Top: 18, Right: 20, Bottom: 18}, Spacing: 12}, CancelButton: &closeButton, OnSizeChanged: reflow, Children: []d.Widget{
		d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 12, Alignment: d.AlignHNearVCenter}, MaxSize: d.Size{Height: 44}, Children: []d.Widget{
			d.Label{AssignTo: &headingIcon, Text: controllerFormGlyph(key), Font: d.Font{Family: "Segoe MDL2 Assets", PointSize: 16}, TextAlignment: d.AlignCenter, MinSize: d.Size{Width: 32, Height: 32}, MaxSize: d.Size{Width: 32, Height: 32}},
			d.Label{AssignTo: &heading, Text: "PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 16, Bold: true}, Alignment: d.AlignHNearVCenter, EllipsisMode: d.EllipsisEnd, MinSize: d.Size{Height: 36}, StretchFactor: 1},
			d.HSpacer{},
		}},
		d.Composite{AssignTo: &body, Layout: d.VBox{MarginsZero: true}, StretchFactor: 1},
		d.Label{Name: "form.status", AssignTo: &status, Text: "", EllipsisMode: d.EllipsisEnd, MinSize: d.Size{Height: 20}, MaxSize: d.Size{Height: 20}},
		d.Composite{Name: "form.footer", AssignTo: &footer.root, Layout: d.Grid{Columns: 3, MarginsZero: true, Spacing: 8, Alignment: d.AlignHNearVCenter}, MaxSize: d.Size{Height: 32}, Children: []d.Widget{
			d.Composite{AssignTo: &footer.refreshCell, Column: 0, MaxSize: d.Size{Width: 92}, Layout: d.HBox{MarginsZero: true, SpacingZero: true}, Children: []d.Widget{
				d.PushButton{AssignTo: &refreshButton, Text: s.nativeText("Refresh"), MinSize: d.Size{Width: 92, Height: 32}, MaxSize: d.Size{Width: 92, Height: 32}, OnClicked: func() { load() }}, d.HSpacer{},
			}},
			d.ScrollView{AssignTo: &actionsBody, Column: 1, VerticalFixed: true, Layout: d.VBox{MarginsZero: true, SpacingZero: true, Alignment: d.AlignHNearVNear}, StretchFactor: 4},
			d.Composite{AssignTo: &footer.closeCell, Column: 2, MaxSize: d.Size{Width: 92}, Layout: d.HBox{MarginsZero: true, SpacingZero: true}, Children: []d.Widget{
				d.HSpacer{}, d.PushButton{AssignTo: &closeButton, Text: s.nativeText("Close"), MinSize: d.Size{Width: 92, Height: 32}, MaxSize: d.Size{Width: 92, Height: 32}, OnClicked: func() { dialog.Cancel() }},
			}},
		}},
	}}
	if err := spec.Create(s.mw); err != nil {
		s.showError("PCController", err)
		return
	}
	defer dialog.Dispose()
	footer.actions, footer.refresh, footer.close = actionsBody, refreshButton, closeButton
	_ = dialog.SetRightToLeftLayout(s.isRTL())
	_ = dialog.SetRightToLeftReading(s.isRTL())
	geometry, err := winfx.AttachResponsiveDialogGeometry(uintptr(dialog.Handle()))
	if err != nil {
		s.showError("PCController", err)
		return
	}
	defer geometry.Dispose()
	dialog.SetCenterInOwnerWhenRun(false)
	theme = dialogs.NewThemeSession(dialog)
	defer theme.Dispose()
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	_ = theme.WatchSystemChanges(func() {
		styleForm()
		if tableModel != nil {
			appearance, _ := winfx.ResolveAppearance(s.Config().UI.Theme)
			colors := controllerPalette(appearance.Dark)
			tableModel.highContrast, tableModel.muted, tableModel.active = appearance.HighContrast, colors.secondary, colors.success
			if table != nil {
				table.Invalidate()
			}
		}
	})
	_ = dialog.SetIcon(s.appIcon)
	if err := winfx.PrepareOwnedDialogPlacement(uintptr(dialog.Handle()), uintptr(s.mw.Handle()), winfx.DialogPlacement{X: -1, Y: -1, Width: 720, Height: controllerFormWindowHeight(key)}); err != nil {
		s.showError("PCController", err)
		return
	}
	dialog.Closing().Attach(func(cancelClose *bool, _ walk.CloseReason) {
		if busy || !askDiscard() {
			*cancelClose = true
		} else {
			closed = true
			cancel()
		}
	})
	load()
	updateRows := func(next presentation.Page) {
		if table == nil || tableModel == nil {
			return
		}
		selected := ""
		if index := table.CurrentIndex(); index >= 0 && index < len(page.Rows) && len(page.Rows[index]) > 0 {
			selected = page.Rows[index][0]
		}
		page.Rows, page.RowValues, page.Receipts, page.Owner = next.Rows, next.RowValues, next.Receipts, next.Owner
		page.Actions = next.Actions
		for slot, button := range actionButtons {
			if *button == nil {
				continue
			}
			found := false
			for _, action := range page.Actions {
				if controllerFormActionSlot(key, action.ID) == slot {
					found = true
					_ = (*button).SetText(s.nativeText(action.Label))
					(*button).SetEnabled(action.Enabled && !busy && (!controllerFormRequiresChanges(action) || dirty))
					break
				}
			}
			if !found {
				(*button).SetEnabled(false)
			}
		}
		rfSession.decorate(&page, time.Now())
		tableModel.rows, tableModel.receipts = page.Rows, page.Receipts
		suppressSelection = true
		tableModel.PublishRowsReset()
		for index, row := range page.Rows {
			if len(row) > 0 && row[0] == selected {
				_ = table.SetCurrentIndex(index)
				break
			}
		}
		suppressSelection = false
		refreshActionState()
		reflow()
	}
	if key == "rf" {
		go func() {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					dialog.Synchronize(func() {
						if !closed && tableModel != nil && rfSession.decorate(&page, time.Now()) {
							updateRows(page)
						}
					})
				}
			}
		}()
	}
	if (key == "events" || key == "firmware" || key == "updates" || key == "rf" || key == "buzzer") && s.controller.PageChanges != nil {
		resume := make(chan struct{}, 1)
		retryLive := func() {
			time.AfterFunc(100*time.Millisecond, func() {
				select {
				case <-ctx.Done():
				case resume <- struct{}{}:
				default:
				}
			})
		}
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-resume:
				case _, ok := <-s.controller.PageChanges:
					if !ok {
						return
					}
				}
				// Coalesce event bursts, never poll the serial owner or reset a draft.
				delay := 300 * time.Millisecond
				if key == "rf" {
					delay = 25 * time.Millisecond
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				dialog.Synchronize(func() {
					if closed {
						return
					}
					if busy {
						retryLive()
						return
					}
					version := uint64(0)
					if s.controller.PageVersion != nil {
						version = s.controller.PageVersion(key)
						if version == lastPageVersion {
							return
						}
					}
					if key == "rf" || key == "buzzer" {
						if liveBusy {
							retryLive()
							return
						}
						liveBusy = true
						generation := renderVersion
						go func() {
							request, done := context.WithTimeout(ctx, 5*time.Second)
							defer done()
							next, err := s.controller.LoadPage(request, key)
							dialog.Synchronize(func() {
								liveBusy = false
								if closed {
									return
								}
								if busy || generation != renderVersion {
									retryLive()
									return
								}
								if err != nil {
									status.SetText(err.Error())
									return
								}
								lastPageVersion = version
								if key == "buzzer" {
									next.DraftBaseline = map[string]string{}
									for i := range next.Fields {
										f := &next.Fields[i]
										if f.Hidden || f.ReadOnly {
											continue
										}
										if get := getters[f.Key]; get != nil {
											f.Value = get()
											next.DraftBaseline[f.Key] = baseline[f.Key]
										}
									}
									render(next)
								} else {
									updateRows(next)
								}
							})
						}()
						return
					}
					if !closed && !busy && !dirty {
						load()
					}
				})
			}
		}()
	}
	dialog.Run()
}

func controllerMissingPage(key string) error {
	return fmt.Errorf("native page %q is not available", key)
}

func controllerFormActionSlot(key, id string) string {
	if key == "rf" && (id == "learn" || id == "cancel") {
		return "learning"
	}
	return id
}

func findControllerFormTabs(root walk.Container) *walk.TabWidget {
	for index := 0; index < root.Children().Len(); index++ {
		child := root.Children().At(index)
		if tabs, ok := child.(*walk.TabWidget); ok {
			return tabs
		}
		if container, ok := child.(walk.Container); ok {
			if tabs := findControllerFormTabs(container); tabs != nil {
				return tabs
			}
		}
	}
	return nil
}

// Fields from different capabilities may be appended out of order. Each section
// owns one tab; appending a live fact must never create a duplicate editor tab.
func controllerFormOrderedFields(fields []presentation.Field) []presentation.Field {
	var order []string
	groups := map[string][]presentation.Field{}
	for _, field := range fields {
		if _, exists := groups[field.Section]; !exists {
			order = append(order, field.Section)
		}
		groups[field.Section] = append(groups[field.Section], field)
	}
	ordered := make([]presentation.Field, 0, len(fields))
	for _, section := range order {
		ordered = append(ordered, groups[section]...)
	}
	return ordered
}

func controllerFormConfirmation(key string, action presentation.PageAction, values map[string]string) string {
	if (key == "host-automations" || key == "rf-bindings") && action.ID == "run" {
		return "Run this rule now? Its configured hardware, application, process or OS actions may execute.\n\n" + values["selected"]
	}
	if key == "melodies" && action.ID == "remove-melody" {
		return "Remove this stored melody? Built-in recovery melodies remain available as fallbacks.\n\n" + values["selected"]
	}
	if key == "effect-library" && action.ID == "import" && values["mode"] == "replace" {
		return "Replace the entire effect library? Effects and folders absent from this file will be removed."
	}
	if key == "updates" && action.ID == "apply" {
		kind, digest := values["selected_kind"], values["selected"]
		if values["selected_sha256"] != "" {
			digest = values["selected_sha256"]
		}
		warning := "Board control is interrupted during programming."
		if kind == "eeprom" {
			warning = "This replaces the board's EEPROM settings and RF assignments."
		}
		if kind == "host-executable" {
			warning = "The controller host will restart through its approved updater."
		}
		return "Apply " + kind + "?\n\nSHA-256: " + digest + "\n\n" + warning
	}
	if key == "programs" && action.ID == "delete" {
		return "Delete effect “" + values["name"] + "” (" + values["selected"] + ")?"
	}
	if values["selected"] != "" && action.SelectionRequired {
		return action.Label + "?\n\n" + values["selected"]
	}
	if key == "host-tools" && action.ID == "power" {
		return "Request " + values["action"] + " on the selected host?"
	}
	return action.Label + "?"
}

func controllerFormRequiresChanges(action presentation.PageAction) bool {
	return action.ID == "save" || action.RequiresChanges
}

func controllerFormAdjacentSelection(action presentation.PageAction, index, count int) bool {
	return action.SelectionOffset == 0 || index >= 0 && index+action.SelectionOffset >= 0 && index+action.SelectionOffset < count
}

func validateControllerFormAction(fields []presentation.Field, action presentation.PageAction, values map[string]string) error {
	if action.Section == "" {
		return validateControllerFormFields(fields, values)
	}
	var relevant []presentation.Field
	for _, field := range fields {
		if field.Section == action.Section {
			relevant = append(relevant, field)
		}
	}
	return validateControllerFormFields(relevant, values)
}

func validateControllerFormFields(fields []presentation.Field, values map[string]string) error {
	for _, field := range fields {
		if !field.Numeric || field.ReadOnly || !controllerFormFieldVisible(field, values) || field.OptionalNumeric && strings.TrimSpace(values[field.Key]) == "" {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(values[field.Key]), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < field.Min || value > field.Max || field.Decimals == 0 && value != math.Trunc(value) {
			return fmt.Errorf("%s must be %g–%g", field.Label, field.Min, field.Max)
		}
	}
	return nil
}

func controllerFormFieldVisible(field presentation.Field, values map[string]string) bool {
	if field.WhenKey == "" {
		return true
	}
	for _, value := range field.WhenValues {
		if value == values[field.WhenKey] {
			return true
		}
	}
	return false
}
