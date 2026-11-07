//go:build windows && !no_native_gui

package main

import (
	"context"
	"errors"
	"fmt"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
	"strconv"
	"strings"
)

func (p nativePages) loadNativeMelodies(ctx context.Context) (desktop.Page, error) {
	c, err := readNativeAuthoring[appconfig.Melody](ctx, p, "melodies")
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Melody library", Columns: []string{"Name", "Notes", "Duration (ms)"}, Fields: []desktop.Field{{Key: "revision", Value: c.ETag, Hidden: true}}}
	for _, m := range c.Items {
		duration := 0
		for _, n := range m.Notes {
			duration += int(n.DurationMS) + int(n.GapMS)
		}
		page.Rows = append(page.Rows, []string{m.Name, strconv.Itoa(len(m.Notes)), strconv.Itoa(duration)})
	}
	page.Actions = []desktop.PageAction{nativeSelectionAction("open", "Edit melody", !p.preview), pageAction("new", "New melody", !p.preview), {ID: "remove-melody", Label: "Remove melody override", Enabled: !p.preview, SelectionRequired: true, Confirm: true}}
	return page, nil
}
func nativeMelodyPage(c nativeAuthoringCollection[appconfig.Melody], m appconfig.Melody, original string, preview bool) desktop.Page {
	page := desktop.Page{Title: "Melody editor", Columns: []string{"#", "Frequency (Hz)", "Duration (ms)", "Gap (ms)"}, RowValues: map[string]map[string]string{}}
	for i, n := range m.Notes {
		id := strconv.Itoa(i)
		page.Rows = append(page.Rows, []string{id, fmt.Sprint(n.FrequencyHz), fmt.Sprint(n.DurationMS), fmt.Sprint(n.GapMS)})
		page.RowValues[id] = map[string]string{"note_hz": fmt.Sprint(n.FrequencyHz), "note_ms": fmt.Sprint(n.DurationMS), "note_gap": fmt.Sprint(n.GapMS)}
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: c.ETag, Hidden: true}, {Key: "original", Value: original, Hidden: true}, field("melody_name", "Melody name", m.Name), nativeNumber("note_hz", "Note frequency (Hz, 0 = rest)", 880, 0, 20000), nativeNumber("note_ms", "Note duration (ms)", 115, 1, 5000), nativeNumber("note_gap", "Gap after note (ms)", 0, 0, 5000)}
	for i := range page.Fields {
		if !page.Fields[i].Hidden {
			page.Fields[i].Section = "Notes"
		}
	}
	page.Actions = []desktop.PageAction{{ID: "save-name", Label: "Save melody name", Enabled: !preview && len(m.Notes) > 0}, {ID: "note-save", Label: "Save note", Enabled: !preview, SelectionRequired: true}, {ID: "note-add", Label: "Add note", Enabled: !preview && len(m.Notes) < appconfig.MaxMelodyNotes}, {ID: "note-remove", Label: "Remove note", Enabled: !preview && len(m.Notes) > 1, SelectionRequired: true, Confirm: true}, {ID: "note-up", Label: "Move note up", Enabled: !preview && len(m.Notes) > 1, SelectionRequired: true}, {ID: "note-down", Label: "Move note down", Enabled: !preview && len(m.Notes) > 1, SelectionRequired: true}, {ID: "back", Label: "Back to melodies", Enabled: true, DiscardChanges: true}}
	for i := range page.Actions {
		if page.Actions[i].ID == "note-up" {
			page.Actions[i].SelectionOffset = -1
		}
		if page.Actions[i].ID == "note-down" {
			page.Actions[i].SelectionOffset = 1
		}
		if page.Actions[i].ID == "save-name" || page.Actions[i].ID == "note-save" {
			page.Actions[i].RequiresChanges = true
		}
	}
	return page
}
func (p nativePages) submitNativeMelodies(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	c, err := readNativeAuthoring[appconfig.Melody](ctx, p, "melodies")
	if err != nil {
		return desktop.Page{}, err
	}
	if action == "back" {
		return p.loadNativeMelodies(ctx)
	}
	if action == "new" {
		return nativeMelodyPage(c, appconfig.Melody{}, "", p.preview), nil
	}
	name := v["original"]
	if action == "open" || action == "remove-melody" {
		name = v["selected"]
	}
	index := -1
	for i, m := range c.Items {
		if m.Name == name {
			index = i
			break
		}
	}
	if name != "" && index < 0 {
		return desktop.Page{}, errors.New("melody changed; refresh first")
	}
	if action == "open" {
		if index < 0 {
			return desktop.Page{}, errors.New("select a melody")
		}
		return nativeMelodyPage(c, c.Items[index], name, p.preview), nil
	}
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot save melodies")
	}
	if c.ETag != v["revision"] {
		return desktop.Page{}, errors.New("Melodies changed; refresh before saving")
	}
	if action == "remove-melody" {
		if index < 0 {
			return desktop.Page{}, errors.New("select a melody")
		}
		c.Items = append(c.Items[:index:index], c.Items[index+1:]...)
		if err := saveNativeAuthoring(ctx, p, "melodies", c); err != nil {
			return desktop.Page{}, err
		}
		return p.loadNativeMelodies(ctx)
	}
	m := appconfig.Melody{}
	if index >= 0 {
		m = c.Items[index]
		m.Notes = append([]appconfig.MelodyNote(nil), m.Notes...)
	}
	m.Name = strings.TrimSpace(v["melody_name"])
	selected := -1
	if action != "save-name" && action != "note-add" {
		selected, err = nativeFormInteger(v, "selected", 0, len(m.Notes)-1)
		if err != nil {
			return desktop.Page{}, err
		}
	}
	switch action {
	case "save-name":
	case "note-add", "note-save":
		n := appconfig.MelodyNote{}
		for _, x := range []struct {
			k        string
			min, max int
			dst      *uint16
		}{{"note_hz", 0, 20000, &n.FrequencyHz}, {"note_ms", 1, 5000, &n.DurationMS}, {"note_gap", 0, 5000, &n.GapMS}} {
			value, e := nativeFormInteger(v, x.k, x.min, x.max)
			if e != nil {
				return desktop.Page{}, e
			}
			*x.dst = uint16(value)
		}
		if action == "note-add" {
			m.Notes = append(m.Notes, n)
			selected = len(m.Notes) - 1
		} else {
			m.Notes[selected] = n
		}
	case "note-remove":
		m.Notes = append(m.Notes[:selected:selected], m.Notes[selected+1:]...)
		selected = -1
	case "note-up", "note-down":
		to := selected - 1
		if action == "note-down" {
			to = selected + 1
		}
		if to < 0 || to >= len(m.Notes) {
			return desktop.Page{}, errors.New("note is already at the edge")
		}
		m.Notes[selected], m.Notes[to] = m.Notes[to], m.Notes[selected]
		selected = to
	default:
		return desktop.Page{}, errors.New("unknown melody operation")
	}
	if err := appconfig.ValidateMelody(m); err != nil {
		return desktop.Page{}, err
	}
	if index < 0 {
		c.Items = append(c.Items, m)
	} else {
		c.Items[index] = m
	}
	if err := saveNativeAuthoring(ctx, p, "melodies", c); err != nil {
		return desktop.Page{}, err
	}
	next, err := readNativeAuthoring[appconfig.Melody](ctx, p, "melodies")
	if err != nil {
		return desktop.Page{}, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	page := nativeMelodyPage(next, m, m.Name, false)
	if selected >= 0 {
		page.SelectedRow = strconv.Itoa(selected)
	}
	if action == "save-name" || action == "note-up" || action == "note-down" {
		page = preserveNativeFields(page, v, func(f desktop.Field) bool { return strings.HasPrefix(f.Key, "note_") })
	}
	return page, nil
}
