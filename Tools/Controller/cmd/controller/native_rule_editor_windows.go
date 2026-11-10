//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/desktop/desktop"
)

type nativeAuthoringCollection[T any] struct {
	Items []T    `json:"items"`
	ETag  string `json:"etag"`
}

func readNativeAuthoring[T any](ctx context.Context, p nativePages, kind string) (nativeAuthoringCollection[T], error) {
	var c nativeAuthoringCollection[T]
	if p.preview {
		return c, nil
	}
	err := p.call(ctx, "controller."+kind+".get", map[string]any{}, &c)
	return c, err
}
func saveNativeAuthoring[T any](ctx context.Context, p nativePages, kind string, c nativeAuthoringCollection[T]) error {
	var result any
	return p.call(ctx, "controller."+kind+".set", map[string]any{"items": c.Items, "if_match": c.ETag}, &result)
}
func nativeRFRule(r appconfig.Automation) bool {
	return r.Match.Kind == "rf.gesture" && r.Match.Source == "rf"
}

func (p nativePages) decorateNativeRulePage(ctx context.Context, page desktop.Page) (desktop.Page, error) {
	catalog, err := p.readNativePeripherals(ctx)
	if err != nil {
		return page, err
	}
	choices, labels := []string{""}, []string{""}
	for _, channel := range catalog.Controls {
		if channel.Hidden || channel.Locked {
			continue
		}
		for _, action := range channel.Actions {
			choices = append(choices, action.ID)
			labels = append(labels, channel.Name+" · "+action.Name)
		}
	}
	for i := range page.Fields {
		if page.Fields[i].Key == "action_id" {
			page.Fields[i].Choices = choices
			page.Fields[i].ChoiceLabels = labels
		}
	}
	return page, nil
}

func (p nativePages) loadNativeRules(ctx context.Context, rfOnly bool) (desktop.Page, error) {
	c, err := readNativeAuthoring[appconfig.Automation](ctx, p, "automations")
	if err != nil {
		return desktop.Page{}, err
	}
	title := "Host event automations"
	if rfOnly {
		title = "RF host bindings"
	}
	page := desktop.Page{Title: title, Columns: []string{"Name", "Enabled", "Event", "Gesture", "Actions"}, RowValues: map[string]map[string]string{}}
	for _, r := range c.Items {
		if rfOnly && !nativeRFRule(r) {
			continue
		}
		page.Rows = append(page.Rows, []string{r.Name, fmt.Sprint(r.Enabled), r.Match.Kind, r.Match.Gesture, fmt.Sprint(len(r.Actions))})
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: c.ETag, Hidden: true}}
	page.Actions = []desktop.PageAction{nativeSelectionAction("open", "Edit rule", !p.preview), pageAction("new", "New rule", !p.preview), {ID: "remove-rule", Label: "Remove rule", Enabled: !p.preview, SelectionRequired: true, Confirm: true}, {ID: "run", Label: "Run selected rule", Enabled: !p.preview, SelectionRequired: true, Confirm: true}}
	return page, nil
}

func nativeRulePage(c nativeAuthoringCollection[appconfig.Automation], r appconfig.Automation, original string, preview bool) desktop.Page {
	base, _ := json.Marshal(r)
	page := desktop.Page{Title: "Automation editor", Columns: []string{"#", "Type", "Action"}, RowValues: map[string]map[string]string{}}
	for i, a := range r.Actions {
		id := strconv.Itoa(i)
		summary := a.Command
		if summary == "" {
			summary = a.ActionID
		}
		if summary == "" {
			summary = a.Macro
		}
		if summary == "" {
			summary = a.Executable
		}
		if summary == "" {
			summary = a.Event
		}
		if summary == "" {
			summary = a.VirtualKey
		}
		if summary == "" {
			summary = a.AppKind
		}
		page.Rows = append(page.Rows, []string{id, a.Type, summary})
		page.RowValues[id] = nativeActionValues(a)
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: c.ETag, Hidden: true}, {Key: "original", Value: original, Hidden: true}, {Key: "rule_base", Value: string(base), Hidden: true}, field("rule_name", "Rule name", r.Name), nativeBool("rule_enabled", "Rule enabled", r.Enabled), nativeNumber("cooldown", "Cooldown (ms)", r.CooldownMS, 0, 3600000), field("match_kind", "Event kind", r.Match.Kind), field("match_lifecycle", "Lifecycle filter", r.Match.Lifecycle), field("match_state", "State filter", r.Match.State), field("match_contains", "Text contains", r.Match.Contains), nativeNumber("match_key", "Front-panel key (0 = any)", int(r.Match.Key), 0, 4), choice("match_gesture", "Gesture", r.Match.Gesture, "", "down", "up", "tap", "click", "double", "hold", "repeat", "hold-end"), choice("match_source", "Event source", r.Match.Source, "", "physical", "rf", "host", "pc-keyboard"), {Key: "match_rf_id", Label: "Learned RF ID (blank = any)", Numeric: true, OptionalNumeric: true, Min: 0, Max: 255}, {Key: "match_rf_code", Label: "RF code (blank = any)", Numeric: true, OptionalNumeric: true, Min: 0, Max: 4294967295}, nativeNumber("match_rf_bits", "RF bits (0 = any)", int(r.Match.RFBits), 0, 32), nativeNumber("match_rf_protocol", "RF protocol (0 = any)", int(r.Match.RFProtocol), 0, 12)}
	for i := range page.Fields {
		if page.Fields[i].Key == "match_rf_id" && r.Match.RFID != nil {
			page.Fields[i].Value = fmt.Sprint(*r.Match.RFID)
		}
		if page.Fields[i].Key == "match_rf_code" && r.Match.RFCode != nil {
			page.Fields[i].Value = fmt.Sprint(*r.Match.RFCode)
		}
		if !page.Fields[i].Hidden {
			page.Fields[i].Section = "Trigger"
		}
	}
	page.Fields = append(page.Fields, nativeActionFields()...)
	page.Actions = []desktop.PageAction{{ID: "save-rule", Label: "Save trigger", Section: "Trigger", Enabled: !preview && len(r.Actions) > 0, Confirm: true}, {ID: "action-save", Label: "Save action", Section: "Actions", Enabled: !preview, SelectionRequired: true, Confirm: true}, {ID: "action-add", Label: "Add action", Section: "Actions", Enabled: !preview && len(r.Actions) < 8, Confirm: true}, {ID: "action-remove", Label: "Remove action", Section: "Actions", Enabled: !preview && len(r.Actions) > 1, SelectionRequired: true, Confirm: true}, {ID: "action-up", Label: "Move action up", Section: "Actions", Enabled: !preview && len(r.Actions) > 1, SelectionRequired: true}, {ID: "action-down", Label: "Move action down", Section: "Actions", Enabled: !preview && len(r.Actions) > 1, SelectionRequired: true}, {ID: "back", Label: "Back to rules", Enabled: true, DiscardChanges: true}}
	for i := range page.Actions {
		if page.Actions[i].ID == "action-up" {
			page.Actions[i].SelectionOffset = -1
		}
		if page.Actions[i].ID == "action-down" {
			page.Actions[i].SelectionOffset = 1
		}
		if page.Actions[i].ID == "save-rule" || page.Actions[i].ID == "action-save" {
			page.Actions[i].RequiresChanges = true
		}
	}
	return page
}

func nativeActionFields() []desktop.Field {
	f := []desktop.Field{choice("action_type", "Action type", "control", "control", "board", "macro", "effect", "rf", "host", "script", "emit", "virtual-key", "power", "app"), field("action_id", "Channel action ID", ""), field("command", "Controller command", ""), field("macro", "Effect reference", ""), field("executable", "Executable", ""), {Key: "args", Label: "Arguments (one per line)", Multiline: true}, nativeBool("detached", "Detached process", false), field("script", "Configured script name", ""), field("event", "Emitted event", ""), field("virtual_key", "Virtual key", ""), nativeNumber("hold_ms", "Key hold (ms, 0 = default)", 0, 0, 1000), choice("power", "Power action", "lock", "lock", "sleep", "hibernate", "shutdown", "restart"), field("confirm", "Policy confirmation", ""), field("app_kind", "Application action kind", ""), field("app_target", "Application target", ""), field("app_value", "Application value", ""), {Key: "rf_code", Label: "Transmit code", Value: "1", Numeric: true, Min: 1, Max: 4294967295}, nativeNumber("rf_bits", "Transmit bits", 24, 1, 32), nativeNumber("rf_protocol", "Transmit protocol", 1, 1, 12), nativeNumber("rf_pulse", "Transmit pulse (µs)", 0, 0, 65535), nativeNumber("rf_repeats", "Transmit repeats (0 = default)", 0, 0, 20)}
	for i := range f {
		f[i].Section = "Actions"
		if f[i].Key == "action_type" {
			continue
		}
		f[i].WhenKey = "action_type"
		switch f[i].Key {
		case "action_id":
			f[i].WhenValues = []string{"control"}
		case "command":
			f[i].WhenValues = []string{"board"}
		case "macro":
			f[i].WhenValues = []string{"macro", "effect"}
		case "executable", "args", "detached":
			f[i].WhenValues = []string{"host"}
		case "script":
			f[i].WhenValues = []string{"script"}
		case "event":
			f[i].WhenValues = []string{"emit"}
		case "virtual_key", "hold_ms":
			f[i].WhenValues = []string{"virtual-key", "virtual_key", "vk"}
		case "power", "confirm":
			f[i].WhenValues = []string{"power"}
		case "app_kind", "app_target", "app_value":
			f[i].WhenValues = []string{"app"}
		default:
			f[i].WhenValues = []string{"rf"}
		}
	}
	return f
}
func nativeActionValues(a appconfig.AutomationAction) map[string]string {
	v := map[string]string{"action_type": a.Type, "action_id": a.ActionID, "command": a.Command, "macro": a.Macro, "executable": a.Executable, "args": strings.Join(a.Args, "\r\n"), "detached": fmt.Sprint(a.Detached), "script": a.Script, "event": a.Event, "virtual_key": a.VirtualKey, "hold_ms": fmt.Sprint(a.HoldMS), "power": a.Power, "confirm": a.Confirm, "app_kind": a.AppKind, "app_target": a.AppTarget, "app_value": a.AppValue, "rf_code": "1", "rf_bits": "24", "rf_protocol": "1", "rf_pulse": "0", "rf_repeats": "0"}
	if a.RF != nil {
		v["rf_code"] = fmt.Sprint(a.RF.Code)
		v["rf_bits"] = fmt.Sprint(a.RF.Bits)
		v["rf_protocol"] = fmt.Sprint(a.RF.Protocol)
		v["rf_pulse"] = fmt.Sprint(a.RF.PulseUS)
		v["rf_repeats"] = fmt.Sprint(a.RF.Repeats)
	}
	return v
}
func nativeActionDraft(v map[string]string, old appconfig.AutomationAction) (appconfig.AutomationAction, error) {
	a := old
	if v["action_type"] != old.Type {
		a = appconfig.AutomationAction{}
	}
	a.Type = v["action_type"]
	switch a.Type {
	case "control":
		a.ActionID = v["action_id"]
	case "board":
		a.Command = v["command"]
	case "macro", "effect":
		a.Macro = v["macro"]
	case "host":
		a.Executable = v["executable"]
		a.Detached = v["detached"] == "true"
		if v["args"] != strings.Join(old.Args, "\r\n") {
			a.Args = nil
			if v["args"] != "" {
				a.Args = strings.Split(strings.ReplaceAll(v["args"], "\r\n", "\n"), "\n")
			}
		}
	case "script":
		a.Script = v["script"]
	case "emit":
		a.Event = v["event"]
	case "virtual-key", "virtual_key", "vk":
		a.VirtualKey = v["virtual_key"]
		n, err := nativeFormInteger(v, "hold_ms", 0, 1000)
		if err != nil {
			return a, err
		}
		a.HoldMS = n
	case "power":
		a.Power = v["power"]
		a.Confirm = v["confirm"]
	case "app":
		a.AppKind = v["app_kind"]
		a.AppTarget = v["app_target"]
		a.AppValue = v["app_value"]
	case "rf":
		r := appconfig.RFTransmit{}
		code, err := strconv.ParseUint(v["rf_code"], 10, 32)
		if err != nil || code == 0 {
			return a, errors.New("transmit code must be 1..4294967295")
		}
		r.Code = uint32(code)
		for _, x := range []struct {
			k   string
			max byte
			dst *byte
		}{{"rf_bits", 32, &r.Bits}, {"rf_protocol", 12, &r.Protocol}, {"rf_repeats", 20, &r.Repeats}} {
			min := byte(1)
			if x.k == "rf_repeats" {
				min = 0
			}
			n, err := nativeFormUint8(v, x.k, min, x.max)
			if err != nil {
				return a, err
			}
			*x.dst = n
		}
		n, err := nativeFormUint16(v, "rf_pulse", 0, 65535)
		if err != nil {
			return a, err
		}
		r.PulseUS = n
		a.RF = &r
	default:
		return a, errors.New("select an action type")
	}
	return a, nil
}
func nativeRuleDraft(v map[string]string, r appconfig.Automation) (appconfig.Automation, error) {
	r.Name = strings.TrimSpace(v["rule_name"])
	r.Enabled = v["rule_enabled"] == "true"
	var err error
	r.CooldownMS, err = nativeFormInteger(v, "cooldown", 0, 3600000)
	if err != nil {
		return r, err
	}
	m := &r.Match
	m.Kind = v["match_kind"]
	m.Lifecycle = v["match_lifecycle"]
	m.State = v["match_state"]
	m.Contains = v["match_contains"]
	m.Gesture = v["match_gesture"]
	m.Source = v["match_source"]
	for _, x := range []struct {
		k   string
		max byte
		dst *byte
	}{{"match_key", 4, &m.Key}, {"match_rf_bits", 32, &m.RFBits}, {"match_rf_protocol", 12, &m.RFProtocol}} {
		n, e := nativeFormUint8(v, x.k, 0, x.max)
		if e != nil {
			return r, e
		}
		*x.dst = n
	}
	m.RFID = nil
	if v["match_rf_id"] != "" {
		n, e := nativeFormUint8(v, "match_rf_id", 0, 255)
		if e != nil {
			return r, e
		}
		m.RFID = &n
	}
	m.RFCode = nil
	if v["match_rf_code"] != "" {
		n, e := strconv.ParseUint(v["match_rf_code"], 10, 32)
		if e != nil {
			return r, errors.New("RF code must be 0..4294967295 or blank")
		}
		b := uint32(n)
		m.RFCode = &b
	}
	return r, nil
}

func (p nativePages) submitNativeRules(ctx context.Context, action string, v map[string]string, rfOnly bool) (desktop.Page, error) {
	c, err := readNativeAuthoring[appconfig.Automation](ctx, p, "automations")
	if err != nil {
		return desktop.Page{}, err
	}
	if action == "back" {
		return p.loadNativeRules(ctx, rfOnly)
	}
	if action == "new" {
		r := appconfig.Automation{Match: appconfig.AutomationMatch{Kind: "host.ready"}}
		if rfOnly {
			r.Match.Kind = "rf.gesture"
			r.Match.Source = "rf"
			r.Match.Gesture = "tap"
		}
		return p.decorateNativeRulePage(ctx, nativeRulePage(c, r, "", p.preview))
	}
	name := v["original"]
	if action == "open" || action == "remove-rule" || action == "run" {
		name = v["selected"]
	}
	index := -1
	for i, r := range c.Items {
		if r.Name == name {
			index = i
			break
		}
	}
	if name != "" && (index < 0 || rfOnly && !nativeRFRule(c.Items[index])) {
		return desktop.Page{}, errors.New("selected rule changed; refresh first")
	}
	if action == "open" {
		if index < 0 {
			return desktop.Page{}, errors.New("select a rule")
		}
		return p.decorateNativeRulePage(ctx, nativeRulePage(c, c.Items[index], name, p.preview))
	}
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify or run rules")
	}
	if v["revision"] != c.ETag {
		return desktop.Page{}, errors.New("Automation rules changed; refresh before saving")
	}
	if action == "run" {
		if index < 0 {
			return desktop.Page{}, errors.New("select a rule")
		}
		output, e := p.command(ctx, shell.Join([]string{"automation", "run", name}))
		if e != nil {
			return desktop.Page{}, e
		}
		page, e := p.loadNativeRules(ctx, rfOnly)
		page.Output = output
		return page, e
	}
	if action == "remove-rule" {
		if index < 0 {
			return desktop.Page{}, errors.New("select a rule")
		}
		c.Items = append(c.Items[:index:index], c.Items[index+1:]...)
		if err := saveNativeAuthoring(ctx, p, "automations", c); err != nil {
			return desktop.Page{}, err
		}
		return p.loadNativeRules(ctx, rfOnly)
	}
	r := appconfig.Automation{}
	if index >= 0 {
		r = c.Items[index]
		r.Actions = append([]appconfig.AutomationAction(nil), r.Actions...)
	}
	r, err = nativeRuleDraft(v, r)
	if err != nil {
		return desktop.Page{}, err
	}
	if rfOnly && !nativeRFRule(r) {
		return desktop.Page{}, errors.New("RF editor requires rf.gesture from source rf")
	}
	selected := -1
	if action != "save-rule" && action != "action-add" {
		selected, err = nativeFormInteger(v, "selected", 0, len(r.Actions)-1)
		if err != nil {
			return desktop.Page{}, err
		}
	}
	switch action {
	case "save-rule":
	case "action-save", "action-add":
		old := appconfig.AutomationAction{}
		if selected >= 0 {
			old = r.Actions[selected]
		}
		a, e := nativeActionDraft(v, old)
		if e != nil {
			return desktop.Page{}, e
		}
		if action == "action-add" {
			r.Actions = append(r.Actions, a)
			selected = len(r.Actions) - 1
		} else {
			r.Actions[selected] = a
		}
	case "action-remove":
		r.Actions = append(r.Actions[:selected:selected], r.Actions[selected+1:]...)
		selected = -1
	case "action-up", "action-down":
		to := selected - 1
		if action == "action-down" {
			to = selected + 1
		}
		if to < 0 || to >= len(r.Actions) {
			return desktop.Page{}, errors.New("action is already at the edge")
		}
		r.Actions[selected], r.Actions[to] = r.Actions[to], r.Actions[selected]
		selected = to
	default:
		return desktop.Page{}, errors.New("unknown automation operation")
	}
	if index < 0 {
		c.Items = append(c.Items, r)
	} else {
		c.Items[index] = r
	}
	if err := saveNativeAuthoring(ctx, p, "automations", c); err != nil {
		return desktop.Page{}, err
	}
	next, err := readNativeAuthoring[appconfig.Automation](ctx, p, "automations")
	if err != nil {
		return desktop.Page{}, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	page := nativeRulePage(next, r, r.Name, false)
	if selected >= 0 {
		page.SelectedRow = strconv.Itoa(selected)
	}
	if action == "save-rule" || action == "action-up" || action == "action-down" {
		page = preserveNativeFields(page, v, func(f desktop.Field) bool { return f.Section == "Actions" })
	}
	page, err = p.decorateNativeRulePage(ctx, page)
	if err != nil {
		return page, fmt.Errorf("save acknowledged; catalog refresh failed: %w", err)
	}
	return page, nil
}
