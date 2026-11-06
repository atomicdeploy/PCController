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
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/desktop/desktop"
)

func (p nativePages) loadNativeBoardAutomations(ctx context.Context, state control.Snapshot) (desktop.Page, error) {
	ready := !p.preview && state.Connected && !state.Programming.Active && !state.EmergencyStop.Active && state.Hello.SupportsBoardAutomation()
	page := desktop.Page{Title: "Board-owned automations", Columns: []string{"ID", "Enabled", "Event", "Value / mask", "Action", "Target", "Value / extra"}, RowValues: map[string]map[string]string{}}
	var rules control.BoardAutomationSnapshot
	if ready {
		if err := p.call(ctx, "controller.board_automation.list", map[string]any{}, &rules); err != nil {
			return page, err
		}
	}
	events := []string{"1 · Door", "2 · Bluetooth", "3 · Host", "4 · Relay", "5 · Learned RF", "6 · Key", "7 · Alert", "8 · Boot"}
	actions := []string{"1 · Safe stop", "2 · Motion stop", "3 · Relay", "4 · PWM", "5 · Status cue", "6 · Buzzer", "7 · RF transmit", "8 · Host macro request"}
	for _, rule := range rules.Records {
		id := strconv.Itoa(int(rule.ID))
		event, action := fmt.Sprint(rule.EventKind), fmt.Sprint(rule.ActionKind)
		if rule.EventKind >= 1 && int(rule.EventKind) <= len(events) {
			event = events[rule.EventKind-1]
		}
		if rule.ActionKind >= 1 && int(rule.ActionKind) <= len(actions) {
			action = actions[rule.ActionKind-1]
		}
		page.Rows = append(page.Rows, []string{id, strconv.FormatBool(rule.Enabled()), event, fmt.Sprintf("%d / %d", rule.EventValue, rule.EventMask), action, fmt.Sprint(rule.ActionTarget), fmt.Sprintf("%d / %d", rule.Value, rule.Extra)})
		page.RowValues[id] = map[string]string{"id": id, "enabled": strconv.FormatBool(rule.Enabled()), "event": event, "event_value": fmt.Sprint(rule.EventValue), "event_mask": fmt.Sprint(rule.EventMask), "action": action, "target": fmt.Sprint(rule.ActionTarget), "value": fmt.Sprint(rule.Value), "extra": fmt.Sprint(rule.Extra)}
	}
	page.Fields = []desktop.Field{{Key: "revision", Value: nativeDigest(rules), Hidden: true}, {Key: "owner_generation", Value: fmt.Sprint(state.ConnectionGeneration), Hidden: true}, {Key: "id", Value: "255", Hidden: true}, nativeBool("enabled", "Rule enabled", false), choice("event", "Board event", events[0], events...), nativeNumber("event_value", "Event value", 0, 0, 255), nativeNumber("event_mask", "Event mask (0 = any)", 0, 0, 255), choice("action", "Action", actions[0], actions...), nativeNumber("target", "Action target", 0, 0, 255), nativeNumber("value", "Action value", 0, 0, 65535), nativeNumber("extra", "Action extra", 0, 0, 65535)}
	page.Actions = []desktop.PageAction{pageAction("new", "New rule", ready), {ID: "save", Label: "Save board rule", Enabled: ready, Confirm: true}, {ID: "remove", Label: "Remove selected", Enabled: ready, Confirm: true, SelectionRequired: true}}
	return page, nil
}

func (p nativePages) submitNativeBoardAutomations(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify board rules")
	}
	var state control.Snapshot
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, err
	}
	if action == "new" {
		page, err := p.loadNativeBoardAutomations(ctx, state)
		page.ClearSelection = true
		return page, err
	}
	if !state.Connected || state.Programming.Active || state.EmergencyStop.Active || fmt.Sprint(state.ConnectionGeneration) != v["owner_generation"] {
		return desktop.Page{}, errors.New("board owner changed or is not writable")
	}
	var current control.BoardAutomationSnapshot
	if err := p.call(ctx, "controller.board_automation.list", map[string]any{}, &current); err != nil {
		return desktop.Page{}, err
	}
	if nativeDigest(current) != v["revision"] {
		return desktop.Page{}, errors.New("Board rules changed; refresh before saving")
	}
	var result any
	switch action {
	case "remove":
		id, err := nativeFormInteger(v, "selected", 0, int(native.AutomationCapacity)-1)
		if err != nil {
			return desktop.Page{}, err
		}
		if err := p.call(ctx, "controller.board_automation.remove", map[string]int{"id": id}, &result); err != nil {
			return desktop.Page{}, err
		}
	case "save":
		var record native.AutomationRecord
		for _, item := range []struct {
			key    string
			target *byte
		}{{"id", &record.ID}, {"event_value", &record.EventValue}, {"event_mask", &record.EventMask}, {"target", &record.ActionTarget}} {
			n, err := nativeFormInteger(v, item.key, 0, 255)
			if err != nil {
				return desktop.Page{}, err
			}
			*item.target = byte(n)
		}
		for _, item := range []struct {
			key    string
			target *byte
		}{{"event", &record.EventKind}, {"action", &record.ActionKind}} {
			parts := strings.Fields(v[item.key])
			if len(parts) == 0 {
				return desktop.Page{}, errors.New("select event and action")
			}
			n, err := strconv.Atoi(parts[0])
			if err != nil || n < 1 || n > 8 {
				return desktop.Page{}, errors.New("invalid board event/action")
			}
			*item.target = byte(n)
		}
		for _, item := range []struct {
			key    string
			target *uint16
		}{{"value", &record.Value}, {"extra", &record.Extra}} {
			n, err := nativeFormInteger(v, item.key, 0, 65535)
			if err != nil {
				return desktop.Page{}, err
			}
			*item.target = uint16(n)
		}
		if v["enabled"] == "true" {
			record.Flags = native.AutomationEnabled
		}
		if err := native.ValidateAutomationRecord(record); err != nil {
			return desktop.Page{}, err
		}
		if err := p.call(ctx, "controller.board_automation.put", record, &result); err != nil {
			return desktop.Page{}, err
		}
	default:
		return desktop.Page{}, errors.New("unknown board automation action")
	}
	page, err := p.loadNativeBoardAutomations(ctx, state)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	page.Output = string(encoded)
	return page, nil
}

func (p nativePages) loadNativeRFBindings(ctx context.Context) (desktop.Page, error) {
	var catalog struct {
		Bindings []appconfig.Automation `json:"bindings"`
	}
	if !p.preview {
		if err := p.call(ctx, "controller.rf.catalog", map[string]bool{"read_board": false}, &catalog); err != nil {
			return desktop.Page{}, err
		}
	}
	page := desktop.Page{Title: "RF host bindings", Columns: []string{"Name", "Enabled", "Gesture", "Waveform"}, RowValues: map[string]map[string]string{}}
	for _, binding := range catalog.Bindings {
		encoded, _ := json.MarshalIndent(binding, "", "  ")
		wave := ""
		if binding.Match.RFCode != nil {
			wave = fmt.Sprintf("%d / %d / %d", *binding.Match.RFCode, binding.Match.RFBits, binding.Match.RFProtocol)
		}
		page.Rows = append(page.Rows, []string{binding.Name, strconv.FormatBool(binding.Enabled), binding.Match.Gesture, wave})
		page.RowValues[binding.Name] = map[string]string{"definition": string(encoded), "revision": nativeDigest(binding)}
	}
	page.Fields = []desktop.Field{{Key: "revision", Hidden: true}, {Key: "definition", Label: "Binding definition (JSON)", Multiline: true}}
	page.Actions = []desktop.PageAction{nativeSelectionAction("save", "Save binding", !p.preview), {ID: "remove", Label: "Remove binding", Enabled: !p.preview, SelectionRequired: true, Confirm: true}}
	return page, nil
}

func (p nativePages) submitNativeRFBindings(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot edit RF bindings")
	}
	page, err := p.loadNativeRFBindings(ctx)
	if err != nil {
		return page, err
	}
	current := page.RowValues[v["selected"]]
	if current == nil || current["revision"] != v["revision"] {
		return page, errors.New("RF binding changed; refresh before saving")
	}
	var result any
	switch action {
	case "save":
		var binding appconfig.Automation
		if err := json.Unmarshal([]byte(v["definition"]), &binding); err != nil {
			return page, err
		}
		err = p.call(ctx, "controller.rf.binding.put", map[string]any{"binding": binding, "previous_name": v["selected"]}, &result)
	case "remove":
		err = p.call(ctx, "controller.rf.binding.remove", map[string]string{"name": v["selected"]}, &result)
	default:
		err = errors.New("unknown binding operation")
	}
	if err != nil {
		return page, err
	}
	page, err = p.loadNativeRFBindings(ctx)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	return page, nil
}
