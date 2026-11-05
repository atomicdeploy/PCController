//go:build windows

package ui

import (
	"context"
	"fmt"
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"pccontroller.local/desktop/internal/dialogs"
	"pccontroller.local/desktop/presentation"
	"strconv"
	"strings"
	"time"
)

type controllerTable struct {
	walk.TableModelBase
	rows [][]string
}

func (m *controllerTable) RowCount() int { return len(m.rows) }
func (m *controllerTable) Value(row, column int) any {
	if row < 0 || row >= len(m.rows) || column >= len(m.rows[row]) {
		return ""
	}
	return m.rows[row][column]
}

// Typed native fields, not an editable JSON dump. Requests run outside the
// window thread, errors retain drafts, and modal dismissal cannot lose edits.
func (s *Shell) openControllerForm(key string) {
	if s.controller.LoadPage == nil {
		return
	}
	var dialog *walk.Dialog
	var body *walk.Composite
	var heading *walk.Label
	var status *walk.Label
	var closeButton, refreshButton *walk.PushButton
	busy, dirty, closed := false, false, false
	getters := map[string]func() string{}
	baseline := map[string]string{}
	var saveButtons []**walk.PushButton
	building := false
	firstRender := true
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
		for _, button := range saveButtons {
			if *button != nil {
				(*button).SetEnabled(dirty && !busy)
			}
		}
	}
	var table *walk.TableView
	var page presentation.Page
	var theme *dialogs.ThemeSession
	var load func()
	var render func(presentation.Page)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	askDiscard := func() bool {
		return !dirty || walk.MsgBox(dialog, "Unsaved changes", "Discard changes to this form?", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) == walk.DlgCmdYes
	}
	render = func(next presentation.Page) {
		building = true
		defer func() { building = false; changed() }()
		selected := ""
		if table != nil {
			i := table.CurrentIndex()
			if i >= 0 && i < len(page.Rows) && len(page.Rows[i]) > 0 {
				selected = page.Rows[i][0]
			}
		}
		for body.Children().Len() > 0 {
			body.Children().At(0).Dispose()
		}
		page = next
		getters = map[string]func() string{}
		baseline = map[string]string{}
		saveButtons = nil
		table = nil
		_ = dialog.SetTitle(next.Title + " — " + s.controller.Title)
		_ = heading.SetText(strings.ReplaceAll(next.Title, "&", "&&"))
		children := []d.Widget{}
		if len(next.Columns) > 0 {
			columns := []d.TableViewColumn{}
			for _, name := range next.Columns {
				columns = append(columns, d.TableViewColumn{Title: name, Width: 130})
			}
			children = append(children, d.TableView{AssignTo: &table, Columns: columns, Model: &controllerTable{rows: next.Rows}, LastColumnStretched: true, ColumnsSizable: true, MinSize: d.Size{Height: 140}, StretchFactor: 1})
		}
		section := ""
		var sectionRows []d.Widget
		flushSection := func() {
			if len(sectionRows) == 0 {
				return
			}
			rows := []d.Widget{d.Label{Text: strings.ReplaceAll(section, "&", "&&"), Font: d.Font{Family: s.preferredFontFamily(), PointSize: 11, Bold: true}}}
			rows = append(rows, sectionRows...)
			children = append(children, d.Composite{Layout: d.VBox{Margins: d.Margins{Left: 12, Top: 12, Right: 12, Bottom: 14}, Spacing: 10, Alignment: d.AlignHNearVNear}, Children: rows})
			sectionRows = nil
		}
		for _, item := range next.Fields {
			field := item
			if field.Section != section {
				flushSection()
				section = field.Section
			}
			baseline[field.Key] = field.Value
			row := []d.Widget{d.Label{Text: field.Label, MinSize: d.Size{Width: 180, Height: 30}}}
			if field.ReadOnly {
				row = append(row, d.Label{Text: field.Value, StretchFactor: 1})
			} else if field.Numeric {
				var input *walk.NumberEdit
				value, _ := strconv.ParseFloat(field.Value, 64)
				row = append(row, d.NumberEdit{AssignTo: &input, Value: value, MinValue: field.Min, MaxValue: field.Max, SpinButtonsVisible: true, MinSize: d.Size{Width: 120, Height: 30}, StretchFactor: 1, OnValueChanged: changed})
				getters[field.Key] = func() string {
					if input == nil {
						return field.Value
					}
					return strconv.FormatFloat(input.Value(), 'f', 0, 64)
				}
			} else if len(field.Choices) == 2 && field.Choices[0] == "false" && field.Choices[1] == "true" {
				var input *walk.CheckBox
				row = append(row, d.CheckBox{AssignTo: &input, Checked: field.Value == "true", StretchFactor: 1, MinSize: d.Size{Height: 30}, OnCheckedChanged: changed})
				getters[field.Key] = func() string {
					if input == nil {
						return field.Value
					}
					return strconv.FormatBool(input.Checked())
				}
			} else if len(field.Choices) > 0 {
				var combo *walk.ComboBox
				index := 0
				for i, value := range field.Choices {
					if value == field.Value {
						index = i
					}
				}
				row = append(row, d.ComboBox{AssignTo: &combo, Model: field.Choices, CurrentIndex: index, MinSize: d.Size{Height: 30}, StretchFactor: 1, OnCurrentIndexChanged: changed})
				getters[field.Key] = func() string {
					if combo == nil {
						return field.Value
					}
					return combo.Text()
				}
			} else {
				var edit *walk.LineEdit
				row = append(row, d.LineEdit{AssignTo: &edit, Text: field.Value, MinSize: d.Size{Height: 30}, StretchFactor: 1, OnTextChanged: changed})
				getters[field.Key] = func() string {
					if edit == nil {
						return field.Value
					}
					return edit.Text()
				}
			}
			sectionRows = append(sectionRows, d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 16}, Children: row})
		}
		flushSection()
		buttons := []d.Widget{}
		for _, item := range next.Actions {
			action := item
			var button *walk.PushButton
			if action.ID == "save" && action.Enabled {
				saveButtons = append(saveButtons, &button)
			}
			buttons = append(buttons, d.PushButton{AssignTo: &button, Text: action.Label, Enabled: action.Enabled, MinSize: d.Size{Width: 100, Height: 30}, MaxSize: d.Size{Width: 200, Height: 32}, OnClicked: func() {
				if busy || s.controller.SubmitPage == nil {
					return
				}
				if action.ID == "save" && !dirty {
					return
				}
				values := map[string]string{}
				for name, get := range getters {
					values[name] = get()
				}
				if table != nil {
					index := table.CurrentIndex()
					if index >= 0 && index < len(page.Rows) && len(page.Rows[index]) > 0 {
						values["selected"] = page.Rows[index][0]
					}
				}
				if action.Confirm {
					message := "Apply this operation to the connected controller?"
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
				body.SetEnabled(false)
				refreshButton.SetEnabled(false)
				status.SetText("")
				go func() {
					requestCtx, done := context.WithTimeout(ctx, 15*time.Second)
					defer done()
					updated, err := s.controller.SubmitPage(requestCtx, key, action.ID, values)
					dialog.Synchronize(func() {
						if closed {
							return
						}
						busy = false
						body.SetEnabled(true)
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
		if len(buttons) > 0 {
			// A bounded grid wraps actions instead of widening the form off-screen.
			children = append(children, d.Composite{Layout: d.Grid{Columns: min(3, len(buttons)), Spacing: 8, MarginsZero: true, Alignment: d.AlignHNearVNear}, Children: buttons})
		}
		if err := (d.Composite{Layout: d.VBox{MarginsZero: true, Spacing: 12}, Children: children}).Create(d.NewBuilder(body)); err != nil {
			status.SetText(err.Error())
		}
		if table != nil && selected != "" {
			for i, row := range next.Rows {
				if len(row) > 0 && row[0] == selected {
					_ = table.SetCurrentIndex(i)
					break
				}
			}
		}
		dirty = false
		theme.Refresh(s.Config().UI.Theme, s.motionEnabled)
		if firstRender {
			firstRender = false
			height := 240 + 52*len(next.Fields)
			if len(next.Columns) > 0 {
				height += 180
			}
			_ = dialog.SetSize(walk.Size{Width: 680, Height: min(740, max(380, height))})
		}
	}
	load = func() {
		if busy || !askDiscard() {
			return
		}
		busy = true
		refreshButton.SetEnabled(false)
		status.SetText("Loading…")
		go func() {
			requestCtx, done := context.WithTimeout(ctx, 10*time.Second)
			defer done()
			next, err := s.controller.LoadPage(requestCtx, key)
			dialog.Synchronize(func() {
				if closed {
					return
				}
				busy = false
				refreshButton.SetEnabled(true)
				status.SetText("")
				if err != nil {
					status.SetText(err.Error())
					return
				}
				render(next)
			})
		}()
	}
	spec := d.Dialog{AssignTo: &dialog, Title: "PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 560, Height: 300}, Size: d.Size{Width: 680, Height: 420}, Layout: d.VBox{Margins: d.Margins{Left: 20, Top: 18, Right: 20, Bottom: 18}, Spacing: 14}, CancelButton: &closeButton, Children: []d.Widget{
		d.Label{AssignTo: &heading, Text: "PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 16, Bold: true}, MinSize: d.Size{Height: 36}},
		d.ScrollView{Layout: d.VBox{MarginsZero: true, Alignment: d.AlignHNearVNear}, StretchFactor: 1, Children: []d.Widget{d.Composite{AssignTo: &body, Layout: d.VBox{MarginsZero: true}}, d.VSpacer{}}},
		d.Label{AssignTo: &status, Text: "", MinSize: d.Size{Height: 24}},
		d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{
			d.PushButton{AssignTo: &refreshButton, Text: "Refresh", OnClicked: func() { load() }},
			d.HSpacer{},
			d.PushButton{AssignTo: &closeButton, Text: "Close", OnClicked: func() { dialog.Cancel() }},
		}},
	}}
	if err := spec.Create(s.mw); err != nil {
		s.showError("PCController", err)
		return
	}
	defer dialog.Dispose()
	theme = dialogs.NewThemeSession(dialog)
	defer theme.Dispose()
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	_ = theme.WatchSystemChanges(nil)
	_ = dialog.SetIcon(s.appIcon)
	dialog.Closing().Attach(func(cancelClose *bool, _ walk.CloseReason) {
		if busy || !askDiscard() {
			*cancelClose = true
		} else {
			closed = true
			cancel()
		}
	})
	load()
	if (key == "events" || key == "firmware") && s.controller.PageChanges != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-s.controller.PageChanges:
					if !ok {
						return
					}
				}
				// Coalesce event bursts, never poll the serial owner or reset a draft.
				timer := time.NewTimer(300 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				dialog.Synchronize(func() {
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
