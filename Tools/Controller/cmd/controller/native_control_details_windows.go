//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
	"strconv"
)

// Keep unrelated tabs' literal drafts and their authoritative dirty baseline.
func preserveNativeFields(page desktop.Page, v map[string]string, keep func(desktop.Field) bool) desktop.Page {
	page.DraftBaseline = map[string]string{}
	selected := page.SelectedRow
	if selected == "" {
		selected = v["selected"]
	}
	row := page.RowValues[selected]
	for i := range page.Fields {
		f := &page.Fields[i]
		if f.Hidden || f.ReadOnly || !keep(*f) {
			continue
		}
		value, ok := v[f.Key]
		if !ok {
			continue
		}
		before := f.Value
		if old, ok := row[f.Key]; ok {
			before = old
		}
		if before != value {
			page.DraftBaseline[f.Key] = before
		}
		f.Value = value
		if row != nil {
			if _, ok := row[f.Key]; ok {
				row[f.Key] = value
			}
		}
	}
	return page
}

func (p nativePages) loadNativeLCD(ctx context.Context, state control.Snapshot) (desktop.Page, error) {
	prefs := nativeHostPreferences{Settings: appconfig.Defaults().UI}
	var live control.LCDPresentationState
	if !p.preview {
		if err := p.call(ctx, "controller.ui.preferences.get", map[string]any{}, &prefs); err != nil {
			return desktop.Page{}, err
		}
		if err := p.call(ctx, "controller.lcd.presentation.status", map[string]any{}, &live); err != nil {
			return desktop.Page{}, err
		}
	}
	baseline, _ := json.Marshal(prefs.Settings)
	page := desktop.Page{Title: "LCD presentation", Columns: []string{"Property", "Value"}, Rows: [][]string{{"Service enabled", fmt.Sprint(live.Enabled)}, {"Firmware mirror confirmed", fmt.Sprint(live.FirmwareMirror)}, {"Physical LCD detected", fmt.Sprint(live.Physical)}}}
	if live.ActiveLine1 != "" || live.ActiveLine2 != "" {
		page.Rows = append(page.Rows, []string{"Active line 1", live.ActiveLine1}, []string{"Active line 2", live.ActiveLine2})
	}
	if live.Physical {
		page.Rows = append(page.Rows, []string{"I²C address", fmt.Sprintf("0x%02X", live.Address)})
	}
	if live.LastError != "" {
		page.Output = live.LastError
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: prefs.ETag, Hidden: true}, {Key: "baseline", Value: string(baseline), Hidden: true}, nativeBool("lcd_service_enabled", "LCD service enabled", prefs.Settings.LCDServiceEnabled), nativeBool("mirror_prompt_to_lcd", "Mirror console prompt", prefs.Settings.MirrorPromptToLCD), nativeNumber("lcd_prompt_debounce_ms", "Prompt debounce (ms)", prefs.Settings.LCDPromptDebounceMS, 20, 5000), nativeNumber("lcd_priority_hold_ms", "Default priority hold (ms)", prefs.Settings.LCDPriorityHoldMS, 250, 60000), field("prompt1", "Prompt line 1", live.PromptLine1), field("prompt2", "Prompt line 2", live.PromptLine2), choice("priority_kind", "Priority", "operational", "operational", "bluetooth", "door", "relay", "rf", "macro", "motion", "warning", "error", "hot"), field("priority1", "Priority line 1", ""), field("priority2", "Priority line 2", ""), nativeNumber("priority_ms", "Priority hold (ms)", 2000, 250, 60000)}
	for i := range page.Fields {
		f := &page.Fields[i]
		f.Section = "LCD preferences"
		if f.Key == "prompt1" || f.Key == "prompt2" {
			f.Section = "Prompt"
		}
		if f.Key == "priority_kind" || f.Key == "priority1" || f.Key == "priority2" || f.Key == "priority_ms" {
			f.Section = "Priority message"
		}
	}
	ready := !p.preview && state.Connected && !state.Programming.Active && !state.EmergencyStop.Active
	page.Fields = append(page.Fields, desktop.Field{Key: "owner_generation", Value: strconv.FormatUint(state.ConnectionGeneration, 10), Hidden: true})
	page.Actions = []desktop.PageAction{{ID: "save", Label: "Save LCD preferences", Section: "LCD preferences", Enabled: !p.preview}, {ID: "prompt", Label: "Show prompt", Section: "Prompt", Enabled: ready && live.Enabled}, {ID: "priority", Label: "Show priority message", Section: "Priority message", Enabled: ready && live.Enabled}, {ID: "rescan", Label: "Rescan LCD", Enabled: ready}}
	return page, nil
}
func (p nativePages) submitNativeLCD(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot control LCD")
	}
	var result any
	var err error
	section := ""
	if action != "save" {
		var state control.Snapshot
		if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
			return desktop.Page{}, err
		}
		if !state.Connected || state.Programming.Active || state.EmergencyStop.Active || v["owner_generation"] != strconv.FormatUint(state.ConnectionGeneration, 10) {
			return desktop.Page{}, errors.New("LCD board owner changed or is not writable")
		}
	}
	switch action {
	case "save":
		section = "LCD preferences"
		values := map[string]string{"revision": v["revision"], "baseline": v["baseline"]}
		for _, k := range []string{"lcd_service_enabled", "mirror_prompt_to_lcd", "lcd_prompt_debounce_ms", "lcd_priority_hold_ms"} {
			values[k] = v[k]
		}
		err = p.saveHostPreferences(ctx, values)
	case "prompt":
		section = "Prompt"
		err = p.call(ctx, "controller.lcd.prompt", map[string]string{"line1": v["prompt1"], "line2": v["prompt2"]}, &result)
	case "priority":
		section = "Priority message"
		ms, e := nativeFormInteger(v, "priority_ms", 250, 60000)
		if e != nil {
			return desktop.Page{}, e
		}
		var receipt struct {
			Accepted bool `json:"accepted"`
		}
		err = p.call(ctx, "controller.lcd.priority", map[string]any{"kind": v["priority_kind"], "line1": v["priority1"], "line2": v["priority2"], "hold_ms": ms}, &receipt)
		if err == nil && !receipt.Accepted {
			err = errors.New("LCD priority message was not accepted; a higher-priority message may be active")
		}
	case "rescan":
		_, err = p.command(ctx, "i2c lcd rescan")
	default:
		return desktop.Page{}, errors.New("unknown LCD action")
	}
	if err != nil {
		return desktop.Page{}, err
	}
	var state control.Snapshot
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	page, err := p.loadNativeLCD(ctx, state)
	return preserveNativeFields(page, v, func(f desktop.Field) bool { return f.Section != section }), err
}

type nativePeripheralCatalog struct {
	BoardProfile nativeProfileDescriptor          `json:"board_profile"`
	Peripherals  []appconfig.PeripheralDescriptor `json:"peripherals"`
	Controls     []appconfig.ControlDescriptor    `json:"controls"`
	Folders      []appconfig.ChannelFolder        `json:"folders"`
}

func (p nativePages) readNativePeripherals(ctx context.Context) (nativePeripheralCatalog, error) {
	var c nativePeripheralCatalog
	if p.preview {
		c.Peripherals = appconfig.PeripheralDescriptors()
		return c, nil
	}
	err := p.call(ctx, "controller.peripherals.get", map[string]any{}, &c)
	return c, err
}
func (p nativePages) loadNativePeripherals(ctx context.Context) (desktop.Page, error) {
	c, err := p.readNativePeripherals(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Peripheral presentation", Columns: []string{"Key", "Name", "Kind", "Role", "Hidden", "Locked"}, RowValues: map[string]map[string]string{}, Fields: []desktop.Field{{Key: "revision", Value: c.BoardProfile.Revision, Hidden: true}}}
	for _, d := range c.Peripherals {
		name := d.Name
		if name == "" {
			name = d.DefaultName
		}
		page.Rows = append(page.Rows, []string{d.Key, name, d.Kind, d.Role, fmt.Sprint(d.Hidden), fmt.Sprint(d.Locked)})
	}
	page.Actions = []desktop.PageAction{nativeSelectionAction("open", "Edit peripheral", !p.preview)}
	return page, nil
}
func nativePeripheralPage(c nativePeripheralCatalog, d appconfig.PeripheralDescriptor, preview bool) desktop.Page {
	name := d.Name
	if name == "" {
		name = d.DefaultName
	}
	groups := []string{""}
	for _, g := range c.Folders {
		if g.Kind == appconfig.ChannelFolderKind(d.Kind) {
			groups = append(groups, g.Name)
		}
	}
	order := d.Index
	if d.Order != nil {
		order = *d.Order
	}
	page := desktop.Page{Title: "Peripheral presentation", Fields: []desktop.Field{{Key: "revision", Value: c.BoardProfile.Revision, Hidden: true}, {Key: "key", Value: d.Key, Hidden: true}, {Key: "identity", Label: "Peripheral", Value: d.Key, ReadOnly: true}, field("name", "Display name", name), field("icon", "Icon ID", d.Icon), field("color", "Color (#RRGGBB, blank = automatic)", d.Color), choice("group", "Folder", d.Group, groups...), nativeNumber("order", "Display order", order, 0, appconfig.MaxPeripheralNames-1), nativeBool("hidden", "Hidden", d.Hidden), nativeBool("locked", "Locked", d.Locked)}}
	if d.Kind == "motion" {
		page.Fields = append(page.Fields, field("up_color", "Up color (#RRGGBB)", d.UpColor), field("down_color", "Down color (#RRGGBB)", d.DownColor))
	}
	for i := range page.Fields {
		if page.Fields[i].Key == "icon" {
			page.Fields[i].IconPicker = true
		}
	}
	page.Actions = []desktop.PageAction{{ID: "save", Label: "Save presentation", Enabled: !preview && c.BoardProfile.Attached && c.BoardProfile.Configured}, {ID: "back", Label: "Back to peripherals", Enabled: true, DiscardChanges: true}}
	return page
}
func (p nativePages) submitNativePeripherals(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if action == "back" {
		return p.loadNativePeripherals(ctx)
	}
	c, err := p.readNativePeripherals(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	key := v["key"]
	if action == "open" {
		key = v["selected"]
	}
	var item *appconfig.PeripheralDescriptor
	for i := range c.Peripherals {
		if c.Peripherals[i].Key == key {
			item = &c.Peripherals[i]
			break
		}
	}
	if item == nil {
		return desktop.Page{}, errors.New("select a current peripheral")
	}
	if action == "open" {
		return nativePeripheralPage(c, *item, p.preview), nil
	}
	if action != "save" || p.preview {
		return desktop.Page{}, errors.New("peripheral update unavailable")
	}
	if v["revision"] == "" || v["revision"] != c.BoardProfile.Revision {
		return desktop.Page{}, errors.New("Board profile changed; refresh before saving")
	}
	order, err := strconv.Atoi(v["order"])
	if err != nil || order < 0 || order >= appconfig.MaxPeripheralNames {
		return desktop.Page{}, errors.New("invalid peripheral order")
	}
	params := map[string]any{"key": key, "expected_revision": v["revision"], "name": v["name"], "icon": v["icon"], "color": v["color"], "group": v["group"], "order": order, "hidden": v["hidden"] == "true", "locked": v["locked"] == "true"}
	if item.Kind == "motion" {
		params["up_color"] = v["up_color"]
		params["down_color"] = v["down_color"]
	}
	var result any
	if err := p.call(ctx, "controller.peripheral.presentation.update", params, &result); err != nil {
		return desktop.Page{}, err
	}
	next, err := p.readNativePeripherals(ctx)
	if err != nil {
		return desktop.Page{}, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	for _, d := range next.Peripherals {
		if d.Key == key {
			return nativePeripheralPage(next, d, false), nil
		}
	}
	return desktop.Page{}, errors.New("save acknowledged; peripheral is no longer advertised")
}
